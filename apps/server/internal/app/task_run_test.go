package app_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"product-agent-workbench/internal/app"
	"product-agent-workbench/internal/domain"
	"product-agent-workbench/internal/store"
	"product-agent-workbench/internal/store/memory"
)

var taskRunTestTime = time.Date(2026, time.October, 4, 10, 30, 0, 0, time.UTC)

func TestNewRejectsMissingClock(t *testing.T) {
	// 已完成练习 1：Application 必须显式接收 Clock，构造期间不读取它。

	generatedIDs := 0
	application, err := app.New(app.Dependencies{
		Store: memory.New(),
		NewID: func() string {
			generatedIDs++
			return "run-1"
		},
	})
	if !errors.Is(err, app.ErrInvalidDependencies) {
		t.Fatalf("New() error = %v, want ErrInvalidDependencies", err)
	}
	if application != nil {
		t.Fatalf("New() application = %#v, want nil", application)
	}
	if generatedIDs != 0 {
		t.Fatalf("New() 调用了 ID 生成器 %d 次，want 0", generatedIDs)
	}
}

func TestApplication_EnqueueTaskRun(t *testing.T) {
	// 已完成练习 2：输入校验、父实体检查、领域构造和持久化。

	t.Run("规范化输入并持久化 queued TaskRun", func(t *testing.T) {
		storeAdapter := memory.New()
		createRequirementFixture(t, storeAdapter, "req-1")

		generatedIDs := 0
		readTimes := 0
		application := newTaskRunApplication(
			t,
			storeAdapter,
			func() string {
				generatedIDs++
				return "run-1"
			},
			func() time.Time {
				readTimes++
				return taskRunTestTime
			},
		)

		got, err := application.EnqueueTaskRun(
			context.Background(),
			app.EnqueueTaskRunInput{
				RequirementID:   "  req-1  ",
				TicketVersionID: "  req-1-tickets-v1  ",
				TicketID:        "  ticket-1  ",
				Attempt:         1,
			},
		)
		if err != nil {
			t.Fatalf("EnqueueTaskRun() error = %v", err)
		}

		want := domain.TaskRun{
			ID:              "run-1",
			RequirementID:   "req-1",
			TicketVersionID: "req-1-tickets-v1",
			TicketID:        "ticket-1",
			Attempt:         1,
			Status:          domain.TaskStatusQueued,
			CreatedAt:       taskRunTestTime,
		}
		if got != want {
			t.Fatalf("EnqueueTaskRun() = %#v, want %#v", got, want)
		}
		if generatedIDs != 1 || readTimes != 1 {
			t.Fatalf("NewID 调用 %d 次，Now 调用 %d 次，want 都为 1", generatedIDs, readTimes)
		}

		persisted, err := storeAdapter.GetTask(context.Background(), want.ID)
		if err != nil {
			t.Fatalf("GetTask() persisted error = %v", err)
		}
		if persisted != want {
			t.Fatalf("persisted TaskRun = %#v, want %#v", persisted, want)
		}
	})

	t.Run("无效输入不会读取父实体或生成运行事实", func(t *testing.T) {
		storeAdapter := memory.New()
		generatedIDs := 0
		readTimes := 0
		application := newTaskRunApplication(
			t,
			storeAdapter,
			func() string {
				generatedIDs++
				return "run-1"
			},
			func() time.Time {
				readTimes++
				return taskRunTestTime
			},
		)

		tests := []struct {
			name  string
			input app.EnqueueTaskRunInput
		}{
			{name: "缺少 RequirementID", input: app.EnqueueTaskRunInput{TicketID: "ticket-1", Attempt: 1}},
			{name: "TicketID 只有空白", input: app.EnqueueTaskRunInput{RequirementID: "req-1", TicketID: " \t ", Attempt: 1}},
			{name: "Attempt 小于一", input: app.EnqueueTaskRunInput{RequirementID: "req-1", TicketID: "ticket-1", Attempt: 0}},
		}

		for _, tt := range tests {
			t.Run(tt.name, func(t *testing.T) {
				_, err := application.EnqueueTaskRun(context.Background(), tt.input)
				if !errors.Is(err, domain.ErrInvalidInput) {
					t.Fatalf("EnqueueTaskRun() error = %v, want ErrInvalidInput", err)
				}
			})
		}
		if generatedIDs != 0 || readTimes != 0 {
			t.Fatalf("无效输入调用 NewID %d 次、Now %d 次，want 都为 0", generatedIDs, readTimes)
		}
	})

	t.Run("父 Requirement 不存在时不生成 ID 和时间", func(t *testing.T) {
		generatedIDs := 0
		readTimes := 0
		application := newTaskRunApplication(
			t,
			memory.New(),
			func() string {
				generatedIDs++
				return "run-1"
			},
			func() time.Time {
				readTimes++
				return taskRunTestTime
			},
		)

		_, err := application.EnqueueTaskRun(
			context.Background(),
			app.EnqueueTaskRunInput{RequirementID: "req-missing", TicketVersionID: "req-missing-tickets-v1", TicketID: "ticket-1", Attempt: 1},
		)
		if !errors.Is(err, store.ErrNotFound) {
			t.Fatalf("EnqueueTaskRun() error = %v, want ErrNotFound", err)
		}
		if generatedIDs != 0 || readTimes != 0 {
			t.Fatalf("父实体不存在时调用 NewID %d 次、Now %d 次，want 都为 0", generatedIDs, readTimes)
		}
	})

	t.Run("生成的 ID 也要规范化", func(t *testing.T) {
		storeAdapter := memory.New()
		createRequirementFixture(t, storeAdapter, "req-1")
		application := newTaskRunApplication(
			t,
			storeAdapter,
			func() string { return "  run-1  " },
			func() time.Time { return taskRunTestTime },
		)

		got, err := application.EnqueueTaskRun(
			context.Background(),
			app.EnqueueTaskRunInput{RequirementID: "req-1", TicketVersionID: "req-1-tickets-v1", TicketID: "ticket-1", Attempt: 1},
		)
		if err != nil {
			t.Fatalf("EnqueueTaskRun() error = %v", err)
		}
		if got.ID != "run-1" {
			t.Fatalf("EnqueueTaskRun() ID = %q, want %q", got.ID, "run-1")
		}
		if _, err := storeAdapter.GetTask(context.Background(), "run-1"); err != nil {
			t.Fatalf("GetTask(clean ID) error = %v", err)
		}
	})

	t.Run("取消的 context 不会生成 ID 和时间", func(t *testing.T) {
		generatedIDs := 0
		readTimes := 0
		application := newTaskRunApplication(
			t,
			memory.New(),
			func() string {
				generatedIDs++
				return "run-1"
			},
			func() time.Time {
				readTimes++
				return taskRunTestTime
			},
		)
		ctx, cancel := context.WithCancel(context.Background())
		cancel()

		_, err := application.EnqueueTaskRun(
			ctx,
			app.EnqueueTaskRunInput{RequirementID: "req-1", TicketID: "ticket-1", Attempt: 1},
		)
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("EnqueueTaskRun(cancelled) error = %v, want context.Canceled", err)
		}
		if generatedIDs != 0 || readTimes != 0 {
			t.Fatalf("取消请求调用 NewID %d 次、Now %d 次，want 都为 0", generatedIDs, readTimes)
		}
	})
}

func TestApplication_GetTaskRun(t *testing.T) {
	// 已完成练习 3：按规范化 ID 读取 TaskRun，并传播 context 和 Store 错误。

	storeAdapter := memory.New()
	createRequirementFixture(t, storeAdapter, "req-1")
	want, err := domain.NewTaskRun(domain.NewTaskRunInput{
		ID:              "run-1",
		RequirementID:   "req-1",
		TicketVersionID: "req-1-tickets-v1",
		TicketID:        "ticket-1",
		Attempt:         1,
	}, taskRunTestTime)
	if err != nil {
		t.Fatalf("NewTaskRun() fixture error = %v", err)
	}
	if err := storeAdapter.EnqueueTask(context.Background(), want); err != nil {
		t.Fatalf("EnqueueTask() fixture error = %v", err)
	}
	application := newTaskRunApplication(t, storeAdapter, func() string { return "unused" }, func() time.Time { return taskRunTestTime })

	t.Run("清理 ID 并返回实体", func(t *testing.T) {
		got, err := application.GetTaskRun(context.Background(), "  run-1  ")
		if err != nil {
			t.Fatalf("GetTaskRun() error = %v", err)
		}
		if got != want {
			t.Fatalf("GetTaskRun() = %#v, want %#v", got, want)
		}
	})

	t.Run("空白 ID 返回输入错误", func(t *testing.T) {
		_, err := application.GetTaskRun(context.Background(), " \t ")
		if !errors.Is(err, domain.ErrInvalidInput) {
			t.Fatalf("GetTaskRun() error = %v, want ErrInvalidInput", err)
		}
	})

	t.Run("不存在时透传 Store 错误", func(t *testing.T) {
		_, err := application.GetTaskRun(context.Background(), "run-missing")
		if !errors.Is(err, store.ErrNotFound) {
			t.Fatalf("GetTaskRun() error = %v, want ErrNotFound", err)
		}
	})

	t.Run("取消的 context 原样传播", func(t *testing.T) {
		ctx, cancel := context.WithCancel(context.Background())
		cancel()

		if _, err := application.GetTaskRun(ctx, want.ID); !errors.Is(err, context.Canceled) {
			t.Fatalf("GetTaskRun(cancelled) error = %v, want context.Canceled", err)
		}
	})
}

func createRequirementFixture(t *testing.T, storeAdapter store.Store, id string) {
	t.Helper()

	requirement := domain.Requirement{
		ID:        id,
		ProjectID: "project-1",
		Title:     "TaskRun 父需求",
		Stage:     domain.RequirementStageGrilling,
		Revision:  1,
	}
	if err := storeAdapter.CreateRequirement(context.Background(), requirement); err != nil {
		t.Fatalf("CreateRequirement() fixture error = %v", err)
	}
}

func newTaskRunApplication(
	t *testing.T,
	storeAdapter store.Store,
	newID app.IDGenerator,
	now app.Clock,
) *app.Application {
	t.Helper()

	application, err := app.New(app.Dependencies{
		Store: storeAdapter,
		NewID: newID,
		Now:   now,
	})
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	return application
}
