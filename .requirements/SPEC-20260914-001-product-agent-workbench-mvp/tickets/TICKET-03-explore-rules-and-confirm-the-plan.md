# TICKET-03 只读探索规则并确认 Spec 与执行票

**Type:** AFK  
**Status:** Partial

## Parent

[SPEC-20260914-001 Product Agent Workbench MVP](../spec.md)

## What to build

Grilling 确认后，对目标范围执行只读探索：定位目标路由和候选代码，解析根 `AGENTS.md` 与最接近修改范围的目录规则，发现目标项目 Skills，并以可追溯原因区分可用与生效状态。

探索结果形成候选 Spec、验收条件和一到多张串行执行票。产品显式确认前不得进入实现；鉴权、数据库、共享契约、公共组件、跨应用或发布影响必须插入边界确认。

## User stories covered

- US-15～US-22：规则与 Skill 的发现、作用域、状态和来源。
- US-23～US-32：只读探索、Spec、执行票、串行约束和边界确认。

## Acceptance criteria

- [ ] 探索只读取已确认范围内的源码，并记录检查数量、候选代码位置和现有行为证据。
- [ ] 根 `AGENTS.md` 始终生效，嵌套规则只选择最接近拟修改范围的一层。
- [ ] 发现目标仓库标准位置的全部 Skills；默认只读取 frontmatter，激活时保留可追溯来源。
- [ ] 工作流阶段与执行 Agent 真正可调用的 Skills 分开建模和展示。
- [ ] Spec 包含期望行为、目标页面、确认范围、验收条件和探索证据。
- [ ] 简单需求生成一张执行票，复合结果拆成多张可独立验证且串行的执行票。
- [ ] 高风险影响或规则冲突进入边界确认，不由系统静默裁决。
- [ ] 产品确认 Spec、执行票及必要边界前，不开放候选实现。
- [ ] 自动化测试覆盖规则作用域、Skill 激活/不激活、单票/多票和边界冲突。

## Blocked by

- TICKET-02 建立产品需求、Grilling 门禁与可恢复状态。

## Audit result · 2026-09-15

主功能已实现：只读代码定位、根/最近目录规则、Skill 元数据发现、候选 Spec、多票切分和边界确认均已落地。

仍缺：

- 工作流阶段虽然在 UI 中按 `kind` 分组，但仍与目标项目 Skill 共用一个 `Requirement.skills` 字段。
- 执行 prompt 会发送所有 active Skill 名称，包括 `to-spec`、`to-tickets`、`implement` 等工作流阶段；这些不等于 Codex 真正可调用的 Skill。
- 目标项目 Skill 只读取 frontmatter，当前执行链没有显式加载或传入已激活 Skill 的完整指令。
- 浏览器级测试未覆盖单票需求、高风险边界和 Spec/仓库规则冲突路径。

