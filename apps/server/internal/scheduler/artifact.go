package scheduler

import "context"

// ArtifactReader 是 Scheduler 读取“已确认执行票版本”的 seam。
//
// 为什么不让 Reconcile 直接接收浏览器传来的 []Ticket：浏览器可以篡改 TicketID 或
// blockedBy，从而绕过确认过的依赖图。后续文件 Adapter 应使用 requirementID 和
// ticketVersionID 定位只读 artifact，再还原 TicketVersion。
//
// 测试会提供内存 Adapter，生产代码会提供文件 Adapter；Scheduler 只依赖这个小接口，
// 不需要知道 Markdown 文件如何组织。
type ArtifactReader interface {
	ReadConfirmedTicketVersion(
		ctx context.Context,
		requirementID string,
		ticketVersionID string,
	) (TicketVersion, error)
}
