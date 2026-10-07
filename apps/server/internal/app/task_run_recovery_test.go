package app_test

import (
	"context"
	"strings"
	"testing"
	"time"

	"product-agent-workbench/internal/app"
	"product-agent-workbench/internal/domain"
	"product-agent-workbench/internal/store/memory"
)

func TestApplication_RecoverInterruptedTaskRuns(t *testing.T) {
	ctx := context.Background()
	storeAdapter := memory.New()
	recoveredAt := time.Date(2026, time.October, 8, 10, 0, 0, 0, time.UTC)
	application, err := app.New(app.Dependencies{
		Store: storeAdapter,
		NewID: func() string { return "unused" },
		Now:   func() time.Time { return recoveredAt },
	})
	if err != nil {
		t.Fatalf("app.New() error = %v", err)
	}
	if err := storeAdapter.CreateRequirement(ctx, domain.Requirement{
		ID: "req-1", ProjectID: "project-1", Title: "恢复任务", Stage: domain.RequirementStageImplementation, Revision: 1,
	}); err != nil {
		t.Fatalf("CreateRequirement() error = %v", err)
	}

	createdAt := recoveredAt.Add(-time.Hour)
	claimed := recoveryRunFixture(t, "run-claimed", "ticket-a", createdAt)
	claimed, err = claimed.Claim(createdAt.Add(time.Minute))
	if err != nil {
		t.Fatalf("Claim() error = %v", err)
	}
	running := recoveryRunFixture(t, "run-running", "ticket-b", createdAt.Add(time.Minute))
	running, err = running.Claim(createdAt.Add(2 * time.Minute))
	if err != nil {
		t.Fatalf("Claim(running) error = %v", err)
	}
	running, err = running.Start(createdAt.Add(3 * time.Minute))
	if err != nil {
		t.Fatalf("Start() error = %v", err)
	}
	running, err = running.AttachCodexThread("thread-1")
	if err != nil {
		t.Fatalf("AttachCodexThread() error = %v", err)
	}
	queued := recoveryRunFixture(t, "run-queued", "ticket-c", createdAt.Add(2*time.Minute))

	for _, run := range []domain.TaskRun{claimed, running, queued} {
		base := recoveryRunFixture(t, run.ID, run.TicketID, run.CreatedAt)
		if err := storeAdapter.EnqueueTask(ctx, base); err != nil {
			t.Fatalf("EnqueueTask(%s) error = %v", run.ID, err)
		}
		if run.Status != domain.TaskStatusQueued {
			if err := storeAdapter.SaveTask(ctx, run, domain.TaskStatusQueued); err != nil {
				t.Fatalf("SaveTask(%s) error = %v", run.ID, err)
			}
		}
	}

	recovered, err := application.RecoverInterruptedTaskRuns(ctx)
	if err != nil {
		t.Fatalf("RecoverInterruptedTaskRuns() error = %v", err)
	}
	if len(recovered) != 2 {
		t.Fatalf("recovered count = %d, want 2", len(recovered))
	}
	for _, run := range recovered {
		if run.Status != domain.TaskStatusFailed || run.FinishedAt == nil || !run.FinishedAt.Equal(recoveredAt) ||
			!strings.Contains(run.FailureReason, "服务进程") {
			t.Fatalf("recovered run = %#v, want failed restart fact", run)
		}
	}
	persistedRunning, err := storeAdapter.GetTask(ctx, running.ID)
	if err != nil {
		t.Fatalf("GetTask(running) error = %v", err)
	}
	if persistedRunning.CodexThreadID != "thread-1" {
		t.Fatalf("recovery lost CodexThreadID: %#v", persistedRunning)
	}
	persistedQueued, err := storeAdapter.GetTask(ctx, queued.ID)
	if err != nil || persistedQueued.Status != domain.TaskStatusQueued {
		t.Fatalf("queued task changed during recovery: run=%#v err=%v", persistedQueued, err)
	}

	again, err := application.RecoverInterruptedTaskRuns(ctx)
	if err != nil || len(again) != 0 {
		t.Fatalf("second recovery = %#v, %v; want empty idempotent result", again, err)
	}
	// failed 已经离开活动唯一索引，同一 Ticket 的新 attempt 可以显式入队。
	retry, err := domain.NewTaskRun(domain.NewTaskRunInput{
		ID: "run-retry", RequirementID: "req-1", TicketVersionID: "version-1", TicketID: "ticket-b", Attempt: 2,
	}, recoveredAt.Add(time.Minute))
	if err != nil {
		t.Fatalf("NewTaskRun(retry) error = %v", err)
	}
	if err := storeAdapter.EnqueueTask(ctx, retry); err != nil {
		t.Fatalf("EnqueueTask(retry) error = %v", err)
	}
}

func TestApplication_RecordTaskRunSession(t *testing.T) {
	ctx := context.Background()
	storeAdapter := memory.New()
	now := time.Date(2026, time.October, 8, 10, 0, 0, 0, time.UTC)
	application, err := app.New(app.Dependencies{
		Store: storeAdapter,
		NewID: func() string { return "unused" },
		Now:   func() time.Time { return now },
	})
	if err != nil {
		t.Fatalf("app.New() error = %v", err)
	}
	if err := storeAdapter.CreateRequirement(ctx, domain.Requirement{
		ID: "req-1", ProjectID: "project-1", Title: "保存会话", Stage: domain.RequirementStageImplementation, Revision: 1,
	}); err != nil {
		t.Fatalf("CreateRequirement() error = %v", err)
	}
	base := recoveryRunFixture(t, "run-1", "ticket-1", now.Add(-time.Hour))
	if err := storeAdapter.EnqueueTask(ctx, base); err != nil {
		t.Fatalf("EnqueueTask() error = %v", err)
	}
	claimed, err := base.Claim(now.Add(-2 * time.Minute))
	if err != nil {
		t.Fatalf("Claim() error = %v", err)
	}
	running, err := claimed.Start(now.Add(-time.Minute))
	if err != nil {
		t.Fatalf("Start() error = %v", err)
	}
	if err := storeAdapter.SaveTask(ctx, running, domain.TaskStatusQueued); err != nil {
		t.Fatalf("SaveTask(running) error = %v", err)
	}

	got, err := application.RecordTaskRunSession(ctx, app.RecordTaskRunSessionInput{
		ID: " run-1 ", CodexThreadID: " thread-1 ",
	})
	if err != nil {
		t.Fatalf("RecordTaskRunSession() error = %v", err)
	}
	if got.CodexThreadID != "thread-1" || got.Status != domain.TaskStatusRunning {
		t.Fatalf("RecordTaskRunSession() = %#v", got)
	}
	persisted, err := storeAdapter.GetTask(ctx, "run-1")
	if err != nil || persisted.CodexThreadID != "thread-1" {
		t.Fatalf("persisted session = %#v, %v", persisted, err)
	}
}

func recoveryRunFixture(t *testing.T, id, ticketID string, now time.Time) domain.TaskRun {
	t.Helper()
	run, err := domain.NewTaskRun(domain.NewTaskRunInput{
		ID: id, RequirementID: "req-1", TicketVersionID: "version-1", TicketID: ticketID, Attempt: 1,
	}, now)
	if err != nil {
		t.Fatalf("NewTaskRun() error = %v", err)
	}
	return run
}
