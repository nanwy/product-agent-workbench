# TICKET-04 在安全需求分支中连续执行 Codex 票

**Type:** AFK  
**Status:** Partial

## Parent

[SPEC-20260914-001 Product Agent Workbench MVP](../spec.md)

## What to build

Spec 与边界确认后，记录目标仓库的 base branch、base revision 和起始 working tree，在干净起点创建一条 `product/<spec-id>-<slug>` 需求分支。已有改动或正在进行的 merge/rebase 必须阻止执行，不 stash、reset 或静默纳入他人工作。

每个 Requirement 使用一个连续 Codex session，按执行票串行运行 turn。工作台把 CLI JSONL 归一化为稳定事件，通过 SSE 展示 session、当前票、消息、文件变化、完成、失败和取消；取消只停止当前 turn，保留分支与 session 供恢复。

## User stories covered

- US-33～US-42：需求分支、Git 起点、连续 Session、串行执行、事件、取消和改动文件。
- US-60：执行失败后可恢复，不要求重建需求。

## Acceptance criteria

- [ ] 执行前记录 base branch、base revision 和完整 working-tree 状态。
- [ ] 脏工作区、merge/rebase 或无法安全分离的变更会阻止建分支，且不破坏现有内容。
- [ ] 首次执行创建需求分支；失败或取消后的重试复用同一 Requirement 和分支。
- [ ] Codex 以目标仓库为 cwd，首票创建 session，后续票通过 resume 使用同一 session ID。
- [ ] 只有一张 active Ticket；完成后自动推进下一张，失败时当前票标为 blocked。
- [ ] 执行上下文只声明真实可用的目标项目 Skill，并让 Codex 能读取生效规则和 Skill 指令。
- [ ] 每个 turn 前后核对分支、HEAD 和修改范围；范围外修改阻止继续执行并明确显示文件。
- [ ] SSE 只推送状态变化，刷新后从持久化 Requirement 恢复，不重复执行写动作。
- [ ] 取消会终止工作台拥有的 Codex 进程组，保留 session ID，并允许继续当前票。
- [ ] 自动化测试覆盖建分支、脏工作区保护、多票串行、session 续接、失败、取消和范围越界。

## Blocked by

- TICKET-03 只读探索规则并确认 Spec 与执行票。

## Audit result · 2026-09-15

主功能已实现：Git 起点记录、脏工作区阻断、需求分支、串行票、连续 session、SSE、取消与恢复按钮均已落地。

仍缺：

- Codex 的 workspace-write 沙箱覆盖整个目标仓库；修改范围目前依赖 prompt 和 turn 后 Git 检查，不能阻止范围外文件先被写入。
- 工作台发现范围外修改后会阻止后续执行，但不会也不应擅自回滚，因此需要明确的人工恢复路径。
- Node 服务若在 Codex 运行中退出，持久状态可能停在 `running`，重启后没有内存队列且 UI 不提供恢复按钮。
- Codex adapter 测试没有完整覆盖 message、tool、file、failure 等 JSONL 归一化事件。
- “执行上下文只包含真实可用 Skill”仍受 TICKET-03 的 Skill 真值缺口影响。

