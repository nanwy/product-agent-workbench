// Package app 负责用例编排和应用生命周期。
//
// 这里是唯一可以同时知道多个具体 Adapter 的地方。HTTP handler、工作流模块和
// Runner 只能接收依赖，不能在内部自行创建 SQLite 连接或子进程。
//
// 学习顺序：
//  1. 完成 domain 状态机。
//  2. 完成内存 Store。
//  3. 再定义 App 的启动、执行和关闭行为。
//
// 每个在此启动的 goroutine 都必须回答三个问题：谁拥有它、怎样取消它、怎样等待它退出。
package app
