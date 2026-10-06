package store_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"product-agent-workbench/internal/domain"
	"product-agent-workbench/internal/store"
)

// runTaskEventStoreContract 验证事件批量追加、游标读取、原子性和深拷贝。
//
// Memory 与 SQLite 必须通过同一组行为契约。这样测试关注的是 Store 对调用方的承诺，
// 而不是某个 Adapter 内部使用 map 还是 SQL。
func runTaskEventStoreContract(t *testing.T, factory storeFactory) {
	t.Helper()

	t.Run("事件批量追加连续、原子且使用深拷贝", func(t *testing.T) {
		subject := factory(t)
		ctx := context.Background()
		createRequirementForTask(t, ctx, subject, "req-1")
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
		none, err := subject.ListTaskEvents(ctx, run.ID, 2)
		if err != nil {
			t.Fatalf("无新事件 ListTaskEvents() error = %v", err)
		}
		if none == nil || len(none) != 0 {
			t.Fatalf("无新事件 ListTaskEvents() = %#v, want non-nil empty slice", none)
		}
		if _, err := subject.ListTaskEvents(ctx, "run-missing", 0); !errors.Is(err, store.ErrNotFound) {
			t.Fatalf("不存在 TaskRun 的 ListTaskEvents() error = %v, want ErrNotFound", err)
		}
		if emptyLastID, err := subject.AppendTaskEvents(ctx, nil); err != nil || emptyLastID != 2 {
			t.Fatalf("空批次 AppendTaskEvents() = (%d, %v), want (2, nil)", emptyLastID, err)
		}

		mixedTaskRun := []domain.TaskEvent{
			{TaskRunID: run.ID, Sequence: 3, Type: "task.output", CreatedAt: createdAt},
			{TaskRunID: "run-other", Sequence: 4, Type: "task.output", CreatedAt: createdAt},
		}
		if _, err := subject.AppendTaskEvents(ctx, mixedTaskRun); !errors.Is(err, store.ErrInvalidEventSequence) {
			t.Fatalf("混合 TaskRun AppendTaskEvents() error = %v, want ErrInvalidEventSequence", err)
		}
		blankType := domain.TaskEvent{TaskRunID: run.ID, Sequence: 3, Type: "  ", CreatedAt: createdAt}
		if _, err := subject.AppendTaskEvents(ctx, []domain.TaskEvent{blankType}); !errors.Is(err, domain.ErrInvalidInput) {
			t.Fatalf("空白事件类型 AppendTaskEvents() error = %v, want ErrInvalidInput", err)
		}
		zeroCreatedAt := domain.TaskEvent{TaskRunID: run.ID, Sequence: 3, Type: "task.output"}
		if _, err := subject.AppendTaskEvents(ctx, []domain.TaskEvent{zeroCreatedAt}); !errors.Is(err, domain.ErrInvalidInput) {
			t.Fatalf("零创建时间 AppendTaskEvents() error = %v, want ErrInvalidInput", err)
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

		// 没有附加数据的事件仍然合法；SQLite Adapter 需将 nil 规范化为空 BLOB，
		// 不能因为数据库的 payload NOT NULL 约束拒绝它。
		withoutPayload := domain.TaskEvent{
			TaskRunID: run.ID,
			Sequence:  4,
			Type:      "task.heartbeat",
			CreatedAt: createdAt.Add(4 * time.Second),
		}
		if _, err := subject.AppendTaskEvents(ctx, []domain.TaskEvent{withoutPayload}); err != nil {
			t.Fatalf("无 Payload AppendTaskEvents() error = %v", err)
		}
	})
}
