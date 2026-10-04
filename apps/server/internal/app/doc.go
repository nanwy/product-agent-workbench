// Package app 负责用例编排和应用生命周期。
//
// 这里负责调用领域模块和 Store seam，但不知道具体 Adapter。main 是唯一负责选择并装配
// memory、SQLite、Runner 等具体 Adapter 的地方；HTTP handler 也只能接收 Application，
// 不能自行创建数据库连接或子进程。
//
// 学习顺序：
//  1. 完成 domain 状态机。
//  2. 完成内存 Store。
//  3. 完成同步的 Requirement 应用用例。
//  4. 再定义长任务的启动、执行和关闭行为。
//
// 每个在此启动的 goroutine 都必须回答三个问题：谁拥有它、怎样取消它、怎样等待它退出。
package app
