// Package sqlite 提供 Store seam 的 SQLite Adapter。
//
// 这个包只负责参数化查询、事务、行扫描和稳定错误映射，不负责：
//
//   - 在构造函数中偷偷建表或迁移；
//   - 选择数据库文件路径或 SQLite driver；
//   - 保存 Spec/Ticket Markdown 等文件 artifact；
//   - 用进程内 mutex 代替数据库事务。
//
// main 最终负责打开 *sql.DB、配置连接池、运行经过人工评审的 migration，再把连接注入
// Store。切换完成前，旧 Node JSON Store 与新 SQLite 不双写、不互相覆盖。
package sqlite
