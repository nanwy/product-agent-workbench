package store_test

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"product-agent-workbench/internal/domain"
	"product-agent-workbench/internal/store"
)

// runClaimStoreContract 验证领取顺序、空队列语义和原子性。
//
// 测试中的两条活动 TaskRun 分属不同 Requirement，因为 migration 明确禁止
// 同一 Requirement 同时存在两条 queued/claimed/running 记录。
func runClaimStoreContract(t *testing.T, factory storeFactory) {
	t.Helper()

	t.Run("claim 遵守入队顺序且不会重复领取", func(t *testing.T) {
		subject := factory(t)
		ctx := context.Background()
		createRequirementForTask(t, ctx, subject, "req-1")
		createRequirementForTask(t, ctx, subject, "req-2")

		createdAt := testTime()
		first := queuedTaskFixtureFor(t, "run-first", "req-1", "ticket-1", 1, createdAt)
		second := queuedTaskFixtureFor(t, "run-second", "req-2", "ticket-2", 1, createdAt)
		if err := subject.EnqueueTask(ctx, first); err != nil {
			t.Fatalf("EnqueueTask(first) error = %v", err)
		}
		if err := subject.EnqueueTask(ctx, second); err != nil {
			t.Fatalf("EnqueueTask(second) error = %v", err)
		}

		claimedAt := createdAt.Add(2 * time.Second)
		claimed, err := subject.ClaimNextTask(ctx, claimedAt)
		if err != nil {
			t.Fatalf("ClaimNextTask() error = %v", err)
		}
		if claimed.ID != first.ID || claimed.Status != domain.TaskStatusClaimed {
			t.Fatalf("ClaimNextTask() = %#v, want first task claimed", claimed)
		}
		if claimed.ClaimedAt == nil || !claimed.ClaimedAt.Equal(claimedAt) {
			t.Fatalf("ClaimNextTask() ClaimedAt = %v, want %v", claimed.ClaimedAt, claimedAt)
		}

		claimed, err = subject.ClaimNextTask(ctx, claimedAt.Add(time.Second))
		if err != nil {
			t.Fatalf("第二次 ClaimNextTask() error = %v", err)
		}
		if claimed.ID != second.ID {
			t.Fatalf("第二次 ClaimNextTask() ID = %q, want %q", claimed.ID, second.ID)
		}
		if _, err := subject.ClaimNextTask(ctx, claimedAt.Add(2*time.Second)); !errors.Is(err, store.ErrNotFound) {
			t.Fatalf("队列为空 ClaimNextTask() error = %v, want ErrNotFound", err)
		}
	})

	t.Run("两个 goroutine 不能领取同一任务", func(t *testing.T) {
		subject := factory(t)
		ctx := context.Background()
		createRequirementForTask(t, ctx, subject, "req-1")

		createdAt := testTime()
		run := queuedTaskFixtureFor(t, "run-race", "req-1", "ticket-1", 1, createdAt)
		if err := subject.EnqueueTask(ctx, run); err != nil {
			t.Fatalf("EnqueueTask() error = %v", err)
		}

		start := make(chan struct{})
		results := make(chan error, 2)
		var wait sync.WaitGroup
		for range 2 {
			wait.Add(1)
			go func() {
				defer wait.Done()
				<-start
				_, err := subject.ClaimNextTask(ctx, createdAt.Add(time.Second))
				results <- err
			}()
		}
		close(start)
		wait.Wait()
		close(results)

		var succeeded int
		var notFound int
		for err := range results {
			switch {
			case err == nil:
				succeeded++
			case errors.Is(err, store.ErrNotFound):
				notFound++
			default:
				t.Fatalf("并发 ClaimNextTask() 返回意外错误：%v", err)
			}
		}
		if succeeded != 1 || notFound != 1 {
			t.Fatalf("并发领取结果 success=%d notFound=%d, want 1/1", succeeded, notFound)
		}
	})

	t.Run("claim 遵守 context 取消", func(t *testing.T) {
		subject := factory(t)
		cancelled, cancel := context.WithCancel(context.Background())
		cancel()

		if _, err := subject.ClaimNextTask(cancelled, testTime()); !errors.Is(err, context.Canceled) {
			t.Fatalf("ClaimNextTask(cancelled) error = %v, want context.Canceled", err)
		}
	})
}

func queuedTaskFixtureFor(
	t *testing.T,
	id string,
	requirementID string,
	ticketID string,
	attempt int,
	now time.Time,
) domain.TaskRun {
	t.Helper()

	run, err := domain.NewTaskRun(
		domain.NewTaskRunInput{
			ID:            id,
			RequirementID: requirementID,
			TicketID:      ticketID,
			Attempt:       attempt,
		},
		now,
	)
	if err != nil {
		t.Fatalf("NewTaskRun() error = %v", err)
	}
	return run
}
