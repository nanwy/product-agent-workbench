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
		{name: "任务 ID 只有空白", input: NewTaskRunInput{ID: " \t", RequirementID: valid.RequirementID, TicketID: valid.TicketID, Attempt: 1}, now: now, wantErr: ErrInvalidInput},
		{name: "需求 ID 只有空白", input: NewTaskRunInput{ID: valid.ID, RequirementID: "\n", TicketID: valid.TicketID, Attempt: 1}, now: now, wantErr: ErrInvalidInput},
		{name: "执行票 ID 只有空白", input: NewTaskRunInput{ID: valid.ID, RequirementID: valid.RequirementID, TicketID: " ", Attempt: 1}, now: now, wantErr: ErrInvalidInput},
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
	zero := time.Time{}
	tests := []struct {
		name      string
		claimedAt *time.Time
		startedAt time.Time
	}{
		{name: "缺少领取时间", startedAt: claimedAt.Add(time.Minute)},
		{name: "领取时间为零值", claimedAt: &zero, startedAt: claimedAt.Add(time.Minute)},
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

// Complete 的成功契约：running 任务进入 completed，并保存互斥的成功结果。
func TestTaskRun_Complete(t *testing.T) {
	t.Parallel()

	startedAt := time.Date(2026, time.October, 4, 10, 2, 0, 0, time.UTC)
	finishedAt := startedAt.Add(time.Minute)
	result := TaskResult{Summary: "实现完成", CodexThreadID: "thread-1", WorktreePath: "/tmp/worktree"}
	original := TaskRun{
		ID:            "RUN-001",
		Status:        TaskStatusRunning,
		StartedAt:     &startedAt,
		FailureReason: "旧失败原因",
	}

	got, err := original.Complete(result, finishedAt)
	if err != nil {
		t.Fatalf("Complete() 返回错误：%v", err)
	}
	want := original
	want.Status = TaskStatusCompleted
	want.FinishedAt = &finishedAt
	want.Result = &result
	want.FailureReason = ""
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("Complete() = %#v，期望 %#v", got, want)
	}
}

func TestTaskRun_CompleteRejectsInvalidStatus(t *testing.T) {
	t.Parallel()

	startedAt := time.Date(2026, time.October, 4, 10, 2, 0, 0, time.UTC)
	statuses := []TaskStatus{TaskStatusQueued, TaskStatusClaimed, TaskStatusCompleted, TaskStatusFailed, TaskStatusCancelled}
	for _, status := range statuses {
		t.Run(string(status), func(t *testing.T) {
			t.Parallel()
			original := TaskRun{ID: "RUN-001", Status: status, StartedAt: &startedAt}
			got, err := original.Complete(TaskResult{Summary: "完成"}, startedAt)
			assertTransitionFailure(t, got, err, original, ErrInvalidTransition)
		})
	}
}

func TestTaskRun_CompleteRejectsInvalidInput(t *testing.T) {
	t.Parallel()

	startedAt := time.Date(2026, time.October, 4, 10, 2, 0, 0, time.UTC)
	for _, summary := range []string{"", " \t\n"} {
		original := TaskRun{ID: "RUN-001", Status: TaskStatusRunning, StartedAt: &startedAt}
		got, err := original.Complete(TaskResult{Summary: summary}, startedAt)
		assertTransitionFailure(t, got, err, original, ErrInvalidInput)
	}
}

func TestTaskRun_CompleteRejectsInvalidTime(t *testing.T) {
	t.Parallel()

	startedAt := time.Date(2026, time.October, 4, 10, 2, 0, 0, time.UTC)
	zero := time.Time{}
	tests := []struct {
		name       string
		startedAt  *time.Time
		finishedAt time.Time
	}{
		{name: "缺少开始时间", finishedAt: startedAt.Add(time.Minute)},
		{name: "开始时间为零值", startedAt: &zero, finishedAt: startedAt.Add(time.Minute)},
		{name: "完成时间为零值", startedAt: &startedAt},
		{name: "完成时间早于开始时间", startedAt: &startedAt, finishedAt: startedAt.Add(-time.Nanosecond)},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			original := TaskRun{ID: "RUN-001", Status: TaskStatusRunning, StartedAt: tt.startedAt}
			got, err := original.Complete(TaskResult{Summary: "完成"}, tt.finishedAt)
			assertTransitionFailure(t, got, err, original, ErrInvalidTime)
		})
	}
}

// Fail 的成功契约：claimed 和 running 都能失败，并按各自最近的阶段时间校验。
func TestTaskRun_Fail(t *testing.T) {
	t.Parallel()

	claimedAt := time.Date(2026, time.October, 4, 10, 1, 0, 0, time.UTC)
	startedAt := claimedAt.Add(time.Minute)
	result := TaskResult{Summary: "不应保留"}
	tests := []struct {
		name string
		run  TaskRun
		now  time.Time
	}{
		{name: "claimed 可以失败", run: TaskRun{ID: "RUN-001", Status: TaskStatusClaimed, ClaimedAt: &claimedAt, Result: &result}, now: claimedAt},
		{name: "running 可以失败", run: TaskRun{ID: "RUN-001", Status: TaskStatusRunning, StartedAt: &startedAt, Result: &result}, now: startedAt},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got, err := tt.run.Fail("执行失败", tt.now)
			if err != nil {
				t.Fatalf("Fail() 返回错误：%v", err)
			}
			want := tt.run
			want.Status = TaskStatusFailed
			want.FinishedAt = &tt.now
			want.Result = nil
			want.FailureReason = "执行失败"
			if !reflect.DeepEqual(got, want) {
				t.Fatalf("Fail() = %#v，期望 %#v", got, want)
			}
		})
	}
}

func TestTaskRun_FailRejectsInvalidStatusOrInput(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, time.October, 4, 10, 2, 0, 0, time.UTC)
	for _, status := range []TaskStatus{TaskStatusQueued, TaskStatusCompleted, TaskStatusFailed, TaskStatusCancelled} {
		original := TaskRun{ID: "RUN-001", Status: status, StartedAt: &now}
		got, err := original.Fail("失败", now)
		assertTransitionFailure(t, got, err, original, ErrInvalidTransition)
	}
	for _, reason := range []string{"", " \t\n"} {
		original := TaskRun{ID: "RUN-001", Status: TaskStatusRunning, StartedAt: &now}
		got, err := original.Fail(reason, now)
		assertTransitionFailure(t, got, err, original, ErrInvalidInput)
	}
}

func TestTaskRun_FailRejectsInvalidTime(t *testing.T) {
	t.Parallel()

	baseline := time.Date(2026, time.October, 4, 10, 2, 0, 0, time.UTC)
	zero := time.Time{}
	tests := []struct {
		name string
		run  TaskRun
		now  time.Time
	}{
		{name: "claimed 缺少领取时间", run: TaskRun{ID: "RUN-001", Status: TaskStatusClaimed}, now: baseline},
		{name: "claimed 领取时间为零值", run: TaskRun{ID: "RUN-001", Status: TaskStatusClaimed, ClaimedAt: &zero}, now: baseline},
		{name: "claimed 失败时间倒流", run: TaskRun{ID: "RUN-001", Status: TaskStatusClaimed, ClaimedAt: &baseline}, now: baseline.Add(-time.Nanosecond)},
		{name: "running 缺少开始时间", run: TaskRun{ID: "RUN-001", Status: TaskStatusRunning}, now: baseline},
		{name: "running 开始时间为零值", run: TaskRun{ID: "RUN-001", Status: TaskStatusRunning, StartedAt: &zero}, now: baseline},
		{name: "running 失败时间倒流", run: TaskRun{ID: "RUN-001", Status: TaskStatusRunning, StartedAt: &baseline}, now: baseline.Add(-time.Nanosecond)},
		{name: "失败时间为零值", run: TaskRun{ID: "RUN-001", Status: TaskStatusRunning, StartedAt: &baseline}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got, err := tt.run.Fail("失败", tt.now)
			assertTransitionFailure(t, got, err, tt.run, ErrInvalidTime)
		})
	}
}

// Cancel 的成功契约：三个活动状态都能取消，并清除互斥的结果字段。
func TestTaskRun_Cancel(t *testing.T) {
	t.Parallel()

	createdAt := time.Date(2026, time.October, 4, 10, 0, 0, 0, time.UTC)
	claimedAt := createdAt.Add(time.Minute)
	startedAt := claimedAt.Add(time.Minute)
	result := TaskResult{Summary: "不应保留"}
	tests := []struct {
		name string
		run  TaskRun
		now  time.Time
	}{
		{name: "queued 可以取消", run: TaskRun{ID: "RUN-001", Status: TaskStatusQueued, CreatedAt: createdAt, Result: &result, FailureReason: "旧错误"}, now: createdAt},
		{name: "claimed 可以取消", run: TaskRun{ID: "RUN-001", Status: TaskStatusClaimed, ClaimedAt: &claimedAt, Result: &result, FailureReason: "旧错误"}, now: claimedAt},
		{name: "running 可以取消", run: TaskRun{ID: "RUN-001", Status: TaskStatusRunning, StartedAt: &startedAt, Result: &result, FailureReason: "旧错误"}, now: startedAt},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got, err := tt.run.Cancel(tt.now)
			if err != nil {
				t.Fatalf("Cancel() 返回错误：%v", err)
			}
			want := tt.run
			want.Status = TaskStatusCancelled
			want.FinishedAt = &tt.now
			want.Result = nil
			want.FailureReason = ""
			if !reflect.DeepEqual(got, want) {
				t.Fatalf("Cancel() = %#v，期望 %#v", got, want)
			}
		})
	}
}

func TestTaskRun_CancelRejectsInvalidStatus(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, time.October, 4, 10, 2, 0, 0, time.UTC)
	for _, status := range []TaskStatus{TaskStatusCompleted, TaskStatusFailed, TaskStatusCancelled} {
		original := TaskRun{ID: "RUN-001", Status: status, StartedAt: &now}
		got, err := original.Cancel(now)
		assertTransitionFailure(t, got, err, original, ErrInvalidTransition)
	}
}

func TestTaskRun_CancelRejectsInvalidTime(t *testing.T) {
	t.Parallel()

	baseline := time.Date(2026, time.October, 4, 10, 2, 0, 0, time.UTC)
	zero := time.Time{}
	tests := []struct {
		name string
		run  TaskRun
		now  time.Time
	}{
		{name: "queued 缺少创建时间", run: TaskRun{ID: "RUN-001", Status: TaskStatusQueued}, now: baseline},
		{name: "claimed 缺少领取时间", run: TaskRun{ID: "RUN-001", Status: TaskStatusClaimed}, now: baseline},
		{name: "claimed 领取时间为零值", run: TaskRun{ID: "RUN-001", Status: TaskStatusClaimed, ClaimedAt: &zero}, now: baseline},
		{name: "running 缺少开始时间", run: TaskRun{ID: "RUN-001", Status: TaskStatusRunning}, now: baseline},
		{name: "running 开始时间为零值", run: TaskRun{ID: "RUN-001", Status: TaskStatusRunning, StartedAt: &zero}, now: baseline},
		{name: "取消时间倒流", run: TaskRun{ID: "RUN-001", Status: TaskStatusRunning, StartedAt: &baseline}, now: baseline.Add(-time.Nanosecond)},
		{name: "取消时间为零值", run: TaskRun{ID: "RUN-001", Status: TaskStatusRunning, StartedAt: &baseline}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got, err := tt.run.Cancel(tt.now)
			assertTransitionFailure(t, got, err, tt.run, ErrInvalidTime)
		})
	}
}

// 阶段时间允许相等；契约只禁止“早于”，不要求每一步至少经过一个时钟 tick。
func TestTaskRun_AllowsEqualTransitionTimes(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, time.October, 4, 10, 0, 0, 0, time.UTC)
	run, err := NewTaskRun(NewTaskRunInput{ID: "RUN-001", RequirementID: "REQ-001", TicketID: "TICKET-001", Attempt: 1}, now)
	if err != nil {
		t.Fatal(err)
	}
	run, err = run.Claim(now)
	if err != nil {
		t.Fatalf("Claim() 拒绝相等时间：%v", err)
	}
	run, err = run.Start(now)
	if err != nil {
		t.Fatalf("Start() 拒绝相等时间：%v", err)
	}
	if _, err = run.Complete(TaskResult{Summary: "完成"}, now); err != nil {
		t.Fatalf("Complete() 拒绝相等时间：%v", err)
	}
}

func assertTransitionFailure(t *testing.T, got TaskRun, err error, original TaskRun, wantErr error) {
	t.Helper()
	if !errors.Is(err, wantErr) {
		t.Fatalf("error = %v，期望 errors.Is(error, %v)", err, wantErr)
	}
	if !reflect.DeepEqual(got, original) {
		t.Fatalf("失败迁移返回 %#v，期望保持 %#v", got, original)
	}
}
