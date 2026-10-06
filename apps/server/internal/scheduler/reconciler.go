package scheduler

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"product-agent-workbench/internal/domain"
	storecontract "product-agent-workbench/internal/store"
)

var ErrInvalidDependencies = errors.New("Scheduler 依赖无效")

// TaskStore 是 Scheduler 使用的最小持久化 seam。
//
// 它故意不暴露 claim、事件或 Requirement 更新；Scheduler 只读取一个 TicketVersion 的
// 执行历史，并创建首次 ready 的 queued TaskRun。
type TaskStore interface {
	ListTasksByTicketVersion(
		ctx context.Context,
		requirementID string,
		ticketVersionID string,
	) ([]domain.TaskRun, error)
	EnqueueTask(ctx context.Context, run domain.TaskRun) error
}

type IDGenerator func() string
type Clock func() time.Time

// Dependencies 是构造 Reconciler 所需的外部模块和变化值。
type Dependencies struct {
	Artifacts ArtifactReader
	Tasks     TaskStore
	NewID     IDGenerator
	Now       Clock
}

// Reconciler 把确认的 Ticket DAG 与已持久化执行历史收敛成 queued TaskRun。
//
// 同一个版本可以在确认后、某张票完成后以及进程重启后反复调用。它不会领取或执行任务，
// 也不会自动重试 failed/cancelled Ticket。
type Reconciler struct {
	artifacts ArtifactReader
	tasks     TaskStore
	newID     IDGenerator
	now       Clock
}

// ReconcileResult 只报告本次调用真正新建的 TaskRun。
//
// 已被另一个并发 Reconcile 创建的任务不重复返回；调用方如需完整队列，应从 Store 查询。
type ReconcileResult struct {
	TicketVersionID string
	Enqueued        []domain.TaskRun
}

func New(dependencies Dependencies) (*Reconciler, error) {
	if dependencies.Artifacts == nil || dependencies.Tasks == nil || dependencies.NewID == nil || dependencies.Now == nil {
		return nil, ErrInvalidDependencies
	}
	return &Reconciler{
		artifacts: dependencies.Artifacts,
		tasks:     dependencies.Tasks,
		newID:     dependencies.NewID,
		now:       dependencies.Now,
	}, nil
}

// Reconcile 读取服务端确认版本，计算 ready 集合，并幂等创建 queued TaskRun。
//
// 幂等策略：先按 RequirementID + TicketVersionID 读取历史；并发调用仍可能同时读到“尚未
// 入队”，因此 EnqueueTask 的数据库唯一约束是最终防线。收到 ErrAlreadyExists 后必须重新
// 读取并确认目标 Ticket 已存在，才能把它视为另一条 Reconcile 路径已经完成，而不是吞掉
// 随机 TaskRun ID 冲突或旧版本活动任务冲突。
func (r *Reconciler) Reconcile(
	ctx context.Context,
	requirementID string,
	ticketVersionID string,
) (ReconcileResult, error) {
	if err := ctx.Err(); err != nil {
		return ReconcileResult{}, err
	}
	requirementID = strings.TrimSpace(requirementID)
	ticketVersionID = strings.TrimSpace(ticketVersionID)
	if requirementID == "" || ticketVersionID == "" {
		return ReconcileResult{}, domain.ErrInvalidInput
	}

	version, err := r.artifacts.ReadConfirmedTicketVersion(ctx, requirementID, ticketVersionID)
	if err != nil {
		return ReconcileResult{}, fmt.Errorf("读取已确认 TicketVersion: %w", err)
	}
	if version.RequirementID != requirementID || version.ID != ticketVersionID {
		return ReconcileResult{}, fmt.Errorf("%w: artifact 身份与请求不一致", ErrInvalidTicketGraph)
	}

	history, err := r.tasks.ListTasksByTicketVersion(ctx, requirementID, ticketVersionID)
	if err != nil {
		return ReconcileResult{}, fmt.Errorf("读取 TicketVersion 执行历史: %w", err)
	}
	snapshot, err := executionSnapshot(version, history)
	if err != nil {
		return ReconcileResult{}, err
	}
	ready, err := ReadyTickets(version, snapshot)
	if err != nil {
		return ReconcileResult{}, err
	}

	result := ReconcileResult{
		TicketVersionID: ticketVersionID,
		Enqueued:        make([]domain.TaskRun, 0, len(ready)),
	}
	for _, ticket := range ready {
		if err := ctx.Err(); err != nil {
			return result, err
		}
		run, err := domain.NewTaskRun(domain.NewTaskRunInput{
			ID:              strings.TrimSpace(r.newID()),
			RequirementID:   requirementID,
			TicketVersionID: ticketVersionID,
			TicketID:        ticket.ID,
			Attempt:         1,
		}, r.now())
		if err != nil {
			return result, fmt.Errorf("创建 Ticket %q 的 TaskRun: %w", ticket.ID, err)
		}
		if err := r.tasks.EnqueueTask(ctx, run); err != nil {
			if !errors.Is(err, storecontract.ErrAlreadyExists) {
				return result, fmt.Errorf("Ticket %q 入队: %w", ticket.ID, err)
			}
			createdByConcurrentReconcile, verifyErr := r.ticketNowExists(
				ctx,
				requirementID,
				ticketVersionID,
				ticket.ID,
			)
			if verifyErr != nil {
				return result, verifyErr
			}
			if createdByConcurrentReconcile {
				continue
			}
			return result, fmt.Errorf("Ticket %q 入队: %w", ticket.ID, err)
		}
		result.Enqueued = append(result.Enqueued, run)
	}
	return result, nil
}

// executionSnapshot 选择每张 Ticket 最大 attempt 的状态，避免依赖查询返回顺序猜测最新记录。
func executionSnapshot(version TicketVersion, history []domain.TaskRun) (ExecutionSnapshot, error) {
	ticketIDs := make(map[string]struct{}, len(version.Tickets))
	for _, ticket := range version.Tickets {
		ticketIDs[ticket.ID] = struct{}{}
	}

	snapshot := make(ExecutionSnapshot, len(history))
	latestAttempt := make(map[string]int, len(history))
	for _, run := range history {
		if run.RequirementID != version.RequirementID || run.TicketVersionID != version.ID {
			return nil, fmt.Errorf("%w: TaskRun %q 归属错误", ErrInvalidTicketGraph, run.ID)
		}
		if _, exists := ticketIDs[run.TicketID]; !exists {
			return nil, fmt.Errorf("%w: TaskRun %q 引用了版本外 Ticket", ErrInvalidTicketGraph, run.ID)
		}
		if attempt, exists := latestAttempt[run.TicketID]; exists && attempt >= run.Attempt {
			continue
		}
		latestAttempt[run.TicketID] = run.Attempt
		snapshot[run.TicketID] = run.Status
	}
	return snapshot, nil
}

func (r *Reconciler) ticketNowExists(
	ctx context.Context,
	requirementID string,
	ticketVersionID string,
	ticketID string,
) (bool, error) {
	history, err := r.tasks.ListTasksByTicketVersion(ctx, requirementID, ticketVersionID)
	if err != nil {
		return false, fmt.Errorf("确认并发入队结果: %w", err)
	}
	for _, run := range history {
		if run.TicketID == ticketID {
			return true, nil
		}
	}
	return false, nil
}
