# TICKET-07 生成可审查交付文件并完成真实目标冒烟验收

**Type:** AFK  
**Status:** Blocked

## Parent

[SPEC-20260914-001 Product Agent Workbench MVP](../spec.md)

## What to build

把工作台中已验收的交付卡从“只存在本地 Requirement JSON 的页面摘要”提升为前端可以在目标仓库审查的跟踪文件。最终 Spec、Tickets、验证矩阵、未验证风险和 Frontend Delivery Handoff 必须描述同一个产品行为，且与业务代码纳入同一个候选提交。

Handoff 文件不嵌入它所在 commit 的完整 hash，因为 commit hash 取决于包含该文件的 tree，这会形成不可解的自引用。文件使用“承载本文件的 commit”作为稳定 revision 语义；commit 完成后，Requirement JSON 和页面交付卡记录实际 hash。

为 Agent 会话转接提供独立入口：调用本地 `$handoff` Skill 在系统临时目录生成紧凑上下文。该文件不提交到目标仓库，页面文案必须明确区分“Agent 会话转接”和“Frontend Delivery Handoff”。

最后用配置的 `Mysterious-Realm-v2 / front` 和一个最小文案需求跑通真实冒烟流程，保留分支、session、验证、预览和交付证据。冒烟验收用 `agent-browser` 进行语义化探索与视觉确认，再用 Playwright 固化稳定断言；不使用 Computer Use。

## User stories covered

- US-51–US-55：稳定候选提交、完整交付卡、意图一致和前端合并所有权。
- US-58：形成可用于面试讲解的可审计工作流证据。
- US-62、US-63：支持 Agent 会话转接，并与前端交付 Handoff 区分。

## Acceptance criteria

- [ ] 产品验收时生成可追踪的 Spec、Ticket 和 Frontend Delivery Handoff 文件，并与业务代码一起进入同一稳定候选提交。
- [ ] 交付内容包含 Requirement/Spec 身份、需求分支、承载 commit 的 revision 语义、改动文件、diff 统计、执行票结果、验证矩阵、未验证项和责任边界。
- [ ] Frontend Delivery Handoff 与业务代码位于同一个 commit；提交完成后 Requirement JSON 记录该 commit 的实际 hash，刷新或重启服务后仍可恢复。
- [ ] 验收流程不 push、不创建 PR、不合并，页面明示“产品验收不等于生产批准”。
- [ ] “Agent 会话转接”通过本地 `$handoff` Skill 在系统临时目录生成，不被纳入 Git commit。
- [ ] UI 和领域模型使用两个明确名称，不把 Agent 会话转接文档称为 Frontend Delivery Handoff。
- [ ] 使用真实 `Mysterious-Realm-v2 / front` 完成一次最小需求冒烟流程，并记录未能验证的外部边界，不伪造通过证据。
- [ ] UI 探索和视觉确认使用 `agent-browser` 的 Accessibility Tree 和语义化引用；稳定回归使用 Playwright，不使用 Computer Use。
- [ ] 新增的交付边界、Git 原子性、临时文件生命周期和 Skill 调用代码必须有简体中文注释；修改旧调用链时同步补齐相关边界注释。
- [ ] 自动化测试覆盖交付文件生成、提交失败不写入 accepted、会话转接文件不入 Git 和重启恢复。

## Blocked by

- [TICKET-06 在同一需求中继续产品返修并记录协作时间](./TICKET-06-continue-product-refinement-and-measure-lead-time.md)

## Explicitly excluded

- 不实现 `docs/issues/0001-0004`；这些问题仍作为独立后续工作。
- 不扩展为通用发布系统，不连接 GitHub、CI 或部署平台。
- 不实现历史需求搜索、分析仪表盘或跨项目交付。
