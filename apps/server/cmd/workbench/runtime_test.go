package main

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"product-agent-workbench/internal/domain"
	"product-agent-workbench/internal/httpapi"
	"product-agent-workbench/internal/runner"
	"product-agent-workbench/internal/scheduler"
	"product-agent-workbench/internal/store/memory"
	"product-agent-workbench/internal/workspace"
)

func TestBuildRuntime(t *testing.T) {
	components, err := buildRuntime(validRuntimeDependencies())
	if err != nil {
		t.Fatalf("buildRuntime() error = %v", err)
	}
	if components.Handler == nil {
		t.Fatal("buildRuntime() Handler = nil")
	}
	if components.Supervisor == nil {
		t.Fatal("buildRuntime() Supervisor = nil")
	}

	// buildRuntime 只完成路由注册，不监听真实端口；这里直接像调用普通函数一样验证 Handler。
	request := httptest.NewRequest(http.MethodGet, httpapi.PathHealth, nil)
	response := httptest.NewRecorder()
	components.Handler.ServeHTTP(response, request)
	if response.Code != http.StatusOK {
		t.Fatalf("health status = %d, want %d", response.Code, http.StatusOK)
	}

	// 空队列不是错误。这个断言也证明 Supervisor 创建出的闭包 Worker 已经连到同一个 Store。
	result, err := components.Supervisor.Drain(context.Background())
	if err != nil {
		t.Fatalf("Supervisor.Drain() error = %v", err)
	}
	if len(result.Runs) != 0 {
		t.Fatalf("Supervisor.Drain() runs = %d, want 0", len(result.Runs))
	}
}

func TestBuildRuntimeRejectsInvalidDependencies(t *testing.T) {
	tests := []struct {
		name   string
		mutate func(*runtimeDependencies)
	}{
		{name: "缺少 Store", mutate: func(dependencies *runtimeDependencies) { dependencies.Store = nil }},
		{name: "缺少 ArtifactReader", mutate: func(dependencies *runtimeDependencies) { dependencies.Artifacts = nil }},
		{name: "缺少 ExecutionFactsReader", mutate: func(dependencies *runtimeDependencies) { dependencies.ExecutionFacts = nil }},
		{name: "缺少 Workspace Manager", mutate: func(dependencies *runtimeDependencies) { dependencies.Workspaces = nil }},
		{name: "缺少 Executor", mutate: func(dependencies *runtimeDependencies) { dependencies.Executor = nil }},
		{name: "缺少 ID 生成器", mutate: func(dependencies *runtimeDependencies) { dependencies.NewID = nil }},
		{name: "缺少时钟", mutate: func(dependencies *runtimeDependencies) { dependencies.Now = nil }},
		{name: "并发数为零", mutate: func(dependencies *runtimeDependencies) { dependencies.Concurrency = 0 }},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			dependencies := validRuntimeDependencies()
			test.mutate(&dependencies)

			components, err := buildRuntime(dependencies)
			if !errors.Is(err, runner.ErrInvalidDependencies) {
				t.Fatalf("buildRuntime() error = %v, want ErrInvalidDependencies", err)
			}
			if components.Handler != nil || components.Supervisor != nil {
				t.Fatalf("buildRuntime() components = %#v, want zero value", components)
			}
		})
	}
}

func validRuntimeDependencies() runtimeDependencies {
	return runtimeDependencies{
		Store:          memory.New(),
		Artifacts:      runtimeArtifactReader{},
		ExecutionFacts: runtimeExecutionFactsReader{},
		Workspaces:     runtimeWorkspaceManager{},
		Executor:       runtimeExecutor{},
		NewID:          func() string { return "run-1" },
		Now:            func() time.Time { return time.Unix(1, 0).UTC() },
		Concurrency:    2,
	}
}

// 以下 fake 只用于证明 composition root 可以接受接口实现；空队列测试不会调用它们。
type runtimeArtifactReader struct{}

func (runtimeArtifactReader) ReadConfirmedTicketVersion(
	context.Context,
	string,
	string,
) (scheduler.TicketVersion, error) {
	return scheduler.TicketVersion{}, nil
}

type runtimeExecutionFactsReader struct{}

func (runtimeExecutionFactsReader) ReadExecutionFacts(
	context.Context,
	domain.TaskRun,
) (runner.ExecutionFacts, error) {
	return runner.ExecutionFacts{}, nil
}

type runtimeWorkspaceManager struct{}

func (runtimeWorkspaceManager) Prepare(
	context.Context,
	workspace.PrepareRequest,
) (workspace.Workspace, error) {
	return workspace.Workspace{}, nil
}

func (runtimeWorkspaceManager) ChangedFiles(context.Context, workspace.Workspace) ([]string, error) {
	return []string{}, nil
}

func (runtimeWorkspaceManager) Integrate(
	context.Context,
	workspace.IntegrateRequest,
) (workspace.Integration, error) {
	return workspace.Integration{}, nil
}

func (runtimeWorkspaceManager) Release(context.Context, workspace.Workspace) error {
	return nil
}

func (runtimeWorkspaceManager) Recover(context.Context) ([]workspace.Workspace, error) {
	return []workspace.Workspace{}, nil
}

type runtimeExecutor struct{}

func (runtimeExecutor) Run(
	context.Context,
	runner.RunRequest,
	runner.EventSink,
) (runner.RunResult, error) {
	return runner.RunResult{}, nil
}
