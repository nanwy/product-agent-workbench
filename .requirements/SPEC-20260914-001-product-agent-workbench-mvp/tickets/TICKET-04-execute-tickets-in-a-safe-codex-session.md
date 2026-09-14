# TICKET-04 在安全需求分支中连续执行 Codex 票

**Type:** AFK
**Status:** Done

## Parent

[SPEC-20260914-001 Product Agent Workbench MVP](../spec.md)

## What to build

Spec 与边界确认后，记录目标仓库的 base branch、base revision 和起始 working tree，在干净起点创建一条 `product/<spec-id>-<slug>` 需求分支。已有改动或正在进行的 merge/rebase 必须阻止执行，不 stash、reset 或静默纳入他人工作。

每个 Requirement 使用一个连续 Codex session，按执行票串行运行 turn。工作台把 CLI JSONL 归一化为稳定事件，通过 SSE 展示 session、当前票、消息、文件变化、完成、失败和取消；取消只停止当前 turn，保留分支与 session 供恢复。

## User stories covered

- US-33～US-42：需求分支、Git 起点、连续 Session、串行执行、事件、取消和改动文件。
- US-60：执行失败后可恢复，不要求重建需求。

## Acceptance criteria

- [x] 执行前记录 base branch、base revision 和完整 working-tree 状态。
- [x] 脏工作区、merge/rebase 或无法安全分离的变更会阻止建分支，且不破坏现有内容。
- [x] 首次执行创建需求分支；失败或取消后的重试复用同一 Requirement 和分支。
- [x] Codex 以确认修改范围为 cwd，首票创建 session，后续票通过 resume 使用同一 session ID。
- [x] 只有一张 active Ticket；完成后自动推进下一张，失败时当前票标为 blocked。
- [x] 执行上下文只声明真实可用的目标项目 Skill，并让 Codex 能读取生效规则和 Skill 指令。
- [x] Codex 沙箱在写入前限制修改范围，并在每个 turn 前后复核分支、HEAD 和改动文件。
- [x] SSE 只推送状态变化；刷新从持久化 Requirement 恢复，服务重启遗留状态转为可续接失败态。
- [x] 取消会终止工作台拥有的 Codex 进程组，保留 session ID，并允许继续当前票。
- [x] 自动化测试覆盖建分支、脏工作区保护、多票串行、session 续接、失败、取消、范围和 JSONL 事件。

## Blocked by

- TICKET-03 只读探索规则并确认 Spec 与执行票。

## Audit result · 2026-09-15

主功能已实现：Git 起点记录、脏工作区阻断、需求分支、串行票、连续 session、SSE、取消与恢复按钮均已落地。

补齐结果：Codex 的 cwd 与 `workspace-write` 已收窄至确认目录，turn 后 Git 检查继续作为纵深防御；服务重启会把失去进程的 `running` 恢复为保留 session 的可重试状态；adapter 已覆盖 session、message、tool、file、completed、failed 和未知 JSONL 事件。目标项目 Skill 使用可追溯绝对入口路径读取。
