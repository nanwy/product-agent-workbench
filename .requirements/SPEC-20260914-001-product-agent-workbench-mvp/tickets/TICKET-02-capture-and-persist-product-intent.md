# TICKET-02 建立产品需求、Grilling 门禁与可恢复状态

**Type:** AFK  
**Status:** Complete

## Parent

[SPEC-20260914-001 Product Agent Workbench MVP](../spec.md)

## What to build

允许产品提交需求标题、目标站内页面和期望结果，并建立唯一活动的 Requirement 聚合。系统先通过 `grilling` 的最小设计树确认成功结果、写入边界和验收信号；全部问题确认前不得生成 Spec 或修改目标代码。

Requirement 及阶段证据保存到本地 JSON。所有写动作需要幂等键并采用原子写入，刷新页面或网络重试不能重复创建需求、重复追加证据或丢失当前阶段。

## User stories covered

- US-1～US-6：自然语言产品需求、通用输入、显式阶段和阶段证据。
- US-23、US-24：产品意图与工程边界分离，冲突需要确认。
- US-59～US-61：刷新恢复、失败可重试、单活动需求。

## Acceptance criteria

- [ ] 页面收集标题、以 `/` 开头的目标页面和期望结果，并把输入作为数据而非命令。
- [ ] 创建 Requirement 时生成稳定 Requirement ID、Spec ID、时间戳、目标配置快照和初始证据。
- [ ] Grilling 至少确认成功结果、修改边界和验收信号；缺少回答时不能继续。
- [ ] 用户确认的修改范围只能等于或收窄配置范围，绝对路径和 `../` 目录穿越被拒绝。
- [ ] Requirement 使用原子 JSON 写入，进程重启或浏览器刷新后可以恢复当前需求。
- [ ] 每个阶段以输入、动作、输出和记录时间追加证据，不以日志文案代替领域状态。
- [ ] 所有写 Server Function 使用幂等键；同一动作的并发重试只产生一次副作用。
- [ ] 自动化测试覆盖输入校验、Grilling 未完成门禁、范围收窄、重复提交和刷新恢复。

## Blocked by

- TICKET-01 建立本地工作台并托管真实目标项目预览。

## Closure · 2026-09-15

输入校验、三道 Grilling、范围收窄、JSON 原子写入、证据追加、幂等去重和浏览器刷新恢复均已落地。服务端现在拒绝覆盖未验收的活动 Requirement；测试覆盖输入错误、Grilling 缺答、范围穿越、唯一活动需求，并通过全新 `RequirementStore` 实例证明服务内存丢失后仍从磁盘恢复同一聚合。
