package workspace

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"sync"
	"time"
)

var (
	// ErrInvalidDependencies 表示 GitManager 缺少受管根目录等启动配置。
	ErrInvalidDependencies = errors.New("GitManager 依赖无效")
	// ErrInvalidRequest 表示路径、revision、分支或修改范围不满足安全约束。
	ErrInvalidRequest = errors.New("Git Worktree 请求无效")
	// ErrInvalidOwnership 表示磁盘记录与实时路径/Git 事实不一致。
	ErrInvalidOwnership = errors.New("Git Worktree ownership 无效")
	// ErrIntegrationConflict 表示 Ticket 修改无法干净整合到 Requirement 分支。
	ErrIntegrationConflict = errors.New("Requirement 分支整合冲突")
	// ErrNoChanges 表示 Ticket Worktree 没有可以提交的修改。
	ErrNoChanges = errors.New("Git Worktree 没有修改")
	// ErrScopeViolation 表示 Worktree 中存在确认范围外的修改。
	ErrScopeViolation = errors.New("Git Worktree 修改越界")
)

var fullRevisionPattern = regexp.MustCompile(`^[0-9a-fA-F]{40}([0-9a-fA-F]{24})?$`)

// GitManagerDependencies 配置真实 Git Adapter。
type GitManagerDependencies struct {
	// Root 必须是工作台专用绝对目录，不能指向用户仓库或其父目录。
	Root string
	// GitBinary 允许生产装配固定 Git 可执行文件；空值使用 PATH 中的 git。
	GitBinary string
}

// GitManager 只管理 Root 下且拥有 ownership 记录的 Worktree。
//
// integrationMu 把同一进程内的 Requirement 分支写入串行化。Git 自身仍提供 ref/index 锁，
// 因此即使误启动第二个进程，也会失败而不是静默覆盖分支。
type GitManager struct {
	root          string
	gitBinary     string
	integrationMu sync.Mutex
}

// NewGitManager 创建受管目录并保存 Git 配置，不读取或修改任何目标仓库。
func NewGitManager(dependencies GitManagerDependencies) (*GitManager, error) {
	root := strings.TrimSpace(dependencies.Root)
	if root == "" || !filepath.IsAbs(root) {
		return nil, ErrInvalidDependencies
	}
	root = filepath.Clean(root)
	if root == string(filepath.Separator) {
		return nil, ErrInvalidDependencies
	}
	if err := os.MkdirAll(root, 0o700); err != nil {
		return nil, fmt.Errorf("创建受管 Worktree 根目录: %w", err)
	}
	if err := os.Chmod(root, 0o700); err != nil {
		return nil, fmt.Errorf("设置受管 Worktree 根目录权限: %w", err)
	}
	canonicalRoot, err := filepath.EvalSymlinks(root)
	if err != nil {
		return nil, fmt.Errorf("解析受管 Worktree 根目录: %w", err)
	}

	gitBinary := strings.TrimSpace(dependencies.GitBinary)
	if gitBinary == "" {
		gitBinary = "git"
	}
	return &GitManager{root: canonicalRoot, gitBinary: gitBinary}, nil
}

// Prepare 幂等创建一张 Ticket 独占的分支和 Worktree。
func (m *GitManager) Prepare(ctx context.Context, request PrepareRequest) (Workspace, error) {
	repositoryRoot, err := m.validatePrepareRequest(ctx, request)
	if err != nil {
		return Workspace{}, err
	}

	m.integrationMu.Lock()
	defer m.integrationMu.Unlock()

	workspacePath := m.workspacePath("tickets", repositoryRoot, request.BranchName)
	if record, err := m.readOwnership(workspacePath); err == nil {
		if record.Kind != workspaceKindTicket || record.RepositoryRoot != repositoryRoot ||
			record.BranchName != request.BranchName || record.BaseRevision != request.BaseRevision ||
			record.RequirementID != request.RequirementID {
			return Workspace{}, ErrInvalidOwnership
		}
		return m.workspaceFromRecord(ctx, record)
	} else if !errors.Is(err, os.ErrNotExist) {
		return Workspace{}, err
	}

	if _, err := os.Lstat(workspacePath); err == nil {
		return Workspace{}, fmt.Errorf("%w: 目标路径已存在但没有 ownership 记录", ErrInvalidOwnership)
	} else if !errors.Is(err, os.ErrNotExist) {
		return Workspace{}, fmt.Errorf("检查 Worktree 目标路径: %w", err)
	}
	if exists, err := m.branchExists(ctx, repositoryRoot, request.BranchName); err != nil {
		return Workspace{}, err
	} else if exists {
		return Workspace{}, fmt.Errorf("%w: Ticket 分支已存在但不受当前 Manager 管理", ErrInvalidOwnership)
	}
	if err := ensureManagedDirectory(m.root, filepath.Dir(workspacePath)); err != nil {
		return Workspace{}, fmt.Errorf("创建 Ticket Worktree 父目录: %w", err)
	}

	if _, err := m.git(ctx, repositoryRoot, "worktree", "add", "-b", request.BranchName, workspacePath, request.BaseRevision); err != nil {
		return Workspace{}, fmt.Errorf("创建 Ticket Worktree: %w", err)
	}
	record := ownershipRecord{
		SchemaVersion:  ownershipSchemaVersion,
		Kind:           workspaceKindTicket,
		RepositoryRoot: repositoryRoot,
		Path:           workspacePath,
		BranchName:     request.BranchName,
		BaseRevision:   request.BaseRevision,
		RequirementID:  strings.TrimSpace(request.RequirementID),
		CreatedAt:      time.Now().UTC(),
	}
	if err := m.writeOwnership(record); err != nil {
		cleanupContext, cancel := context.WithTimeout(context.WithoutCancel(ctx), 15*time.Second)
		cleanupErr := m.removeNewWorkspace(cleanupContext, record, true)
		cancel()
		return Workspace{}, errors.Join(err, cleanupErr)
	}
	return m.workspaceFromRecord(ctx, record)
}

// ChangedFiles 返回相对当前 Ticket HEAD 的已跟踪和未跟踪修改。
func (m *GitManager) ChangedFiles(ctx context.Context, subject Workspace) ([]string, error) {
	m.integrationMu.Lock()
	defer m.integrationMu.Unlock()
	record, err := m.verifyOwnedWorkspace(ctx, subject, workspaceKindTicket)
	if err != nil {
		return nil, err
	}
	return m.changedFiles(ctx, record.Path)
}

// Integrate 提交 Ticket 修改，并用非快进 merge 把提交整合到受管 Requirement 分支。
//
// 非快进 merge 保留 Ticket commit 的祖先关系：如果进程在 merge 成功后、写 ownership 前
// 崩溃，重启后可以通过 merge-base --is-ancestor 判断整合已经完成，安全地继续收尾。
func (m *GitManager) Integrate(ctx context.Context, request IntegrateRequest) (Integration, error) {
	if err := validateScopes(request.ModificationScopes); err != nil ||
		strings.TrimSpace(request.RequirementBranch) == "" || strings.HasPrefix(request.RequirementBranch, "-") {
		return Integration{}, ErrInvalidRequest
	}
	normalizedScopes := normalizeScopes(request.ModificationScopes)

	m.integrationMu.Lock()
	defer m.integrationMu.Unlock()
	record, err := m.verifyOwnedWorkspace(ctx, request.Workspace, workspaceKindTicket)
	if err != nil {
		return Integration{}, err
	}
	if err := m.validateBranch(ctx, record.RepositoryRoot, request.RequirementBranch); err != nil {
		return Integration{}, err
	}
	if record.RequirementBranch != "" && record.RequirementBranch != request.RequirementBranch {
		return Integration{}, ErrInvalidOwnership
	}
	// 第一步：首次调用时重新读取工作区和 Ticket 分支累计修改，校验范围并创建 commit。
	// Executor 即使自行 commit，也必须接受同一套范围检查，不能通过提前提交绕过门禁。
	if record.CommittedRevision == "" {
		workingFiles, err := m.changedFiles(ctx, record.Path)
		if err != nil {
			return Integration{}, err
		}
		committedFiles, err := m.filesBetween(ctx, record.Path, record.BaseRevision)
		if err != nil {
			return Integration{}, err
		}
		allFiles := mergePaths(committedFiles, workingFiles)
		if len(allFiles) == 0 {
			return Integration{}, ErrNoChanges
		}
		if outside := filesOutsideScopes(allFiles, normalizedScopes); len(outside) > 0 {
			return Integration{}, fmt.Errorf("%w: %s", ErrScopeViolation, strings.Join(outside, ", "))
		}

		if len(workingFiles) > 0 {
			if _, err := m.git(ctx, record.Path, append([]string{"add", "-A", "--"}, normalizedScopes...)...); err != nil {
				return Integration{}, fmt.Errorf("暂存 Ticket 修改: %w", err)
			}
			staged, err := m.stagedFiles(ctx, record.Path)
			if err != nil {
				return Integration{}, err
			}
			if len(staged) == 0 {
				return Integration{}, ErrNoChanges
			}
			if outside := filesOutsideScopes(staged, normalizedScopes); len(outside) > 0 {
				return Integration{}, fmt.Errorf("%w: %s", ErrScopeViolation, strings.Join(outside, ", "))
			}

			message := fmt.Sprintf("workbench: %s", record.BranchName)
			if _, err := m.git(ctx, record.Path,
				"-c", "user.name=Product Agent Workbench",
				"-c", "user.email=workbench@localhost",
				"commit", "--no-gpg-sign", "-m", message,
			); err != nil {
				return Integration{}, fmt.Errorf("提交 Ticket 修改: %w", err)
			}
		}
		committedRevision, err := m.headRevision(ctx, record.Path)
		if err != nil {
			return Integration{}, err
		}
		allFiles, err = m.filesBetween(ctx, record.Path, record.BaseRevision)
		if err != nil {
			return Integration{}, err
		}
		if len(allFiles) == 0 {
			return Integration{}, ErrNoChanges
		}
		if outside := filesOutsideScopes(allFiles, normalizedScopes); len(outside) > 0 {
			return Integration{}, fmt.Errorf("%w: %s", ErrScopeViolation, strings.Join(outside, ", "))
		}
		record.CommittedRevision = committedRevision
		record.RequirementBranch = request.RequirementBranch
		record.ChangedFiles = append([]string(nil), allFiles...)
		if err := m.writeOwnership(record); err != nil {
			return Integration{}, err
		}
	}

	// 第二步：创建或恢复 Requirement 的专用整合 Worktree；不会切换用户 checkout。
	integrationRecord, err := m.prepareIntegrationWorkspace(ctx, record, request.RequirementBranch)
	if err != nil {
		return Integration{}, err
	}
	currentIntegrationHead, err := m.headRevision(ctx, integrationRecord.Path)
	if err != nil {
		return Integration{}, err
	}
	if record.IntegratedRevision != "" {
		containsTicket, err := m.isAncestor(ctx, integrationRecord.Path, record.CommittedRevision, "HEAD")
		if err != nil || !containsTicket {
			return Integration{}, ErrInvalidOwnership
		}
		containsRecordedIntegration, err := m.isAncestor(ctx, integrationRecord.Path, record.IntegratedRevision, "HEAD")
		if err != nil || !containsRecordedIntegration {
			return Integration{}, ErrInvalidOwnership
		}
		return integrationFromRecord(record, integrationRecord.Path), nil
	}

	// 第三步：重启恢复时先检查 Ticket commit 是否已经成为 Requirement 分支祖先。
	alreadyIntegrated, err := m.isAncestor(ctx, integrationRecord.Path, record.CommittedRevision, "HEAD")
	if err != nil {
		return Integration{}, err
	}
	if !alreadyIntegrated {
		if currentIntegrationHead != integrationRecord.StableRevision {
			// HEAD 发生了不是本 Ticket 带来的变化，不能猜测或覆盖其来源。
			return Integration{}, ErrInvalidOwnership
		}
		beforeRevision := currentIntegrationHead
		integrationRecord.StableRevision = beforeRevision
		if err := m.writeOwnership(integrationRecord); err != nil {
			return Integration{}, err
		}
		_, mergeErr := m.git(ctx, integrationRecord.Path,
			"-c", "user.name=Product Agent Workbench",
			"-c", "user.email=workbench@localhost",
			"merge", "--no-ff", "--no-edit", record.CommittedRevision,
		)
		if mergeErr != nil {
			// merge --abort 是冲突路径的补偿动作。它不继承已经触发的取消，但拥有独立短超时，
			// 既给 Git 收尾机会，也避免清理无限运行。
			abortContext, cancel := context.WithTimeout(context.WithoutCancel(ctx), 15*time.Second)
			_, abortErr := m.git(abortContext, integrationRecord.Path, "merge", "--abort")
			afterRevision, headErr := m.headRevision(abortContext, integrationRecord.Path)
			cancel()
			if headErr == nil && afterRevision != beforeRevision {
				headErr = fmt.Errorf("冲突回滚后 Requirement 分支 revision 发生变化")
			}
			return Integration{}, errors.Join(
				ErrIntegrationConflict,
				fmt.Errorf("合并 Ticket commit: %w", mergeErr),
				abortErr,
				headErr,
			)
		}
	}

	integratedRevision, err := m.headRevision(ctx, integrationRecord.Path)
	if err != nil {
		return Integration{}, err
	}
	integrationRecord.StableRevision = integratedRevision
	if err := m.writeOwnership(integrationRecord); err != nil {
		return Integration{}, err
	}
	record.IntegratedRevision = integratedRevision
	record.RequirementBranch = request.RequirementBranch
	if err := m.writeOwnership(record); err != nil {
		return Integration{}, err
	}
	return integrationFromRecord(record, integrationRecord.Path), nil
}

// Release 只删除有有效 ownership 记录且实时 Git 事实匹配的 Worktree。
func (m *GitManager) Release(ctx context.Context, subject Workspace) error {
	m.integrationMu.Lock()
	defer m.integrationMu.Unlock()
	if strings.TrimSpace(subject.Path) == "" || !pathWithinRoot(m.root, subject.Path) {
		return ErrInvalidOwnership
	}
	record, err := m.readOwnership(subject.Path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return ErrInvalidOwnership
		}
		return err
	}
	if subject.RepositoryRoot != "" && filepath.Clean(subject.RepositoryRoot) != record.RepositoryRoot ||
		subject.BranchName != "" && subject.BranchName != record.BranchName ||
		subject.BaseRevision != "" && subject.BaseRevision != record.BaseRevision {
		return ErrInvalidOwnership
	}
	if _, statErr := os.Lstat(record.Path); statErr == nil {
		if _, err := m.workspaceFromRecord(ctx, record); err != nil {
			return err
		}
	} else if !errors.Is(statErr, os.ErrNotExist) {
		return fmt.Errorf("检查受管 Worktree: %w", statErr)
	}
	return m.removeNewWorkspace(ctx, record, record.Kind == workspaceKindTicket)
}

// Recover 在进程重启后重新验证全部 ownership 记录，不接管未知目录。
func (m *GitManager) Recover(ctx context.Context) ([]Workspace, error) {
	m.integrationMu.Lock()
	defer m.integrationMu.Unlock()

	entries, err := os.ReadDir(m.ownershipDirectory())
	if errors.Is(err, os.ErrNotExist) {
		return []Workspace{}, nil
	}
	if err != nil {
		return nil, fmt.Errorf("读取 ownership 目录: %w", err)
	}

	workspaces := make([]Workspace, 0, len(entries))
	for _, entry := range entries {
		if entry.IsDir() || filepath.Ext(entry.Name()) != ".json" {
			continue
		}
		path := filepath.Join(m.ownershipDirectory(), entry.Name())
		record, err := readOwnershipFile(m.root, path)
		if err != nil {
			return nil, err
		}
		if filepath.Clean(path) != filepath.Clean(m.ownershipPath(record.Path)) {
			return nil, ErrInvalidOwnership
		}
		workspace, err := m.workspaceFromRecord(ctx, record)
		if err != nil {
			return nil, err
		}
		if record.Kind == workspaceKindIntegration {
			if err := m.restoreInterruptedIntegration(ctx, record); err != nil {
				return nil, err
			}
		}
		workspaces = append(workspaces, workspace)
	}
	sort.Slice(workspaces, func(i, j int) bool { return workspaces[i].Path < workspaces[j].Path })
	return workspaces, nil
}

func (m *GitManager) validatePrepareRequest(ctx context.Context, request PrepareRequest) (string, error) {
	if strings.TrimSpace(request.RequirementID) == "" || strings.TrimSpace(request.BranchName) == "" ||
		strings.HasPrefix(request.BranchName, "-") || !fullRevisionPattern.MatchString(request.BaseRevision) {
		return "", ErrInvalidRequest
	}
	repositoryRoot, err := canonicalExistingDirectory(request.RepositoryRoot)
	if err != nil {
		return "", fmt.Errorf("读取目标仓库: %w", err)
	}
	if pathWithinRoot(repositoryRoot, m.root) || pathWithinRoot(m.root, repositoryRoot) || repositoryRoot == m.root {
		return "", fmt.Errorf("%w: 受管目录不能与目标仓库嵌套", ErrInvalidRequest)
	}
	topLevelOutput, err := m.git(ctx, repositoryRoot, "rev-parse", "--show-toplevel")
	if err != nil {
		return "", fmt.Errorf("确认 Git 仓库根目录: %w", err)
	}
	topLevel, err := canonicalExistingDirectory(strings.TrimSpace(string(topLevelOutput)))
	if err != nil || topLevel != repositoryRoot {
		return "", fmt.Errorf("%w: RepositoryRoot 必须是 Git 仓库根目录", ErrInvalidRequest)
	}
	if err := m.validateBranch(ctx, repositoryRoot, request.BranchName); err != nil {
		return "", err
	}
	resolved, err := m.resolveRevision(ctx, repositoryRoot, request.BaseRevision)
	if err != nil {
		return "", err
	}
	if !strings.EqualFold(resolved, request.BaseRevision) {
		return "", fmt.Errorf("%w: BaseRevision 必须是完整 commit ID", ErrInvalidRequest)
	}
	return repositoryRoot, nil
}

func (m *GitManager) validateBranch(ctx context.Context, repositoryRoot string, branch string) error {
	if strings.TrimSpace(branch) != branch || strings.HasPrefix(branch, "-") {
		return ErrInvalidRequest
	}
	if _, err := m.git(ctx, repositoryRoot, "check-ref-format", "--branch", branch); err != nil {
		return fmt.Errorf("%w: 分支名称不合法", ErrInvalidRequest)
	}
	return nil
}

func (m *GitManager) resolveRevision(ctx context.Context, repositoryRoot string, revision string) (string, error) {
	output, err := m.git(ctx, repositoryRoot, "rev-parse", "--verify", revision+"^{commit}")
	if err != nil {
		return "", fmt.Errorf("解析 Git revision: %w", err)
	}
	return strings.TrimSpace(string(output)), nil
}

func (m *GitManager) workspacePath(kind string, repositoryRoot string, branch string) string {
	key := stablePathKey(repositoryRoot + "\x00" + branch)
	return filepath.Join(m.root, kind, key)
}

func stablePathKey(value string) string {
	// branch/repository 只参与摘要，不直接成为路径片段，避免路径穿越和平台非法字符。
	digest := sha256Sum(value)
	return digest[:24]
}

func canonicalExistingDirectory(value string) (string, error) {
	absolute, err := filepath.Abs(strings.TrimSpace(value))
	if err != nil {
		return "", err
	}
	canonical, err := filepath.EvalSymlinks(absolute)
	if err != nil {
		return "", err
	}
	info, err := os.Stat(canonical)
	if err != nil {
		return "", err
	}
	if !info.IsDir() {
		return "", ErrInvalidRequest
	}
	return filepath.Clean(canonical), nil
}

func ensureManagedDirectory(root string, directory string) error {
	if !pathWithinRoot(root, directory) {
		return ErrInvalidOwnership
	}
	if err := os.MkdirAll(directory, 0o700); err != nil {
		return err
	}
	canonical, err := filepath.EvalSymlinks(directory)
	if err != nil {
		return err
	}
	if filepath.Clean(canonical) != filepath.Clean(directory) {
		return ErrInvalidOwnership
	}
	info, err := os.Lstat(directory)
	if err != nil || info.Mode()&os.ModeSymlink != 0 || !info.IsDir() {
		return ErrInvalidOwnership
	}
	if err := os.Chmod(directory, 0o700); err != nil {
		return err
	}
	return nil
}

func validateScopes(scopes []string) error {
	if len(scopes) == 0 {
		return ErrInvalidRequest
	}
	for _, scope := range scopes {
		trimmed := strings.TrimSpace(scope)
		normalized := path.Clean(strings.ReplaceAll(trimmed, "\\", "/"))
		if trimmed == "" || path.IsAbs(normalized) || normalized == ".." || strings.HasPrefix(normalized, "../") {
			return ErrInvalidRequest
		}
	}
	return nil
}

func normalizeScopes(scopes []string) []string {
	normalized := make([]string, 0, len(scopes))
	seen := make(map[string]struct{}, len(scopes))
	for _, scope := range scopes {
		value := path.Clean(strings.ReplaceAll(strings.TrimSpace(scope), "\\", "/"))
		if _, exists := seen[value]; exists {
			continue
		}
		seen[value] = struct{}{}
		normalized = append(normalized, value)
	}
	return normalized
}

func filesOutsideScopes(files []string, scopes []string) []string {
	outside := make([]string, 0)
	for _, file := range files {
		normalizedFile := path.Clean(strings.ReplaceAll(file, "\\", "/"))
		allowed := normalizedFile != "." && normalizedFile != ".." && !path.IsAbs(normalizedFile) &&
			!strings.HasPrefix(normalizedFile, "../")
		if allowed {
			allowed = false
			for _, scope := range scopes {
				normalizedScope := path.Clean(strings.ReplaceAll(strings.TrimSpace(scope), "\\", "/"))
				if normalizedScope == "." || normalizedFile == normalizedScope || strings.HasPrefix(normalizedFile, normalizedScope+"/") {
					allowed = true
					break
				}
			}
		}
		if !allowed {
			outside = append(outside, file)
		}
	}
	return outside
}

// 编译期确认真实 Adapter 满足 Runner 使用的 Manager seam。
var _ Manager = (*GitManager)(nil)
