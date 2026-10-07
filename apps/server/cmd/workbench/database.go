package main

import (
	"context"
	"database/sql"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"

	"product-agent-workbench/migrations"

	// SQLite driver 只在进程入口注册。internal/store/sqlite 仍只依赖 database/sql。
	_ "modernc.org/sqlite"
)

// openDatabase 创建连接池、确认数据库可用并按文件名顺序执行尚未应用的 migration。
//
// 前端类比：这比 localStorage 初始化严格得多。数据库连接是需要 Close 的资源，migration
// 也必须记录版本，不能每次刷新都重新执行 CREATE TABLE。
func openDatabase(ctx context.Context, databasePath string) (*sql.DB, error) {
	if err := os.MkdirAll(filepath.Dir(databasePath), 0o700); err != nil {
		return nil, fmt.Errorf("创建数据库目录: %w", err)
	}
	dsn := (&url.URL{
		Scheme:   "file",
		Path:     databasePath,
		RawQuery: "_foreign_keys=on&_busy_timeout=5000&_txlock=immediate",
	}).String()
	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, fmt.Errorf("打开 SQLite: %w", err)
	}
	// 保留多个物理连接，真实并发 claim 仍由 SQLite immediate 写事务串行化。
	db.SetMaxOpenConns(4)
	db.SetMaxIdleConns(4)
	if err := db.PingContext(ctx); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("连接 SQLite: %w", err)
	}
	if err := applyMigrations(ctx, db); err != nil {
		_ = db.Close()
		return nil, err
	}
	return db, nil
}

func applyMigrations(ctx context.Context, db *sql.DB) error {
	const createMigrationTable = `
		CREATE TABLE IF NOT EXISTS schema_migrations (
			name TEXT NOT NULL PRIMARY KEY,
			applied_at INTEGER NOT NULL
		)`
	if _, err := db.ExecContext(ctx, createMigrationTable); err != nil {
		return fmt.Errorf("创建 migration 记录表: %w", err)
	}

	entries, err := migrations.Files.ReadDir(".")
	if err != nil {
		return fmt.Errorf("读取 migration: %w", err)
	}
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".up.sql") {
			continue
		}
		applied, err := migrationApplied(ctx, db, entry.Name())
		if err != nil {
			return err
		}
		if applied {
			continue
		}
		migrationSQL, err := migrations.Files.ReadFile(entry.Name())
		if err != nil {
			return fmt.Errorf("读取 migration %q: %w", entry.Name(), err)
		}
		if err := applyMigration(ctx, db, entry.Name(), string(migrationSQL)); err != nil {
			return err
		}
	}
	return nil
}

func migrationApplied(ctx context.Context, db *sql.DB, name string) (bool, error) {
	var count int
	if err := db.QueryRowContext(
		ctx,
		"SELECT COUNT(*) FROM schema_migrations WHERE name = ?",
		name,
	).Scan(&count); err != nil {
		return false, fmt.Errorf("查询 migration %q: %w", name, err)
	}
	return count == 1, nil
}

func applyMigration(ctx context.Context, db *sql.DB, name string, migrationSQL string) error {
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("开始 migration %q: %w", name, err)
	}
	defer tx.Rollback()

	if _, err := tx.ExecContext(ctx, migrationSQL); err != nil {
		return fmt.Errorf("执行 migration %q: %w", name, err)
	}
	if _, err := tx.ExecContext(
		ctx,
		"INSERT INTO schema_migrations(name, applied_at) VALUES (?, ?)",
		name,
		time.Now().UTC().UnixNano(),
	); err != nil {
		return fmt.Errorf("记录 migration %q: %w", name, err)
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("提交 migration %q: %w", name, err)
	}
	return nil
}
