# 0001 让生效 Skill 与执行 Agent 的真实能力一致

**Type:** AFK
**Status:** Done

## What to build

把工作台自身的 SDD 阶段与 Codex 可调用的 Skill 分开建模。`grilling → to-spec → to-tickets → implement` 应表达工作台已经完成或正在执行的流程阶段，不能仅凭硬编码名称出现在执行 Agent 的“生效 Skills”中。

启动执行票时，只向 Agent 声明已经从目标项目发现、具有有效 `SKILL.md` 来源且为当前需求激活的 Skill。用户级或系统级 Skill 若未来需要参与，也必须以 Codex 会话实际暴露的能力为准，不能由工作台单方面宣称可用。

该切片完成后，工作台页面仍能展示 SDD 阶段，但执行 prompt 不再触发“Skill 未出现在当前可用 Skills 中”的降级路径。

## Acceptance criteria

- [x] 工作流阶段和可用/生效 Skill 使用不同的领域字段与页面文案。
- [x] 执行 prompt 的“生效 Skills”只包含具有可追溯 Skill 来源且当前激活的条目。
- [x] `to-spec`、`to-tickets`、`implement` 不会仅因工作流阶段名称相同而被发送给执行 Agent。
- [x] 目标项目要求的 `product-copy-change` 仍能被发现、展示并写入执行 prompt。
- [x] 自动化测试覆盖“工作流阶段存在但对应 Skill 不可调用”和“目标项目 Skill 可调用”两种情况。

## Blocked by

None - can start immediately.
