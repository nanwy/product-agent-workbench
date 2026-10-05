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

func TestApplication_FailTaskRun(t *testing.T) {
	// 练习 7 已完成：保留这组测试作为 fail 应用用例的回归保护。

	storeAdapter := memory.New()
	startedAt := taskRunTestTime.Add(2 * time.Minute)
	running := runningTaskFixture(t, storeAdapter, "run-1", startedAt)
	failedAt := startedAt.Add(time.Minute)
	application := newTaskRunApplication(t, storeAdapter, func() string { return "unused" }, func() time.Time { return failedAt })

	got, err := application.FailTaskRun(context.Background(), app.FailTaskRunInput{
		ID:     "  run-1  ",
		Reason: "  Codex 退出码为 1  ",
	})
	if err != nil {
		t.Fatalf("FailTaskRun() error = %v", err)
	}
	if got.Status != domain.TaskStatusFailed || got.FailureReason != "Codex 退出码为 1" {
		t.Fatalf("FailTaskRun() = %#v, want failed with normalized reason", got)
	}
	if got.FinishedAt == nil || !got.FinishedAt.Equal(failedAt) {
		t.Fatalf("FailTaskRun() FinishedAt = %v, want %v", got.FinishedAt, failedAt)
	}
	persisted, err := storeAdapter.GetTask(context.Background(), running.ID)
	if err != nil {
		t.Fatalf("GetTask(failed) error = %v", err)
	}
	if persisted.Status != domain.TaskStatusFailed || persisted.FailureReason != got.FailureReason {
		t.Fatalf("persisted TaskRun = %#v, want failed", persisted)
	}

	t.Run("无效输入和不存在任务不读取时钟", func(t *testing.T) {
		readTimes := 0
		subject := newTaskRunApplication(t, memory.New(), func() string { return "unused" }, func() time.Time {
			readTimes++
			return taskRunTestTime
		})
		if _, err := subject.FailTaskRun(context.Background(), app.FailTaskRunInput{ID: " ", Reason: "失败"}); !errors.Is(err, domain.ErrInvalidInput) {
			t.Fatalf("FailTaskRun(blank ID) error = %v, want ErrInvalidInput", err)
		}
		if _, err := subject.FailTaskRun(context.Background(), app.FailTaskRunInput{ID: "run-1", Reason: " "}); !errors.Is(err, domain.ErrInvalidInput) {
			t.Fatalf("FailTaskRun(blank reason) error = %v, want ErrInvalidInput", err)
		}
		if _, err := subject.FailTaskRun(context.Background(), app.FailTaskRunInput{ID: "missing", Reason: "失败"}); !errors.Is(err, store.ErrNotFound) {
			t.Fatalf("FailTaskRun(missing) error = %v, want ErrNotFound", err)
		}
		if readTimes != 0 {
			t.Fatalf("失败前置校验读取时钟 %d 次，want 0", readTimes)
		}
	})
}

func TestApplication_CancelTaskRun(t *testing.T) {
	// 练习 8 已完成：保留这组测试作为 cancel 应用用例的回归保护。

	storeAdapter := memory.New()
	startedAt := taskRunTestTime.Add(2 * time.Minute)
	running := runningTaskFixture(t, storeAdapter, "run-1", startedAt)
	cancelledAt := startedAt.Add(time.Minute)
	application := newTaskRunApplication(t, storeAdapter, func() string { return "unused" }, func() time.Time { return cancelledAt })

	got, err := application.CancelTaskRun(context.Background(), "  run-1  ")
	if err != nil {
		t.Fatalf("CancelTaskRun() error = %v", err)
	}
	if got.Status != domain.TaskStatusCancelled || got.FinishedAt == nil || !got.FinishedAt.Equal(cancelledAt) {
		t.Fatalf("CancelTaskRun() = %#v, want cancelled at %v", got, cancelledAt)
	}
	persisted, err := storeAdapter.GetTask(context.Background(), running.ID)
	if err != nil {
		t.Fatalf("GetTask(cancelled) error = %v", err)
	}
	if persisted.Status != domain.TaskStatusCancelled {
		t.Fatalf("persisted status = %q, want cancelled", persisted.Status)
	}

	t.Run("空白 ID、不存在任务和取消 context 不读取时钟", func(t *testing.T) {
		readTimes := 0
		subject := newTaskRunApplication(t, memory.New(), func() string { return "unused" }, func() time.Time {
			readTimes++
			return taskRunTestTime
		})
		if _, err := subject.CancelTaskRun(context.Background(), " \t "); !errors.Is(err, domain.ErrInvalidInput) {
			t.Fatalf("CancelTaskRun(blank) error = %v, want ErrInvalidInput", err)
		}
		if _, err := subject.CancelTaskRun(context.Background(), "missing"); !errors.Is(err, store.ErrNotFound) {
			t.Fatalf("CancelTaskRun(missing) error = %v, want ErrNotFound", err)
		}
		ctx, cancel := context.WithCancel(context.Background())
		cancel()
		if _, err := subject.CancelTaskRun(ctx, "run-1"); !errors.Is(err, context.Canceled) {
			t.Fatalf("CancelTaskRun(cancelled context) error = %v, want context.Canceled", err)
		}
		if readTimes != 0 {
			t.Fatalf("取消前置校验读取时钟 %d 次，want 0", readTimes)
		}
	})
}
