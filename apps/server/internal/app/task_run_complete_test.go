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

func TestApplication_CompleteTaskRun(t *testing.T) {
	// 已完成练习 6：经领域状态机保存 completed 状态、完成时间和执行结果。

	t.Run("保存 completed 状态和执行结果", func(t *testing.T) {
		storeAdapter := memory.New()
		startedAt := taskRunTestTime.Add(2 * time.Minute)
		running := runningTaskFixture(t, storeAdapter, "run-1", startedAt)
		finishedAt := startedAt.Add(time.Minute)
		result := domain.TaskResult{
			Summary:       "实现完成并通过宿主检查",
			CodexThreadID: "thread-1",
			WorktreePath:  "/tmp/worktrees/run-1",
		}
		readTimes := 0
		application := newTaskRunApplication(
			t,
			storeAdapter,
			func() string { return "unused" },
			func() time.Time {
				readTimes++
				return finishedAt
			},
		)

		got, err := application.CompleteTaskRun(
			context.Background(),
			app.CompleteTaskRunInput{ID: "  run-1  ", Result: result},
		)
		if err != nil {
			t.Fatalf("CompleteTaskRun() error = %v", err)
		}
		if got.Status != domain.TaskStatusCompleted {
			t.Fatalf("CompleteTaskRun() status = %q, want completed", got.Status)
		}
		if got.FinishedAt == nil || !got.FinishedAt.Equal(finishedAt) {
			t.Fatalf("CompleteTaskRun() FinishedAt = %v, want %v", got.FinishedAt, finishedAt)
		}
		if got.Result == nil || *got.Result != result {
			t.Fatalf("CompleteTaskRun() Result = %#v, want %#v", got.Result, result)
		}
		if got.FailureReason != "" {
			t.Fatalf("CompleteTaskRun() FailureReason = %q, want empty", got.FailureReason)
		}
		if readTimes != 1 {
			t.Fatalf("CompleteTaskRun() 读取时钟 %d 次，want 1", readTimes)
		}

		persisted, err := storeAdapter.GetTask(context.Background(), running.ID)
		if err != nil {
			t.Fatalf("GetTask(completed) error = %v", err)
		}
		if persisted.Status != domain.TaskStatusCompleted || persisted.Result == nil || *persisted.Result != result {
			t.Fatalf("persisted TaskRun = %#v, want completed with result %#v", persisted, result)
		}
	})

	t.Run("空白 ID 或不存在任务不会读取时钟", func(t *testing.T) {
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
		result := domain.TaskResult{Summary: "完成"}

		_, err := application.CompleteTaskRun(
			context.Background(),
			app.CompleteTaskRunInput{ID: " \t ", Result: result},
		)
		if !errors.Is(err, domain.ErrInvalidInput) {
			t.Fatalf("CompleteTaskRun(blank) error = %v, want ErrInvalidInput", err)
		}
		_, err = application.CompleteTaskRun(
			context.Background(),
			app.CompleteTaskRunInput{ID: "run-missing", Result: result},
		)
		if !errors.Is(err, store.ErrNotFound) {
			t.Fatalf("CompleteTaskRun(missing) error = %v, want ErrNotFound", err)
		}
		if readTimes != 0 {
			t.Fatalf("空白 ID 或不存在任务读取时钟 %d 次，want 0", readTimes)
		}
	})

	t.Run("无效结果不会覆盖 running 任务", func(t *testing.T) {
		storeAdapter := memory.New()
		startedAt := taskRunTestTime.Add(2 * time.Minute)
		running := runningTaskFixture(t, storeAdapter, "run-1", startedAt)
		application := newTaskRunApplication(
			t,
			storeAdapter,
			func() string { return "unused" },
			func() time.Time { return startedAt.Add(time.Minute) },
		)

		_, err := application.CompleteTaskRun(
			context.Background(),
			app.CompleteTaskRunInput{ID: running.ID, Result: domain.TaskResult{Summary: " \t "}},
		)
		if !errors.Is(err, domain.ErrInvalidInput) {
			t.Fatalf("CompleteTaskRun(invalid result) error = %v, want ErrInvalidInput", err)
		}
		persisted, err := storeAdapter.GetTask(context.Background(), running.ID)
		if err != nil {
			t.Fatalf("GetTask(running) error = %v", err)
		}
		if persisted.Status != domain.TaskStatusRunning || persisted.Result != nil || persisted.FinishedAt != nil {
			t.Fatalf("无效结果修改了 TaskRun：got %#v", persisted)
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

		_, err := application.CompleteTaskRun(
			context.Background(),
			app.CompleteTaskRunInput{ID: queued.ID, Result: domain.TaskResult{Summary: "完成"}},
		)
		if !errors.Is(err, domain.ErrInvalidTransition) {
			t.Fatalf("CompleteTaskRun(queued) error = %v, want ErrInvalidTransition", err)
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

		_, err := application.CompleteTaskRun(
			ctx,
			app.CompleteTaskRunInput{ID: "run-1", Result: domain.TaskResult{Summary: "完成"}},
		)
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("CompleteTaskRun(cancelled) error = %v, want context.Canceled", err)
		}
		if readTimes != 0 {
			t.Fatalf("取消请求读取时钟 %d 次，want 0", readTimes)
		}
	})
}

func runningTaskFixture(
	t *testing.T,
	storeAdapter store.Store,
	id string,
	startedAt time.Time,
) domain.TaskRun {
	t.Helper()

	enqueueTaskFixture(t, storeAdapter, id, startedAt.Add(-2*time.Minute))
	claimed, err := storeAdapter.ClaimNextTask(context.Background(), startedAt.Add(-time.Minute))
	if err != nil {
		t.Fatalf("ClaimNextTask() fixture error = %v", err)
	}
	running, err := claimed.Start(startedAt)
	if err != nil {
		t.Fatalf("Start() fixture error = %v", err)
	}
	if err := storeAdapter.SaveTask(context.Background(), running, domain.TaskStatusClaimed); err != nil {
		t.Fatalf("SaveTask() fixture error = %v", err)
	}
	return running
}
