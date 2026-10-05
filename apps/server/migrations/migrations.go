// Package migrations 把经过评审的 SQL 文件嵌入 Go 二进制。
//
// Store Adapter 不负责迁移；启动程序和集成测试在创建 Store 之前显式应用这些文件。
package migrations

import "embed"

// Files 包含随服务端版本一起发布的 migration。
//
//go:embed *.sql
var Files embed.FS
