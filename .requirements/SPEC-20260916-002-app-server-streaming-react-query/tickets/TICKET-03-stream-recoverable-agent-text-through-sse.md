# 03 — 通过 SSE 展示可恢复的实时 Agent 正文

**What to build:** 将 app-server 提供的 Agent 正文 delta 在后端合并成有序批次，通过 SSE 投影到 React Query，使产品能看到真实的渐进正文和正确的工具时间线；当连接中断、事件重复或缺失时，以完成全文和服务端快照恢复正确结果。

**Blocked by:** 01 — 使用 app-server 完成可恢复的 Codex 执行；02 — 将浏览器服务端状态统一迁入 React Query

**Status:** complete

- [x] Agent 正文按 50 毫秒或 4KB 的页面阈值广播，并按 500 毫秒或 32KB 的持久化阈值保存累计全文。
- [x] 非正文事件和所有终态会先清空正文缓冲，保证 Agent 解释、工具活动和完成状态的真实顺序。
- [x] SSE 批次携带产品需求、Turn、Item、连续序号和快照 revision，并提供周期心跳。
- [x] React Query 在一次不可变更新中应用批次，忽略重复序号，发现缺口时停止猜测并重新获取权威快照。
- [x] Agent 消息完成时使用权威全文覆盖临时拼接内容，不重复追加完整消息。
- [x] SSE 健康时停止三秒常驻轮询；断线时启用降级轮询，重连后先校准快照再恢复实时投影。
- [x] 较旧 revision 的迟到请求不能覆盖较新的 mutation 或流式状态。
- [x] fake app-server 浏览器测试证明正文渐进出现、最终内容稳定、取消和刷新恢复正确；真实 app-server 冒烟记录实际 delta 粒度。

**Verification:** `pnpm typecheck`、72 个 Vitest 用例、生产构建和 Playwright 浏览器验收均通过。2026-09-16 使用项目固定的真实 `codex app-server` 运行只读临时 Turn，回复“通过”产生 1 个正文 delta、共 2 个字符并正常完成；实现按上游真实粒度展示，不伪造逐字动画。
