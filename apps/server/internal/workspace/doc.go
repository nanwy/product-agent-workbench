// Package workspace 负责受管仓库和 Git Worktree 的生命周期。
//
// 本包绝不能切换、stash、reset、commit 或 clean 用户已有的 checkout。所有修改都发生在
// 工作台拥有的 checkout/worktree 中，并且必须在 Codex 启动前记录路径和基线 revision。
//
// 当前 GitManager 已实现 ownership 原子记录、重启验证、修改范围门禁、Ticket 提交、
// Requirement 分支非快进整合和冲突回滚。清理只能删除能证明由工作台创建并拥有的路径；
// Git 参数始终以独立参数传递，不经过 Shell。
package workspace
