# 01 — SDK 对话内核与真实 Grilling

**What to build:** 将中间区域变为由 Codex SDK 驱动、可恢复且流畅渲染的统一对话。创建产品需求后，系统在只读规划环境中自动加载 `grilling` Skill，按 design tree 的当前 frontier 分轮追问；用户始终通过一个 composer 回答，并在 Agent 给出共同理解摘要后显式确认。服务端 Requirement 继续掌握门禁真值，Zustand 只维护客户端投影和流式缓冲。

**Blocked by:** None — can start immediately.

**Status:** completed

- [x] `@openai/codex-sdk` 负责 Thread 创建、流式事件、恢复和 AbortSignal 取消，不再由应用直接启动 `codex exec` 和解析原始 JSONL。
- [x] 现有实现执行的 Session 续接、事件归一化、取消、失败恢复、输出脱敏和修改范围保护继续通过回归测试。
- [x] Requirement 持久化一条逻辑对话、规划 Thread ID、消息、segments、活动、门禁事件和终态；刷新后可以完整恢复。
- [x] 旧的固定 Grilling 和单 Session Requirement 无需删除数据即可迁移并读取。
- [x] 创建需求后自动在只读 Thread 中明确加载 `grilling` Skill，固定三项只作为最低契约覆盖，不再作为三个固定输入框。
- [x] 每个 Agent 回合一次询问当前全部 frontier 问题，每题有编号与推荐答案；未解锁问题不提前出现。
- [x] frontier 为空时生成共同理解摘要，但只有用户点击“确认共同理解”才能完成 Grilling 门禁。
- [x] 对话顶部只显示紧凑的四阶段状态；问题、回答、活动和阶段详情全部位于同一时间线。
- [x] Zustand 使用按用途拆分的 selector；流式 delta 以 16–33ms 窗口或动画帧合并，完成、失败、取消前强制 flush。
- [x] 更新当前 assistant 消息不会重渲染已经完成的历史消息；未闭合代码块不反复执行完整语法高亮。
- [x] 用户停留底部时自动跟随，主动上滚后停止强制滚动并出现“回到底部”入口。
- [x] SDK 不可用、Turn 失败或取消时保留对话和阶段，并提供重试当前阶段；不回退固定表单。
- [x] 类型检查、相关单元测试、生产构建及 Grilling 浏览器流程通过。
