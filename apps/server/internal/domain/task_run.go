package domain

import (
	"errors"
	"strings"
	"time"
)

var (
	// ErrInvalidInput 表示调用方提供的数据缺少必需字段或不满足基本格式。
	ErrInvalidInput = errors.New("输入无效")
	// ErrInvalidTransition 表示当前状态不允许执行请求的迁移。
	ErrInvalidTransition = errors.New("状态迁移无效")
	// ErrInvalidTime 表示必需的阶段时间缺失、为零值或发生倒流。
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
// TicketVersionID 把执行事实固定到一份不可变依赖图；不能只保存 TicketID，因为不同版本
// 会重复使用 TICKET-01 这类局部编号。ID 暂时使用 string，模型稳定后再决定是否引入专用类型。
type NewTaskRunInput struct {
	ID              string
	RequirementID   string
	TicketVersionID string
	TicketID        string
	Attempt         int
}

// TaskResult 保存完成一次执行后，后续验证与交付需要引用的事实。
type TaskResult struct {
	Summary       string
	CodexThreadID string
	WorktreePath  string
	Revision      string
}

// TaskRun 表示一次执行尝试，而不是执行票本身。
//
// 同一张执行票失败后重试，应创建 Attempt 更大的新 TaskRun，不能覆盖旧记录。
// 时间字段使用指针表示“这个阶段尚未发生”，而不是用零值时间猜测状态。
type TaskRun struct {
	ID              string
	RequirementID   string
	TicketVersionID string
	TicketID        string
	Attempt         int
	Status          TaskStatus
	CreatedAt       time.Time
	ClaimedAt       *time.Time
	StartedAt       *time.Time
	FinishedAt      *time.Time
	// CodexThreadID 在 Thread 打开后立即保存，即使本次执行随后失败或进程重启也保留。
	// 前端类比：它像一次长会话的 sessionId，不是只有“请求成功”时才存在的响应字段。
	CodexThreadID string
	Result        *TaskResult
	FailureReason string
}

// NewTaskRun 校验创建输入，并返回 queued 状态的新执行尝试。
func NewTaskRun(input NewTaskRunInput, now time.Time) (TaskRun, error) {
	if isBlank(input.ID) || isBlank(input.RequirementID) || isBlank(input.TicketVersionID) || isBlank(input.TicketID) {
		return TaskRun{}, ErrInvalidInput
	}
	if now.IsZero() {
		return TaskRun{}, ErrInvalidTime
	}
	if input.Attempt < 1 {
		return TaskRun{}, ErrInvalidInput
	}
	return TaskRun{
		ID:              input.ID,
		RequirementID:   input.RequirementID,
		TicketVersionID: input.TicketVersionID,
		TicketID:        input.TicketID,
		Attempt:         input.Attempt,
		Status:          TaskStatusQueued,
		CreatedAt:       now,
	}, nil
}

// Claim 把 queued 任务迁移为 claimed，并记录领取时间。
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
func (run TaskRun) Start(now time.Time) (TaskRun, error) {
	if run.Status != TaskStatusClaimed {
		return run, ErrInvalidTransition
	}
	if run.ClaimedAt == nil || run.ClaimedAt.IsZero() || now.IsZero() || now.Before(*run.ClaimedAt) {
		return run, ErrInvalidTime
	}
	run.Status = TaskStatusRunning
	run.StartedAt = &now
	return run, nil
}

// AttachCodexThread 把外部执行会话绑定到 running TaskRun。
//
// 相同 ID 可以重复写入，便于重试持久化；一条 TaskRun 一旦绑定 Thread，就不能悄悄换成
// 另一个 Thread，否则事件流和最终结果会指向不同会话。
func (run TaskRun) AttachCodexThread(threadID string) (TaskRun, error) {
	threadID = strings.TrimSpace(threadID)
	if run.Status != TaskStatusRunning {
		return run, ErrInvalidTransition
	}
	if threadID == "" {
		return run, ErrInvalidInput
	}
	if run.CodexThreadID != "" && run.CodexThreadID != threadID {
		return run, ErrInvalidTransition
	}
	run.CodexThreadID = threadID
	return run, nil
}

// Complete 把 running 任务迁移为 completed，并保存可审计结果。
func (run TaskRun) Complete(result TaskResult, now time.Time) (TaskRun, error) {
	if run.Status != TaskStatusRunning {
		return run, ErrInvalidTransition
	}
	if isBlank(result.Summary) {
		return run, ErrInvalidInput
	}
	if run.CodexThreadID != "" {
		if result.CodexThreadID == "" {
			result.CodexThreadID = run.CodexThreadID
		} else if result.CodexThreadID != run.CodexThreadID {
			return run, ErrInvalidInput
		}
	} else if !isBlank(result.CodexThreadID) {
		// 兼容不支持实时 SessionSink 的简单 Runner；真实 Codex Executor 会更早持久化。
		run.CodexThreadID = strings.TrimSpace(result.CodexThreadID)
	}
	if run.StartedAt == nil || run.StartedAt.IsZero() || now.IsZero() || now.Before(*run.StartedAt) {
		return run, ErrInvalidTime
	}
	run.Status = TaskStatusCompleted
	run.FinishedAt = &now
	run.Result = &result
	run.FailureReason = ""
	return run, nil
}

// Fail 把 claimed 或 running 任务迁移为 failed，并保存失败原因。
func (run TaskRun) Fail(reason string, now time.Time) (TaskRun, error) {
	if run.Status != TaskStatusClaimed && run.Status != TaskStatusRunning {
		return run, ErrInvalidTransition
	}
	baseline, err := run.activeTimeBaseline()
	if isBlank(reason) {
		return run, ErrInvalidInput
	}
	if err != nil || now.IsZero() || now.Before(baseline) {
		return run, ErrInvalidTime
	}
	run.Status = TaskStatusFailed
	run.FinishedAt = &now
	run.Result = nil
	run.FailureReason = reason
	return run, nil
}

// Cancel 把 queued、claimed 或 running 任务迁移为 cancelled。
func (run TaskRun) Cancel(now time.Time) (TaskRun, error) {
	baseline, err := run.activeTimeBaseline()
	if errors.Is(err, ErrInvalidTransition) {
		return run, ErrInvalidTransition
	}
	if err != nil || now.IsZero() || now.Before(baseline) {
		return run, ErrInvalidTime
	}
	run.Status = TaskStatusCancelled
	run.FinishedAt = &now
	run.Result = nil
	run.FailureReason = ""
	return run, nil
}

// activeTimeBaseline 返回活动状态最近一次已经发生的时间事实。
func (run TaskRun) activeTimeBaseline() (time.Time, error) {
	switch run.Status {
	case TaskStatusQueued:
		if run.CreatedAt.IsZero() {
			return time.Time{}, ErrInvalidTime
		}
		return run.CreatedAt, nil
	case TaskStatusClaimed:
		if run.ClaimedAt == nil || run.ClaimedAt.IsZero() {
			return time.Time{}, ErrInvalidTime
		}
		return *run.ClaimedAt, nil
	case TaskStatusRunning:
		if run.StartedAt == nil || run.StartedAt.IsZero() {
			return time.Time{}, ErrInvalidTime
		}
		return *run.StartedAt, nil
	default:
		return time.Time{}, ErrInvalidTransition
	}
}

func isBlank(value string) bool {
	return strings.TrimSpace(value) == ""
}
