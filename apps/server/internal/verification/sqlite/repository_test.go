package sqlite_test

import (
	"context"
	"database/sql"
	"errors"
	"path/filepath"
	"testing"
	"time"

	_ "modernc.org/sqlite"

	"product-agent-workbench/internal/verification"
	verificationsqlite "product-agent-workbench/internal/verification/sqlite"
	"product-agent-workbench/migrations"
)

func TestRepositoryKeepsHistoryAndReturnsLatestReport(t *testing.T) {
	db := openVerificationDatabase(t)
	repository, err := verificationsqlite.NewRepository(db)
	if err != nil {
		t.Fatalf("NewRepository() error = %v", err)
	}
	ctx := context.Background()
	if _, err := repository.LatestReport(ctx, "req-1", "tickets-v1"); !errors.Is(err, verification.ErrReportNotFound) {
		t.Fatalf("LatestReport(empty) error = %v, want ErrReportNotFound", err)
	}

	completedAt := time.Unix(3, 0).UTC()
	first := verificationReportFixture("report-1", verification.ReportStatusBlocked, verification.StatusFailed, completedAt)
	second := verificationReportFixture("report-2", verification.ReportStatusPassed, verification.StatusVerified, completedAt)
	if err := repository.SaveReport(ctx, first); err != nil {
		t.Fatalf("SaveReport(first) error = %v", err)
	}
	if err := repository.SaveReport(ctx, second); err != nil {
		t.Fatalf("SaveReport(second) error = %v", err)
	}

	latest, err := repository.LatestReport(ctx, "req-1", "tickets-v1")
	if err != nil {
		t.Fatalf("LatestReport() error = %v", err)
	}
	if latest.ID != second.ID || latest.Status != verification.ReportStatusPassed ||
		len(latest.Matrix.Records) != 1 || latest.Matrix.Records[0].Status != verification.StatusVerified {
		t.Fatalf("LatestReport() = %#v, want second report", latest)
	}
	var count int
	if err := db.QueryRowContext(ctx, "SELECT COUNT(*) FROM verification_reports").Scan(&count); err != nil {
		t.Fatalf("count reports: %v", err)
	}
	if count != 2 {
		t.Fatalf("report count = %d, want 2", count)
	}
}

func openVerificationDatabase(t *testing.T) *sql.DB {
	t.Helper()
	db, err := sql.Open("sqlite", filepath.Join(t.TempDir(), "verification.db")+"?_foreign_keys=on")
	if err != nil {
		t.Fatalf("sql.Open() error = %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	for _, name := range []string{"000001_initial.up.sql", "000002_verification_reports.up.sql"} {
		migration, readErr := migrations.Files.ReadFile(name)
		if readErr != nil {
			t.Fatalf("ReadFile(%s) error = %v", name, readErr)
		}
		if _, execErr := db.Exec(string(migration)); execErr != nil {
			t.Fatalf("Exec(%s) error = %v", name, execErr)
		}
	}
	if _, err := db.Exec(`
		INSERT INTO requirements (id, project_id, title, stage, revision)
		VALUES ('req-1', 'project-1', '验证', 'implementation', 1)`); err != nil {
		t.Fatalf("insert requirement: %v", err)
	}
	return db
}

func verificationReportFixture(
	id string,
	status verification.ReportStatus,
	evidenceStatus verification.Status,
	completedAt time.Time,
) verification.Report {
	exitCode := 0
	if evidenceStatus == verification.StatusFailed {
		exitCode = 1
	}
	return verification.Report{
		ID: id, RequirementID: "req-1", TicketVersionID: "tickets-v1",
		CandidateRevision: "revision-1", WorkspacePath: "/tmp/worktree", Status: status,
		Matrix: verification.Matrix{Records: []verification.Evidence{{
			ID: "typecheck", Label: "Typecheck", Status: evidenceStatus, Required: true,
			Source: verification.SourceCommand, Command: "go test ./...", ExitCode: &exitCode,
			StartedAt: time.Unix(1, 0).UTC(), CompletedAt: time.Unix(2, 0).UTC(),
		}}},
		StartedAt: time.Unix(1, 0).UTC(), CompletedAt: completedAt,
	}
}
