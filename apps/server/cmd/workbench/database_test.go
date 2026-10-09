package main

import (
	"context"
	"database/sql"
	"path/filepath"
	"testing"
)

func TestOpenDatabaseAppliesMigrationsOnce(t *testing.T) {
	path := filepath.Join(t.TempDir(), "nested", "workbench.db")

	db, err := openDatabase(context.Background(), path)
	if err != nil {
		t.Fatalf("openDatabase() first error = %v", err)
	}
	assertDatabaseTable(t, db, "requirements")
	assertDatabaseTable(t, db, "task_runs")
	assertDatabaseTable(t, db, "task_events")
	assertDatabaseTable(t, db, "verification_reports")
	assertDatabaseTable(t, db, "conversations")
	assertDatabaseTable(t, db, "conversation_turns")
	assertDatabaseTable(t, db, "conversation_messages")
	if err := db.Close(); err != nil {
		t.Fatalf("Close() first error = %v", err)
	}

	// 第二次启动不能重新执行 CREATE TABLE；schema_migrations 会跳过已应用文件。
	db, err = openDatabase(context.Background(), path)
	if err != nil {
		t.Fatalf("openDatabase() second error = %v", err)
	}
	defer db.Close()

	var count int
	if err := db.QueryRow(
		"SELECT COUNT(*) FROM schema_migrations WHERE name = ?",
		"000001_initial.up.sql",
	).Scan(&count); err != nil {
		t.Fatalf("query schema_migrations: %v", err)
	}
	if count != 1 {
		t.Fatalf("migration count = %d, want 1", count)
	}
	if err := db.QueryRow(
		"SELECT COUNT(*) FROM schema_migrations WHERE name = ?",
		"000002_verification_reports.up.sql",
	).Scan(&count); err != nil {
		t.Fatalf("query verification migration: %v", err)
	}
	if count != 1 {
		t.Fatalf("verification migration count = %d, want 1", count)
	}
	if err := db.QueryRow(
		"SELECT COUNT(*) FROM schema_migrations WHERE name = ?",
		"000005_conversations.up.sql",
	).Scan(&count); err != nil {
		t.Fatalf("query conversation migration: %v", err)
	}
	if count != 1 {
		t.Fatalf("conversation migration count = %d, want 1", count)
	}
}

func assertDatabaseTable(t *testing.T, db *sql.DB, name string) {
	t.Helper()
	var count int
	if err := db.QueryRow(
		"SELECT COUNT(*) FROM sqlite_master WHERE type = 'table' AND name = ?",
		name,
	).Scan(&count); err != nil {
		t.Fatalf("query table %q: %v", name, err)
	}
	if count != 1 {
		t.Fatalf("table %q count = %d, want 1", name, count)
	}
}
