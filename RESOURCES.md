# 可恢复流式对话 Resources

## Knowledge

- [OpenAI Codex app-server README](https://github.com/openai/codex/blob/main/codex-rs/app-server/README.md)
  app-server 的 JSON-RPC 生命周期、Thread/Turn API、`item/agentMessage/delta` 与 `item/completed` 语义。当前链路的上游协议依据。
- [TanStack Query: QueryClient](https://tanstack.com/query/latest/docs/framework/react/reference/classes/QueryClient)
  `setQueryData`、`invalidateQueries` 与 `refetchQueries` 的官方接口说明。用于核对同步不可变投影和快照校准。
- [MDN: Using server-sent events](https://developer.mozilla.org/en-US/docs/Web/API/Server-sent_events/Using_server-sent_events)
  SSE 与 `EventSource` 的权威参考，包括单向连接、事件格式、断线重连和关闭连接。用于理解为什么 SSE 适合发通知，却不等于持久状态。
- [React: memo](https://react.dev/reference/react/memo)
  React 官方的跳过无关重渲染说明。用于理解按消息 ID 订阅与稳定对象引用的价值。
- [`src/server/codex-app-server.ts`](./src/server/codex-app-server.ts)
  本项目的进程级 app-server Adapter，负责 JSON-RPC、Thread/Turn 生命周期、取消和清理。
- [`src/server/codex-adapter.ts`](./src/server/codex-adapter.ts)
  将 app-server delta 与完整事件分流，并为同一 Turn 内的 item 建立稳定 ID。
- [`src/server/agent-events.ts`](./src/server/agent-events.ts)
  50ms/4KB SSE 批处理、连续序号和终态前 flush。
- [`src/server/workbench-conversation.ts`](./src/server/workbench-conversation.ts)
  本项目按 ID 替换累计消息和活动状态，并在完整协议与 Turn 完成后开放确认。
- [`src/store/workbench-query.ts`](./src/store/workbench-query.ts)
  React Query 中的快照接收、对话投影、批次去重、缺口检测与 revision 防倒退。
- [`tests/workbench-query.test.ts`](./tests/workbench-query.test.ts)
  可执行证据：引用复用、Requirement 隔离、批次去重、缺口拒绝和 Markdown 围栏处理。

## Wisdom (Communities)

- [OpenAI Codex Discussions](https://github.com/openai/codex/discussions)
  与 SDK 使用者和维护者核对事件语义、恢复策略及实际集成问题。
- [React Discussions](https://github.com/reactjs/react.dev/discussions)
  核对渲染性能、组件边界和外部状态订阅的工程取舍。
