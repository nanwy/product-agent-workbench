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
	// CodexThreadID 为空时创建新 Thread；非空时恢复此前持久化的 Thread。
	// 它必须由服务端根据当前或前一次 TaskRun 的持久化会话填写，不能接受浏览器任意输入。
	CodexThreadID string
	Workspace     WorkspacePlan
}

// WorkspacePlan 描述一张 Ticket 如何获得独立工作目录并整合回 Requirement 分支。
//
// TicketBranch 每次 attempt 都必须唯一；RequirementBranch 是多个无依赖 Ticket 最终汇合的
// 分支。BaseRevision 是创建本次 Ticket Worktree 时已经确认的 Git 起点；ModificationScopes
// 限制 Executor 允许修改的仓库相对路径。
type WorkspacePlan struct {
	BaseRevision       string
	TicketBranch       string
	RequirementBranch  string
	ModificationScopes []string
}

// RunResult 是一次 Runner 执行完成后返回给控制面的事实。
type RunResult struct {
	Result domain.TaskResult
}

// EventSink 接收 Runner 产生的有序事件。
//
// 返回错误表示事件无法可靠保存，Runner 应停止继续产生不可审计的工作。
type EventSink func(ctx context.Context, event domain.TaskEvent) error

// SessionSink 在外部 Thread 打开后、Turn 开始前持久化会话 ID。
//
// 这相当于前端在长任务建立 WebSocket/session 后立刻把 sessionId 写入 store，而不是等
// 整个任务成功才记录。返回错误时 Runner 必须停止，不能开始一个无法恢复的 Turn。
type SessionSink func(ctx context.Context, taskRunID string, threadID string) error

// Runner 隐藏 Codex、Git Worktree 和目标进程等执行细节。
type Runner interface {
	Run(
		ctx context.Context,
		request RunRequest,
		events EventSink,
		sessions SessionSink,
	) (RunResult, error)
}
