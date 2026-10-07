package runner

import (
	"context"
	"errors"
	"reflect"
	"testing"

	"product-agent-workbench/internal/domain"
)

func TestNewStoreEventSink(t *testing.T) {
	if _, err := NewStoreEventSink(nil); !errors.Is(err, ErrInvalidDependencies) {
		t.Fatalf("NewStoreEventSink(nil) error = %v, want ErrInvalidDependencies", err)
	}

	wantEvent := domain.TaskEvent{TaskRunID: "run-1", Sequence: 2, Type: "codex.text.delta"}
	var gotEvents []domain.TaskEvent
	appender := taskEventAppenderFunc(func(_ context.Context, events []domain.TaskEvent) (int64, error) {
		gotEvents = append([]domain.TaskEvent(nil), events...)
		return 42, nil
	})
	sink, err := NewStoreEventSink(appender)
	if err != nil {
		t.Fatalf("NewStoreEventSink() error = %v", err)
	}
	if err := sink(context.Background(), wantEvent); err != nil {
		t.Fatalf("EventSink() error = %v", err)
	}
	if !reflect.DeepEqual(gotEvents, []domain.TaskEvent{wantEvent}) {
		t.Fatalf("AppendTaskEvents() events = %#v, want %#v", gotEvents, []domain.TaskEvent{wantEvent})
	}
}

func TestStoreEventSinkPropagatesCancellationAndStoreError(t *testing.T) {
	calls := 0
	storeErr := errors.New("disk unavailable")
	appender := taskEventAppenderFunc(func(_ context.Context, _ []domain.TaskEvent) (int64, error) {
		calls++
		return 0, storeErr
	})
	sink, err := NewStoreEventSink(appender)
	if err != nil {
		t.Fatalf("NewStoreEventSink() error = %v", err)
	}

	cancelled, cancel := context.WithCancel(context.Background())
	cancel()
	if err := sink(cancelled, domain.TaskEvent{}); !errors.Is(err, context.Canceled) {
		t.Fatalf("EventSink(cancelled) error = %v, want context.Canceled", err)
	}
	if calls != 0 {
		t.Fatalf("cancelled EventSink called Store %d times, want 0", calls)
	}

	if err := sink(context.Background(), domain.TaskEvent{TaskRunID: "run-1", Sequence: 1}); !errors.Is(err, storeErr) {
		t.Fatalf("EventSink(store error) = %v, want wrapped Store error", err)
	}
}

func TestNewTrustedRequestBuilder(t *testing.T) {
	if _, err := NewTrustedRequestBuilder(nil); !errors.Is(err, ErrInvalidDependencies) {
		t.Fatalf("NewTrustedRequestBuilder(nil) error = %v, want ErrInvalidDependencies", err)
	}

	run := runningTaskRunFixture()
	facts := executionFactsFixture(run)
	reader := executionFactsReaderFunc(func(_ context.Context, got domain.TaskRun) (ExecutionFacts, error) {
		if got != run {
			t.Fatalf("ReadExecutionFacts() run = %#v, want %#v", got, run)
		}
		return facts, nil
	})
	builder, err := NewTrustedRequestBuilder(reader)
	if err != nil {
		t.Fatalf("NewTrustedRequestBuilder() error = %v", err)
	}

	request, err := builder(context.Background(), run)
	if err != nil {
		t.Fatalf("RequestBuilder() error = %v", err)
	}
	if request.TaskRun != run || request.RepositoryRoot != facts.RepositoryRoot ||
		request.Prompt != facts.Prompt || request.CodexThreadID != facts.CodexThreadID {
		t.Fatalf("RequestBuilder() request = %#v, facts = %#v", request, facts)
	}
	if !reflect.DeepEqual(request.Workspace, facts.Workspace) {
		t.Fatalf("RequestBuilder() Workspace = %#v, want %#v", request.Workspace, facts.Workspace)
	}

	facts.Workspace.ModificationScopes[0] = "changed-by-reader"
	if request.Workspace.ModificationScopes[0] != "apps/web" {
		t.Fatal("RequestBuilder retained reader's ModificationScopes slice")
	}
}

func TestTrustedRequestBuilderRejectsInvalidOrMismatchedFacts(t *testing.T) {
	run := runningTaskRunFixture()
	tests := []struct {
		name   string
		run    domain.TaskRun
		mutate func(*ExecutionFacts)
		want   error
	}{
		{
			name: "not running",
			run: func() domain.TaskRun {
				invalid := run
				invalid.Status = domain.TaskStatusClaimed
				return invalid
			}(),
			want: ErrInvalidExecutionFacts,
		},
		{
			name: "blank task identity",
			run: func() domain.TaskRun {
				invalid := run
				invalid.TicketID = " "
				return invalid
			}(),
			want: ErrInvalidExecutionFacts,
		},
		{
			name: "mismatched ticket",
			run:  run,
			mutate: func(facts *ExecutionFacts) {
				facts.TicketID = "ticket-2"
			},
			want: ErrInvalidExecutionFacts,
		},
		{
			name: "invalid workspace",
			run:  run,
			mutate: func(facts *ExecutionFacts) {
				facts.RepositoryRoot = ""
			},
			want: ErrInvalidWorkspacePlan,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			facts := executionFactsFixture(run)
			if test.mutate != nil {
				test.mutate(&facts)
			}
			builder, err := NewTrustedRequestBuilder(executionFactsReaderFunc(
				func(context.Context, domain.TaskRun) (ExecutionFacts, error) { return facts, nil },
			))
			if err != nil {
				t.Fatalf("NewTrustedRequestBuilder() error = %v", err)
			}
			if _, err := builder(context.Background(), test.run); !errors.Is(err, test.want) {
				t.Fatalf("RequestBuilder() error = %v, want %v", err, test.want)
			}
		})
	}
}

func TestTrustedRequestBuilderPropagatesCancellationAndReaderError(t *testing.T) {
	run := runningTaskRunFixture()
	calls := 0
	readerErr := errors.New("artifact unavailable")
	builder, err := NewTrustedRequestBuilder(executionFactsReaderFunc(
		func(context.Context, domain.TaskRun) (ExecutionFacts, error) {
			calls++
			return ExecutionFacts{}, readerErr
		},
	))
	if err != nil {
		t.Fatalf("NewTrustedRequestBuilder() error = %v", err)
	}

	cancelled, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := builder(cancelled, run); !errors.Is(err, context.Canceled) {
		t.Fatalf("RequestBuilder(cancelled) error = %v, want context.Canceled", err)
	}
	if calls != 0 {
		t.Fatalf("cancelled RequestBuilder called reader %d times, want 0", calls)
	}

	if _, err := builder(context.Background(), run); !errors.Is(err, readerErr) {
		t.Fatalf("RequestBuilder(reader error) = %v, want wrapped reader error", err)
	}
}

type taskEventAppenderFunc func(context.Context, []domain.TaskEvent) (int64, error)

func (fn taskEventAppenderFunc) AppendTaskEvents(
	ctx context.Context,
	events []domain.TaskEvent,
) (int64, error) {
	return fn(ctx, events)
}

type executionFactsReaderFunc func(context.Context, domain.TaskRun) (ExecutionFacts, error)

func (fn executionFactsReaderFunc) ReadExecutionFacts(
	ctx context.Context,
	run domain.TaskRun,
) (ExecutionFacts, error) {
	return fn(ctx, run)
}

func runningTaskRunFixture() domain.TaskRun {
	return domain.TaskRun{
		ID:              "run-1",
		RequirementID:   "req-1",
		TicketVersionID: "tickets-v1",
		TicketID:        "ticket-1",
		Status:          domain.TaskStatusRunning,
	}
}

func executionFactsFixture(run domain.TaskRun) ExecutionFacts {
	return ExecutionFacts{
		RequirementID:   run.RequirementID,
		TicketVersionID: run.TicketVersionID,
		TicketID:        run.TicketID,
		RepositoryRoot:  "/tmp/product-agent-workbench-fixture",
		Prompt:          "实现 ticket-1",
		CodexThreadID:   "thread-1",
		Workspace: WorkspacePlan{
			BaseRevision:       "0123456789012345678901234567890123456789",
			TicketBranch:       "workbench/ticket-1",
			RequirementBranch:  "workbench/req-1",
			ModificationScopes: []string{"apps/web"},
		},
	}
}
