package codex

import "context"

// TurnRequest 描述启动或续接一次 Codex Turn 所需的受控输入。
//
// WorkingDirectory 必须来自 Workspace Adapter，不能来自产品输入。SandboxMode 只能从
// 工作台允许列表中选择，不能把任意命令或权限字符串透传给子进程。
type TurnRequest struct {
	WorkingDirectory string
	Prompt           string
	ThreadID         string
	SandboxMode      string
}

// Notification 是从 app-server JSON-RPC 通知归一化后的内部事件。
type Notification struct {
	Method string
	Body   []byte
}

// TurnResult 保存一次 Turn 的稳定结束事实。
type TurnResult struct {
	ThreadID string
	ExitCode int
}

// NotificationSink 接收已经解析并脱敏的 app-server 通知。
type NotificationSink func(ctx context.Context, notification Notification) error

// ReadOnlyConversationRequest 描述一次可续接的只读产品对话 Turn。
// ThreadID 为空时创建新 Thread，非空时恢复原 Thread。
type ReadOnlyConversationRequest struct {
	WorkingDirectory string
	Prompt           string
	ThreadID         string
}

// ReadOnlyConversationResult 是 app-server Turn 的稳定结束事实。
type ReadOnlyConversationResult struct {
	ThreadID string
	TurnID   string
	Status   string
	Message  string
	Summary  string
}

// ReadOnlyConversationCallbacks 是运行时向应用层提交副作用的两个出口。
// Event 必须串行调用；Session 必须在 turn/start 之前成功。
type ReadOnlyConversationCallbacks struct {
	Event   func(ctx context.Context, eventType string, payload []byte) error
	Session func(ctx context.Context, threadID string) error
}

// 练习 16A～16D 已完成 JSONL 传输、进程生命周期、initialize、Thread 创建/恢复、
// Turn 执行、通知归一化和中断。Executor 把这些内部步骤压缩成 runner.Runner 的单一接口。
