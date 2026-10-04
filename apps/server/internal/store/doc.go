// Package store 在工作流用例的消费位置定义持久化接口。
//
// 不要为每张表创建一套通用 CRUD。一个“领取任务”方法应在同一事务中校验前态、修改
// 状态、增加 Requirement revision 并追加事件，把复杂性隐藏在较小的接口后面。
//
// 学习顺序：先实现内存 Adapter，再根据同一组契约测试实现 SQLite Adapter。契约测试要覆盖
// revision 冲突、资源不存在、任务领取顺序、事务回滚和有序事件重放。
package store
