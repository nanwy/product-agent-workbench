//go:build integration

package codex_test

import (
	"context"
	"os"
	"os/exec"
	"sync"
	"testing"
	"time"

	"product-agent-workbench/internal/codex"
	"product-agent-workbench/internal/domain"
	"product-agent-workbench/internal/runner"
)

// TestExecutorRealCodexSmoke 是显式启用的真实 app-server 冒烟测试。
//
// 默认单元测试不会启动用户机器上的 Codex。需要验证安装、认证与线上协议时运行：
// WORKBENCH_CODEX_SMOKE=1 go test -tags=integration ./internal/codex -run TestExecutorRealCodexSmoke
func TestExecutorRealCodexSmoke(t *testing.T) {
	if os.Getenv("WORKBENCH_CODEX_SMOKE") != "1" {
		t.Skip("设置 WORKBENCH_CODEX_SMOKE=1 后才运行真实 Codex 冒烟测试")
	}
	executable := os.Getenv("WORKBENCH_CODEX_COMMAND")
	if executable == "" {
		executable = "codex"
	}
	if _, err := exec.LookPath(executable); err != nil {
		t.Skipf("找不到 Codex 可执行文件 %q: %v", executable, err)
	}

	executor, err := codex.NewExecutor(codex.ExecutorConfig{Executable: executable})
	if err != nil {
		t.Fatalf("NewExecutor() error = %v", err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()

	var mu sync.Mutex
	events := make([]domain.TaskEvent, 0)
	threadID := ""
	result, err := executor.Run(
		ctx,
		runner.RunRequest{
			TaskRun:        domain.TaskRun{ID: "codex-smoke", TicketID: "smoke", Status: domain.TaskStatusRunning},
			RepositoryRoot: t.TempDir(),
			Prompt:         "只回复：smoke ok。不要创建或修改文件。",
		},
		func(_ context.Context, event domain.TaskEvent) error {
			mu.Lock()
			defer mu.Unlock()
			events = append(events, event)
			return nil
		},
		func(_ context.Context, _ string, openedThreadID string) error {
			mu.Lock()
			defer mu.Unlock()
			threadID = openedThreadID
			return nil
		},
	)
	if err != nil {
		t.Fatalf("Executor.Run() error = %v", err)
	}
	mu.Lock()
	defer mu.Unlock()
	if threadID == "" || result.Result.CodexThreadID != threadID || len(events) == 0 {
		t.Fatalf("smoke result=%#v threadID=%q events=%d", result, threadID, len(events))
	}
}
