// Package workspace 负责受管仓库和 Git Worktree 的生命周期。
//
// 本包绝不能切换、stash、reset、commit 或 clean 用户已有的 checkout。所有修改都发生在
// 工作台拥有的 checkout/worktree 中，并且必须在 Codex 启动前记录路径和基线 revision。
//
// 学习顺序：先设计 ownership 记录和恢复测试，再实现清理。清理只能删除能证明由工作台创建
// 并拥有的路径。Git 参数要以独立参数传递，不能通过 Shell 执行。
package workspace
