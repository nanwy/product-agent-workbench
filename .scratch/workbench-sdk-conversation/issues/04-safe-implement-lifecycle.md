# 04 — 安全 Implement 与完整生命周期

**What to build:** 用户确认 Tickets 并点击“开始实现”后，工作台创建独立的 `workspace-write` Codex Thread，把已确认的完整对话、Spec、Tickets、生效规则和边界交接进去，并自动加载 `implement` Skill。实现说明、工具活动、文件变更、验证、失败、取消、重试和返修继续显示在同一逻辑对话；产品验收后由工作台统一提交候选代码和确认版交付资料。

**Blocked by:** 03 — 对话式 Tickets 门禁.

**Status:** completed

- [x] 本地 `implement` Skill 明确禁止 Agent commit、push、创建 PR 或发布；稳定 commit 仍由工作台在验证和产品验收后创建。
- [x] Implement 使用独立 `workspace-write` Thread，规划 Thread 保持只读；两者拥有不同 Thread ID，但归属于同一逻辑对话。
- [x] 实现 Thread 收到当前确认版对话摘要、Spec、Tickets、生效规则、边界、目标项目配置和修改范围。
- [x] 用户未确认 Tickets 或未点击“开始实现”时，服务端拒绝创建需求分支、写入 Thread 或任何目标项目变更。
- [x] Agent 解释以消息显示；命令、工具、文件和验证事件以同一时间线中的可折叠活动显示。
- [x] 取消、失败和超时在 flush 已接收文本后记录独立终态，并能从保存的 Requirement、需求分支和 Session 重试。
- [x] Implement 开始后的新产品意见进入返修轮次，保留已确认的 Grilling、Spec、Tickets 和上一版验证证据。
- [x] 自动验证仍通过宿主验证矩阵、真实预览和范围检查完成；Agent 自述不能替代验证事实。
- [x] 产品验收后，当前确认版 Spec 和 Tickets 被复制到目标项目授权范围内的交付资料，再由工作台创建唯一稳定 commit。
- [x] 工作台不会自动 push、merge、部署或发布，交付结果保持为前端可审查的需求分支、commit、差异和 Handoff。
- [x] 从创建需求、四道门禁、执行取消/恢复、返修、验证到最终验收的完整端到端流程通过。
- [x] 类型检查、全部单元/集成测试、生产构建和桌面/响应式视觉检查通过。
