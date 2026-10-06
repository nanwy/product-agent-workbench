package scheduler

import (
	"errors"
	"fmt"
	"strings"

	"product-agent-workbench/internal/domain"
)

var (
	// ErrInvalidTicketGraph 表示已确认 artifact 的身份或依赖图不合法。
	ErrInvalidTicketGraph = errors.New("执行票依赖图无效")
)

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
	if err := validateTicketVersion(version); err != nil {
		return nil, err
	}

	ready := make([]Ticket, 0)
	for _, ticket := range version.Tickets {
		// 任何历史状态都意味着这张票已经被调度过。失败/取消后的重试必须由显式用例创建，
		// 不能由一次普通 Reconcile 偷偷触发。
		if _, exists := snapshot[ticket.ID]; exists {
			continue
		}

		dependenciesCompleted := true
		for _, blockerID := range ticket.BlockedBy {
			if snapshot[blockerID] != domain.TaskStatusCompleted {
				dependenciesCompleted = false
				break
			}
		}
		if !dependenciesCompleted {
			continue
		}

		copyTicket := ticket
		copyTicket.BlockedBy = append([]string(nil), ticket.BlockedBy...)
		ready = append(ready, copyTicket)
	}
	return ready, nil
}

// validateTicketVersion 在产生任何入队结果前验证完整依赖图。
//
// 使用 Kahn 拓扑排序检测环：每移除一个入度为零的节点，就降低它所有后继节点的入度；
// 最终没能移除全部节点，说明剩余节点互相依赖成环。
func validateTicketVersion(version TicketVersion) error {
	if strings.TrimSpace(version.ID) == "" || strings.TrimSpace(version.RequirementID) == "" || len(version.Tickets) == 0 {
		return ErrInvalidTicketGraph
	}

	byID := make(map[string]Ticket, len(version.Tickets))
	for _, ticket := range version.Tickets {
		id := strings.TrimSpace(ticket.ID)
		if id == "" || id != ticket.ID {
			return ErrInvalidTicketGraph
		}
		if _, exists := byID[id]; exists {
			return fmt.Errorf("%w: 重复 Ticket %q", ErrInvalidTicketGraph, id)
		}
		byID[id] = ticket
	}

	indegree := make(map[string]int, len(version.Tickets))
	dependents := make(map[string][]string, len(version.Tickets))
	for _, ticket := range version.Tickets {
		seenBlockers := make(map[string]struct{}, len(ticket.BlockedBy))
		for _, rawBlockerID := range ticket.BlockedBy {
			blockerID := strings.TrimSpace(rawBlockerID)
			if blockerID == "" || blockerID != rawBlockerID || blockerID == ticket.ID {
				return fmt.Errorf("%w: Ticket %q 的依赖无效", ErrInvalidTicketGraph, ticket.ID)
			}
			if _, exists := byID[blockerID]; !exists {
				return fmt.Errorf("%w: Ticket %q 依赖不存在的 %q", ErrInvalidTicketGraph, ticket.ID, blockerID)
			}
			if _, duplicate := seenBlockers[blockerID]; duplicate {
				return fmt.Errorf("%w: Ticket %q 重复依赖 %q", ErrInvalidTicketGraph, ticket.ID, blockerID)
			}
			seenBlockers[blockerID] = struct{}{}
			indegree[ticket.ID]++
			dependents[blockerID] = append(dependents[blockerID], ticket.ID)
		}
	}

	queue := make([]string, 0, len(version.Tickets))
	for _, ticket := range version.Tickets {
		if indegree[ticket.ID] == 0 {
			queue = append(queue, ticket.ID)
		}
	}
	visited := 0
	for len(queue) > 0 {
		id := queue[0]
		queue = queue[1:]
		visited++
		for _, dependentID := range dependents[id] {
			indegree[dependentID]--
			if indegree[dependentID] == 0 {
				queue = append(queue, dependentID)
			}
		}
	}
	if visited != len(version.Tickets) {
		return fmt.Errorf("%w: blockedBy 存在循环依赖", ErrInvalidTicketGraph)
	}
	return nil
}
