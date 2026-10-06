package store

import (
	"context"
	"time"

	"product-agent-workbench/internal/domain"
)

// Store 描述工作流真正需要的持久化能力。
//
// 先在 internal/store/memory 中实现它并通过契约测试，再创建 SQLite Adapter。不要为了
// “以后可能替换数据库”继续拆出 RequirementRepository、TaskRepository 等浅接口。
type Store interface {
	// CreateRequirement 首次保存 Requirement。
	//
	// requirement.ID 已存在时返回 ErrAlreadyExists，不能覆盖旧值。
	CreateRequirement(ctx context.Context, requirement domain.Requirement) error

	// GetRequirement 按 ID 返回 Requirement 的值副本。
	GetRequirement(ctx context.Context, id string) (domain.Requirement, error)

	// SaveRequirement 使用乐观锁更新 Requirement。
	//
	// 只有已保存版本等于 expectedRevision 时才能更新；成功后 Store 把 Revision
	// 设置为 expectedRevision+1，并返回这个新版本。冲突时返回 ErrRevisionConflict，
	// 且不能修改已经保存的值。
	SaveRequirement(
		ctx context.Context,
		requirement domain.Requirement,
		expectedRevision int64,
	) (int64, error)

	// EnqueueTask 首次保存 queued TaskRun。
	//
	// 同一 Requirement 的不同 Ticket 可以同时活动；同一 Ticket 已有 queued、claimed 或
	// running attempt 时返回 ErrAlreadyExists，避免 Scheduler 重复入队。
	EnqueueTask(ctx context.Context, run domain.TaskRun) error

	// GetTask 按 ID 返回 TaskRun 的深拷贝。
	//
	// TaskRun 含有时间和结果指针，只复制结构体仍会泄漏 Store 内部可变数据。
	GetTask(ctx context.Context, id string) (domain.TaskRun, error)

	// ListTasksByTicketVersion 返回一个 Requirement 在指定 TicketVersion 下的全部执行历史。
	//
	// 结果按稳定入队顺序排列并使用深拷贝；Requirement 不存在时返回 ErrNotFound。
	// 空 TicketVersionID 只用于迁移期间读取旧的手动入队记录。
	ListTasksByTicketVersion(
		ctx context.Context,
		requirementID string,
		ticketVersionID string,
	) ([]domain.TaskRun, error)

	// ClaimNextTask 原子领取最早入队的 queued TaskRun。
	//
	// “查找候选、调用 TaskRun.Claim、写回结果”必须处于同一个写锁临界区。
	// now 由调用方传入，使行为可测试，也避免 Adapter 偷偷依赖系统时钟。
	// 没有可领取任务时返回 ErrNotFound。
	ClaimNextTask(ctx context.Context, now time.Time) (domain.TaskRun, error)

	// SaveTask 在当前状态仍等于 expectedStatus 时更新 TaskRun。
	//
	// “比较当前状态、写入新值”必须是一个原子操作。资源不存在返回 ErrNotFound；状态已经
	// 被其它请求改变返回 ErrTaskConflict，且不能覆盖较新的终态。它不允许借此创建新任务。
	SaveTask(ctx context.Context, run domain.TaskRun, expectedStatus domain.TaskStatus) error

	// AppendTaskEvents 原子追加同一 TaskRun 的一批连续事件。
	//
	// 第一条事件的 Sequence 必须是 1，后续必须严格递增；批次中任意事件无效时，
	// 整批都不能写入。返回值是本批最后一条事件的全局 ID。
	AppendTaskEvents(ctx context.Context, events []domain.TaskEvent) (int64, error)

	// ListTaskEvents 返回 sequence > afterSequence 的事件深拷贝，并按 Sequence 升序排列。
	ListTaskEvents(
		ctx context.Context,
		taskRunID string,
		afterSequence int64,
	) ([]domain.TaskEvent, error)
}
