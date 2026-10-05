package sqlite_test

import (
	"errors"
	"testing"

	"product-agent-workbench/internal/store/sqlite"
)

func TestNewRejectsNilDatabase(t *testing.T) {
	t.Parallel()

	storeAdapter, err := sqlite.New(nil)
	if !errors.Is(err, sqlite.ErrInvalidDependencies) {
		t.Fatalf("New(nil) error = %v, want ErrInvalidDependencies", err)
	}
	if storeAdapter != nil {
		t.Fatalf("New(nil) store = %#v, want nil", storeAdapter)
	}
}

func TestStoreContract(t *testing.T) {
	t.Skip("练习 12：选定 driver、完成 migration，并把公共 Store 契约提取为可复用 factory 后解锁")

	// 这里最终不能复制 Memory 测试。把 internal/store/contract_test.go 提取成
	// runStoreContract(t, factory)，Memory 与 SQLite Adapter 必须执行完全相同的行为契约。
}
