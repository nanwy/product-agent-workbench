package app_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"product-agent-workbench/internal/domain"
	"product-agent-workbench/internal/store"
	"product-agent-workbench/internal/store/memory"
)

func TestApplication_ClaimNextTaskRun(t *testing.T) {
	// 已完成练习 4：context 检查后读取时间，并通过 Store 原子领取 queued TaskRun。

	t.Run("领取最早任务并保存 claimed 事实", func(t *testing.T) {
		storeAdapter := memory.New()
		first := enqueueTaskFixtureForTicket(t, storeAdapter, "run-1", "ticket-1", taskRunTestTime)
		second := enqueueTaskFixtureForTicket(t, storeAdapter, "run-2", "ticket-2", taskRunTestTime.Add(time.Second))
		claimedAt := taskRunTestTime.Add(time.Minute)
		readTimes := 0
		application := newTaskRunApplication(
			t,
			storeAdapter,
			func() string { return "unused" },
			func() time.Time {
				readTimes++
				return claimedAt
			},
		)

		got, err := application.ClaimNextTaskRun(context.Background())
		if err != nil {
			t.Fatalf("ClaimNextTaskRun() error = %v", err)
		}
		if got.ID != first.ID || got.Status != domain.TaskStatusClaimed {
			t.Fatalf("ClaimNextTaskRun() = %#v, want first task claimed", got)
		}
		if got.ClaimedAt == nil || !got.ClaimedAt.Equal(claimedAt) {
			t.Fatalf("ClaimNextTaskRun() ClaimedAt = %v, want %v", got.ClaimedAt, claimedAt)
		}
		if readTimes != 1 {
			t.Fatalf("ClaimNextTaskRun() 读取时钟 %d 次，want 1", readTimes)
		}

		persisted, err := storeAdapter.GetTask(context.Background(), first.ID)
		if err != nil {
			t.Fatalf("GetTask(claimed) error = %v", err)
		}
		if persisted.Status != domain.TaskStatusClaimed {
			t.Fatalf("persisted status = %q, want claimed", persisted.Status)
		}
		untouched, err := storeAdapter.GetTask(context.Background(), second.ID)
		if err != nil {
			t.Fatalf("GetTask(second) error = %v", err)
		}
		if untouched.Status != domain.TaskStatusQueued {
			t.Fatalf("second status = %q, want queued", untouched.Status)
		}
	})

	t.Run("没有 queued 任务时透传 Store 错误", func(t *testing.T) {
		application := newTaskRunApplication(
			t,
			memory.New(),
			func() string { return "unused" },
			func() time.Time { return taskRunTestTime },
		)

		_, err := application.ClaimNextTaskRun(context.Background())
		if !errors.Is(err, store.ErrNotFound) {
			t.Fatalf("ClaimNextTaskRun() error = %v, want ErrNotFound", err)
		}
	})

	t.Run("取消的 context 不读取时钟", func(t *testing.T) {
		readTimes := 0
		application := newTaskRunApplication(
			t,
			memory.New(),
			func() string { return "unused" },
			func() time.Time {
				readTimes++
				return taskRunTestTime
			},
		)
		ctx, cancel := context.WithCancel(context.Background())
		cancel()

		_, err := application.ClaimNextTaskRun(ctx)
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("ClaimNextTaskRun(cancelled) error = %v, want context.Canceled", err)
		}
		if readTimes != 0 {
			t.Fatalf("取消请求读取时钟 %d 次，want 0", readTimes)
		}
	})
}

func TestApplication_StartTaskRun(t *testing.T) {
	// 已完成练习 5：规范化 ID，经领域状态机迁移后保存 running TaskRun。

	t.Run("把 claimed 任务迁移并保存为 running", func(t *testing.T) {
		storeAdapter := memory.New()
		claimedAt := taskRunTestTime.Add(time.Minute)
		claimed := claimedTaskFixture(t, storeAdapter, "run-1", claimedAt)
		startedAt := claimedAt.Add(time.Minute)
		application := newTaskRunApplication(
			t,
			storeAdapter,
			func() string { return "unused" },
			func() time.Time { return startedAt },
		)

		got, err := application.StartTaskRun(context.Background(), "  run-1  ")
		if err != nil {
			t.Fatalf("StartTaskRun() error = %v", err)
		}
		if got.Status != domain.TaskStatusRunning {
			t.Fatalf("StartTaskRun() status = %q, want running", got.Status)
		}
		if got.StartedAt == nil || !got.StartedAt.Equal(startedAt) {
			t.Fatalf("StartTaskRun() StartedAt = %v, want %v", got.StartedAt, startedAt)
		}

		persisted, err := storeAdapter.GetTask(context.Background(), claimed.ID)
		if err != nil {
			t.Fatalf("GetTask(running) error = %v", err)
		}
		if persisted.Status != domain.TaskStatusRunning || persisted.StartedAt == nil || !persisted.StartedAt.Equal(startedAt) {
			t.Fatalf("persisted TaskRun = %#v, want running at %v", persisted, startedAt)
		}
	})

	t.Run("无效 ID 或不存在任务不会读取时钟", func(t *testing.T) {
		readTimes := 0
		application := newTaskRunApplication(
			t,
			memory.New(),
			func() string { return "unused" },
			func() time.Time {
				readTimes++
				return taskRunTestTime
			},
		)

		if _, err := application.StartTaskRun(context.Background(), " \t "); !errors.Is(err, domain.ErrInvalidInput) {
			t.Fatalf("StartTaskRun(blank) error = %v, want ErrInvalidInput", err)
		}
		if _, err := application.StartTaskRun(context.Background(), "run-missing"); !errors.Is(err, store.ErrNotFound) {
			t.Fatalf("StartTaskRun(missing) error = %v, want ErrNotFound", err)
		}
		if readTimes != 0 {
			t.Fatalf("无效或不存在任务读取时钟 %d 次，want 0", readTimes)
		}
	})

	t.Run("非法前态不会覆盖已保存任务", func(t *testing.T) {
		storeAdapter := memory.New()
		queued := enqueueTaskFixture(t, storeAdapter, "run-queued", taskRunTestTime)
		application := newTaskRunApplication(
			t,
			storeAdapter,
			func() string { return "unused" },
			func() time.Time { return taskRunTestTime.Add(time.Minute) },
		)

		_, err := application.StartTaskRun(context.Background(), queued.ID)
		if !errors.Is(err, domain.ErrInvalidTransition) {
			t.Fatalf("StartTaskRun(queued) error = %v, want ErrInvalidTransition", err)
		}
		persisted, err := storeAdapter.GetTask(context.Background(), queued.ID)
		if err != nil {
			t.Fatalf("GetTask(queued) error = %v", err)
		}
		if persisted != queued {
			t.Fatalf("非法迁移修改了 TaskRun：got %#v, want %#v", persisted, queued)
		}
	})

	t.Run("取消的 context 不读取时钟", func(t *testing.T) {
		readTimes := 0
		application := newTaskRunApplication(
			t,
			memory.New(),
			func() string { return "unused" },
			func() time.Time {
				readTimes++
				return taskRunTestTime
			},
		)
		ctx, cancel := context.WithCancel(context.Background())
		cancel()

		_, err := application.StartTaskRun(ctx, "run-1")
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("StartTaskRun(cancelled) error = %v, want context.Canceled", err)
		}
		if readTimes != 0 {
			t.Fatalf("取消请求读取时钟 %d 次，want 0", readTimes)
		}
	})
}

func enqueueTaskFixture(
	t *testing.T,
	storeAdapter store.Store,
	id string,
	createdAt time.Time,
) domain.TaskRun {
	t.Helper()
	return enqueueTaskFixtureForTicket(t, storeAdapter, id, "ticket-1", createdAt)
}

func enqueueTaskFixtureForTicket(
	t *testing.T,
	storeAdapter store.Store,
	id string,
	ticketID string,
	createdAt time.Time,
) domain.TaskRun {
	t.Helper()

	run, err := domain.NewTaskRun(domain.NewTaskRunInput{
		ID:            id,
		RequirementID: "req-1",
		TicketID:      ticketID,
		Attempt:       1,
	}, createdAt)
	if err != nil {
		t.Fatalf("NewTaskRun() fixture error = %v", err)
	}
	if err := storeAdapter.EnqueueTask(context.Background(), run); err != nil {
		t.Fatalf("EnqueueTask() fixture error = %v", err)
	}
	return run
}

func claimedTaskFixture(
	t *testing.T,
	storeAdapter store.Store,
	id string,
	claimedAt time.Time,
) domain.TaskRun {
	t.Helper()

	enqueueTaskFixture(t, storeAdapter, id, claimedAt.Add(-time.Minute))
	claimed, err := storeAdapter.ClaimNextTask(context.Background(), claimedAt)
	if err != nil {
		t.Fatalf("ClaimNextTask() fixture error = %v", err)
	}
	return claimed
}
