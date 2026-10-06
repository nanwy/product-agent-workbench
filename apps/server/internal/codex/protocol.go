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

// 练习 16A 已完成底层 JSON-RPC 连接；16B 已完成 app-server 进程启动、stderr 尾部、
// 优雅关闭与超时强杀。后续在这两个 seam 之上依次增加 initialize、Thread 创建/恢复、
// Turn 执行和中断，避免把协议、进程与业务状态混进一个大函数。
