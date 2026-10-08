package sqlite

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"time"

	"product-agent-workbench/internal/domain"
	storecontract "product-agent-workbench/internal/store"
)

type ticketVersionPayload struct {
	RepositoryRoot    string          `json:"repositoryRoot"`
	BaseRevision      string          `json:"baseRevision"`
	RequirementBranch string          `json:"requirementBranch"`
	Tickets           []ticketPayload `json:"tickets"`
}

type ticketPayload struct {
	ID                 string   `json:"id"`
	Title              string   `json:"title"`
	BlockedBy          []string `json:"blockedBy"`
	Deliverable        string   `json:"deliverable"`
	AcceptanceCriteria []string `json:"acceptanceCriteria"`
	ModificationScopes []string `json:"modificationScopes"`
}

func (s *Store) CreateTicketVersion(
	ctx context.Context,
	requirement domain.Requirement,
	expectedRevision int64,
	version domain.TicketVersion,
) (int64, error) {
	documentJSON, err := json.Marshal(ticketVersionPayload{
		RepositoryRoot:    version.RepositoryRoot,
		BaseRevision:      version.BaseRevision,
		RequirementBranch: version.RequirementBranch,
		Tickets:           encodeTickets(version.Tickets),
	})
	if err != nil {
		return 0, fmt.Errorf("编码 TicketVersion: %w", err)
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return 0, fmt.Errorf("开始保存 TicketVersion: %w", err)
	}
	defer tx.Rollback()

	// 前端类比：新草稿像创建新的不可变 Query Cache 版本，旧草稿只退出 active 状态。
	if _, err := tx.ExecContext(ctx, `
		UPDATE ticket_versions
		SET status = 'superseded', confirmed_at = NULL
		WHERE requirement_id = ? AND status = 'draft'`, requirement.ID); err != nil {
		return 0, fmt.Errorf("失效旧 TicketVersion 草稿: %w", err)
	}
	result, err := tx.ExecContext(ctx, `
		INSERT INTO ticket_versions (
			id, requirement_id, spec_version_id, version, status,
			document_json, created_at, confirmed_at
		) VALUES (?, ?, ?, ?, ?, ?, ?, NULL)
		ON CONFLICT DO NOTHING`,
		version.ID,
		version.RequirementID,
		version.SpecVersionID,
		version.Version,
		version.Status,
		documentJSON,
		version.CreatedAt.UnixNano(),
	)
	if err != nil {
		return 0, fmt.Errorf("保存 TicketVersion: %w", err)
	}
	if affected, rowsErr := result.RowsAffected(); rowsErr != nil {
		return 0, fmt.Errorf("读取 TicketVersion 保存结果: %w", rowsErr)
	} else if affected == 0 {
		return 0, storecontract.ErrAlreadyExists
	}
	if err := updateRequirementInTx(ctx, tx, requirement, expectedRevision); err != nil {
		return 0, err
	}
	if err := tx.Commit(); err != nil {
		return 0, fmt.Errorf("提交 TicketVersion 草稿: %w", err)
	}
	return expectedRevision + 1, nil
}

func (s *Store) ListTicketVersions(
	ctx context.Context,
	requirementID string,
) ([]domain.TicketVersion, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT id, requirement_id, spec_version_id, version, status,
		       document_json, created_at, confirmed_at
		FROM ticket_versions
		WHERE requirement_id = ?
		ORDER BY version ASC`, requirementID)
	if err != nil {
		return nil, fmt.Errorf("查询 TicketVersion: %w", err)
	}
	defer rows.Close()

	versions := make([]domain.TicketVersion, 0)
	for rows.Next() {
		version, scanErr := scanTicketVersion(rows)
		if scanErr != nil {
			return nil, scanErr
		}
		versions = append(versions, version)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("遍历 TicketVersion: %w", err)
	}
	return versions, nil
}

func (s *Store) ConfirmTicketVersion(
	ctx context.Context,
	requirement domain.Requirement,
	expectedRevision int64,
	versionID string,
	confirmedAt time.Time,
) (int64, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return 0, fmt.Errorf("开始确认 TicketVersion: %w", err)
	}
	defer tx.Rollback()
	result, err := tx.ExecContext(ctx, `
		UPDATE ticket_versions
		SET status = 'confirmed', confirmed_at = ?
		WHERE id = ? AND requirement_id = ? AND status = 'draft'`,
		confirmedAt.UTC().UnixNano(),
		versionID,
		requirement.ID,
	)
	if err != nil {
		return 0, fmt.Errorf("确认 TicketVersion: %w", err)
	}
	if affected, rowsErr := result.RowsAffected(); rowsErr != nil {
		return 0, fmt.Errorf("读取 TicketVersion 确认结果: %w", rowsErr)
	} else if affected == 0 {
		return 0, storecontract.ErrNotFound
	}
	if err := updateRequirementInTx(ctx, tx, requirement, expectedRevision); err != nil {
		return 0, err
	}
	if err := tx.Commit(); err != nil {
		return 0, fmt.Errorf("提交 TicketVersion 确认: %w", err)
	}
	return expectedRevision + 1, nil
}

func scanTicketVersion(row rowScanner) (domain.TicketVersion, error) {
	var version domain.TicketVersion
	var documentJSON []byte
	var createdAt int64
	var confirmedAt sql.NullInt64
	if err := row.Scan(
		&version.ID,
		&version.RequirementID,
		&version.SpecVersionID,
		&version.Version,
		&version.Status,
		&documentJSON,
		&createdAt,
		&confirmedAt,
	); err != nil {
		return domain.TicketVersion{}, fmt.Errorf("扫描 TicketVersion: %w", err)
	}
	var payload ticketVersionPayload
	if err := json.Unmarshal(documentJSON, &payload); err != nil {
		return domain.TicketVersion{}, fmt.Errorf("解析 TicketVersion %q: %w", version.ID, err)
	}
	version.RepositoryRoot = payload.RepositoryRoot
	version.BaseRevision = payload.BaseRevision
	version.RequirementBranch = payload.RequirementBranch
	version.Tickets = decodeTickets(payload.Tickets)
	version.CreatedAt = time.Unix(0, createdAt).UTC()
	if confirmedAt.Valid {
		value := time.Unix(0, confirmedAt.Int64).UTC()
		version.ConfirmedAt = &value
	}
	return version, nil
}

func encodeTickets(tickets []domain.Ticket) []ticketPayload {
	result := make([]ticketPayload, 0, len(tickets))
	for _, ticket := range tickets {
		result = append(result, ticketPayload{
			ID:                 ticket.ID,
			Title:              ticket.Title,
			BlockedBy:          append([]string{}, ticket.BlockedBy...),
			Deliverable:        ticket.Deliverable,
			AcceptanceCriteria: append([]string{}, ticket.AcceptanceCriteria...),
			ModificationScopes: append([]string{}, ticket.ModificationScopes...),
		})
	}
	return result
}

func decodeTickets(tickets []ticketPayload) []domain.Ticket {
	result := make([]domain.Ticket, 0, len(tickets))
	for _, ticket := range tickets {
		result = append(result, domain.Ticket{
			ID:                 ticket.ID,
			Title:              ticket.Title,
			BlockedBy:          append([]string{}, ticket.BlockedBy...),
			Deliverable:        ticket.Deliverable,
			AcceptanceCriteria: append([]string{}, ticket.AcceptanceCriteria...),
			ModificationScopes: append([]string{}, ticket.ModificationScopes...),
		})
	}
	return result
}
