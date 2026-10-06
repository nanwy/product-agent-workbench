package scheduler

import (
	"errors"

	"product-agent-workbench/internal/domain"
)

var errReadyTicketsNotImplemented = errors.New("ReadyTickets 尚未实现")

// Ticket 是 Scheduler 真正需要的最小执行票信息。
//
// 标题、Markdown 路径和验收条件属于 artifact 展示/执行输入，不参与依赖就绪计算，
// 因此不放进这个纯计算模型。
type Ticket struct {
	ID        string
	BlockedBy []string
}

// TicketVersion 表示已经确认且不可变的一版执行票依赖图。
//
// 后续 ArtifactReader 必须根据 ID 读取服务端确认过的版本；浏览器不能直接提交一组
// Ticket 来绕过确认门禁。
type TicketVersion struct {
	ID            string
	RequirementID string
	Tickets       []Ticket
}

// ExecutionSnapshot 保存每张执行票最后一次 TaskRun 的状态。
//
// map 中没有 TicketID 表示这张票从未入队。存在任何状态都表示 Scheduler 不能偷偷创建
// 第二次执行：queued/claimed/running 正在活动，completed 已完成，failed/cancelled 需要
// 显式重试策略，而不是自动重试。
type ExecutionSnapshot map[string]domain.TaskStatus

// ReadyTickets 按 artifact 中的稳定顺序返回当前可以首次入队的执行票。
//
// 这是练习 13 的第一个纯函数 seam，不访问数据库、文件、时钟或 ID 生成器。
// 请按下面顺序实现：
//  1. 校验 TicketVersion ID、RequirementID、Ticket ID 均非空；
//  2. 建立本版本 Ticket ID 集合，拒绝重复 ID、不存在的依赖和自依赖；
//  3. 依次遍历 Tickets：snapshot 已有该 Ticket 时跳过，Scheduler 不负责自动重试；
//  4. 没有前置依赖的票立即 ready；有依赖时，只有每个 blocker 都是 completed 才 ready；
//  5. 返回新的切片，保持 artifact 顺序，不修改 version、BlockedBy 或 snapshot。
//
// 非 completed 状态都不能解锁后继票。特别是 failed/cancelled 表示执行已经终止，
// 但不代表依赖产物已经成功产生。
func ReadyTickets(version TicketVersion, snapshot ExecutionSnapshot) ([]Ticket, error) {
	return nil, errReadyTicketsNotImplemented
}
