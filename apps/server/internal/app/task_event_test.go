package app_test

import (
	"context"
	"testing"
	"time"

	"product-agent-workbench/internal/app"
	"product-agent-workbench/internal/domain"
	"product-agent-workbench/internal/store/memory"
)

func TestApplication_ReplayTaskEvents(t *testing.T) {
	storeAdapter := memory.New()
	createRequirementFixture(t, storeAdapter, "req-1")
	application := newTaskRunApplication(t, storeAdapter, func() string { return "unused" }, time.Now)
	createdAt := taskRunTestTime

	run, err := domain.NewTaskRun(domain.NewTaskRunInput{
		ID:              "run-1",
		RequirementID:   "req-1",
		TicketVersionID: "req-1-tickets-v1",
		TicketID:        "ticket-1",
		Attempt:         1,
	}, createdAt)
	if err != nil {
		t.Fatalf("NewTaskRun() error = %v", err)
	}
	if err := storeAdapter.EnqueueTask(context.Background(), run); err != nil {
		t.Fatalf("EnqueueTask() error = %v", err)
	}
	if _, err := storeAdapter.AppendTaskEvents(context.Background(), []domain.TaskEvent{
		{TaskRunID: run.ID, Sequence: 1, Type: "task.started", CreatedAt: createdAt},
		{TaskRunID: run.ID, Sequence: 2, Type: "task.output", CreatedAt: createdAt.Add(time.Second)},
	}); err != nil {
		t.Fatalf("AppendTaskEvents() error = %v", err)
	}

	replay, err := application.ReplayTaskEvents(context.Background(), app.ReplayTaskEventsInput{
		AfterID: 1,
		Limit:   10,
	})
	if err != nil {
		t.Fatalf("ReplayTaskEvents() error = %v", err)
	}
	if replay.ResetRequired || replay.OldestID != 1 || replay.LatestID != 2 {
		t.Fatalf("ReplayTaskEvents() metadata = %#v", replay)
	}
	if len(replay.Events) != 1 || replay.Events[0].ID != 2 {
		t.Fatalf("ReplayTaskEvents() events = %#v, want event 2", replay.Events)
	}

	reset, err := application.ReplayTaskEvents(context.Background(), app.ReplayTaskEventsInput{
		AfterID: 99,
		Limit:   10,
	})
	if err != nil {
		t.Fatalf("ReplayTaskEvents(stale) error = %v", err)
	}
	if !reset.ResetRequired || len(reset.Events) != 0 || reset.LatestID != 2 {
		t.Fatalf("ReplayTaskEvents(stale) = %#v, want reset", reset)
	}
}
