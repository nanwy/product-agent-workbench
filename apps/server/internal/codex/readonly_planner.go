package codex

import (
	"context"
	"errors"
	"fmt"
	"strings"
)

// ReadOnlyPlanner 复用 app-server 协议执行规划 Turn，但固定使用 read-only sandbox。
// 它不创建 Worktree，也不实现 TaskRun；规划阶段只能读取目标项目并返回结构化文本。
type ReadOnlyPlanner struct {
	config ExecutorConfig
}

func NewReadOnlyPlanner(config ExecutorConfig) (*ReadOnlyPlanner, error) {
	executor, err := NewExecutor(config)
	if err != nil {
		return nil, err
	}
	return &ReadOnlyPlanner{config: executor.config}, nil
}

// RunTextTurn 执行一个独立只读 Turn，返回 Agent 汇总文本。
func (p *ReadOnlyPlanner) RunTextTurn(
	ctx context.Context,
	workingDirectory string,
	prompt string,
) (text string, runErr error) {
	if ctx == nil || strings.TrimSpace(workingDirectory) == "" || strings.TrimSpace(prompt) == "" {
		return "", ErrInvalidTurnRequest
	}
	lifecycleContext, cancelLifecycle := context.WithCancel(context.WithoutCancel(ctx))
	observer := newTurnObserver(func(context.Context, string, []byte) error { return nil })
	process, err := LaunchAppServer(lifecycleContext, AppServerLaunchConfig{
		Executable:       p.config.Executable,
		PrefixArgs:       p.config.PrefixArgs,
		WorkingDirectory: workingDirectory,
		Environment:      p.config.Environment,
		NotificationSink: observer.Handle,
		RequestHandler:   rejectServerRequest,
		MaxMessageBytes:  p.config.MaxMessageBytes,
		StderrLimitBytes: p.config.StderrLimitBytes,
		ShutdownGrace:    p.config.ShutdownGrace,
	})
	if err != nil {
		cancelLifecycle()
		return "", err
	}
	defer func() {
		shutdownContext, cancel := context.WithTimeout(
			context.WithoutCancel(ctx),
			normalizedShutdownGrace(p.config.ShutdownGrace),
		)
		defer cancel()
		shutdownErr := process.Shutdown(shutdownContext)
		cancelLifecycle()
		if runErr == nil && shutdownErr != nil {
			runErr = fmt.Errorf("关闭规划 Codex app-server: %w", shutdownErr)
		}
	}()

	client, err := NewAppServerClient(process.Connection(), p.config.ClientInfo)
	if err != nil {
		return "", err
	}
	handshakeContext, cancelHandshake := context.WithTimeout(ctx, p.config.HandshakeTimeout)
	err = client.Initialize(handshakeContext)
	cancelHandshake()
	if err != nil {
		return "", err
	}
	request := TurnRequest{
		WorkingDirectory: workingDirectory,
		Prompt:           prompt,
		SandboxMode:      SandboxReadOnly,
	}
	threadContext, cancelThread := context.WithTimeout(ctx, p.config.ThreadTimeout)
	threadID, err := client.OpenThread(threadContext, request)
	cancelThread()
	if err != nil {
		return "", err
	}
	// HTTP 请求取消和进程级 TurnTimeout 共同控制规划时长；前者类似前端 AbortSignal，
	// 后者保证浏览器一直不关闭时也不会留下无限运行的规划进程。
	turnContext, cancelTurn := context.WithTimeout(ctx, p.config.ThreadTimeout)
	defer cancelTurn()
	turnID, err := client.StartTurn(turnContext, threadID, request)
	if err != nil {
		return "", err
	}
	observer.setTurnID(turnID)

	select {
	case completion := <-observer.Completion():
		if normalizeTurnStatus(completion.Status) != "completed" {
			return "", fmt.Errorf("%w: %s", ErrTurnFailed, completion.Message)
		}
		text := strings.TrimSpace(observer.Summary())
		if text == "" {
			return "", fmt.Errorf("%w: 规划 Turn 没有返回文本", ErrTurnFailed)
		}
		return text, nil
	case <-process.Connection().Done():
		connectionErr := process.Connection().Wait()
		if connectionErr == nil {
			connectionErr = errors.New("规划 Turn 完成前收到 EOF")
		}
		return "", fmt.Errorf("规划 Codex 协议连接提前关闭: %w", connectionErr)
	case <-process.Done():
		return "", fmt.Errorf("规划 Codex 进程提前退出")
	case <-turnContext.Done():
		interruptContext, cancel := context.WithTimeout(context.WithoutCancel(ctx), defaultInterruptTimeout)
		defer cancel()
		return "", errors.Join(turnContext.Err(), client.InterruptTurn(interruptContext, threadID, turnID))
	}
}

var _ interface {
	RunTextTurn(context.Context, string, string) (string, error)
} = (*ReadOnlyPlanner)(nil)
