package workspace

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

func sha256Sum(value string) string {
	digest := sha256.Sum256([]byte(value))
	return hex.EncodeToString(digest[:])
}

func (m *GitManager) workspaceFromRecord(ctx context.Context, record ownershipRecord) (Workspace, error) {
	info, err := os.Lstat(record.Path)
	if err != nil {
		return Workspace{}, fmt.Errorf("读取受管 Worktree: %w", err)
	}
	if info.Mode()&os.ModeSymlink != 0 || !info.IsDir() {
		return Workspace{}, ErrInvalidOwnership
	}
	canonicalPath, err := filepath.EvalSymlinks(record.Path)
	if err != nil || filepath.Clean(canonicalPath) != filepath.Clean(record.Path) {
		return Workspace{}, ErrInvalidOwnership
	}

	// 同时核对仓库 common dir、当前分支和基线祖先，防止 ownership JSON 被复制到无关目录。
	repositoryCommonDir, err := m.gitCommonDirectory(ctx, record.RepositoryRoot)
	if err != nil {
		return Workspace{}, err
	}
	workspaceCommonDir, err := m.gitCommonDirectory(ctx, record.Path)
	if err != nil {
		return Workspace{}, err
	}
	if repositoryCommonDir != workspaceCommonDir {
		return Workspace{}, ErrInvalidOwnership
	}
	branchOutput, err := m.git(ctx, record.Path, "symbolic-ref", "--quiet", "--short", "HEAD")
	if err != nil || strings.TrimSpace(string(branchOutput)) != record.BranchName {
		return Workspace{}, ErrInvalidOwnership
	}
	baseIsAncestor, err := m.isAncestor(ctx, record.Path, record.BaseRevision, "HEAD")
	if err != nil || !baseIsAncestor {
		return Workspace{}, ErrInvalidOwnership
	}

	return Workspace{
		RepositoryRoot: record.RepositoryRoot,
		Path:           record.Path,
		BranchName:     record.BranchName,
		BaseRevision:   record.BaseRevision,
	}, nil
}

func (m *GitManager) verifyOwnedWorkspace(
	ctx context.Context,
	subject Workspace,
	expectedKind workspaceKind,
) (ownershipRecord, error) {
	if strings.TrimSpace(subject.Path) == "" || !pathWithinRoot(m.root, subject.Path) {
		return ownershipRecord{}, ErrInvalidOwnership
	}
	record, err := m.readOwnership(subject.Path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return ownershipRecord{}, ErrInvalidOwnership
		}
		return ownershipRecord{}, err
	}
	if expectedKind != "" && record.Kind != expectedKind {
		return ownershipRecord{}, ErrInvalidOwnership
	}
	if subject.RepositoryRoot != "" && filepath.Clean(subject.RepositoryRoot) != record.RepositoryRoot ||
		subject.BranchName != "" && subject.BranchName != record.BranchName ||
		subject.BaseRevision != "" && subject.BaseRevision != record.BaseRevision {
		return ownershipRecord{}, ErrInvalidOwnership
	}
	if _, err := m.workspaceFromRecord(ctx, record); err != nil {
		return ownershipRecord{}, err
	}
	return record, nil
}

func (m *GitManager) gitCommonDirectory(ctx context.Context, cwd string) (string, error) {
	output, err := m.git(ctx, cwd, "rev-parse", "--git-common-dir")
	if err != nil {
		return "", fmt.Errorf("读取 Git common dir: %w", err)
	}
	commonDirectory := strings.TrimSpace(string(output))
	if !filepath.IsAbs(commonDirectory) {
		commonDirectory = filepath.Join(cwd, commonDirectory)
	}
	canonical, err := filepath.EvalSymlinks(commonDirectory)
	if err != nil {
		return "", fmt.Errorf("解析 Git common dir: %w", err)
	}
	return filepath.Clean(canonical), nil
}

func (m *GitManager) changedFiles(ctx context.Context, workspacePath string) ([]string, error) {
	tracked, err := m.git(ctx, workspacePath, "diff", "--name-only", "-z", "HEAD", "--")
	if err != nil {
		return nil, fmt.Errorf("读取已跟踪修改: %w", err)
	}
	untracked, err := m.git(ctx, workspacePath, "ls-files", "--others", "--exclude-standard", "-z", "--")
	if err != nil {
		return nil, fmt.Errorf("读取未跟踪修改: %w", err)
	}
	return mergeNULPaths(tracked, untracked), nil
}

func (m *GitManager) stagedFiles(ctx context.Context, workspacePath string) ([]string, error) {
	output, err := m.git(ctx, workspacePath, "diff", "--cached", "--name-only", "-z", "--")
	if err != nil {
		return nil, fmt.Errorf("读取已暂存修改: %w", err)
	}
	return mergeNULPaths(output), nil
}

func (m *GitManager) filesBetween(ctx context.Context, workspacePath string, baseRevision string) ([]string, error) {
	output, err := m.git(ctx, workspacePath, "diff", "--name-only", "-z", baseRevision+"..HEAD", "--")
	if err != nil {
		return nil, fmt.Errorf("读取 Ticket 分支累计修改: %w", err)
	}
	return mergeNULPaths(output), nil
}

func mergeNULPaths(groups ...[]byte) []string {
	unique := make(map[string]struct{})
	for _, group := range groups {
		for _, entry := range strings.Split(string(group), "\x00") {
			if entry != "" {
				unique[entry] = struct{}{}
			}
		}
	}
	paths := make([]string, 0, len(unique))
	for entry := range unique {
		paths = append(paths, entry)
	}
	sort.Strings(paths)
	return paths
}

func mergePaths(groups ...[]string) []string {
	unique := make(map[string]struct{})
	for _, group := range groups {
		for _, entry := range group {
			if entry != "" {
				unique[entry] = struct{}{}
			}
		}
	}
	paths := make([]string, 0, len(unique))
	for entry := range unique {
		paths = append(paths, entry)
	}
	sort.Strings(paths)
	return paths
}

func (m *GitManager) branchExists(ctx context.Context, repositoryRoot string, branch string) (bool, error) {
	_, err := m.git(ctx, repositoryRoot, "show-ref", "--verify", "--quiet", "refs/heads/"+branch)
	if err == nil {
		return true, nil
	}
	if exitCode, ok := gitExitCode(err); ok && exitCode == 1 {
		return false, nil
	}
	return false, fmt.Errorf("检查 Git 分支: %w", err)
}

func (m *GitManager) headRevision(ctx context.Context, workspacePath string) (string, error) {
	output, err := m.git(ctx, workspacePath, "rev-parse", "--verify", "HEAD^{commit}")
	if err != nil {
		return "", fmt.Errorf("读取 Worktree HEAD: %w", err)
	}
	return strings.TrimSpace(string(output)), nil
}

func (m *GitManager) isAncestor(ctx context.Context, cwd string, ancestor string, descendant string) (bool, error) {
	_, err := m.git(ctx, cwd, "merge-base", "--is-ancestor", ancestor, descendant)
	if err == nil {
		return true, nil
	}
	if exitCode, ok := gitExitCode(err); ok && exitCode == 1 {
		return false, nil
	}
	return false, fmt.Errorf("检查 Git 祖先关系: %w", err)
}

func (m *GitManager) prepareIntegrationWorkspace(
	ctx context.Context,
	ticket ownershipRecord,
	requirementBranch string,
) (ownershipRecord, error) {
	workspacePath := m.workspacePath("integrations", ticket.RepositoryRoot, requirementBranch)
	if record, err := m.readOwnership(workspacePath); err == nil {
		if record.Kind != workspaceKindIntegration || record.RepositoryRoot != ticket.RepositoryRoot ||
			record.BranchName != requirementBranch || record.RequirementID != ticket.RequirementID {
			return ownershipRecord{}, ErrInvalidOwnership
		}
		if _, err := m.workspaceFromRecord(ctx, record); err != nil {
			return ownershipRecord{}, err
		}
		if err := m.restoreInterruptedIntegration(ctx, record); err != nil {
			return ownershipRecord{}, err
		}
		return record, nil
	} else if !errors.Is(err, os.ErrNotExist) {
		return ownershipRecord{}, err
	}

	if _, err := os.Lstat(workspacePath); err == nil {
		return ownershipRecord{}, fmt.Errorf("%w: 整合路径已存在但没有 ownership 记录", ErrInvalidOwnership)
	} else if !errors.Is(err, os.ErrNotExist) {
		return ownershipRecord{}, err
	}
	if err := ensureManagedDirectory(m.root, filepath.Dir(workspacePath)); err != nil {
		return ownershipRecord{}, fmt.Errorf("创建整合 Worktree 父目录: %w", err)
	}

	branchExists, err := m.branchExists(ctx, ticket.RepositoryRoot, requirementBranch)
	if err != nil {
		return ownershipRecord{}, err
	}
	if branchExists {
		// 没有 ownership 记录就不能证明这个已有分支属于工作台，拒绝接管。
		return ownershipRecord{}, fmt.Errorf("%w: Requirement 分支已存在但没有 ownership 记录", ErrInvalidOwnership)
	}
	if _, err := m.git(ctx, ticket.RepositoryRoot,
		"worktree", "add", "-b", requirementBranch, workspacePath, ticket.BaseRevision,
	); err != nil {
		return ownershipRecord{}, fmt.Errorf("创建 Requirement 整合 Worktree: %w", err)
	}

	record := ownershipRecord{
		SchemaVersion:  ownershipSchemaVersion,
		Kind:           workspaceKindIntegration,
		RepositoryRoot: ticket.RepositoryRoot,
		Path:           workspacePath,
		BranchName:     requirementBranch,
		BaseRevision:   ticket.BaseRevision,
		RequirementID:  ticket.RequirementID,
		StableRevision: ticket.BaseRevision,
		CreatedAt:      time.Now().UTC(),
	}
	if err := m.writeOwnership(record); err != nil {
		cleanupContext, cancel := context.WithTimeout(context.WithoutCancel(ctx), 15*time.Second)
		cleanupErr := m.removeNewWorkspace(cleanupContext, record, true)
		cancel()
		return ownershipRecord{}, errors.Join(err, cleanupErr)
	}
	return record, nil
}

func (m *GitManager) restoreInterruptedIntegration(ctx context.Context, record ownershipRecord) error {
	mergeInProgress, err := m.refExists(ctx, record.Path, "MERGE_HEAD")
	if err != nil {
		return err
	}
	if mergeInProgress {
		cleanupContext, cancel := context.WithTimeout(context.WithoutCancel(ctx), 15*time.Second)
		_, abortErr := m.git(cleanupContext, record.Path, "merge", "--abort")
		cancel()
		if abortErr != nil {
			return fmt.Errorf("恢复中断的 Requirement 分支 merge: %w", abortErr)
		}
	}

	workingFiles, err := m.changedFiles(ctx, record.Path)
	if err != nil {
		return err
	}
	if len(workingFiles) > 0 {
		// 整合 Worktree 不运行 Executor；任何普通脏改动都无法由 Manager 证明来源。
		return ErrInvalidOwnership
	}
	stableIsAncestor, err := m.isAncestor(ctx, record.Path, record.StableRevision, "HEAD")
	if err != nil || !stableIsAncestor {
		return ErrInvalidOwnership
	}
	return nil
}

func (m *GitManager) refExists(ctx context.Context, cwd string, ref string) (bool, error) {
	_, err := m.git(ctx, cwd, "rev-parse", "--verify", "--quiet", ref)
	if err == nil {
		return true, nil
	}
	if exitCode, ok := gitExitCode(err); ok && exitCode == 1 {
		return false, nil
	}
	return false, fmt.Errorf("检查 Git 引用: %w", err)
}

func integrationFromRecord(record ownershipRecord) Integration {
	return Integration{
		RequirementBranch: record.RequirementBranch,
		Revision:          record.IntegratedRevision,
		ChangedFiles:      append([]string(nil), record.ChangedFiles...),
	}
}

func (m *GitManager) removeNewWorkspace(ctx context.Context, record ownershipRecord, deleteBranch bool) error {
	var errs []error
	if _, err := os.Lstat(record.Path); err == nil {
		if _, err := m.git(ctx, record.RepositoryRoot, "worktree", "remove", "--force", record.Path); err != nil {
			errs = append(errs, fmt.Errorf("移除受管 Worktree: %w", err))
		}
	} else if !errors.Is(err, os.ErrNotExist) {
		errs = append(errs, fmt.Errorf("检查受管 Worktree: %w", err))
	}

	if len(errs) == 0 && deleteBranch {
		if exists, err := m.branchExists(ctx, record.RepositoryRoot, record.BranchName); err != nil {
			errs = append(errs, err)
		} else if exists {
			if _, err := m.git(ctx, record.RepositoryRoot, "branch", "-D", record.BranchName); err != nil {
				errs = append(errs, fmt.Errorf("删除受管 Ticket 分支: %w", err))
			}
		}
	}
	if len(errs) == 0 {
		if err := os.Remove(m.ownershipPath(record.Path)); err != nil && !errors.Is(err, os.ErrNotExist) {
			errs = append(errs, fmt.Errorf("删除 ownership 记录: %w", err))
		}
	}
	return errors.Join(errs...)
}
