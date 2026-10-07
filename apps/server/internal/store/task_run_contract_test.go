package store_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"product-agent-workbench/internal/domain"
	"product-agent-workbench/internal/store"
)

// runTaskRunStoreContract 验证 TaskRun 的入队、行还原和状态 CAS。
//
// 这一组暂时不包含 ClaimNextTask：claim 需要额外的事务和双连接并发测试，
// 会在基础 TaskRun 读写稳定后单独解锁。
func runTaskRunStoreContract(t *testing.T, factory storeFactory) {
	t.Helper()

	t.Run("只按入队顺序列出 claimed 和 running", func(t *testing.T) {
		subject := factory(t)
		ctx := context.Background()
		createRequirementForTask(t, ctx, subject, "req-1")
		createdAt := testTime()

		queued := queuedTaskFixtureFor(t, "run-queued", "req-1", "ticket-queued", 1, createdAt)
		claimedBase := queuedTaskFixtureFor(t, "run-claimed", "req-1", "ticket-claimed", 1, createdAt.Add(time.Second))
		runningBase := queuedTaskFixtureFor(t, "run-running", "req-1", "ticket-running", 1, createdAt.Add(2*time.Second))
		terminalBase := queuedTaskFixtureFor(t, "run-terminal", "req-1", "ticket-terminal", 1, createdAt.Add(3*time.Second))
		for _, run := range []domain.TaskRun{queued, claimedBase, runningBase, terminalBase} {
			if err := subject.EnqueueTask(ctx, run); err != nil {
				t.Fatalf("EnqueueTask(%s) error = %v", run.ID, err)
			}
		}

		claimed, err := claimedBase.Claim(createdAt.Add(4 * time.Second))
		if err != nil {
			t.Fatalf("Claim(claimed) fixture error = %v", err)
		}
		if err := subject.SaveTask(ctx, claimed, domain.TaskStatusQueued); err != nil {
			t.Fatalf("SaveTask(claimed) error = %v", err)
		}

		runningClaimed, err := runningBase.Claim(createdAt.Add(5 * time.Second))
		if err != nil {
			t.Fatalf("Claim(running) fixture error = %v", err)
		}
		running, err := runningClaimed.Start(createdAt.Add(6 * time.Second))
		if err != nil {
			t.Fatalf("Start(running) fixture error = %v", err)
		}
		running, err = running.AttachCodexThread("thread-running")
		if err != nil {
			t.Fatalf("AttachCodexThread() fixture error = %v", err)
		}
		if err := subject.SaveTask(ctx, running, domain.TaskStatusQueued); err != nil {
			t.Fatalf("SaveTask(running) error = %v", err)
		}

		terminalClaimed, err := terminalBase.Claim(createdAt.Add(7 * time.Second))
		if err != nil {
			t.Fatalf("Claim(terminal) fixture error = %v", err)
		}
		terminal, err := terminalClaimed.Fail("fixture 失败", createdAt.Add(8*time.Second))
		if err != nil {
			t.Fatalf("Fail(terminal) fixture error = %v", err)
		}
		if err := subject.SaveTask(ctx, terminal, domain.TaskStatusQueued); err != nil {
			t.Fatalf("SaveTask(terminal) error = %v", err)
		}

		got, err := subject.ListInFlightTasks(ctx)
		if err != nil {
			t.Fatalf("ListInFlightTasks() error = %v", err)
		}
		if len(got) != 2 || got[0].ID != claimed.ID || got[1].ID != running.ID {
			t.Fatalf("ListInFlightTasks() = %#v, want claimed then running", got)
		}
		if got[1].CodexThreadID != "thread-running" || got[1].Result != nil {
			t.Fatalf("running session round trip = %#v, want thread without terminal result", got[1])
		}
		*got[0].ClaimedAt = got[0].ClaimedAt.Add(time.Hour)
		again, err := subject.GetTask(ctx, claimed.ID)
		if err != nil {
			t.Fatalf("GetTask(claimed) error = %v", err)
		}
		if again.ClaimedAt.Equal(*got[0].ClaimedAt) {
			t.Fatal("修改 ListInFlightTasks 返回值污染了 Store 内部状态")
		}

		cancelled, cancel := context.WithCancel(ctx)
		cancel()
		if _, err := subject.ListInFlightTasks(cancelled); !errors.Is(err, context.Canceled) {
			t.Fatalf("ListInFlightTasks(cancelled) error = %v, want context.Canceled", err)
		}
	})

	t.Run("同一 Requirement 的不同 Ticket 可同时入队，同一 Ticket 只能有一个活动 attempt", func(t *testing.T) {
		subject := factory(t)
		ctx := context.Background()
		createRequirementForTask(t, ctx, subject, "req-1")
		createdAt := testTime()

		first := queuedTaskFixtureFor(t, "run-a-1", "req-1", "ticket-a", 1, createdAt)
		parallel := queuedTaskFixtureFor(t, "run-b-1", "req-1", "ticket-b", 1, createdAt)
		if err := subject.EnqueueTask(ctx, first); err != nil {
			t.Fatalf("EnqueueTask(ticket-a) error = %v", err)
		}
		if err := subject.EnqueueTask(ctx, parallel); err != nil {
			t.Fatalf("同一 Requirement 的不同 Ticket 入队 error = %v", err)
		}

		overlappingRetry := queuedTaskFixtureFor(t, "run-a-2", "req-1", "ticket-a", 2, createdAt.Add(time.Second))
		if err := subject.EnqueueTask(ctx, overlappingRetry); !errors.Is(err, store.ErrAlreadyExists) {
			t.Fatalf("同一 Ticket 的第二条活动 attempt error = %v, want ErrAlreadyExists", err)
		}

		claimed, err := first.Claim(createdAt.Add(time.Second))
		if err != nil {
			t.Fatalf("Claim() fixture error = %v", err)
		}
		running, err := claimed.Start(createdAt.Add(2 * time.Second))
		if err != nil {
			t.Fatalf("Start() fixture error = %v", err)
		}
		completed, err := running.Complete(domain.TaskResult{Summary: "ticket-a 完成"}, createdAt.Add(3*time.Second))
		if err != nil {
			t.Fatalf("Complete() fixture error = %v", err)
		}
		if err := subject.SaveTask(ctx, completed, domain.TaskStatusQueued); err != nil {
			t.Fatalf("SaveTask(completed) error = %v", err)
		}
		if err := subject.EnqueueTask(ctx, overlappingRetry); err != nil {
			t.Fatalf("前一 attempt 终止后重试入队 error = %v", err)
		}

		duplicateAttempt := queuedTaskFixtureFor(t, "run-a-2-duplicate", "req-1", "ticket-a", 2, createdAt.Add(2*time.Second))
		if err := subject.EnqueueTask(ctx, duplicateAttempt); !errors.Is(err, store.ErrAlreadyExists) {
			t.Fatalf("重复 attempt error = %v, want ErrAlreadyExists", err)
		}
	})

	t.Run("TaskRun 入队、读取与保存使用深拷贝", func(t *testing.T) {
		subject := factory(t)
		ctx := context.Background()
		createRequirementForTask(t, ctx, subject, "req-1")
		run := completedTaskFixture(t, "run-copy", testTime())

		queued, err := domain.NewTaskRun(
			domain.NewTaskRunInput{
				ID:              run.ID,
				RequirementID:   run.RequirementID,
				TicketVersionID: run.TicketVersionID,
				TicketID:        run.TicketID,
				Attempt:         run.Attempt,
			},
			run.CreatedAt,
		)
		if err != nil {
			t.Fatalf("NewTaskRun() error = %v", err)
		}
		if err := subject.EnqueueTask(ctx, queued); err != nil {
			t.Fatalf("EnqueueTask() error = %v", err)
		}
		if err := subject.EnqueueTask(ctx, queued); !errors.Is(err, store.ErrAlreadyExists) {
			t.Fatalf("重复 EnqueueTask() error = %v, want ErrAlreadyExists", err)
		}
		invalid := run
		invalid.ID = "run-not-queued"
		if err := subject.EnqueueTask(ctx, invalid); !errors.Is(err, domain.ErrInvalidTransition) {
			t.Fatalf("非 queued EnqueueTask() error = %v, want ErrInvalidTransition", err)
		}
		if err := subject.SaveTask(ctx, run, domain.TaskStatusQueued); err != nil {
			t.Fatalf("SaveTask() error = %v", err)
		}

		got, err := subject.GetTask(ctx, run.ID)
		if err != nil {
			t.Fatalf("GetTask() error = %v", err)
		}
		if got.Status != domain.TaskStatusCompleted || got.Result == nil {
			t.Fatalf("GetTask() = %#v, want completed result", got)
		}

		*got.ClaimedAt = got.ClaimedAt.Add(24 * time.Hour)
		got.Result.Summary = "调用方篡改"

		again, err := subject.GetTask(ctx, run.ID)
		if err != nil {
			t.Fatalf("再次 GetTask() error = %v", err)
		}
		if !again.ClaimedAt.Equal(*run.ClaimedAt) || again.Result.Summary != run.Result.Summary {
			t.Fatalf("修改返回值污染了 Store 内部状态：got %#v", again)
		}

		missing := queued
		missing.ID = "run-missing"
		if _, err := subject.GetTask(ctx, missing.ID); !errors.Is(err, store.ErrNotFound) {
			t.Fatalf("读取不存在 TaskRun error = %v, want ErrNotFound", err)
		}
		if err := subject.SaveTask(ctx, missing, domain.TaskStatusQueued); !errors.Is(err, store.ErrNotFound) {
			t.Fatalf("保存不存在 TaskRun error = %v, want ErrNotFound", err)
		}
	})

	t.Run("TaskRun 状态 CAS 阻止旧快照覆盖并发终态", func(t *testing.T) {
		subject := factory(t)
		ctx := context.Background()
		createRequirementForTask(t, ctx, subject, "req-1")
		createdAt := testTime()
		queued := queuedTaskFixture(t, "run-cas", createdAt)
		if err := subject.EnqueueTask(ctx, queued); err != nil {
			t.Fatalf("EnqueueTask() error = %v", err)
		}
		claimed, err := queued.Claim(createdAt.Add(time.Minute))
		if err != nil {
			t.Fatalf("Claim() fixture error = %v", err)
		}
		if err := subject.SaveTask(ctx, claimed, domain.TaskStatusQueued); err != nil {
			t.Fatalf("SaveTask(claimed) error = %v", err)
		}
		running, err := claimed.Start(createdAt.Add(2 * time.Minute))
		if err != nil {
			t.Fatalf("Start() fixture error = %v", err)
		}
		if err := subject.SaveTask(ctx, running, domain.TaskStatusClaimed); err != nil {
			t.Fatalf("SaveTask(running) error = %v", err)
		}

		// 模拟两个请求同时读取同一个 running 快照，各自在 Store 之外完成合法迁移。
		failed, err := running.Fail("执行失败", running.StartedAt.Add(time.Minute))
		if err != nil {
			t.Fatalf("Fail() fixture error = %v", err)
		}
		cancelled, err := running.Cancel(running.StartedAt.Add(time.Minute))
		if err != nil {
			t.Fatalf("Cancel() fixture error = %v", err)
		}

		if err := subject.SaveTask(ctx, cancelled, domain.TaskStatusRunning); err != nil {
			t.Fatalf("SaveTask(cancelled) error = %v", err)
		}
		if err := subject.SaveTask(ctx, failed, domain.TaskStatusRunning); !errors.Is(err, store.ErrTaskConflict) {
			t.Fatalf("旧 failed 快照 SaveTask() error = %v, want ErrTaskConflict", err)
		}

		persisted, err := subject.GetTask(ctx, running.ID)
		if err != nil {
			t.Fatalf("GetTask() error = %v", err)
		}
		if persisted.Status != domain.TaskStatusCancelled {
			t.Fatalf("并发旧快照覆盖了取消终态：status = %q, want cancelled", persisted.Status)
		}
	})
}

func createRequirementForTask(t *testing.T, ctx context.Context, subject store.Store, id string) {
	t.Helper()

	requirement := requirementFixture()
	requirement.ID = id
	if err := subject.CreateRequirement(ctx, requirement); err != nil {
		t.Fatalf("创建 TaskRun 父 Requirement: %v", err)
	}
}
