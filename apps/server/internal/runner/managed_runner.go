package runner

import (
	"context"
	"errors"
	"fmt"
	"path"
	"strings"

	"product-agent-workbench/internal/domain"
	"product-agent-workbench/internal/workspace"
)

var (
	// ErrInvalidWorkspacePlan 表示执行请求缺少创建或整合 Worktree 所需的 Git 事实。
	ErrInvalidWorkspacePlan = errors.New("Worktree 计划无效")
	// ErrNoWorkspaceChanges 表示执行器成功退出，但没有产生任何可整合修改。
	ErrNoWorkspaceChanges = errors.New("Worktree 没有修改")
	// ErrWorkspaceScopeViolation 表示执行器修改了确认范围以外的文件。
	ErrWorkspaceScopeViolation = errors.New("Worktree 修改超出允许范围")
)

// ManagedRunnerDependencies 是 Worktree 隔离执行需要的两个 Adapter。
type ManagedRunnerDependencies struct {
	Workspaces workspace.Manager
	Executor   Runner
}

// ManagedRunner 为每张 Ticket 准备独立 Worktree，调用底层 Executor，再把结果整合回
// Requirement 分支。它隐藏 Git 生命周期，使 Worker 仍然只面对 Runner 接口。
type ManagedRunner struct {
	workspaces workspace.Manager
	executor   Runner
}

// NewManagedRunner 只保存依赖，不创建 Worktree，也不启动外部进程。
func NewManagedRunner(dependencies ManagedRunnerDependencies) (*ManagedRunner, error) {
	if dependencies.Workspaces == nil || dependencies.Executor == nil {
		return nil, ErrInvalidDependencies
	}
	return &ManagedRunner{
		workspaces: dependencies.Workspaces,
		executor:   dependencies.Executor,
	}, nil
}

// Run 在独立 Worktree 中执行一张 Ticket，并在成功后整合 Requirement 分支。
//
// 失败现场不会自动 Release：执行错误和整合冲突都需要保留 Worktree 供排查。成功 Worktree
// 也暂时保留到后续验证/交付阶段，避免清理动作失败反过来污染任务的成功语义。
func (r *ManagedRunner) Run(
	ctx context.Context,
	request RunRequest,
	events EventSink,
	sessions SessionSink,
) (RunResult, error) {
	if err := validateWorkspaceRequest(request); err != nil {
		return RunResult{}, err
	}

	// 第一步：从已确认基线创建本 Ticket 独占的分支和 Worktree。
	prepared, err := r.workspaces.Prepare(ctx, workspace.PrepareRequest{
		RepositoryRoot: request.RepositoryRoot,
		BaseRevision:   request.Workspace.BaseRevision,
		BranchName:     request.Workspace.TicketBranch,
		RequirementID:  request.TaskRun.RequirementID,
	})
	if err != nil {
		return RunResult{}, fmt.Errorf("为 TaskRun %q 准备 Worktree: %w", request.TaskRun.ID, err)
	}

	// 第二步：底层 Executor 只能看到受管 Worktree 路径，不能回到用户原始 checkout。
	isolateRequest := request
	isolateRequest.RepositoryRoot = prepared.Path
	runResult, err := r.executor.Run(ctx, isolateRequest, events, sessions)
	if err != nil {
		return RunResult{}, fmt.Errorf("在 Worktree 中执行 TaskRun %q: %w", request.TaskRun.ID, err)
	}
	if strings.TrimSpace(runResult.Result.Summary) == "" {
		// 先验证领域完成所需的最小结果，再整合分支；否则 Git 已经变化，TaskRun 却无法
		// 进入 completed，会形成难以恢复的半成功状态。
		return RunResult{}, fmt.Errorf("TaskRun %q 的执行结果缺少摘要", request.TaskRun.ID)
	}

	// 第三步：成功退出不等于产生了实现。先读取 Git 真值，拒绝空修改。
	changedFiles, err := r.workspaces.ChangedFiles(ctx, prepared)
	if err != nil {
		return RunResult{}, fmt.Errorf("读取 TaskRun %q 的 Worktree 修改: %w", request.TaskRun.ID, err)
	}
	if len(changedFiles) == 0 {
		return RunResult{}, fmt.Errorf("TaskRun %q: %w", request.TaskRun.ID, ErrNoWorkspaceChanges)
	}
	// 不允许修改范围外的文件。
	if outside := filesOutsideScopes(changedFiles, request.Workspace.ModificationScopes); len(outside) > 0 {
		return RunResult{}, fmt.Errorf(
			"TaskRun %q: %w: %s",
			request.TaskRun.ID,
			ErrWorkspaceScopeViolation,
			strings.Join(outside, ", "),
		)
	}

	// 第四步：Manager 负责串行化同一 Requirement 分支的整合，并保证冲突时目标分支不变。
	// 只有整合成功，Worker 才会把 TaskRun 保存为 completed。
	integration, err := r.workspaces.Integrate(ctx, workspace.IntegrateRequest{
		Workspace:          prepared,
		RequirementBranch:  request.Workspace.RequirementBranch,
		ModificationScopes: append([]string(nil), request.Workspace.ModificationScopes...),
	})
	if err != nil {
		return RunResult{}, fmt.Errorf("整合 TaskRun %q 到 Requirement 分支: %w", request.TaskRun.ID, err)
	}
	if strings.TrimSpace(integration.Revision) == "" ||
		integration.RequirementBranch != request.Workspace.RequirementBranch ||
		len(integration.ChangedFiles) == 0 ||
		len(filesOutsideScopes(integration.ChangedFiles, request.Workspace.ModificationScopes)) > 0 {
		return RunResult{}, fmt.Errorf("%w: Worktree Manager 返回的整合事实不完整", ErrInvalidWorkspacePlan)
	}

	// WorktreePath 和 Revision 是后续验证、恢复和交付需要引用的可审计事实。
	// CodexThreadID 和 Summary 仍由底层 Executor 提供。
	runResult.Result.WorktreePath = prepared.Path
	runResult.Result.Revision = integration.Revision
	return runResult, nil
}

func validateWorkspaceRequest(request RunRequest) error {
	if request.TaskRun.Status != domain.TaskStatusRunning ||
		strings.TrimSpace(request.TaskRun.ID) == "" ||
		strings.TrimSpace(request.TaskRun.RequirementID) == "" ||
		strings.TrimSpace(request.RepositoryRoot) == "" ||
		strings.TrimSpace(request.Prompt) == "" ||
		strings.TrimSpace(request.Workspace.BaseRevision) == "" ||
		strings.TrimSpace(request.Workspace.TicketBranch) == "" ||
		strings.TrimSpace(request.Workspace.RequirementBranch) == "" ||
		!validModificationScopes(request.Workspace.ModificationScopes) ||
		request.Workspace.TicketBranch == request.Workspace.RequirementBranch {
		return ErrInvalidWorkspacePlan
	}
	return nil
}

func validModificationScopes(scopes []string) bool {
	if len(scopes) == 0 {
		return false
	}
	for _, scope := range scopes {
		trimmed := strings.TrimSpace(scope)
		normalized := path.Clean(strings.ReplaceAll(trimmed, "\\", "/"))
		if trimmed == "" || path.IsAbs(normalized) || normalized == ".." || strings.HasPrefix(normalized, "../") {
			return false
		}
	}
	return true
}

func filesOutsideScopes(files []string, scopes []string) []string {
	outside := make([]string, 0)
	for _, file := range files {
		normalizedFile := path.Clean(strings.ReplaceAll(file, "\\", "/"))
		if path.IsAbs(normalizedFile) || normalizedFile == "." || normalizedFile == ".." || strings.HasPrefix(normalizedFile, "../") {
			outside = append(outside, file)
			continue
		}

		allowed := false
		for _, scope := range scopes {
			normalizedScope := path.Clean(strings.ReplaceAll(strings.TrimSpace(scope), "\\", "/"))
			if normalizedScope == "." || normalizedFile == normalizedScope || strings.HasPrefix(normalizedFile, normalizedScope+"/") {
				allowed = true
				break
			}
		}
		if !allowed {
			outside = append(outside, file)
		}
	}
	return outside
}

// 编译期确认 ManagedRunner 可以直接注入 WorkerDependencies.Executor。
var _ Runner = (*ManagedRunner)(nil)
