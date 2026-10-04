// Package domain 保存 Product Agent Workbench 的领域词汇和状态不变量。
//
// 从这里开始学习。Requirement 工作流状态与 TaskRun 执行状态必须分开：执行失败不代表
// Spec 失去确认，Spec 已确认也不代表执行已经开始。
//
// 本包只能包含纯状态规则，不能访问数据库、HTTP、Git 或 Codex。先打开 task_run.go，
// 再按 task_run_test.go 的顺序逐个完成 TODO。
package domain
