package domain

import (
	"errors"
	"time"
)

// ErrNotImplemented 表示学习脚手架中的方法还没有由你实现。
//
// 完成某个方法后，应删除该方法返回此错误的占位代码，而不是在业务逻辑中判断这个错误。
var ErrNotImplemented = errors.New("学习任务尚未实现")

var (
	// ErrInvalidInput 表示调用方提供的数据缺少必需字段或不满足基本格式。
	ErrInvalidInput = errors.New("输入无效")
	// ErrInvalidTransition 表示当前状态不允许执行请求的迁移。
	ErrInvalidTransition = errors.New("状态迁移无效")
	// ErrInvalidTime 表示迁移时间早于已经发生的领域事实。
	ErrInvalidTime = errors.New("时间顺序无效")
)

// TaskStatus 表示一次执行尝试的生命周期状态。
//
// 允许的主路径是 queued → claimed → running → completed；failed 和 cancelled
// 是终态。具体的合法前态由 TaskRun 的方法负责保护。
type TaskStatus string

const (
	// TaskStatusQueued 表示任务已经持久化，但尚未被本地 Runner 领取。
	TaskStatusQueued TaskStatus = "queued"
	// TaskStatusClaimed 表示调度器已经领取任务，但 Runner 尚未确认开始执行。
	TaskStatusClaimed TaskStatus = "claimed"
	// TaskStatusRunning 表示 Runner 已经开始执行任务。
	TaskStatusRunning TaskStatus = "running"
	// TaskStatusCompleted 表示任务成功完成并保存了结果。
	TaskStatusCompleted TaskStatus = "completed"
	// TaskStatusFailed 表示任务执行失败，并保存了可解释的错误。
	TaskStatusFailed TaskStatus = "failed"
	// TaskStatusCancelled 表示任务在活动状态下被用户或应用取消。
	TaskStatusCancelled TaskStatus = "cancelled"
)

// NewTaskRunInput 是创建一次全新执行尝试所需的最小输入。
//
// TicketID 暂时使用 string；等 Ticket 领域模型稳定后再决定是否引入专用 ID 类型。
type NewTaskRunInput struct {
	ID            string
	RequirementID string
	TicketID      string
	Attempt       int
}

// TaskResult 保存完成一次执行后，后续验证与交付需要引用的事实。
type TaskResult struct {
	Summary       string
	CodexThreadID string
	WorktreePath  string
}

// TaskRun 表示一次执行尝试，而不是执行票本身。
//
// 同一张执行票失败后重试，应创建 Attempt 更大的新 TaskRun，不能覆盖旧记录。
// 时间字段使用指针表示“这个阶段尚未发生”，而不是用零值时间猜测状态。
type TaskRun struct {
	ID            string
	RequirementID string
	TicketID      string
	Attempt       int
	Status        TaskStatus
	CreatedAt     time.Time
	ClaimedAt     *time.Time
	StartedAt     *time.Time
	FinishedAt    *time.Time
	Result        *TaskResult
	FailureReason string
}

// NewTaskRun 校验创建输入，并返回 queued 状态的新执行尝试。
//
// TODO(学习者):
//   - 拒绝空 ID、空 RequirementID、空 TicketID；
//   - 拒绝小于 1 的 Attempt；
//   - 拒绝零值 now；
//   - 初始化 Status 和 CreatedAt，其余阶段字段保持未发生状态。
func NewTaskRun(input NewTaskRunInput, now time.Time) (TaskRun, error) {
	if input.ID == "" || input.RequirementID == "" || input.TicketID == "" {
		return TaskRun{}, ErrInvalidInput
	}
	if now.IsZero() {
		return TaskRun{}, ErrInvalidTime
	}
	if input.Attempt < 1 {
		return TaskRun{}, ErrInvalidInput
	}
	return TaskRun{
		ID:            input.ID,
		RequirementID: input.RequirementID,
		TicketID:      input.TicketID,
		Attempt:       input.Attempt,
		Status:        TaskStatusQueued,
		CreatedAt:     now,
	}, nil
}

// Claim 把 queued 任务迁移为 claimed，并记录领取时间。
//
// TODO(学习者):只允许 queued → claimed；失败时返回原 TaskRun，不能产生部分修改。
func (run TaskRun) Claim(now time.Time) (TaskRun, error) {
	if run.Status != TaskStatusQueued {
		return run, ErrInvalidTransition
	}
	if now.IsZero() || run.CreatedAt.IsZero() || now.Before(run.CreatedAt) {
		return run, ErrInvalidTime
	}
	run.Status = TaskStatusClaimed
	run.ClaimedAt = &now
	return run, nil
}

// Start 把 claimed 任务迁移为 running，并记录真正的执行开始时间。
//
// TODO(学习者):只允许 claimed → running，并检查 now 不早于 ClaimedAt。
func (run TaskRun) Start(now time.Time) (TaskRun, error) {
	if run.Status != TaskStatusClaimed {
		return run, ErrInvalidTransition
	}
	if run.ClaimedAt == nil || now.IsZero() || now.Before(*run.ClaimedAt) {
		return run, ErrInvalidTime
	}
	run.Status = TaskStatusRunning
	run.StartedAt = &now
	return run, nil
}

// Complete 把 running 任务迁移为 completed，并保存可审计结果。
//
// TODO(学习者):只允许 running → completed；拒绝空 Summary；清空失败原因。
func (run TaskRun) Complete(result TaskResult, now time.Time) (TaskRun, error) {
	if run.Status != TaskStatusRunning {
		return run, ErrInvalidTransition
	}
	if result.Summary == "" {
		return run, ErrInvalidInput
	}
	if run.StartedAt == nil || now.IsZero() || now.Before(*run.StartedAt) {
		return run, ErrInvalidTime
	}
	run.Status = TaskStatusCompleted
	run.FinishedAt = &now
	run.Result = &result
	run.FailureReason = ""
	return run, nil
}

// Fail 把 claimed 或 running 任务迁移为 failed，并保存失败原因。
//
// TODO(学习者):拒绝空白失败原因；终态不能再次失败或被覆盖。
func (run TaskRun) Fail(reason string, now time.Time) (TaskRun, error) {
	if run.Status != TaskStatusClaimed && run.Status != TaskStatusRunning {
		return run, ErrInvalidTransition
	}
	if reason == "" {
		return run, ErrInvalidInput
	}
	if run.StartedAt == nil || now.IsZero() || now.Before(*run.StartedAt) {
		return run, ErrInvalidTime
	}
	run.Status = TaskStatusFailed
	run.FinishedAt = &now
	run.FailureReason = reason
	return run, nil
}

// Cancel 把 queued、claimed 或 running 任务迁移为 cancelled。
//
// TODO(学习者):终态不能取消；取消不能伪造成功结果或失败原因。
func (run TaskRun) Cancel(now time.Time) (TaskRun, error) {
	if run.Status != TaskStatusQueued && run.Status != TaskStatusClaimed && run.Status != TaskStatusRunning {
		return run, ErrInvalidTransition
	}
	if run.StartedAt == nil || now.IsZero() || now.Before(*run.StartedAt) {
		return run, ErrInvalidTime
	}
	run.Status = TaskStatusCancelled
	run.FinishedAt = &now
	run.Result = nil
	run.FailureReason = ""
	return run, nil
}
