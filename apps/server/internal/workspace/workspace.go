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
	Path         string
	BranchName   string
	BaseRevision string
}

// Manager 是 Runner 消费的 Git Worktree seam。
//
// 第一个实现只需要 fake Manager。真实 Git Adapter 必须通过 exec 参数数组调用 Git，
// 并在清理前验证 ownership 记录。
type Manager interface {
	Prepare(ctx context.Context, request PrepareRequest) (Workspace, error)
	ChangedFiles(ctx context.Context, workspace Workspace) ([]string, error)
	Release(ctx context.Context, workspace Workspace) error
}
