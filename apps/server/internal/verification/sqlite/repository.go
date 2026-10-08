// Package sqlite 持久化 Verification 报告；命令执行和门禁规则仍属于父包。
package sqlite

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"product-agent-workbench/internal/verification"
)

type Repository struct {
	db *sql.DB
}

func NewRepository(db *sql.DB) (*Repository, error) {
	if db == nil {
		return nil, verification.ErrInvalidDependencies
	}
	return &Repository{db: db}, nil
}

// SaveReport 只插入，不更新。重新执行验证会生成新的 report ID，保留上一轮失败证据。
func (r *Repository) SaveReport(ctx context.Context, report verification.Report) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	validIdentity := strings.TrimSpace(report.ID) != "" && strings.TrimSpace(report.RequirementID) != "" &&
		strings.TrimSpace(report.TicketVersionID) != "" && strings.TrimSpace(report.WorkspacePath) != ""
	validTime := !report.StartedAt.IsZero() && !report.CompletedAt.Before(report.StartedAt)
	validatedMatrix, err := verification.BuildMatrix(report.Matrix.Records)
	if err != nil || !validIdentity || !validTime {
		return fmt.Errorf("%w: Verification 报告字段无效", verification.ErrInvalidEvidence)
	}
	validPassed := report.Status == verification.ReportStatusPassed && !validatedMatrix.BlocksDelivery()
	validBlocked := report.Status == verification.ReportStatusBlocked && validatedMatrix.BlocksDelivery()
	if !validPassed && !validBlocked {
		return fmt.Errorf("%w: Verification 报告状态与矩阵不一致", verification.ErrInvalidEvidence)
	}
	payload, err := json.Marshal(validatedMatrix)
	if err != nil {
		return fmt.Errorf("编码 Verification Matrix: %w", err)
	}
	_, err = r.db.ExecContext(ctx, `
		INSERT INTO verification_reports (
			id, requirement_id, ticket_version_id, candidate_revision, workspace_path,
			status, matrix_json, started_at, completed_at
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		report.ID,
		report.RequirementID,
		report.TicketVersionID,
		report.CandidateRevision,
		report.WorkspacePath,
		report.Status,
		payload,
		report.StartedAt.UTC().UnixNano(),
		report.CompletedAt.UTC().UnixNano(),
	)
	if err != nil {
		return fmt.Errorf("写入 Verification 报告: %w", err)
	}
	return nil
}

// LatestReport 按完成时间和插入序号倒序，返回指定 TicketVersion 最近一次验证尝试。
func (r *Repository) LatestReport(
	ctx context.Context,
	requirementID string,
	ticketVersionID string,
) (verification.Report, error) {
	if err := ctx.Err(); err != nil {
		return verification.Report{}, err
	}
	requirementID = strings.TrimSpace(requirementID)
	ticketVersionID = strings.TrimSpace(ticketVersionID)
	if requirementID == "" || ticketVersionID == "" {
		return verification.Report{}, verification.ErrInvalidPlan
	}

	var report verification.Report
	var matrixJSON []byte
	var startedAt, completedAt int64
	err := r.db.QueryRowContext(ctx, `
		SELECT id, requirement_id, ticket_version_id, candidate_revision, workspace_path,
		       status, matrix_json, started_at, completed_at
		FROM verification_reports
		WHERE requirement_id = ? AND ticket_version_id = ?
		ORDER BY completed_at DESC, report_seq DESC
		LIMIT 1`, requirementID, ticketVersionID).Scan(
		&report.ID,
		&report.RequirementID,
		&report.TicketVersionID,
		&report.CandidateRevision,
		&report.WorkspacePath,
		&report.Status,
		&matrixJSON,
		&startedAt,
		&completedAt,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return verification.Report{}, verification.ErrReportNotFound
	}
	if err != nil {
		return verification.Report{}, fmt.Errorf("读取 Verification 报告: %w", err)
	}
	if err := json.Unmarshal(matrixJSON, &report.Matrix); err != nil {
		return verification.Report{}, fmt.Errorf("解码 Verification Matrix: %w", err)
	}
	report.StartedAt = time.Unix(0, startedAt).UTC()
	report.CompletedAt = time.Unix(0, completedAt).UTC()
	validatedMatrix, err := verification.BuildMatrix(report.Matrix.Records)
	if err != nil {
		return verification.Report{}, fmt.Errorf("校验已保存 Verification Matrix: %w", err)
	}
	report.Matrix = validatedMatrix
	validPassed := report.Status == verification.ReportStatusPassed && !report.Matrix.BlocksDelivery()
	validBlocked := report.Status == verification.ReportStatusBlocked && report.Matrix.BlocksDelivery()
	if !validPassed && !validBlocked {
		return verification.Report{}, fmt.Errorf("校验已保存 Verification 状态: %w", verification.ErrInvalidEvidence)
	}
	return report, nil
}

var _ verification.ReportRepository = (*Repository)(nil)
