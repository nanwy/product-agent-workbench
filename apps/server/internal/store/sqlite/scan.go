package sqlite

// 本文件以后集中放 SQL 行到领域对象的还原逻辑。
//
// 开始写查询前先确定并记录：
//
//   - 时间统一保存为何种 UTC 编码；
//   - ClaimedAt/StartedAt/FinishedAt 如何用 sql.NullTime 或可空文本表达；
//   - TaskResult 的三个字段如何保证“整体存在或整体不存在”；
//   - 从数据库读出的 status、时间组合如何再次验证领域不变量。
//
// 不要把同一套 Scan 参数复制到多个查询里；列顺序一旦变化会产生难发现的数据错位。
