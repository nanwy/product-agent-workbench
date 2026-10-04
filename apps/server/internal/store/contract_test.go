package store_test

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"product-agent-workbench/internal/domain"
	"product-agent-workbench/internal/store"
	"product-agent-workbench/internal/store/memory"
)

// TestMemoryStoreContract 是内存 Adapter 的行为契约。
//
// 这些契约现在会全部执行。以后增加 SQLite Adapter 时，把下面的测试主体提取成接收
// factory 的辅助函数，让两个 Adapter
// 运行完全相同的契约，而不是复制一套测试。
func TestMemoryStoreContract(t *testing.T) {
	t.Run("Requirement 创建、读取与乐观锁更新", func(t *testing.T) {
		subject := memory.New()
		ctx := context.Background()
		original := requirementFixture()

		if err := subject.CreateRequirement(ctx, original); err != nil {
			t.Fatalf("CreateRequirement() error = %v", err)
		}
		if err := subject.CreateRequirement(ctx, original); !errors.Is(err, store.ErrAlreadyExists) {
			t.Fatalf("重复 CreateRequirement() error = %v, want ErrAlreadyExists", err)
		}

		got, err := subject.GetRequirement(ctx, original.ID)
		if err != nil {
			t.Fatalf("GetRequirement() error = %v", err)
		}
		if got != original {
			t.Fatalf("GetRequirement() = %#v, want %#v", got, original)
		}

		updated := original
		updated.Title = "新的标题"
		updated.Stage = domain.RequirementStageSpec
		updated.Revision = 999 // Store 必须根据 expectedRevision 计算版本，不能信任这个值。

		newRevision, err := subject.SaveRequirement(ctx, updated, original.Revision)
		if err != nil {
			t.Fatalf("SaveRequirement() error = %v", err)
		}
		if newRevision != original.Revision+1 {
			t.Fatalf("SaveRequirement() revision = %d, want %d", newRevision, original.Revision+1)
		}

		got, err = subject.GetRequirement(ctx, original.ID)
		if err != nil {
			t.Fatalf("更新后 GetRequirement() error = %v", err)
		}
		if got.Title != updated.Title || got.Stage != updated.Stage || got.Revision != newRevision {
			t.Fatalf("更新后 GetRequirement() = %#v", got)
		}

		stale := updated
		stale.Title = "过期请求不能覆盖的新标题"
		if _, err := subject.SaveRequirement(ctx, stale, original.Revision); !errors.Is(err, store.ErrRevisionConflict) {
			t.Fatalf("过期 SaveRequirement() error = %v, want ErrRevisionConflict", err)
		}

		got, err = subject.GetRequirement(ctx, original.ID)
		if err != nil {
			t.Fatalf("冲突后 GetRequirement() error = %v", err)
		}
		if got.Title != updated.Title || got.Revision != newRevision {
			t.Fatalf("版本冲突修改了已保存值：got %#v", got)
		}

		missing := original
		missing.ID = "req-missing"
		if _, err := subject.GetRequirement(ctx, missing.ID); !errors.Is(err, store.ErrNotFound) {
			t.Fatalf("读取不存在 Requirement error = %v, want ErrNotFound", err)
		}
		if _, err := subject.SaveRequirement(ctx, missing, 1); !errors.Is(err, store.ErrNotFound) {
			t.Fatalf("更新不存在 Requirement error = %v, want ErrNotFound", err)
		}
	})

	t.Run("Requirement 并发更新只允许一个成功", func(t *testing.T) {
		// 练习 2 已完成：SaveRequirement 的版本检查和写回位于同一写锁临界区。

		subject := memory.New()
		ctx := context.Background()
		original := requirementFixture()
		if err := subject.CreateRequirement(ctx, original); err != nil {
			t.Fatalf("CreateRequirement() error = %v", err)
		}

		start := make(chan struct{})
		errorsFromSave := make(chan error, 2)
		var wait sync.WaitGroup
		for index := range 2 {
			wait.Add(1)
			go func() {
				defer wait.Done()
				<-start

				candidate := original
				candidate.Title = "并发标题 " + string(rune('A'+index))
				_, err := subject.SaveRequirement(ctx, candidate, original.Revision)
				errorsFromSave <- err
			}()
		}
		close(start)
		wait.Wait()
		close(errorsFromSave)

		var succeeded int
		var conflicted int
		for err := range errorsFromSave {
			switch {
			case err == nil:
				succeeded++
			case errors.Is(err, store.ErrRevisionConflict):
				conflicted++
			default:
				t.Fatalf("并发 SaveRequirement() 返回意外错误：%v", err)
			}
		}
		if succeeded != 1 || conflicted != 1 {
			t.Fatalf("并发更新结果 success=%d conflict=%d, want 1/1", succeeded, conflicted)
		}
	})

	t.Run("TaskRun 入队、读取与保存使用深拷贝", func(t *testing.T) {
		subject := memory.New()
		ctx := context.Background()
		run := completedTaskFixture(t, "run-copy", testTime())

		queued, err := domain.NewTaskRun(
			domain.NewTaskRunInput{
				ID:            run.ID,
				RequirementID: run.RequirementID,
				TicketID:      run.TicketID,
				Attempt:       run.Attempt,
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
		if err := subject.SaveTask(ctx, run); err != nil {
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
		if err := subject.SaveTask(ctx, missing); !errors.Is(err, store.ErrNotFound) {
			t.Fatalf("保存不存在 TaskRun error = %v, want ErrNotFound", err)
		}
	})

	t.Run("claim 遵守入队顺序且不会重复领取", func(t *testing.T) {
		subject := memory.New()
		ctx := context.Background()
		createdAt := testTime()
		first := queuedTaskFixture(t, "run-first", createdAt)
		second := queuedTaskFixture(t, "run-second", createdAt.Add(time.Second))
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
		subject := memory.New()
		ctx := context.Background()
		createdAt := testTime()
		run := queuedTaskFixture(t, "run-race", createdAt)
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
		if err := subject.SaveTask(cancelled, run); !errors.Is(err, context.Canceled) {
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
