package sqlite

import (
	"context"

	"product-agent-workbench/internal/domain"
)

// CreateRequirement 使用参数化 INSERT 保存初始 Requirement。
//
// 练习要点：ExecContext；重复键映射 ErrAlreadyExists；不要 INSERT OR REPLACE。
func (s *Store) CreateRequirement(ctx context.Context, requirement domain.Requirement) error {
	return errNotImplemented
}

// GetRequirement 按 ID 查询 Requirement。
//
// 练习要点：QueryRowContext + Scan；sql.ErrNoRows 映射 store.ErrNotFound；其它错误用 %w
// 包装并保留原因，但不能把 SQL 文本暴露给 HTTP。
func (s *Store) GetRequirement(ctx context.Context, id string) (domain.Requirement, error) {
	return domain.Requirement{}, errNotImplemented
}

// SaveRequirement 使用 expectedRevision 完成乐观锁更新。
//
// 练习要点：单条条件 UPDATE；RowsAffected 为 0 时进一步区分资源不存在和 revision 冲突；
// 成功后返回 expectedRevision+1。不要先读后无条件写。
func (s *Store) SaveRequirement(
	ctx context.Context,
	requirement domain.Requirement,
	expectedRevision int64,
) (int64, error) {
	return 0, errNotImplemented
}
