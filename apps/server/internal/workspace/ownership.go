package workspace

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"time"
)

const ownershipSchemaVersion = 1

type workspaceKind string

const (
	workspaceKindTicket      workspaceKind = "ticket"
	workspaceKindIntegration workspaceKind = "integration"
)

// ownershipRecord 是磁盘上的安全凭据：只有记录存在且实时 Git 事实匹配，Manager 才会
// 修改或清理对应 Worktree。它不是业务数据库，不保存 Prompt、源码或秘密。
type ownershipRecord struct {
	SchemaVersion      int           `json:"schemaVersion"`
	Kind               workspaceKind `json:"kind"`
	RepositoryRoot     string        `json:"repositoryRoot"`
	Path               string        `json:"path"`
	BranchName         string        `json:"branchName"`
	BaseRevision       string        `json:"baseRevision"`
	RequirementID      string        `json:"requirementId"`
	CommittedRevision  string        `json:"committedRevision,omitempty"`
	IntegratedRevision string        `json:"integratedRevision,omitempty"`
	RequirementBranch  string        `json:"requirementBranch,omitempty"`
	ChangedFiles       []string      `json:"changedFiles,omitempty"`
	StableRevision     string        `json:"stableRevision,omitempty"`
	CreatedAt          time.Time     `json:"createdAt"`
}

func (m *GitManager) ownershipDirectory() string {
	return filepath.Join(m.root, "ownership")
}

func (m *GitManager) ownershipPath(workspacePath string) string {
	digest := sha256.Sum256([]byte(filepath.Clean(workspacePath)))
	return filepath.Join(m.ownershipDirectory(), hex.EncodeToString(digest[:])+".json")
}

func (m *GitManager) writeOwnership(record ownershipRecord) error {
	if err := validateOwnershipRecord(m.root, record); err != nil {
		return err
	}
	if err := ensureManagedDirectory(m.root, m.ownershipDirectory()); err != nil {
		return fmt.Errorf("创建 ownership 目录: %w", err)
	}

	// 先写同目录临时文件并 Sync，再 Rename；崩溃时不会留下半截 JSON。
	temporary, err := os.CreateTemp(m.ownershipDirectory(), ".ownership-*.tmp")
	if err != nil {
		return fmt.Errorf("创建 ownership 临时文件: %w", err)
	}
	temporaryPath := temporary.Name()
	cleanup := func() {
		_ = temporary.Close()
		_ = os.Remove(temporaryPath)
	}
	defer cleanup()

	if err := temporary.Chmod(0o600); err != nil {
		return fmt.Errorf("设置 ownership 文件权限: %w", err)
	}
	encoder := json.NewEncoder(temporary)
	encoder.SetIndent("", "  ")
	if err := encoder.Encode(record); err != nil {
		return fmt.Errorf("编码 ownership 记录: %w", err)
	}
	if err := temporary.Sync(); err != nil {
		return fmt.Errorf("同步 ownership 记录: %w", err)
	}
	if err := temporary.Close(); err != nil {
		return fmt.Errorf("关闭 ownership 临时文件: %w", err)
	}
	if err := os.Rename(temporaryPath, m.ownershipPath(record.Path)); err != nil {
		return fmt.Errorf("发布 ownership 记录: %w", err)
	}
	if err := syncDirectory(m.ownershipDirectory()); err != nil {
		return fmt.Errorf("同步 ownership 目录: %w", err)
	}
	return nil
}

func syncDirectory(path string) error {
	directory, err := os.Open(path)
	if err != nil {
		return err
	}
	defer directory.Close()
	return directory.Sync()
}

func (m *GitManager) readOwnership(workspacePath string) (ownershipRecord, error) {
	return readOwnershipFile(m.root, m.ownershipPath(workspacePath))
}

func readOwnershipFile(root string, recordPath string) (ownershipRecord, error) {
	info, err := os.Lstat(recordPath)
	if err != nil {
		return ownershipRecord{}, err
	}
	if info.Mode()&os.ModeSymlink != 0 || !info.Mode().IsRegular() || info.Size() > 64*1024 {
		return ownershipRecord{}, ErrInvalidOwnership
	}
	file, err := os.Open(recordPath)
	if err != nil {
		return ownershipRecord{}, err
	}
	defer file.Close()

	decoder := json.NewDecoder(io.LimitReader(file, 64*1024))
	decoder.DisallowUnknownFields()
	var record ownershipRecord
	if err := decoder.Decode(&record); err != nil {
		return ownershipRecord{}, fmt.Errorf("读取 ownership 记录: %w", err)
	}
	if err := decoder.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
		return ownershipRecord{}, fmt.Errorf("读取 ownership 记录: 存在多余 JSON 内容")
	}
	if err := validateOwnershipRecord(root, record); err != nil {
		return ownershipRecord{}, err
	}
	return record, nil
}

func validateOwnershipRecord(root string, record ownershipRecord) error {
	if record.SchemaVersion != ownershipSchemaVersion ||
		(record.Kind != workspaceKindTicket && record.Kind != workspaceKindIntegration) ||
		record.RepositoryRoot == "" || record.Path == "" || record.BranchName == "" ||
		record.BaseRevision == "" || record.RequirementID == "" || record.CreatedAt.IsZero() {
		return ErrInvalidOwnership
	}
	if record.Kind == workspaceKindIntegration && record.StableRevision == "" {
		return ErrInvalidOwnership
	}
	if !pathWithinRoot(root, record.Path) {
		return ErrInvalidOwnership
	}
	return nil
}

func pathWithinRoot(root string, candidate string) bool {
	relative, err := filepath.Rel(filepath.Clean(root), filepath.Clean(candidate))
	if err != nil {
		return false
	}
	return relative != "." && relative != ".." && !filepath.IsAbs(relative) &&
		len(relative) > 0 && !startsWithParent(relative)
}

func startsWithParent(relative string) bool {
	return relative == ".." || len(relative) >= 3 && relative[:3] == ".."+string(filepath.Separator)
}
