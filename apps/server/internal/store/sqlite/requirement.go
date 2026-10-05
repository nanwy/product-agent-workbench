package sqlite

import (
	"context"
	"database/sql"
	"errors"
	"fmt"

	"product-agent-workbench/internal/domain"
	storecontract "product-agent-workbench/internal/store"
)

// CreateRequirement 使用参数化 INSERT 保存初始 Requirement。
//
// 练习要点：ExecContext；重复键映射 ErrAlreadyExists；不要 INSERT OR REPLACE。
//
// 建议按这个顺序实现：
//  1. 用 INSERT ... ON CONFLICT(id) DO NOTHING 和 5 个 ? 占位符写入；
//  2. 参数依次传 ID、ProjectID、Title、Stage、Revision，不拼接字符串；
//  3. 读取 RowsAffected，为 0 时返回 store.ErrAlreadyExists；
//  4. 把其它数据库错误用 fmt.Errorf("...: %w", err) 包装后返回。
func (s *Store) CreateRequirement(ctx context.Context, requirement domain.Requirement) error {
	// SQL 只描述要执行的操作，不把 requirement 的值拼进字符串。
	// 五个 ? 是占位符，真实值会由 ExecContext 后面的参数依次绑定。
	const query = `
		INSERT INTO requirements (id, project_id, title, stage, revision)
		VALUES (?, ?, ?, ?, ?)
		ON CONFLICT(id) DO NOTHING
	`

	// INSERT 不返回数据行，因此使用 ExecContext，而不是 QueryContext。
	// Context 被取消时，database/sql 可以尽快停止这次写入。
	result, err := s.db.ExecContext(
		ctx,
		query,
		requirement.ID,
		requirement.ProjectID,
		requirement.Title,
		requirement.Stage,
		requirement.Revision,
	)
	if err != nil {
		return fmt.Errorf("创建 Requirement: %w", err)
	}

	// ON CONFLICT(id) DO NOTHING 遇到重复 ID 时不会返回 SQL 错误，
	// 而是影响 0 行。因此通过 RowsAffected 把它翻译成 Store 的稳定领域错误。
	rowsAffected, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("读取 Requirement 创建结果: %w", err)
	}
	if rowsAffected == 0 {
		return storecontract.ErrAlreadyExists
	}

	return nil
}

// GetRequirement 按 ID 查询 Requirement。
//
// 练习要点：QueryRowContext + Scan；sql.ErrNoRows 映射 store.ErrNotFound；其它错误用 %w
// 包装并保留原因，但不能把 SQL 文本暴露给 HTTP。
//
// SELECT 固定按 id, project_id, title, stage, revision 的顺序返回，Scan 的目标也必须
// 保持同样顺序。WHERE id = ? 的值作为 QueryRowContext 参数传入。
func (s *Store) GetRequirement(ctx context.Context, id string) (domain.Requirement, error) {
	const query = `
		SELECT id, project_id, title, stage, revision
		FROM requirements
		WHERE id = ?
	`
	var requirement domain.Requirement
	err := s.db.QueryRowContext(ctx, query, id).Scan(
		&requirement.ID,
		&requirement.ProjectID,
		&requirement.Title,
		&requirement.Stage,
		&requirement.Revision,
	)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return domain.Requirement{}, storecontract.ErrNotFound
		}
		return domain.Requirement{}, fmt.Errorf("读取 Requirement: %w", err)
	}
	return requirement, nil
}

// SaveRequirement 使用 expectedRevision 完成乐观锁更新。
//
// 练习要点：单条条件 UPDATE；RowsAffected 为 0 时进一步区分资源不存在和 revision 冲突；
// 成功后返回 expectedRevision+1。不要先读后无条件写。
//
// 关键 SQL 形状：UPDATE ... SET ..., revision = ? WHERE id = ? AND revision = ?。
// 写入的新版本是 expectedRevision+1，不是 requirement.Revision。RowsAffected==0 后再用
// SELECT EXISTS(...) 区分 ErrNotFound 和 ErrRevisionConflict；这个补充查询只用来分类错误。
func (s *Store) SaveRequirement(
	ctx context.Context,
	requirement domain.Requirement,
	expectedRevision int64,
) (int64, error) {
	const query = `
		UPDATE requirements
		SET project_id = ?, title = ?, stage = ?, revision = ?
		WHERE id = ? AND revision = ?
	`
	result, err := s.db.ExecContext(
		ctx,
		query,
		requirement.ProjectID,
		requirement.Title,
		requirement.Stage,
		expectedRevision+1, // 新版本号
		requirement.ID,
		expectedRevision, // 旧版本号
	)
	if err != nil {
		return 0, fmt.Errorf("更新 Requirement: %w", err)
	}

	rowsAffected, err := result.RowsAffected()
	if err != nil {
		return 0, fmt.Errorf("读取 Requirement 更新结果: %w", err)
	}
	if rowsAffected == 0 {
		var exists bool
		err := s.db.QueryRowContext(
			ctx,
			`SELECT EXISTS(SELECT 1 FROM requirements WHERE id = ?)`,
			requirement.ID,
		).Scan(&exists)
		if err != nil {
			return 0, fmt.Errorf("检查 Requirement 是否存在: %w", err)
		}
		if !exists {
			return 0, storecontract.ErrNotFound
		}
		return 0, storecontract.ErrRevisionConflict
	}

	return expectedRevision + 1, nil
}
