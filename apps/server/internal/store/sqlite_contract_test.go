package store_test

import (
	"context"
	"database/sql"
	"net/url"
	"path/filepath"
	"testing"

	"product-agent-workbench/internal/store"
	storeadapter "product-agent-workbench/internal/store/sqlite"
	"product-agent-workbench/migrations"

	// 生产代码只在 cmd/workbench 导入 driver；集成测试是独立二进制，因此在 _test.go 中自行注册。
	_ "modernc.org/sqlite"
)

// TestSQLiteRequirementStoreContract 是练习 12 的第一个纵向切片。
//
// 它复用 Memory Store 已经通过的同一组 Requirement 契约；测试不关心 SQL
// 怎样写，只关心创建、读取、重复键、乐观锁和并发结果是否一致。
func TestSQLiteRequirementStoreContract(t *testing.T) {
	runRequirementStoreContract(t, newSQLiteStore)
}

// TestSQLiteTaskRunStoreContract 是练习 12 的第二个纵向切片。
//
// 当前故意处于 Red：实现 EnqueueTask、GetTask、SaveTask 后才会变绿。
// ClaimNextTask 和 TaskEvent 尚未加入 SQLite 契约。
func TestSQLiteTaskRunStoreContract(t *testing.T) {
	runTaskRunStoreContract(t, newSQLiteStore)
}

// TestSQLiteClaimStoreContract 是原子领取切片。
//
// 测试库允许多个物理连接，两个 goroutine 会真正竞争 SQLite 写权限；
// 不能通过把连接池限制为 1 来让测试“看起来稳定”。
func TestSQLiteClaimStoreContract(t *testing.T) {
	runClaimStoreContract(t, newSQLiteStore)
}

// newSQLiteStore 为每个子测试创建独立的磁盘数据库。
//
// 不使用 :memory:：database/sql 可能打开多个物理连接，而普通内存库在连接之间不共享。
// 这里的连接配置也会让后续并发 claim 测试真正使用两个连接。
func newSQLiteStore(t *testing.T) store.Store {
	t.Helper()

	databasePath := filepath.Join(t.TempDir(), "store.db")
	dsn := (&url.URL{
		Scheme:   "file",
		Path:     databasePath,
		RawQuery: "_foreign_keys=on&_busy_timeout=5000&_txlock=immediate",
	}).String()

	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		t.Fatalf("打开 SQLite 测试数据库：%v", err)
	}
	t.Cleanup(func() {
		if err := db.Close(); err != nil {
			t.Errorf("关闭 SQLite 测试数据库：%v", err)
		}
	})

	// 保留多连接能力；不要用 SetMaxOpenConns(1) 隐藏后续 claim 的并发问题。
	db.SetMaxOpenConns(4)
	db.SetMaxIdleConns(4)

	ctx := context.Background()
	if err := db.PingContext(ctx); err != nil {
		t.Fatalf("连接 SQLite 测试数据库：%v", err)
	}

	migrationSQL, err := migrations.Files.ReadFile("000001_initial.up.sql")
	if err != nil {
		t.Fatalf("读取初始 migration：%v", err)
	}
	if _, err := db.ExecContext(ctx, string(migrationSQL)); err != nil {
		t.Fatalf("应用初始 migration：%v", err)
	}

	subject, err := storeadapter.New(db)
	if err != nil {
		t.Fatalf("创建 SQLite Store：%v", err)
	}
	return subject
}
