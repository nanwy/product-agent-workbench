package verification

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/url"
	"os"
	"os/exec"
	"regexp"
	"sort"
	"strings"
	"sync"
	"time"
)

const maxCommandOutputBytes = 4 << 10

var secretPattern = regexp.MustCompile(
	`(?i)(token|password|secret|authorization)(\s*[:=]\s*)([^\s,;]+)|` +
		`Bearer\s+[A-Za-z0-9._~+/-]+=*`,
)

// Termination 表示子进程为什么结束。退出非零是“检查已运行且发现失败”；启动失败、超时
// 和取消则是“没有得到完整检查结果”，必须映射为 not-run，不能混为 failed。
type Termination string

const (
	TerminationExit        Termination = "exit"
	TerminationStartFailed Termination = "start-failed"
	TerminationTimeout     Termination = "timeout"
	TerminationCancelled   Termination = "cancelled"
)

// CommandResult 是命令 Adapter 返回的原始执行事实。
type CommandResult struct {
	ExitCode    int
	Output      string
	Termination Termination
	StartedAt   time.Time
	CompletedAt time.Time
}

// CommandExecutor 是 Service 消费的命令执行 seam。
type CommandExecutor interface {
	Execute(ctx context.Context, workspacePath string, command Command) CommandResult
}

// HostCommandExecutor 在受管候选 Worktree 中执行声明式命令。
type HostCommandExecutor struct{}

func NewHostCommandExecutor() *HostCommandExecutor { return &HostCommandExecutor{} }

// Execute 运行一条有截止时间、有输出上限的宿主命令。
//
// 前端类比：这像一个只接受固定 operation name 的 mutation adapter。Service 决定执行哪条
// 声明式检查；Adapter 只负责进程 I/O、超时与取消，不判断整份矩阵能否交付。
func (e *HostCommandExecutor) Execute(
	ctx context.Context,
	workspacePath string,
	command Command,
) CommandResult {
	startedAt := time.Now().UTC()
	result := CommandResult{
		ExitCode:    1,
		Termination: TerminationStartFailed,
		StartedAt:   startedAt,
		CompletedAt: startedAt,
	}
	if err := ctx.Err(); err != nil {
		result.ExitCode = 130
		result.Termination = TerminationCancelled
		result.Output = "验证已取消"
		return result
	}

	commandContext, cancel := context.WithTimeout(ctx, command.Timeout)
	defer cancel()
	process := exec.CommandContext(commandContext, command.Executable, command.Args...)
	process.Dir = workspacePath
	process.Env = validationEnvironment(command)
	process.Stdin = nil
	configureCommandTermination(process)

	output := newTailBuffer(maxCommandOutputBytes)
	process.Stdout = output
	process.Stderr = output
	err := process.Run()
	result.CompletedAt = time.Now().UTC()
	result.Output = redactSecrets(strings.TrimSpace(output.String()))

	switch {
	case ctx.Err() != nil:
		result.ExitCode = 130
		result.Termination = TerminationCancelled
		result.Output = joinOutput("验证已取消", result.Output)
	case errors.Is(commandContext.Err(), context.DeadlineExceeded):
		result.ExitCode = 124
		result.Termination = TerminationTimeout
		result.Output = joinOutput(fmt.Sprintf("验证超时（%s）", command.Timeout), result.Output)
	case err == nil:
		result.ExitCode = 0
		result.Termination = TerminationExit
	case process.ProcessState != nil:
		result.ExitCode = process.ProcessState.ExitCode()
		result.Termination = TerminationExit
	default:
		result.Termination = TerminationStartFailed
		result.Output = joinOutput(redactSecrets(err.Error()), result.Output)
	}
	return result
}

// tailBuffer 允许 stdout/stderr 并发写入，但只保留末尾证据，避免无限放大报告和数据库。
type tailBuffer struct {
	mu    sync.Mutex
	limit int
	data  []byte
}

func newTailBuffer(limit int) *tailBuffer {
	return &tailBuffer{limit: limit, data: []byte{}}
}

func (b *tailBuffer) Write(payload []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.data = append(b.data, payload...)
	if len(b.data) > b.limit {
		b.data = append([]byte{}, b.data[len(b.data)-b.limit:]...)
	}
	return len(payload), nil
}

func (b *tailBuffer) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return string(append([]byte{}, b.data...))
}

func validationEnvironment(command Command) []string {
	// 验证命令需要工具链路径和本地缓存目录，但不应默认继承 AWS、GitHub token、数据库
	// URL 等宿主秘密。允许列表相当于前端把 props 显式传给子组件，而不是展开整个 window。
	allowedKeys := []string{
		"CI", "COLORTERM", "COREPACK_HOME", "HOME", "LANG", "LC_ALL", "LOGNAME",
		"NPM_CONFIG_USERCONFIG", "PATH", "PNPM_HOME", "SHELL", "TEMP", "TERM", "TMP",
		"TMPDIR", "USER", "XDG_CACHE_HOME", "XDG_CONFIG_HOME", "XDG_DATA_HOME",
	}
	values := make(map[string]string, len(allowedKeys)+7)
	for _, key := range allowedKeys {
		if value, exists := os.LookupEnv(key); exists {
			values[key] = value
		}
	}
	values["NODE_ENV"] = string(command.Environment)

	proxyKeys := []string{"HTTP_PROXY", "HTTPS_PROXY", "ALL_PROXY", "http_proxy", "https_proxy", "all_proxy"}
	if command.ProxyPolicy != ProxyPolicyOmit {
		for _, key := range proxyKeys {
			value, exists := os.LookupEnv(key)
			if !exists {
				continue
			}
			if command.ProxyPolicy == ProxyPolicyAuto && !usableProxy(value) {
				continue
			}
			values[key] = value
		}
	}

	keys := make([]string, 0, len(values))
	for key := range values {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	environment := make([]string, 0, len(keys))
	for _, key := range keys {
		environment = append(environment, key+"="+values[key])
	}
	return environment
}

func usableProxy(value string) bool {
	parsed, err := url.Parse(value)
	if err != nil || parsed.Hostname() == "" {
		return false
	}
	host := strings.Trim(parsed.Hostname(), "[]")
	isLoopback := host == "localhost" || net.ParseIP(host) != nil && net.ParseIP(host).IsLoopback()
	if !isLoopback {
		return true
	}
	port := parsed.Port()
	if port == "" {
		port = "80"
		if parsed.Scheme == "https" {
			port = "443"
		}
	}
	connection, err := net.DialTimeout("tcp", net.JoinHostPort(host, port), 300*time.Millisecond)
	if err != nil {
		return false
	}
	_ = connection.Close()
	return true
}

func redactSecrets(value string) string {
	return secretPattern.ReplaceAllStringFunc(value, func(match string) string {
		if strings.HasPrefix(strings.ToLower(match), "bearer ") {
			return "Bearer [REDACTED]"
		}
		parts := secretPattern.FindStringSubmatch(match)
		if len(parts) >= 4 {
			return parts[1] + parts[2] + "[REDACTED]"
		}
		return "[REDACTED]"
	})
}

func joinOutput(prefix string, output string) string {
	if output == "" {
		return prefix
	}
	return prefix + "\n" + output
}
