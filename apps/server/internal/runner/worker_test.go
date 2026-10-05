package runner_test

import (
	"context"
	"testing"
	"time"

	"product-agent-workbench/internal/app"
	"product-agent-workbench/internal/domain"
	"product-agent-workbench/internal/runner"
	"product-agent-workbench/internal/store/memory"
)

func TestWorker_RunNextCompletesTaskWithFakeExecutor(t *testing.T) {
	t.Skip("练习 13：完成 fail 用例后，实现同步 Worker.RunNext 并删除本行")

	storeAdapter := memory.New()
	clock := sequenceClock(t,
		time.Date(2026, time.October, 5, 10, 0, 0, 0, time.UTC),
		time.Date(2026, time.October, 5, 10, 1, 0, 0, time.UTC),
		time.Date(2026, time.October, 5, 10, 2, 0, 0, time.UTC),
		time.Date(2026, time.October, 5, 10, 3, 0, 0, time.UTC),
	)
	application, err := app.New(app.Dependencies{
		Store: storeAdapter,
		NewID: func() string { return "run-1" },
		Now:   clock,
	})
	if err != nil {
		t.Fatalf("app.New() error = %v", err)
	}
	requirement := domain.Requirement{ID: "req-1", ProjectID: "project-1", Title: "Worker 练习", Stage: domain.RequirementStageImplementation, Revision: 1}
	if err := storeAdapter.CreateRequirement(context.Background(), requirement); err != nil {
		t.Fatalf("CreateRequirement() fixture error = %v", err)
	}
	if _, err := application.EnqueueTaskRun(context.Background(), app.EnqueueTaskRunInput{RequirementID: requirement.ID, TicketID: "ticket-1", Attempt: 1}); err != nil {
		t.Fatalf("EnqueueTaskRun() fixture error = %v", err)
	}

	executor := &fakeExecutor{result: runner.RunResult{Result: domain.TaskResult{Summary: "fake 执行完成", CodexThreadID: "thread-1"}}}
	worker, err := runner.NewWorker(runner.WorkerDependencies{
		Application: application,
		Executor:    executor,
		BuildRequest: func(_ context.Context, run domain.TaskRun) (runner.RunRequest, error) {
			return runner.RunRequest{TaskRun: run, RepositoryRoot: "/tmp/fake-repo", Prompt: "实现 ticket-1"}, nil
		},
		EventSink: func(context.Context, domain.TaskEvent) error { return nil },
	})
	if err != nil {
		t.Fatalf("NewWorker() error = %v", err)
	}

	got, err := worker.RunNext(context.Background())
	if err != nil {
		t.Fatalf("RunNext() error = %v", err)
	}
	if got.Status != domain.TaskStatusCompleted || got.Result == nil || got.Result.Summary != "fake 执行完成" {
		t.Fatalf("RunNext() = %#v, want completed fake result", got)
	}
	if executor.calls != 1 || executor.request.TaskRun.Status != domain.TaskStatusRunning {
		t.Fatalf("Executor calls=%d request=%#v, want one running request", executor.calls, executor.request)
	}
}

type fakeExecutor struct {
	result  runner.RunResult
	err     error
	calls   int
	request runner.RunRequest
}

func (fake *fakeExecutor) Run(
	_ context.Context,
	request runner.RunRequest,
	_ runner.EventSink,
) (runner.RunResult, error) {
	fake.calls++
	fake.request = request
	return fake.result, fake.err
}

func sequenceClock(t *testing.T, values ...time.Time) app.Clock {
	t.Helper()
	index := 0
	return func() time.Time {
		if index >= len(values) {
			t.Fatalf("Clock 调用超过预期：已有 %d 个时间值", len(values))
		}
		value := values[index]
		index++
		return value
	}
}
