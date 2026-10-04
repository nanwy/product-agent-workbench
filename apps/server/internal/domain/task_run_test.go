package domain

import (
	"errors"
	"reflect"
	"testing"
	"time"
)

// NewTaskRun 的成功契约：完整保留任务身份，并且只初始化 queued 阶段已经发生的事实。
func TestNewTaskRun(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, time.October, 4, 10, 0, 0, 0, time.UTC)
	input := NewTaskRunInput{
		ID:            "RUN-001",
		RequirementID: "REQ-001",
		TicketID:      "TICKET-001",
		Attempt:       1,
	}
	run, err := NewTaskRun(input, now)
	if err != nil {
		t.Fatalf("NewTaskRun() 返回错误：%v", err)
	}

	want := TaskRun{
		ID:            input.ID,
		RequirementID: input.RequirementID,
		TicketID:      input.TicketID,
		Attempt:       input.Attempt,
		Status:        TaskStatusQueued,
		CreatedAt:     now,
	}
	if run != want {
		t.Fatalf("NewTaskRun() = %#v，期望 %#v", run, want)
	}
}

// NewTaskRun 的失败契约：输入与时间错误必须可区分，并且不能返回半初始化任务。
func TestNewTaskRun_RejectsInvalidInput(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, time.October, 4, 10, 0, 0, 0, time.UTC)
	valid := NewTaskRunInput{
		ID:            "RUN-001",
		RequirementID: "REQ-001",
		TicketID:      "TICKET-001",
		Attempt:       1,
	}

	tests := []struct {
		name    string
		input   NewTaskRunInput
		now     time.Time
		wantErr error
	}{
		{name: "缺少任务 ID", input: NewTaskRunInput{RequirementID: valid.RequirementID, TicketID: valid.TicketID, Attempt: 1}, now: now, wantErr: ErrInvalidInput},
		{name: "缺少需求 ID", input: NewTaskRunInput{ID: valid.ID, TicketID: valid.TicketID, Attempt: 1}, now: now, wantErr: ErrInvalidInput},
		{name: "缺少执行票 ID", input: NewTaskRunInput{ID: valid.ID, RequirementID: valid.RequirementID, Attempt: 1}, now: now, wantErr: ErrInvalidInput},
		{name: "执行次数为零", input: NewTaskRunInput{ID: valid.ID, RequirementID: valid.RequirementID, TicketID: valid.TicketID, Attempt: 0}, now: now, wantErr: ErrInvalidInput},
		{name: "执行次数为负数", input: NewTaskRunInput{ID: valid.ID, RequirementID: valid.RequirementID, TicketID: valid.TicketID, Attempt: -1}, now: now, wantErr: ErrInvalidInput},
		{name: "创建时间为零值", input: valid, now: time.Time{}, wantErr: ErrInvalidTime},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			got, err := NewTaskRun(tt.input, tt.now)
			if !errors.Is(err, tt.wantErr) {
				t.Fatalf("NewTaskRun() error = %v，期望 errors.Is(error, %v)", err, tt.wantErr)
			}
			if got != (TaskRun{}) {
				t.Fatalf("NewTaskRun() 失败时返回了半初始化任务：%#v", got)
			}
		})
	}
}

// Claim 的成功契约：只改变当前阶段应产生的状态和时间事实。
func TestTaskRun_Claim(t *testing.T) {
	t.Parallel()

	createdAt := time.Date(2026, time.October, 4, 10, 0, 0, 0, time.UTC)
	claimedAt := createdAt.Add(time.Minute)
	original := TaskRun{
		ID:            "RUN-001",
		RequirementID: "REQ-001",
		TicketID:      "TICKET-001",
		Attempt:       1,
		Status:        TaskStatusQueued,
		CreatedAt:     createdAt,
	}

	got, err := original.Claim(claimedAt)
	if err != nil {
		t.Fatalf("Claim() 返回错误：%v", err)
	}

	want := original
	want.Status = TaskStatusClaimed
	want.ClaimedAt = &claimedAt
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("Claim() = %#v，期望 %#v", got, want)
	}
}

// Claim 的失败契约：只有 queued 可以领取，拒绝时必须返回原 TaskRun。
func TestTaskRun_ClaimRejectsInvalidStatus(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, time.October, 4, 10, 0, 0, 0, time.UTC)
	tests := []struct {
		name   string
		status TaskStatus
	}{
		{name: "claimed 不能再次领取", status: TaskStatusClaimed},
		{name: "running 不能再次领取", status: TaskStatusRunning},
		{name: "completed 不能再次领取", status: TaskStatusCompleted},
		{name: "failed 不能再次领取", status: TaskStatusFailed},
		{name: "cancelled 不能再次领取", status: TaskStatusCancelled},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			original := TaskRun{ID: "RUN-001", Status: tt.status, CreatedAt: now}
			got, err := original.Claim(now)
			if !errors.Is(err, ErrInvalidTransition) {
				t.Fatalf("Claim() error = %v，期望 errors.Is(error, ErrInvalidTransition)", err)
			}
			if got != original {
				t.Fatalf("Claim() 失败后返回 %#v，期望保持 %#v", got, original)
			}
		})
	}
}

// Claim 的时间契约：TaskRun 必须有创建时间，领取时间不能为零或早于创建时间。
func TestTaskRun_ClaimRejectsInvalidTime(t *testing.T) {
	t.Parallel()

	createdAt := time.Date(2026, time.October, 4, 10, 0, 0, 0, time.UTC)
	tests := []struct {
		name      string
		createdAt time.Time
		claimedAt time.Time
	}{
		{name: "缺少创建时间", claimedAt: createdAt.Add(time.Minute)},
		{name: "领取时间为零值", createdAt: createdAt},
		{name: "领取时间早于创建时间", createdAt: createdAt, claimedAt: createdAt.Add(-time.Nanosecond)},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			original := TaskRun{
				ID:        "RUN-001",
				Status:    TaskStatusQueued,
				CreatedAt: tt.createdAt,
			}
			got, err := original.Claim(tt.claimedAt)
			if !errors.Is(err, ErrInvalidTime) {
				t.Fatalf("Claim() error = %v，期望 errors.Is(error, ErrInvalidTime)", err)
			}
			if got != original {
				t.Fatalf("Claim() 失败后返回 %#v，期望保持 %#v", got, original)
			}
		})
	}
}

// Start 的成功契约：claimed 任务进入 running，并记录开始时间，其他事实保持不变。
func TestTaskRun_Start(t *testing.T) {
	t.Parallel()

	createdAt := time.Date(2026, time.October, 4, 10, 0, 0, 0, time.UTC)
	claimedAt := createdAt.Add(time.Minute)
	startedAt := claimedAt.Add(time.Minute)
	original := TaskRun{
		ID:            "RUN-001",
		RequirementID: "REQ-001",
		TicketID:      "TICKET-001",
		Attempt:       1,
		Status:        TaskStatusClaimed,
		CreatedAt:     createdAt,
		ClaimedAt:     &claimedAt,
	}

	got, err := original.Start(startedAt)
	if err != nil {
		t.Fatalf("Start() 返回错误：%v", err)
	}

	want := original
	want.Status = TaskStatusRunning
	want.StartedAt = &startedAt
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("Start() = %#v，期望 %#v", got, want)
	}
}

// Start 的失败契约：只有 claimed 可以开始，拒绝时必须返回原 TaskRun。
func TestTaskRun_StartRejectsInvalidStatus(t *testing.T) {
	t.Parallel()

	claimedAt := time.Date(2026, time.October, 4, 10, 1, 0, 0, time.UTC)
	tests := []struct {
		name   string
		status TaskStatus
	}{
		{name: "queued 不能开始", status: TaskStatusQueued},
		{name: "running 不能再次开始", status: TaskStatusRunning},
		{name: "completed 不能开始", status: TaskStatusCompleted},
		{name: "failed 不能开始", status: TaskStatusFailed},
		{name: "cancelled 不能开始", status: TaskStatusCancelled},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			original := TaskRun{
				ID:        "RUN-001",
				Status:    tt.status,
				ClaimedAt: &claimedAt,
			}
			got, err := original.Start(claimedAt.Add(time.Minute))
			if !errors.Is(err, ErrInvalidTransition) {
				t.Fatalf("Start() error = %v，期望 errors.Is(error, ErrInvalidTransition)", err)
			}
			if !reflect.DeepEqual(got, original) {
				t.Fatalf("Start() 失败后返回 %#v，期望保持 %#v", got, original)
			}
		})
	}
}

// Start 的时间契约：必须已经发生 Claim，开始时间不能为零或早于领取时间。
func TestTaskRun_StartRejectsInvalidTime(t *testing.T) {
	t.Parallel()

	claimedAt := time.Date(2026, time.October, 4, 10, 1, 0, 0, time.UTC)
	tests := []struct {
		name      string
		claimedAt *time.Time
		startedAt time.Time
	}{
		{name: "缺少领取时间", startedAt: claimedAt.Add(time.Minute)},
		{name: "开始时间为零值", claimedAt: &claimedAt},
		{name: "开始时间早于领取时间", claimedAt: &claimedAt, startedAt: claimedAt.Add(-time.Nanosecond)},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			original := TaskRun{
				ID:        "RUN-001",
				Status:    TaskStatusClaimed,
				ClaimedAt: tt.claimedAt,
			}
			got, err := original.Start(tt.startedAt)
			if !errors.Is(err, ErrInvalidTime) {
				t.Fatalf("Start() error = %v，期望 errors.Is(error, ErrInvalidTime)", err)
			}
			if !reflect.DeepEqual(got, original) {
				t.Fatalf("Start() 失败后返回 %#v，期望保持 %#v", got, original)
			}
		})
	}
}

// 第三个练习：自行补充 Start、Complete、Fail、Cancel 的表驱动测试。
// 建议先写一个必然失败的案例，再写实现，不要从方法体开始猜需求。
func TestTaskRun_RemainingTransitions(t *testing.T) {
	t.Skip("学习任务：为剩余状态迁移补充测试和实现")
}
