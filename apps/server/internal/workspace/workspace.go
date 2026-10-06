package workspace

import "context"

// PrepareRequest 描述创建受管 Worktree 所需的已确认事实。
type PrepareRequest struct {
	RepositoryRoot string
	BaseRevision   string
	BranchName     string
	RequirementID  string
}

// Workspace 是工作台拥有并允许 Runner 修改的隔离目录。
type Workspace struct {
	RepositoryRoot string
	Path           string
	BranchName     string
	BaseRevision   string
}

// Integration 是 Ticket Worktree 成功整合到 Requirement 分支后的 Git 事实。
type Integration struct {
	RequirementBranch string
	Revision          string
	ChangedFiles      []string
}

// IntegrateRequest 描述一次受控整合。
//
// ModificationScopes 使用仓库相对路径。Manager 在提交前必须重新读取 Git 修改并验证范围，
// 不能只相信 Runner 之前的 ChangedFiles 快照。
type IntegrateRequest struct {
	Workspace          Workspace
	RequirementBranch  string
	ModificationScopes []string
}

// Manager 是 Runner 消费的 Git Worktree seam。
//
// 真实 GitManager 通过 exec 参数数组调用 Git，并在恢复、整合和清理前验证 ownership 记录；
// 测试或教学场景仍可提供内存 fake。
type Manager interface {
	Prepare(ctx context.Context, request PrepareRequest) (Workspace, error)
	ChangedFiles(ctx context.Context, workspace Workspace) ([]string, error)
	// Integrate 必须把同一 Requirement 分支的并发写入串行化，并在发生冲突时保持目标
	// 分支不变。具体 Git Adapter 可以使用临时整合 Worktree，而不能切换用户 checkout。
	Integrate(ctx context.Context, request IntegrateRequest) (Integration, error)
	Release(ctx context.Context, workspace Workspace) error
	// Recover 只恢复 ownership registry 中存在且 Git 事实仍匹配的 Worktree。
	// 路径存在但没有 ownership 记录时必须拒绝接管。
	Recover(ctx context.Context) ([]Workspace, error)
}
