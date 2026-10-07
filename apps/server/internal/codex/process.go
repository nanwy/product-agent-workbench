package codex

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

const (
	defaultAppServerShutdownGrace = 2 * time.Second
	defaultAppServerStderrBytes   = 32 << 10
)

var ErrInvalidAppServerConfig = errors.New("Codex app-server 启动配置无效")

// AppServerLaunchConfig 描述启动一个原生 Codex app-server 进程所需的受信任配置。
//
// Executable 和 PrefixArgs 只能来自服务端部署配置：例如直接执行 codex，或者执行项目固定
// 版本的 codex.js。实现会固定追加 `app-server --listen stdio://`，不会经过 Shell，也不能把
// Requirement、Ticket 或浏览器输入拼进命令行。
type AppServerLaunchConfig struct {
	Executable       string
	PrefixArgs       []string
	WorkingDirectory string
	Environment      []string

	NotificationSink NotificationSink
	RequestHandler   RequestHandler
	MaxMessageBytes  int
	StderrLimitBytes int
	ShutdownGrace    time.Duration
}

// AppServerExit 是子进程退出后可以安全检查的稳定事实。
//
// StderrTail 只保存有上限的末尾诊断信息。它仍可能包含路径等敏感内容，写日志或持久化前
// 必须经过上层脱敏。
type AppServerExit struct {
	PID        int
	ExitCode   int
	StderrTail string
}

// AppServerProcessError 表示 app-server 没有正常退出。
// 诊断 stderr 放在 AppServerExit 中，不自动拼进 Error 文本，避免日志无意泄露敏感内容。
type AppServerProcessError struct {
	PID      int
	ExitCode int
	Cause    error
}

func (e *AppServerProcessError) Error() string {
	if e == nil {
		return ""
	}
	return fmt.Sprintf("Codex app-server 异常退出（pid=%d, exitCode=%d）：%v", e.PID, e.ExitCode, e.Cause)
}

func (e *AppServerProcessError) Unwrap() error {
	if e == nil {
		return nil
	}
	return e.Cause
}

// AppServerProcess 拥有一个 app-server 子进程及其 JSON-RPC 连接。
//
// 每个实例只能启动一次。cmd.Wait 始终由内部唯一 goroutine 调用，调用方通过 Wait 或
// Shutdown 等待结果，不能直接操作 exec.Cmd，避免重复 Wait 或遗留僵尸进程。
type AppServerProcess struct {
	command    *exec.Cmd
	connection *JSONRPCConnection
	stderr     *tailBuffer
	pid        int

	done chan struct{}

	waitMu  sync.Mutex
	waitErr error
	exit    AppServerExit

	shutdownOnce  sync.Once
	shutdownErrMu sync.Mutex
	shutdownErr   error
	killOnce      sync.Once
	killErr       error

	stopParentCancellation func() bool
}

// LaunchAppServer 启动子进程、连接 stdio，并立即启动 16A 的 JSON-RPC 读写循环。
func LaunchAppServer(ctx context.Context, config AppServerLaunchConfig) (*AppServerProcess, error) {
	if ctx == nil {
		return nil, fmt.Errorf("启动 Codex app-server: %w: context 不能为空", ErrInvalidAppServerConfig)
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}

	resolved, normalized, err := normalizeAppServerConfig(config)
	if err != nil {
		return nil, err
	}
	args := append([]string{}, normalized.PrefixArgs...)
	args = append(args, "app-server", "--listen", "stdio://")

	// exec.Command 直接接收已经分开的 argv，不调用 sh -c。这样即使路径中有空格，也只会
	// 被当作一个参数，产品内容不可能借此插入额外命令。
	command := exec.Command(resolved, args...)
	command.Dir = normalized.WorkingDirectory
	command.Env = append([]string(nil), normalized.Environment...)
	prepareProcessGroup(command)

	stderr := newTailBuffer(normalized.StderrLimitBytes)
	command.Stderr = stderr
	stdout, err := command.StdoutPipe()
	if err != nil {
		return nil, fmt.Errorf("创建 Codex app-server stdout 管道: %w", err)
	}
	stdin, err := command.StdinPipe()
	if err != nil {
		_ = stdout.Close()
		return nil, fmt.Errorf("创建 Codex app-server stdin 管道: %w", err)
	}

	if err := command.Start(); err != nil {
		_ = stdin.Close()
		_ = stdout.Close()
		return nil, fmt.Errorf("启动 Codex app-server 可执行文件 %q: %w", resolved, err)
	}

	connection, err := NewJSONRPCConnection(JSONRPCDependencies{
		Input:            stdout,
		Output:           stdin,
		NotificationSink: normalized.NotificationSink,
		RequestHandler:   normalized.RequestHandler,
		MaxMessageBytes:  normalized.MaxMessageBytes,
	})
	if err != nil {
		cleanupStartedProcess(command, stdin, stdout)
		return nil, fmt.Errorf("创建 Codex app-server 协议连接: %w", err)
	}
	if err := connection.Start(ctx); err != nil {
		cleanupStartedProcess(command, stdin, stdout)
		return nil, fmt.Errorf("启动 Codex app-server 协议连接: %w", err)
	}

	process := &AppServerProcess{
		command:    command,
		connection: connection,
		stderr:     stderr,
		pid:        command.Process.Pid,
		done:       make(chan struct{}),
		exit: AppServerExit{
			PID:      command.Process.Pid,
			ExitCode: -1,
		},
	}

	// 父 context 取消后仍给进程一小段独立清理时间：先关闭 stdin 让 app-server 自行退出，
	// 超时后再强制杀死整个进程组。WithoutCancel 只用于这段资源回收，不能继续业务工作。
	cleanupBase := context.WithoutCancel(ctx)
	process.stopParentCancellation = context.AfterFunc(ctx, func() {
		shutdownContext, cancel := context.WithTimeout(cleanupBase, normalized.ShutdownGrace)
		defer cancel()
		_ = process.Shutdown(shutdownContext)
	})

	go process.wait()
	return process, nil
}

// PID 返回已启动 app-server 的操作系统进程 ID。
func (p *AppServerProcess) PID() int {
	if p == nil {
		return 0
	}
	return p.pid
}

// Connection 返回已经启动的 JSON-RPC 连接。
// 16C 会通过它发送 initialize、initialized 和 Thread 请求。
func (p *AppServerProcess) Connection() *JSONRPCConnection {
	if p == nil {
		return nil
	}
	return p.connection
}

// Done 在操作系统子进程退出后关闭。它只用于 select，不携带退出结果；结果通过 Wait 获取。
func (p *AppServerProcess) Done() <-chan struct{} {
	if p == nil {
		closed := make(chan struct{})
		close(closed)
		return closed
	}
	return p.done
}

// Wait 等待子进程退出；传入的 ctx 只控制“等待多久”，不拥有也不取消子进程。
func (p *AppServerProcess) Wait(ctx context.Context) (AppServerExit, error) {
	if p == nil {
		return AppServerExit{}, fmt.Errorf("等待 Codex app-server: 进程不能为空")
	}
	if ctx == nil {
		return AppServerExit{}, fmt.Errorf("等待 Codex app-server: context 不能为空")
	}

	select {
	case <-p.done:
		return p.result()
	case <-ctx.Done():
		return AppServerExit{PID: p.pid, ExitCode: -1, StderrTail: p.stderr.String()}, ctx.Err()
	}
}

// Shutdown 先关闭 JSON-RPC 管道，让 app-server 从 stdin EOF 正常退出。
// 如果 ctx 先结束，则强制终止受管进程（Unix 下包含整个进程组）；多次或并发调用是安全的。
func (p *AppServerProcess) Shutdown(ctx context.Context) error {
	if p == nil {
		return nil
	}
	if ctx == nil {
		return fmt.Errorf("关闭 Codex app-server: context 不能为空")
	}
	p.shutdownOnce.Do(func() {
		err := p.connection.Close()
		p.shutdownErrMu.Lock()
		p.shutdownErr = err
		p.shutdownErrMu.Unlock()
	})

	select {
	case <-p.done:
		_, waitErr := p.result()
		return errors.Join(p.connectionShutdownError(), waitErr)
	case <-ctx.Done():
		killErr := p.forceKill()
		return errors.Join(ctx.Err(), killErr)
	}
}

func (p *AppServerProcess) wait() {
	waitErr := p.command.Wait()
	exitCode := -1
	if p.command.ProcessState != nil {
		exitCode = p.command.ProcessState.ExitCode()
	}

	p.waitMu.Lock()
	p.waitErr = waitErr
	p.exit = AppServerExit{
		PID:        p.pid,
		ExitCode:   exitCode,
		StderrTail: p.stderr.String(),
	}
	p.waitMu.Unlock()
	close(p.done)

	// 进程已经退出，不再需要父 context 的回调持有本对象。
	if p.stopParentCancellation != nil {
		p.stopParentCancellation()
	}
}

func (p *AppServerProcess) result() (AppServerExit, error) {
	p.waitMu.Lock()
	exit := p.exit
	waitErr := p.waitErr
	p.waitMu.Unlock()

	connectionErr := p.connection.Wait()
	if waitErr == nil && exit.ExitCode == 0 {
		return exit, connectionErr
	}
	if waitErr == nil {
		waitErr = fmt.Errorf("进程退出码为 %d", exit.ExitCode)
	}
	processErr := &AppServerProcessError{
		PID:      exit.PID,
		ExitCode: exit.ExitCode,
		Cause:    waitErr,
	}
	return exit, errors.Join(processErr, connectionErr)
}

func (p *AppServerProcess) connectionShutdownError() error {
	p.shutdownErrMu.Lock()
	defer p.shutdownErrMu.Unlock()
	return p.shutdownErr
}

func (p *AppServerProcess) forceKill() error {
	p.killOnce.Do(func() {
		p.killErr = killProcessGroup(p.command.Process)
	})
	return p.killErr
}

func normalizeAppServerConfig(config AppServerLaunchConfig) (string, AppServerLaunchConfig, error) {
	config.Executable = strings.TrimSpace(config.Executable)
	if config.Executable == "" {
		return "", config, fmt.Errorf("启动 Codex app-server: %w: Executable 不能为空", ErrInvalidAppServerConfig)
	}
	resolved, err := exec.LookPath(config.Executable)
	if err != nil {
		return "", config, fmt.Errorf("查找 Codex app-server 可执行文件 %q: %w", config.Executable, err)
	}

	config.WorkingDirectory = filepath.Clean(strings.TrimSpace(config.WorkingDirectory))
	if config.WorkingDirectory == "." || !filepath.IsAbs(config.WorkingDirectory) {
		return "", config, fmt.Errorf("启动 Codex app-server: %w: WorkingDirectory 必须是绝对路径", ErrInvalidAppServerConfig)
	}
	info, err := os.Stat(config.WorkingDirectory)
	if err != nil {
		return "", config, fmt.Errorf("检查 Codex app-server 工作目录: %w", err)
	}
	if !info.IsDir() {
		return "", config, fmt.Errorf("启动 Codex app-server: %w: WorkingDirectory 不是目录", ErrInvalidAppServerConfig)
	}

	if config.Environment == nil {
		config.Environment = os.Environ()
	} else {
		config.Environment = append([]string(nil), config.Environment...)
	}
	config.PrefixArgs = append([]string(nil), config.PrefixArgs...)

	if config.StderrLimitBytes == 0 {
		config.StderrLimitBytes = defaultAppServerStderrBytes
	}
	if config.StderrLimitBytes < 1 {
		return "", config, fmt.Errorf("启动 Codex app-server: %w: StderrLimitBytes 必须大于 0", ErrInvalidAppServerConfig)
	}
	if config.ShutdownGrace == 0 {
		config.ShutdownGrace = defaultAppServerShutdownGrace
	}
	if config.ShutdownGrace < 0 {
		return "", config, fmt.Errorf("启动 Codex app-server: %w: ShutdownGrace 不能小于 0", ErrInvalidAppServerConfig)
	}
	return resolved, config, nil
}

func cleanupStartedProcess(
	command *exec.Cmd,
	stdin interface{ Close() error },
	stdout interface{ Close() error },
) {
	_ = stdin.Close()
	_ = stdout.Close()
	_ = killProcessGroup(command.Process)
	_ = command.Wait()
}
