# TICKET-06 在同一需求中继续产品返修并记录协作时间

**Type:** AFK  
**Status:** Ready

## Parent

[SPEC-20260914-001 Product Agent Workbench MVP](../spec.md)

## What to build

在候选实现进入待产品验收后，允许产品输入一次具体的返修结果。工作台把返修追加为新的串行执行票，继续使用原 Requirement、需求分支和 Codex session，而不重新进行项目适配、不创建第二条活跃需求。

返修必须保留已确认的修改范围和生效规则。新一轮 Codex turn 完成后重跑第五票已有的验证矩阵；返修失败或取消时，保留可恢复状态和原始候选实现证据。

同时补齐 MVP 可证明的协作指标：需求建立时间、首次可用预览时间、产品验收时间和已完成修改轮次。未采集的值显示为 `uncollected`，没有基线时不生成提效百分比。

## User stories covered

- US-36：每个 Requirement 保留一个连续 Codex session。
- US-40：完成的 Agent 调整记录为修改轮次。
- US-41：预览不符合意图时在同一对话和分支继续返修。
- US-56、US-57：记录真实时间点，不伪造未收集指标。
- US-59、US-60：刷新后恢复返修状态，失败可重试。

## Acceptance criteria

- [ ] 待产品验收页面提供“继续返修”输入，产品文本只作为数据，不能被拼接成 Shell 命令。
- [ ] 提交返修后生成一张新的 queued Ticket，并恢复到 implementation 阶段。
- [ ] 返修继续使用原 Requirement ID、Spec ID、需求分支和 Codex session ID。
- [ ] 每轮仍只有一张 active Ticket，且只能修改原确认范围。
- [ ] 返修 turn 成功后增加修改轮次并自动重跑验证；取消或失败不增加完成轮次。
- [ ] Requirement 持久化需求建立、首次可用预览和产品验收时间；页面显示修改轮次和可计算的 lead time。
- [ ] 未采集时显示 `uncollected`，没有对照基线时不显示任何提升百分比。
- [ ] 新增的领域状态、服务端副作用、会话续接与恢复边界必须有简体中文注释；修改旧调用链时同步补齐相关边界注释。
- [ ] 自动化测试覆盖返修续接、刷新恢复、取消/失败和指标缺失四类路径。

## Blocked by

None - can start immediately.

## Explicitly excluded

- 不实现多 Requirement 并发、worktree 或并行 Ticket。
- 不解决 `docs/issues/0001-0004` 的基础设施增强。
- 不做跨需求分析仪表盘或虚构效率收益。
