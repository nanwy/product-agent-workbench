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

// TODO(学习者):先在测试夹具中实现 fake app-server，再定义 Client 的具体结构。
// 真实实现需要依次解决：进程启动、initialize request、Thread 创建/恢复、Turn 执行、
// notification 分发、中断和进程回收。不要一次把这些步骤写进一个大函数。
