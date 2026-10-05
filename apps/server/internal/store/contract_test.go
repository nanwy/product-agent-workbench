package store_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"product-agent-workbench/internal/domain"
	"product-agent-workbench/internal/store"
	"product-agent-workbench/internal/store/memory"
)

// TestMemoryStoreContract 是内存 Adapter 的行为契约。
//
// Requirement 契约已提取为 factory 并与 SQLite 复用。TaskRun、claim 和 TaskEvent
// 会按纵向切片逐组提取；每次只解锁当前正在实现的一组 SQL。
func TestMemoryStoreContract(t *testing.T) {
	runRequirementStoreContract(t, func(t *testing.T) store.Store {
		t.Helper()
		return memory.New()
	})
	runTaskRunStoreContract(t, func(t *testing.T) store.Store {
		t.Helper()
		return memory.New()
	})
	runClaimStoreContract(t, func(t *testing.T) store.Store {
		t.Helper()
		return memory.New()
	})

	t.Run("事件批量追加连续、原子且使用深拷贝", func(t *testing.T) {
		subject := memory.New()
		ctx := context.Background()
		createdAt := testTime()
		run := queuedTaskFixture(t, "run-events", createdAt)
		if err := subject.EnqueueTask(ctx, run); err != nil {
			t.Fatalf("EnqueueTask() error = %v", err)
		}

		batch := []domain.TaskEvent{
			{
				TaskRunID: run.ID,
				Sequence:  1,
				Type:      "task.claimed",
				Payload:   []byte(`{"worker":"local"}`),
				CreatedAt: createdAt.Add(time.Second),
			},
			{
				TaskRunID: run.ID,
				Sequence:  2,
				Type:      "task.started",
				Payload:   []byte(`{"status":"running"}`),
				CreatedAt: createdAt.Add(2 * time.Second),
			},
		}
		lastID, err := subject.AppendTaskEvents(ctx, batch)
		if err != nil {
			t.Fatalf("AppendTaskEvents() error = %v", err)
		}
		if lastID != 2 {
			t.Fatalf("AppendTaskEvents() lastID = %d, want 2", lastID)
		}

		batch[0].Payload[0] = '!'
		events, err := subject.ListTaskEvents(ctx, run.ID, 0)
		if err != nil {
			t.Fatalf("ListTaskEvents() error = %v", err)
		}
		if len(events) != 2 || events[0].ID != 1 || events[1].ID != 2 {
			t.Fatalf("ListTaskEvents() = %#v, want two ordered events", events)
		}
		if string(events[0].Payload) != `{"worker":"local"}` {
			t.Fatalf("修改输入 Payload 污染了 Store：%q", events[0].Payload)
		}

		events[0].Payload[0] = '!'
		unmodified, err := subject.ListTaskEvents(ctx, run.ID, 0)
		if err != nil {
			t.Fatalf("修改返回值后 ListTaskEvents() error = %v", err)
		}
		if string(unmodified[0].Payload) != `{"worker":"local"}` {
			t.Fatalf("修改返回 Payload 污染了 Store：%q", unmodified[0].Payload)
		}

		again, err := subject.ListTaskEvents(ctx, run.ID, 1)
		if err != nil {
			t.Fatalf("带游标 ListTaskEvents() error = %v", err)
		}
		if len(again) != 1 || again[0].Sequence != 2 {
			t.Fatalf("带游标 ListTaskEvents() = %#v, want only sequence 2", again)
		}
		if _, err := subject.ListTaskEvents(ctx, "run-missing", 0); !errors.Is(err, store.ErrNotFound) {
			t.Fatalf("不存在 TaskRun 的 ListTaskEvents() error = %v, want ErrNotFound", err)
		}

		invalidBatch := []domain.TaskEvent{
			{TaskRunID: run.ID, Sequence: 3, Type: "task.output", Payload: []byte("one"), CreatedAt: createdAt},
			{TaskRunID: run.ID, Sequence: 5, Type: "task.output", Payload: []byte("two"), CreatedAt: createdAt},
		}
		if _, err := subject.AppendTaskEvents(ctx, invalidBatch); !errors.Is(err, store.ErrInvalidEventSequence) {
			t.Fatalf("序号断裂 AppendTaskEvents() error = %v, want ErrInvalidEventSequence", err)
		}
		afterFailure, err := subject.ListTaskEvents(ctx, run.ID, 0)
		if err != nil {
			t.Fatalf("失败批次后 ListTaskEvents() error = %v", err)
		}
		if len(afterFailure) != 2 {
			t.Fatalf("失败批次发生部分写入：len = %d, want 2", len(afterFailure))
		}

		third := domain.TaskEvent{
			TaskRunID: run.ID,
			Sequence:  3,
			Type:      "task.output",
			Payload:   []byte("three"),
			CreatedAt: createdAt.Add(3 * time.Second),
		}
		lastID, err = subject.AppendTaskEvents(ctx, []domain.TaskEvent{third})
		if err != nil {
			t.Fatalf("失败批次后的合法 AppendTaskEvents() error = %v", err)
		}
		if lastID != 3 {
			t.Fatalf("失败批次消耗了全局 ID：lastID = %d, want 3", lastID)
		}
	})

	t.Run("已取消的 context 不允许读取或写入", func(t *testing.T) {
		subject := memory.New()
		cancelled, cancel := context.WithCancel(context.Background())
		cancel()

		requirement := requirementFixture()
		if err := subject.CreateRequirement(cancelled, requirement); !errors.Is(err, context.Canceled) {
			t.Fatalf("CreateRequirement(cancelled) error = %v, want context.Canceled", err)
		}
		if _, err := subject.GetRequirement(cancelled, requirement.ID); !errors.Is(err, context.Canceled) {
			t.Fatalf("GetRequirement(cancelled) error = %v, want context.Canceled", err)
		}
		if _, err := subject.SaveRequirement(cancelled, requirement, requirement.Revision); !errors.Is(err, context.Canceled) {
			t.Fatalf("SaveRequirement(cancelled) error = %v, want context.Canceled", err)
		}

		run := queuedTaskFixture(t, "run-cancelled", testTime())
		if err := subject.EnqueueTask(cancelled, run); !errors.Is(err, context.Canceled) {
			t.Fatalf("EnqueueTask(cancelled) error = %v, want context.Canceled", err)
		}
		if _, err := subject.GetTask(cancelled, run.ID); !errors.Is(err, context.Canceled) {
			t.Fatalf("GetTask(cancelled) error = %v, want context.Canceled", err)
		}
		if _, err := subject.ClaimNextTask(cancelled, testTime()); !errors.Is(err, context.Canceled) {
			t.Fatalf("ClaimNextTask(cancelled) error = %v, want context.Canceled", err)
		}
		if err := subject.SaveTask(cancelled, run, run.Status); !errors.Is(err, context.Canceled) {
			t.Fatalf("SaveTask(cancelled) error = %v, want context.Canceled", err)
		}
		if _, err := subject.AppendTaskEvents(cancelled, []domain.TaskEvent{{TaskRunID: run.ID, Sequence: 1}}); !errors.Is(err, context.Canceled) {
			t.Fatalf("AppendTaskEvents(cancelled) error = %v, want context.Canceled", err)
		}
		if _, err := subject.ListTaskEvents(cancelled, run.ID, 0); !errors.Is(err, context.Canceled) {
			t.Fatalf("ListTaskEvents(cancelled) error = %v, want context.Canceled", err)
		}
	})
}

func requirementFixture() domain.Requirement {
	return domain.Requirement{
		ID:        "req-1",
		ProjectID: "project-1",
		Title:     "实现内存 Store",
		Stage:     domain.RequirementStageGrilling,
		Revision:  1,
	}
}

func queuedTaskFixture(t *testing.T, id string, now time.Time) domain.TaskRun {
	t.Helper()

	run, err := domain.NewTaskRun(
		domain.NewTaskRunInput{
			ID:            id,
			RequirementID: "req-1",
			TicketID:      "ticket-1",
			Attempt:       1,
		},
		now,
	)
	if err != nil {
		t.Fatalf("NewTaskRun() error = %v", err)
	}
	return run
}

func completedTaskFixture(t *testing.T, id string, now time.Time) domain.TaskRun {
	t.Helper()

	run := queuedTaskFixture(t, id, now)
	claimed, err := run.Claim(now.Add(time.Second))
	if err != nil {
		t.Fatalf("Claim() error = %v", err)
	}
	running, err := claimed.Start(now.Add(2 * time.Second))
	if err != nil {
		t.Fatalf("Start() error = %v", err)
	}
	completed, err := running.Complete(
		domain.TaskResult{Summary: "执行完成"},
		now.Add(3*time.Second),
	)
	if err != nil {
		t.Fatalf("Complete() error = %v", err)
	}
	return completed
}

func testTime() time.Time {
	return time.Date(2026, time.October, 4, 10, 0, 0, 0, time.UTC)
}
