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
	runTaskEventStoreContract(t, func(t *testing.T) store.Store {
		t.Helper()
		return memory.New()
	})
	runPlanningStoreContract(t, func(t *testing.T) store.Store {
		t.Helper()
		return memory.New()
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
			ID:              id,
			RequirementID:   "req-1",
			TicketVersionID: "req-1-tickets-v1",
			TicketID:        "ticket-1",
			Attempt:         1,
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
