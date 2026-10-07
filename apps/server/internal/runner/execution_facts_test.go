package runner_test

import (
	"context"
	"testing"

	"product-agent-workbench/internal/domain"
	"product-agent-workbench/internal/runner"
)

func TestResumableExecutionFactsReaderUsesLatestEarlierAttemptThread(t *testing.T) {
	current := domain.TaskRun{
		ID: "run-3", RequirementID: "req-1", TicketVersionID: "version-1", TicketID: "ticket-1", Attempt: 3,
	}
	artifacts := resumableFactsReaderFunc(func(context.Context, domain.TaskRun) (runner.ExecutionFacts, error) {
		return runner.ExecutionFacts{
			RequirementID: "req-1", TicketVersionID: "version-1", TicketID: "ticket-1",
		}, nil
	})
	history := taskHistoryReaderFunc(func(context.Context, string, string) ([]domain.TaskRun, error) {
		return []domain.TaskRun{
			{TicketID: "ticket-1", Attempt: 1, CodexThreadID: "thread-old"},
			{TicketID: "another-ticket", Attempt: 9, CodexThreadID: "thread-other"},
			{TicketID: "ticket-1", Attempt: 2, CodexThreadID: "thread-latest"},
			current,
		}, nil
	})

	reader, err := runner.NewResumableExecutionFactsReader(artifacts, history)
	if err != nil {
		t.Fatalf("NewResumableExecutionFactsReader() error = %v", err)
	}
	facts, err := reader.ReadExecutionFacts(context.Background(), current)
	if err != nil {
		t.Fatalf("ReadExecutionFacts() error = %v", err)
	}
	if facts.CodexThreadID != "thread-latest" {
		t.Fatalf("CodexThreadID = %q, want thread-latest", facts.CodexThreadID)
	}
}

type taskHistoryReaderFunc func(context.Context, string, string) ([]domain.TaskRun, error)

type resumableFactsReaderFunc func(context.Context, domain.TaskRun) (runner.ExecutionFacts, error)

func (fn resumableFactsReaderFunc) ReadExecutionFacts(
	ctx context.Context,
	run domain.TaskRun,
) (runner.ExecutionFacts, error) {
	return fn(ctx, run)
}

func (fn taskHistoryReaderFunc) ListTasksByTicketVersion(
	ctx context.Context,
	requirementID string,
	ticketVersionID string,
) ([]domain.TaskRun, error) {
	return fn(ctx, requirementID, ticketVersionID)
}
