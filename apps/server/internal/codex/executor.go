package codex

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"strings"
	"time"

	"product-agent-workbench/internal/domain"
	"product-agent-workbench/internal/runner"
)

const (
	defaultHandshakeTimeout = 30 * time.Second
	defaultThreadTimeout    = 60 * time.Second
	defaultInterruptTimeout = 2 * time.Second
)

var (
	// ErrInvalidExecutorConfig 表示进程命令、超时或 ClientInfo 配置无效。
	ErrInvalidExecutorConfig = errors.New("Codex Executor 配置无效")
	// ErrTurnFailed 表示 app-server 明确返回 failed 终态。
	ErrTurnFailed = errors.New("Codex Turn 执行失败")
	// ErrTurnInterrupted 表示 Turn 被取消或中断，没有产生成功结果。
	ErrTurnInterrupted = errors.New("Codex Turn 已中断")
)

// ExecutorConfig 是真实 Codex Runner 的进程级配置。
// Executable、PrefixArgs 和 Environment 必须来自服务端配置，不能来自 RunRequest。
type ExecutorConfig struct {
	Executable  string
	PrefixArgs  []string
	Environment []string
	ClientInfo  ClientInfo

	HandshakeTimeout time.Duration
	ThreadTimeout    time.Duration
	InterruptTimeout time.Duration
	ShutdownGrace    time.Duration
	MaxMessageBytes  int
	StderrLimitBytes int
	Clock            func() time.Time
}

// Executor 为每个 TaskRun 启动一个一次性 app-server，并实现 runner.Runner。
// Thread ID 由 TaskResult 持久化；下次 attempt 可以通过 TaskRun 中的可信配置决定是否续接。
type Executor struct {
	config ExecutorConfig
}

// NewExecutor 校验并复制进程配置，不启动 app-server。
func NewExecutor(config ExecutorConfig) (*Executor, error) {
	config.Executable = strings.TrimSpace(config.Executable)
	if config.Executable == "" {
		return nil, ErrInvalidExecutorConfig
	}
	if config.ClientInfo == (ClientInfo{}) {
		config.ClientInfo = ClientInfo{
			Name:    "product-agent-workbench",
			Title:   "Product Agent Workbench",
			Version: "0.1.0",
		}
	} else {
		config.ClientInfo.Name = strings.TrimSpace(config.ClientInfo.Name)
		config.ClientInfo.Title = strings.TrimSpace(config.ClientInfo.Title)
		config.ClientInfo.Version = strings.TrimSpace(config.ClientInfo.Version)
		if config.ClientInfo.Name == "" || config.ClientInfo.Title == "" || config.ClientInfo.Version == "" {
			return nil, ErrInvalidExecutorConfig
		}
	}
	if config.Clock == nil {
		config.Clock = time.Now
	}
	if config.HandshakeTimeout == 0 {
		config.HandshakeTimeout = defaultHandshakeTimeout
	}
	if config.ThreadTimeout == 0 {
		config.ThreadTimeout = defaultThreadTimeout
	}
	if config.InterruptTimeout == 0 {
		config.InterruptTimeout = defaultInterruptTimeout
	}
	if config.HandshakeTimeout < 0 || config.ThreadTimeout < 0 || config.InterruptTimeout < 0 ||
		config.ShutdownGrace < 0 || config.MaxMessageBytes < 0 || config.StderrLimitBytes < 0 {
		return nil, ErrInvalidExecutorConfig
	}
	config.PrefixArgs = append([]string(nil), config.PrefixArgs...)
	config.Environment = codexProcessEnvironment(config.Environment)
	return &Executor{config: config}, nil
}

// Run 执行一次完整的 initialize → Thread → Turn 生命周期。
func (e *Executor) Run(
	ctx context.Context,
	request runner.RunRequest,
	sink runner.EventSink,
) (runResult runner.RunResult, runErr error) {
	if ctx == nil || sink == nil || strings.TrimSpace(request.TaskRun.ID) == "" ||
		strings.TrimSpace(request.RepositoryRoot) == "" || strings.TrimSpace(request.Prompt) == "" {
		return runner.RunResult{}, ErrInvalidTurnRequest
	}
	if err := ctx.Err(); err != nil {
		return runner.RunResult{}, err
	}

	emitter := newEventEmitter(request.TaskRun.ID, sink, e.config.Clock)
	// app-server 必须比业务 ctx 多活到 interrupt 和进程回收结束，因此生命周期 context
	// 只继承值，不继承取消信号；业务取消仍由下面的 select 显式处理。
	lifecycleContext, cancelLifecycle := context.WithCancel(context.WithoutCancel(ctx))

	observer := newTurnObserver(func(_ context.Context, eventType string, payload []byte) error {
		// 调用方取消后不再写新的业务事件，但 observer 仍解析完成通知，供中断流程收尾。
		if err := ctx.Err(); err != nil {
			return nil
		}
		return emitter.Emit(ctx, eventType, payload)
	})
	process, err := LaunchAppServer(lifecycleContext, AppServerLaunchConfig{
		Executable:       e.config.Executable,
		PrefixArgs:       e.config.PrefixArgs,
		WorkingDirectory: request.RepositoryRoot,
		Environment:      e.config.Environment,
		NotificationSink: observer.Handle,
		RequestHandler:   rejectServerRequest,
		MaxMessageBytes:  e.config.MaxMessageBytes,
		StderrLimitBytes: e.config.StderrLimitBytes,
		ShutdownGrace:    e.config.ShutdownGrace,
	})
	if err != nil {
		cancelLifecycle()
		emitter.Close()
		return runner.RunResult{}, err
	}

	// 无论握手、Turn 还是事件保存在哪一步失败，都必须先关闭进程，再停止事件单写者。
	defer func() {
		shutdownContext, cancelShutdown := context.WithTimeout(
			context.WithoutCancel(ctx),
			normalizedShutdownGrace(e.config.ShutdownGrace),
		)
		defer cancelShutdown()
		shutdownErr := process.Shutdown(shutdownContext)
		cancelLifecycle()
		emitter.Close()
		if runErr == nil && shutdownErr != nil {
			runErr = fmt.Errorf("关闭 Codex app-server: %w", shutdownErr)
		}
	}()

	client, err := NewAppServerClient(process.Connection(), e.config.ClientInfo)
	if err != nil {
		return runner.RunResult{}, err
	}
	handshakeContext, cancelHandshake := context.WithTimeout(ctx, e.config.HandshakeTimeout)
	err = client.Initialize(handshakeContext)
	cancelHandshake()
	if err != nil {
		return runner.RunResult{}, err
	}

	turnRequest := TurnRequest{
		WorkingDirectory: request.RepositoryRoot,
		Prompt:           request.Prompt,
		ThreadID:         request.CodexThreadID,
		SandboxMode:      SandboxWorkspaceWrite,
	}
	threadContext, cancelThread := context.WithTimeout(ctx, e.config.ThreadTimeout)
	threadID, err := client.OpenThread(threadContext, turnRequest)
	cancelThread()
	if err != nil {
		return runner.RunResult{}, err
	}
	if err := emitSession(ctx, emitter, threadID); err != nil {
		return runner.RunResult{}, err
	}

	turnID, err := client.StartTurn(ctx, threadID, turnRequest)
	if err != nil {
		if ctx.Err() != nil {
			return runner.RunResult{}, errors.Join(ctx.Err(), e.interrupt(ctx, client, threadID, observer.TurnID()))
		}
		return runner.RunResult{}, err
	}
	observer.setTurnID(turnID)

	select {
	case completion := <-observer.Completion():
		return e.finishTurn(request, threadID, completion, observer.Summary())
	case <-process.Connection().Done():
		connectionErr := process.Connection().Wait()
		if connectionErr == nil {
			connectionErr = fmt.Errorf("Codex app-server 协议连接在 Turn 完成前关闭")
		}
		return runner.RunResult{}, connectionErr
	case <-process.Done():
		exit, processErr := process.Wait(context.WithoutCancel(ctx))
		if processErr == nil {
			processErr = fmt.Errorf("Codex app-server 在 Turn 完成前退出（exitCode=%d）", exit.ExitCode)
		}
		return runner.RunResult{}, processErr
	case <-ctx.Done():
		interruptErr := e.interrupt(ctx, client, threadID, firstNonBlank(observer.TurnID(), turnID))
		return runner.RunResult{}, errors.Join(ctx.Err(), interruptErr)
	}
}

func (e *Executor) finishTurn(
	request runner.RunRequest,
	threadID string,
	completion turnCompletion,
	summary string,
) (runner.RunResult, error) {
	switch normalizeTurnStatus(completion.Status) {
	case "completed":
		if strings.TrimSpace(summary) == "" {
			summary = fmt.Sprintf("Codex 已完成 Ticket %s", request.TaskRun.TicketID)
		}
		return runner.RunResult{Result: domain.TaskResult{
			Summary:       summary,
			CodexThreadID: threadID,
		}}, nil
	case "interrupted":
		return runner.RunResult{}, fmt.Errorf("%w: %s", ErrTurnInterrupted, completion.Message)
	default:
		message := strings.TrimSpace(completion.Message)
		if message == "" {
			message = "app-server 返回 failed 状态"
		}
		return runner.RunResult{}, fmt.Errorf("%w: %s", ErrTurnFailed, message)
	}
}

func (e *Executor) interrupt(
	ctx context.Context,
	client *AppServerClient,
	threadID string,
	turnID string,
) error {
	if strings.TrimSpace(threadID) == "" || strings.TrimSpace(turnID) == "" {
		return nil
	}
	interruptContext, cancelInterrupt := context.WithTimeout(
		context.WithoutCancel(ctx),
		e.config.InterruptTimeout,
	)
	defer cancelInterrupt()
	return client.InterruptTurn(interruptContext, threadID, turnID)
}

func emitSession(ctx context.Context, emitter *eventEmitter, threadID string) error {
	payload, err := json.Marshal(map[string]string{"threadId": threadID})
	if err != nil {
		return err
	}
	return emitter.Emit(ctx, "codex.session", payload)
}

func rejectServerRequest(_ context.Context, request IncomingRequest) RequestResponse {
	if strings.Contains(strings.ToLower(request.Method), "approval") {
		return RequestResponse{Result: map[string]string{"decision": "decline"}}
	}
	return RequestResponse{Error: &RPCError{Code: -32601, Message: "method not found"}}
}

func normalizedShutdownGrace(value time.Duration) time.Duration {
	if value <= 0 {
		return defaultAppServerShutdownGrace
	}
	return value
}

// codexProcessEnvironment 清除宿主开发模式，避免 Codex 在目标 Worktree 中执行前端命令时
// 继承 NODE_ENV=development，并把开发构建误当成生产验证结果。
func codexProcessEnvironment(source []string) []string {
	if source == nil {
		source = os.Environ()
	}
	environment := make([]string, 0, len(source))
	for _, entry := range source {
		key, _, ok := strings.Cut(entry, "=")
		if ok && strings.EqualFold(key, "NODE_ENV") {
			continue
		}
		environment = append(environment, entry)
	}
	return environment
}

var _ runner.Runner = (*Executor)(nil)
