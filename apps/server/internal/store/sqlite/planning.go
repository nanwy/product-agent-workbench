package sqlite

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"product-agent-workbench/internal/domain"
	storecontract "product-agent-workbench/internal/store"
)

// ConfirmGrilling 使用事务保证“共同理解已保存”和“阶段已推进”只能同时成功。
func (s *Store) ConfirmGrilling(
	ctx context.Context,
	requirement domain.Requirement,
	expectedRevision int64,
	understanding domain.GrillingUnderstanding,
) (int64, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return 0, fmt.Errorf("开始确认 Grilling: %w", err)
	}
	defer tx.Rollback()

	result, err := tx.ExecContext(ctx, `
		INSERT INTO grilling_understandings (
			requirement_id, summary, desired_outcome, change_boundary, acceptance_signal, confirmed_at
		) VALUES (?, ?, ?, ?, ?, ?)
		ON CONFLICT(requirement_id) DO NOTHING`,
		understanding.RequirementID,
		understanding.Summary,
		understanding.DesiredOutcome,
		understanding.ChangeBoundary,
		understanding.AcceptanceSignal,
		understanding.ConfirmedAt.UnixNano(),
	)
	if err != nil {
		return 0, fmt.Errorf("保存 Grilling 共同理解: %w", err)
	}
	if affected, err := result.RowsAffected(); err != nil {
		return 0, fmt.Errorf("读取 Grilling 保存结果: %w", err)
	} else if affected == 0 {
		return 0, storecontract.ErrAlreadyExists
	}
	if err := updateRequirementInTx(ctx, tx, requirement, expectedRevision); err != nil {
		return 0, err
	}
	if err := tx.Commit(); err != nil {
		return 0, fmt.Errorf("提交 Grilling 确认: %w", err)
	}
	return expectedRevision + 1, nil
}

func (s *Store) GetGrillingUnderstanding(
	ctx context.Context,
	requirementID string,
) (domain.GrillingUnderstanding, error) {
	const query = `
		SELECT requirement_id, summary, desired_outcome, change_boundary,
		       acceptance_signal, confirmed_at
		FROM grilling_understandings
		WHERE requirement_id = ?`
	var value domain.GrillingUnderstanding
	var confirmedAt int64
	err := s.db.QueryRowContext(ctx, query, requirementID).Scan(
		&value.RequirementID,
		&value.Summary,
		&value.DesiredOutcome,
		&value.ChangeBoundary,
		&value.AcceptanceSignal,
		&confirmedAt,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return domain.GrillingUnderstanding{}, storecontract.ErrNotFound
	}
	if err != nil {
		return domain.GrillingUnderstanding{}, fmt.Errorf("读取 Grilling 共同理解: %w", err)
	}
	value.ConfirmedAt = time.Unix(0, confirmedAt).UTC()
	return value, nil
}

func (s *Store) CreateSpecVersion(
	ctx context.Context,
	requirement domain.Requirement,
	expectedRevision int64,
	version domain.SpecVersion,
) (int64, error) {
	documentJSON, err := json.Marshal(version.Document)
	if err != nil {
		return 0, fmt.Errorf("编码 Spec 文档: %w", err)
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return 0, fmt.Errorf("开始保存 Spec: %w", err)
	}
	defer tx.Rollback()

	// 新草稿不覆盖旧内容，只把旧 draft 退出活动集合。
	if _, err := tx.ExecContext(ctx, `
		UPDATE spec_versions
		SET status = 'superseded', confirmed_at = NULL
		WHERE requirement_id = ? AND status = 'draft'`, requirement.ID); err != nil {
		return 0, fmt.Errorf("失效旧 Spec 草稿: %w", err)
	}
	result, err := tx.ExecContext(ctx, `
		INSERT INTO spec_versions (
			id, requirement_id, version, status, document_json, created_at, confirmed_at
		) VALUES (?, ?, ?, ?, ?, ?, NULL)
		ON CONFLICT DO NOTHING`,
		version.ID,
		version.RequirementID,
		version.Version,
		version.Status,
		documentJSON,
		version.CreatedAt.UnixNano(),
	)
	if err != nil {
		return 0, fmt.Errorf("保存 Spec 版本: %w", err)
	}
	if affected, err := result.RowsAffected(); err != nil {
		return 0, fmt.Errorf("读取 Spec 保存结果: %w", err)
	} else if affected == 0 {
		return 0, storecontract.ErrAlreadyExists
	}
	if err := updateRequirementInTx(ctx, tx, requirement, expectedRevision); err != nil {
		return 0, err
	}
	if err := tx.Commit(); err != nil {
		return 0, fmt.Errorf("提交 Spec 草稿: %w", err)
	}
	return expectedRevision + 1, nil
}

func (s *Store) ListSpecVersions(ctx context.Context, requirementID string) ([]domain.SpecVersion, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT id, requirement_id, version, status, document_json, created_at, confirmed_at
		FROM spec_versions
		WHERE requirement_id = ?
		ORDER BY version ASC`, requirementID)
	if err != nil {
		return nil, fmt.Errorf("查询 Spec 版本: %w", err)
	}
	defer rows.Close()

	versions := make([]domain.SpecVersion, 0)
	for rows.Next() {
		var version domain.SpecVersion
		var documentJSON []byte
		var createdAt int64
		var confirmedAt sql.NullInt64
		if err := rows.Scan(
			&version.ID,
			&version.RequirementID,
			&version.Version,
			&version.Status,
			&documentJSON,
			&createdAt,
			&confirmedAt,
		); err != nil {
			return nil, fmt.Errorf("扫描 Spec 版本: %w", err)
		}
		if err := json.Unmarshal(documentJSON, &version.Document); err != nil {
			return nil, fmt.Errorf("解析 Spec 文档 %q: %w", version.ID, err)
		}
		version.CreatedAt = time.Unix(0, createdAt).UTC()
		if confirmedAt.Valid {
			value := time.Unix(0, confirmedAt.Int64).UTC()
			version.ConfirmedAt = &value
		}
		versions = append(versions, version)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("遍历 Spec 版本: %w", err)
	}
	return versions, nil
}

func (s *Store) ConfirmSpecVersion(
	ctx context.Context,
	requirement domain.Requirement,
	expectedRevision int64,
	versionID string,
	confirmedAt time.Time,
) (int64, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return 0, fmt.Errorf("开始确认 Spec: %w", err)
	}
	defer tx.Rollback()
	result, err := tx.ExecContext(ctx, `
		UPDATE spec_versions
		SET status = 'confirmed', confirmed_at = ?
		WHERE id = ? AND requirement_id = ? AND status = 'draft'`,
		confirmedAt.UTC().UnixNano(),
		versionID,
		requirement.ID,
	)
	if err != nil {
		return 0, fmt.Errorf("确认 Spec 版本: %w", err)
	}
	if affected, err := result.RowsAffected(); err != nil {
		return 0, fmt.Errorf("读取 Spec 确认结果: %w", err)
	} else if affected == 0 {
		return 0, storecontract.ErrNotFound
	}
	if err := updateRequirementInTx(ctx, tx, requirement, expectedRevision); err != nil {
		return 0, err
	}
	if err := tx.Commit(); err != nil {
		return 0, fmt.Errorf("提交 Spec 确认: %w", err)
	}
	return expectedRevision + 1, nil
}

func updateRequirementInTx(
	ctx context.Context,
	tx *sql.Tx,
	requirement domain.Requirement,
	expectedRevision int64,
) error {
	result, err := tx.ExecContext(ctx, `
		UPDATE requirements
		SET project_id = ?, title = ?, stage = ?, revision = ?
		WHERE id = ? AND revision = ?`,
		requirement.ProjectID,
		requirement.Title,
		requirement.Stage,
		expectedRevision+1,
		requirement.ID,
		expectedRevision,
	)
	if err != nil {
		return fmt.Errorf("更新 Planning Requirement: %w", err)
	}
	affected, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("读取 Planning Requirement 更新结果: %w", err)
	}
	if affected == 1 {
		return nil
	}
	var exists bool
	if err := tx.QueryRowContext(
		ctx,
		`SELECT EXISTS(SELECT 1 FROM requirements WHERE id = ?)`,
		requirement.ID,
	).Scan(&exists); err != nil {
		return fmt.Errorf("检查 Planning Requirement: %w", err)
	}
	if !exists {
		return storecontract.ErrNotFound
	}
	return storecontract.ErrRevisionConflict
}
