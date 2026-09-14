# SPEC-20260914-001 执行票

这个目录保存 MVP 全部执行票。TICKET-01～05 根据原始 Spec、提交边界和已确认的
`agent-browser → Playwright` UI 验收策略重建，避免票面继续只存在会话上下文中。

| Ticket | Type | Blocked by | 可演示结果 |
| --- | --- | --- | --- |
| [TICKET-01](./TICKET-01-establish-local-workbench-and-managed-preview.md) | AFK | 无 | 本地工作台托管目标项目，并显示真实预览与进程事实 |
| [TICKET-02](./TICKET-02-capture-and-persist-product-intent.md) | AFK | TICKET-01 | 产品需求经过 Grilling 后持久化并可刷新恢复 |
| [TICKET-03](./TICKET-03-explore-rules-and-confirm-the-plan.md) | AFK | TICKET-02 | 只读探索生成可确认的规则、Skills、Spec 和串行执行票 |
| [TICKET-04](./TICKET-04-execute-tickets-in-a-safe-codex-session.md) | AFK | TICKET-03 | 安全需求分支中连续执行多张票并可取消、恢复 |
| [TICKET-05](./TICKET-05-verify-the-candidate-and-gate-product-acceptance.md) | AFK | TICKET-04 | 宿主验证、agent-browser 视觉验收和产品门禁形成完整矩阵 |
| [TICKET-06](./TICKET-06-continue-product-refinement-and-measure-lead-time.md) | AFK | 无 | 产品在验收前发起返修，同一 Requirement/分支/Session 继续执行，页面展示真实时间指标 |
| [TICKET-07](./TICKET-07-package-auditable-delivery-and-prove-the-real-target.md) | AFK | TICKET-06 | 交付文件可在目标仓库审查，Agent 可转接会话，真实目标冒烟流程可重现 |

`docs/issues/0001-0004` 是真实运行暴露的独立基础设施问题，不属于这两张票的实现范围。
