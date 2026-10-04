package runner

import (
	"context"

	"product-agent-workbench/internal/domain"
)

// RunRequest 是 Runner 执行一张票所需的稳定输入。
//
// 不要放数据库连接、HTTP request 或 UI 状态。Runner 应只依赖已经确认的执行事实。
type RunRequest struct {
	TaskRun        domain.TaskRun
	RepositoryRoot string
	Prompt         string
}

// RunResult 是一次 Runner 执行完成后返回给控制面的事实。
type RunResult struct {
	Result domain.TaskResult
}

// EventSink 接收 Runner 产生的有序事件。
//
// 返回错误表示事件无法可靠保存，Runner 应停止继续产生不可审计的工作。
type EventSink func(ctx context.Context, event domain.TaskEvent) error

// Runner 隐藏 Codex、Git Worktree 和目标进程等执行细节。
type Runner interface {
	Run(
		ctx context.Context,
		request RunRequest,
		sink EventSink,
	) (RunResult, error)
}
