package targetruntime

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"syscall"
	"time"
)

const (
	maxHealthBodyBytes = 16_000
	maxOutputLines     = 30
	stopGracePeriod    = 3 * time.Second
)

type Status string

const (
	StatusStopped  Status = "stopped"
	StatusStarting Status = "starting"
	StatusReady    Status = "ready"
	StatusFailed   Status = "failed"
)

type Ownership string

const (
	OwnershipNone      Ownership = "none"
	OwnershipWorkbench Ownership = "workbench"
	OwnershipExternal  Ownership = "external"
)

// Target 是可以安全返回浏览器的配置视图；仓库绝对路径与复用标记不会泄漏到页面。
type Target struct {
	Name              string `json:"name"`
	ModificationScope string `json:"modificationScope"`
}

// Snapshot 是当前易失运行态。它像前端的 server state：页面只能读取，真正的 *os.Process
// 句柄仍封装在 Manager 内，刷新浏览器不会改变进程所有权。
type Snapshot struct {
	Status       Status     `json:"status"`
	Ownership    Ownership  `json:"ownership"`
	PID          *int       `json:"pid"`
	Command      string     `json:"command"`
	HealthURL    string     `json:"healthUrl"`
	PreviewURL   string     `json:"previewUrl"`
	GatewayURL   string     `json:"gatewayUrl"`
	StartedAt    *time.Time `json:"startedAt"`
	Error        string     `json:"error,omitempty"`
	RecentOutput []string   `json:"recentOutput"`
}

type childProcess struct {
	command *exec.Cmd
	done    chan struct{}
}

// Manager 串行化 start/stop 动作，并用 mu 保护可被 HTTP GET 与 wait goroutine 并发读取的状态。
// operation 对应前端 mutation 的互斥锁；mu 对应 store 中极短的同步状态更新。
type Manager struct {
	config    Config
	client    *http.Client
	gateway   previewGateway
	operation sync.Mutex

	mu          sync.RWMutex
	child       *childProcess
	activeRoot  string
	startCancel context.CancelFunc
	snapshot    Snapshot
}

func NewManager(config Config) (*Manager, error) {
	if err := validateConfig(config); err != nil {
		return nil, err
	}
	manager := &Manager{
		config: config,
		client: &http.Client{
			Timeout: 1500 * time.Millisecond,
			CheckRedirect: func(_ *http.Request, _ []*http.Request) error {
				return http.ErrUseLastResponse
			},
		},
	}
	manager.snapshot = manager.baseSnapshot()
	return manager, nil
}

func (manager *Manager) Target() Target {
	return Target{Name: manager.config.Name, ModificationScope: manager.config.ModificationScope}
}

func (manager *Manager) Snapshot() Snapshot {
	manager.mu.RLock()
	defer manager.mu.RUnlock()
	return cloneSnapshot(manager.snapshot)
}

// Start 只执行配置中的固定命令。启动成功后子进程独立于这次 HTTP request 存活，之后由
// Manager.Close 回收；request context 只负责中断“等待健康”的当前动作。
func (manager *Manager) Start(ctx context.Context, workspacePath string) (Snapshot, error) {
	if ctx == nil {
		return manager.Snapshot(), fmt.Errorf("启动目标项目: context 不能为空")
	}
	workingDirectory, err := manager.resolveWorkingDirectory(workspacePath)
	if err != nil {
		return manager.Snapshot(), err
	}
	manager.operation.Lock()
	defer manager.operation.Unlock()

	current := manager.Snapshot()
	manager.mu.RLock()
	activeRoot := manager.activeRoot
	existingChild := manager.child
	manager.mu.RUnlock()
	if current.Status == StatusReady && activeRoot == workingDirectory {
		return current, nil
	}
	if current.Status == StatusReady && current.Ownership == OwnershipExternal {
		return manager.failStart(ctx, fmt.Errorf("外部复用服务不是当前候选 Worktree，工作台不会终止或覆盖它")), nil
	}
	if existingChild != nil {
		if err := manager.stopOwnedProcess(ctx, existingChild); err != nil {
			return manager.Snapshot(), err
		}
	}
	if err := manager.gateway.Stop(ctx); err != nil {
		return manager.Snapshot(), err
	}
	startupContext, cancel := context.WithCancel(ctx)
	manager.mu.Lock()
	manager.startCancel = cancel
	now := time.Now().UTC()
	manager.snapshot = manager.baseSnapshot()
	manager.snapshot.Status = StatusStarting
	manager.snapshot.StartedAt = &now
	manager.mu.Unlock()
	defer func() {
		cancel()
		manager.mu.Lock()
		manager.startCancel = nil
		manager.mu.Unlock()
	}()

	existing, reason := manager.inspectExistingRuntime(startupContext)
	switch existing {
	case OwnershipExternal:
		if workingDirectory != manager.config.RepositoryRoot {
			return manager.failStart(startupContext, fmt.Errorf("候选 Worktree 不能复用身份只精确到项目的外部服务")), nil
		}
		if err := manager.gateway.Start(manager.config.PreviewURL, manager.config.GatewayPort); err != nil {
			return manager.failStart(startupContext, err), nil
		}
		manager.mu.Lock()
		manager.snapshot.Status = StatusReady
		manager.snapshot.Ownership = OwnershipExternal
		manager.activeRoot = workingDirectory
		manager.mu.Unlock()
		return manager.Snapshot(), nil
	case OwnershipNone:
		if reason != "" {
			return manager.failStart(startupContext, fmt.Errorf("%s", reason)), nil
		}
	}

	// 不使用 exec.CommandContext：HTTP 请求结束不应该杀死已经 ready 的开发服务。
	command := exec.Command(manager.config.StartCommand.Executable, manager.config.StartCommand.Args...)
	command.Dir = workingDirectory
	command.Env = os.Environ()
	command.Stdin = nil
	configureProcessGroup(command)
	output := &boundedOutput{append: manager.appendOutput}
	command.Stdout = output
	command.Stderr = output
	if err := command.Start(); err != nil {
		return manager.failStart(startupContext, fmt.Errorf("启动目标项目进程: %w", err)), nil
	}
	child := &childProcess{command: command, done: make(chan struct{})}
	manager.mu.Lock()
	manager.child = child
	manager.activeRoot = workingDirectory
	manager.snapshot.Ownership = OwnershipWorkbench
	pid := command.Process.Pid
	manager.snapshot.PID = &pid
	manager.mu.Unlock()
	go manager.waitChild(child)

	if err := manager.waitUntilHealthy(startupContext, child); err != nil {
		cleanupContext, cleanupCancel := context.WithTimeout(context.WithoutCancel(startupContext), 5*time.Second)
		_ = manager.stopOwnedProcess(cleanupContext, child)
		cleanupCancel()
		return manager.failStart(startupContext, err), nil
	}
	if err := manager.gateway.Start(manager.config.PreviewURL, manager.config.GatewayPort); err != nil {
		cleanupContext, cleanupCancel := context.WithTimeout(context.WithoutCancel(startupContext), 5*time.Second)
		_ = manager.stopOwnedProcess(cleanupContext, child)
		cleanupCancel()
		return manager.failStart(startupContext, err), nil
	}
	manager.mu.Lock()
	if manager.child == child {
		manager.snapshot.Status = StatusReady
		manager.snapshot.Error = ""
	}
	manager.mu.Unlock()
	return manager.Snapshot(), nil
}

// Stop 可以在 Start 等待健康期间先发出取消信号，再等待同一个串行动作锁。
func (manager *Manager) Stop(ctx context.Context) (Snapshot, error) {
	if ctx == nil {
		return manager.Snapshot(), fmt.Errorf("停止目标项目: context 不能为空")
	}
	manager.mu.RLock()
	cancel := manager.startCancel
	manager.mu.RUnlock()
	if cancel != nil {
		cancel()
	}
	manager.operation.Lock()
	defer manager.operation.Unlock()

	manager.mu.RLock()
	child := manager.child
	manager.mu.RUnlock()
	processErr := manager.stopOwnedProcess(ctx, child)
	gatewayErr := manager.gateway.Stop(ctx)
	manager.mu.Lock()
	manager.snapshot = manager.baseSnapshot()
	manager.activeRoot = ""
	manager.mu.Unlock()
	return manager.Snapshot(), errors.Join(processErr, gatewayErr)
}

// Close 是进程级资源清理入口。谁 NewManager，谁在退出路径调用 Close。
func (manager *Manager) Close(ctx context.Context) error {
	_, err := manager.Stop(ctx)
	return err
}

func (manager *Manager) inspectExistingRuntime(ctx context.Context) (Ownership, string) {
	responded, healthy, body, status := manager.probeHealth(ctx)
	if responded {
		marker := manager.config.ReuseHealthBodyMarker
		if healthy && marker != "" && strings.Contains(body, marker) {
			return OwnershipExternal, ""
		}
		if healthy && marker == "" {
			return OwnershipNone, "健康地址已有进程响应，但配置未允许复用；没有启动或终止该外部进程"
		}
		if marker != "" {
			return OwnershipNone, fmt.Sprintf("健康地址已有响应，但未匹配目标标记：%s", marker)
		}
		return OwnershipNone, fmt.Sprintf("健康地址已有进程响应（HTTP %d），不能安全复用", status)
	}
	address := manager.config.PreviewURL.Host
	connection, err := net.DialTimeout("tcp", address, 300*time.Millisecond)
	if err == nil {
		_ = connection.Close()
		return OwnershipNone, "目标端口已被其他进程占用，但健康检查未通过；没有启动或终止该外部进程"
	}
	return OwnershipNone, ""
}

func (manager *Manager) probeHealth(ctx context.Context) (bool, bool, string, int) {
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, manager.config.HealthURL.String(), nil)
	if err != nil {
		return false, false, "", 0
	}
	response, err := manager.client.Do(request)
	if err != nil {
		return false, false, "", 0
	}
	defer response.Body.Close()
	body, _ := io.ReadAll(io.LimitReader(response.Body, maxHealthBodyBytes))
	return true, response.StatusCode >= 200 && response.StatusCode < 300, string(body), response.StatusCode
}

func (manager *Manager) waitUntilHealthy(ctx context.Context, child *childProcess) error {
	timer := time.NewTimer(manager.config.StartupTimeout)
	defer timer.Stop()
	ticker := time.NewTicker(250 * time.Millisecond)
	defer ticker.Stop()
	lastError := "尚未收到健康响应"

	for {
		select {
		case <-ctx.Done():
			return fmt.Errorf("等待目标项目健康: %w", ctx.Err())
		case <-child.done:
			return fmt.Errorf("目标项目在就绪前退出：%s", lastError)
		case <-timer.C:
			return fmt.Errorf("目标项目启动超时：%s", lastError)
		case <-ticker.C:
			responded, healthy, _, status := manager.probeHealth(ctx)
			if healthy {
				// 健康端点返回后再次确认刚启动的 child 没有因端口冲突退出。
				select {
				case <-child.done:
					lastError = "健康地址由其他进程占用，工作台启动的进程已经退出"
					continue
				case <-time.After(100 * time.Millisecond):
					return nil
				case <-ctx.Done():
					return fmt.Errorf("等待目标项目健康: %w", ctx.Err())
				}
			}
			if responded {
				lastError = fmt.Sprintf("健康检查返回 %d", status)
			}
		}
	}
}

func (manager *Manager) waitChild(child *childProcess) {
	err := child.command.Wait()
	close(child.done)
	manager.mu.Lock()
	if manager.child != child {
		manager.mu.Unlock()
		return
	}
	manager.child = nil
	manager.activeRoot = ""
	manager.snapshot.Status = StatusFailed
	manager.snapshot.Ownership = OwnershipNone
	manager.snapshot.PID = nil
	manager.snapshot.Error = processExitMessage(child.command, err)
	manager.mu.Unlock()
	shutdownContext, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	_ = manager.gateway.Stop(shutdownContext)
}

func (manager *Manager) stopOwnedProcess(ctx context.Context, child *childProcess) error {
	if child == nil {
		return nil
	}
	manager.mu.Lock()
	if manager.child == child {
		manager.child = nil
	}
	manager.mu.Unlock()
	if err := signalProcessGroup(child.command.Process, syscall.SIGTERM); err != nil {
		return fmt.Errorf("停止目标项目进程: %w", err)
	}
	graceTimer := time.NewTimer(stopGracePeriod)
	defer graceTimer.Stop()
	select {
	case <-child.done:
		return nil
	case <-ctx.Done():
		return fmt.Errorf("等待目标项目退出: %w", ctx.Err())
	case <-graceTimer.C:
		if err := signalProcessGroup(child.command.Process, syscall.SIGKILL); err != nil {
			return fmt.Errorf("强制停止目标项目进程: %w", err)
		}
		select {
		case <-child.done:
			return nil
		case <-ctx.Done():
			return fmt.Errorf("等待目标项目强制退出: %w", ctx.Err())
		}
	}
}

func (manager *Manager) failStart(ctx context.Context, err error) Snapshot {
	shutdownContext, cancel := context.WithTimeout(context.WithoutCancel(ctx), 2*time.Second)
	_ = manager.gateway.Stop(shutdownContext)
	cancel()
	manager.mu.Lock()
	manager.snapshot.Status = StatusFailed
	manager.snapshot.Ownership = OwnershipNone
	manager.snapshot.PID = nil
	manager.snapshot.Error = redact(err.Error())
	manager.activeRoot = ""
	manager.mu.Unlock()
	return manager.Snapshot()
}

// Check 让 Verification 在候选 Worktree 中确保运行时就绪。
// 已经运行同一目录时是幂等读取；目录不同则只重启工作台自己拥有的服务。
func (manager *Manager) Check(ctx context.Context, workspacePath string) error {
	snapshot, err := manager.Start(ctx, workspacePath)
	if err != nil {
		return err
	}
	if snapshot.Status != StatusReady {
		return fmt.Errorf("目标运行时未就绪：%s", snapshot.Error)
	}
	return nil
}

func (manager *Manager) resolveWorkingDirectory(workspacePath string) (string, error) {
	workspacePath = strings.TrimSpace(workspacePath)
	if workspacePath == "" {
		return manager.config.RepositoryRoot, nil
	}
	resolved, err := filepath.EvalSymlinks(filepath.Clean(workspacePath))
	if err != nil {
		return "", fmt.Errorf("解析候选 Worktree: %w", err)
	}
	info, err := os.Stat(resolved)
	if err != nil || !info.IsDir() {
		return "", fmt.Errorf("候选 Worktree 不是目录")
	}
	return resolved, nil
}

func (manager *Manager) appendOutput(value string) {
	lines := strings.FieldsFunc(value, func(character rune) bool { return character == '\n' || character == '\r' })
	if len(lines) == 0 {
		return
	}
	manager.mu.Lock()
	for _, line := range lines {
		if trimmed := strings.TrimSpace(line); trimmed != "" {
			manager.snapshot.RecentOutput = append(manager.snapshot.RecentOutput, redact(trimmed))
		}
	}
	if overflow := len(manager.snapshot.RecentOutput) - maxOutputLines; overflow > 0 {
		manager.snapshot.RecentOutput = append([]string{}, manager.snapshot.RecentOutput[overflow:]...)
	}
	manager.mu.Unlock()
}

func (manager *Manager) baseSnapshot() Snapshot {
	return Snapshot{
		Status:       StatusStopped,
		Ownership:    OwnershipNone,
		Command:      strings.Join(append([]string{manager.config.StartCommand.Executable}, manager.config.StartCommand.Args...), " "),
		HealthURL:    manager.config.HealthURL.String(),
		PreviewURL:   manager.config.PreviewURL.String(),
		GatewayURL:   fmt.Sprintf("http://127.0.0.1:%d", manager.config.GatewayPort),
		RecentOutput: []string{},
	}
}

func cloneSnapshot(snapshot Snapshot) Snapshot {
	result := snapshot
	result.RecentOutput = append([]string{}, snapshot.RecentOutput...)
	if snapshot.PID != nil {
		pid := *snapshot.PID
		result.PID = &pid
	}
	if snapshot.StartedAt != nil {
		startedAt := *snapshot.StartedAt
		result.StartedAt = &startedAt
	}
	return result
}

type boundedOutput struct {
	append func(string)
}

func (output *boundedOutput) Write(data []byte) (int, error) {
	output.append(string(data))
	return len(data), nil
}

func processExitMessage(command *exec.Cmd, err error) string {
	exitCode := -1
	if command.ProcessState != nil {
		exitCode = command.ProcessState.ExitCode()
	}
	return redact(fmt.Sprintf("目标项目进程退出（exitCode=%d）：%v", exitCode, err))
}

func redact(value string) string {
	for _, key := range []string{"token", "password", "secret", "authorization"} {
		lower := strings.ToLower(value)
		if index := strings.Index(lower, key+"="); index >= 0 {
			end := strings.IndexAny(value[index:], " ,;\n\r\t")
			if end < 0 {
				end = len(value) - index
			}
			value = value[:index+len(key)+1] + "[REDACTED]" + value[index+end:]
		}
	}
	return value
}
