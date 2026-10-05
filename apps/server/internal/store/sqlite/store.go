package sqlite

import (
	"database/sql"
	"errors"

	storecontract "product-agent-workbench/internal/store"
)

// ErrInvalidDependencies 表示 SQLite Adapter 缺少数据库连接。
var ErrInvalidDependencies = errors.New("SQLite Store 依赖无效")

// errNotImplemented 只用于分阶段脚手架；公共 Store 契约全部通过后删除。
var errNotImplemented = errors.New("SQLite Store 尚未实现")

// Store 使用 database/sql 实现持久化 seam。
//
// db 的生命周期由创建它的 main 管理，本类型不擅自 Close 外部注入的连接池。
type Store struct {
	db *sql.DB
}

// New 保存已经打开并完成 migration 的连接池。
//
// 构造函数只校验依赖，不 Ping、不执行 PRAGMA、不建表；启动期检查由 main 显式编排。
func New(db *sql.DB) (*Store, error) {
	if db == nil {
		return nil, ErrInvalidDependencies
	}
	return &Store{db: db}, nil
}

// 编译期保证方法集满足与 Memory Adapter 相同的 Store interface。
var _ storecontract.Store = (*Store)(nil)
