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
) (string, error) {
	// 结构化 Spec/Tickets 生成仍使用原来的单次文本接口；内部复用新的可续接运行时。
	result, err := p.RunConversationTurn(ctx, ReadOnlyConversationRequest{
		WorkingDirectory: workingDirectory,
		Prompt:           prompt,
	}, ReadOnlyConversationCallbacks{})
	if err != nil {
		return "", err
	}
	text := strings.TrimSpace(result.Summary)
	if text == "" {
		return "", fmt.Errorf("%w: 规划 Turn 没有返回文本", ErrTurnFailed)
	}
	return text, nil
}

// RunConversationTurn 执行原 MVP 的可续接只读 Codex 对话。
//
// 它只负责 app-server 生命周期和事件转发，不解释 Grilling 门禁，也不修改 Conversation 表；
// 这些业务动作由 app 层的 23C/23D 用例负责。
func (p *ReadOnlyPlanner) RunConversationTurn(
	ctx context.Context,
	request ReadOnlyConversationRequest,
	callbacks ReadOnlyConversationCallbacks,
) (result ReadOnlyConversationResult, runErr error) {
	// 第 1 步：校验固定执行边界和用户 prompt。ThreadID 可以为空，表示首轮创建 Thread。
	if ctx == nil || strings.TrimSpace(request.WorkingDirectory) == "" ||
		strings.TrimSpace(request.Prompt) == "" {
		return ReadOnlyConversationResult{}, ErrInvalidTurnRequest
	}
	request.WorkingDirectory = strings.TrimSpace(request.WorkingDirectory)
	request.Prompt = strings.TrimSpace(request.Prompt)
	request.ThreadID = strings.TrimSpace(request.ThreadID)
	// 第 2 步：创建唯一 observer。JSON-RPC readLoop 会串行调用它，因此应用层不会并发折叠消息。
	lifecycleContext, cancelLifecycle := context.WithCancel(context.WithoutCancel(ctx))
	observer := newTurnObserver(func(eventCtx context.Context, eventType string, payload []byte) error {
		if callbacks.Event == nil {
			return nil
		}
		return callbacks.Event(eventCtx, eventType, payload)
	})

	// 第 3 步：启动一次性 app-server。sandbox 固定为 read-only，不接受调用方覆盖。
	process, err := LaunchAppServer(lifecycleContext, AppServerLaunchConfig{
		Executable:       p.config.Executable,
		PrefixArgs:       p.config.PrefixArgs,
		WorkingDirectory: request.WorkingDirectory,
		Environment:      p.config.Environment,
		NotificationSink: observer.Handle,
		RequestHandler:   rejectServerRequest,
		MaxMessageBytes:  p.config.MaxMessageBytes,
		StderrLimitBytes: p.config.StderrLimitBytes,
		ShutdownGrace:    p.config.ShutdownGrace,
	})
	if err != nil {
		cancelLifecycle()
		return ReadOnlyConversationResult{}, err
	}

	// 第 4 步：无论握手、事件回调还是 Turn 在哪里失败，都回收子进程和 JSONL 连接。
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

	// 第 5 步：完成 initialize/initialized 握手。
	client, err := NewAppServerClient(process.Connection(), p.config.ClientInfo)
	if err != nil {
		return ReadOnlyConversationResult{}, err
	}
	handshakeContext, cancelHandshake := context.WithTimeout(ctx, p.config.HandshakeTimeout)
	err = client.Initialize(handshakeContext)
	cancelHandshake()
	if err != nil {
		return ReadOnlyConversationResult{}, err
	}

	// 第 6 步：首轮创建 Thread，后续轮次恢复同一个 Thread；权限始终保持只读。
	turnRequest := TurnRequest{
		WorkingDirectory: request.WorkingDirectory,
		Prompt:           request.Prompt,
		ThreadID:         request.ThreadID,
		SandboxMode:      SandboxReadOnly,
	}
	threadContext, cancelThread := context.WithTimeout(ctx, p.config.ThreadTimeout)
	threadID, err := client.OpenThread(threadContext, turnRequest)
	cancelThread()
	if err != nil {
		return ReadOnlyConversationResult{}, err
	}
	result.ThreadID = threadID

	// 第 7 步：先持久化 Thread ID，再启动 Turn。
	// 如果 Session callback 失败，不能制造一个数据库不知道、下一轮也无法续接的 Codex Thread。
	if callbacks.Session != nil {
		if err := callbacks.Session(ctx, threadID); err != nil {
			return result, fmt.Errorf("保存只读 Codex Thread %q: %w", threadID, err)
		}
	}

	// HTTP 请求取消和进程级 TurnTimeout 共同控制规划时长；前者类似前端 AbortSignal，
	// 后者保证浏览器一直不关闭时也不会留下无限运行的规划进程。
	// 第 8 步：提交 prompt，并记录 app-server Turn ID 供取消路径使用。
	turnContext, cancelTurn := context.WithTimeout(ctx, p.config.ThreadTimeout)
	defer cancelTurn()
	turnID, err := client.StartTurn(turnContext, threadID, turnRequest)
	if err != nil {
		return result, err
	}
	observer.setTurnID(turnID)
	result.TurnID = turnID

	// 第 9 步：等待唯一终态。readLoop 在完成前已经串行转发所有 text/activity 事件。
	select {
	case completion := <-observer.Completion():
		result.Status = normalizeTurnStatus(completion.Status)
		result.Message = completion.Message
		result.Summary = strings.TrimSpace(observer.Summary())
		switch result.Status {
		case "completed":
			if result.Summary == "" {
				result.Status = "failed"
				return result, fmt.Errorf("%w: 规划 Turn 没有返回文本", ErrTurnFailed)
			}
			return result, nil
		case "interrupted":
			return result, fmt.Errorf("%w: %s", ErrTurnInterrupted, completion.Message)
		default:
			return result, fmt.Errorf("%w: %s", ErrTurnFailed, completion.Message)
		}
	case <-process.Connection().Done():
		connectionErr := process.Connection().Wait()
		if connectionErr == nil {
			connectionErr = errors.New("规划 Turn 完成前收到 EOF")
		}
		return result, fmt.Errorf("规划 Codex 协议连接提前关闭: %w", connectionErr)
	case <-process.Done():
		return result, fmt.Errorf("规划 Codex 进程提前退出")
	case <-turnContext.Done():
		// 第 10 步：请求取消或超时时，先 interrupt 远端 Turn，再由 defer 回收进程。
		interruptContext, cancel := context.WithTimeout(context.WithoutCancel(ctx), defaultInterruptTimeout)
		defer cancel()
		return result, errors.Join(turnContext.Err(), client.InterruptTurn(interruptContext, threadID, turnID))
	}
}

var _ interface {
	RunTextTurn(context.Context, string, string) (string, error)
} = (*ReadOnlyPlanner)(nil)
