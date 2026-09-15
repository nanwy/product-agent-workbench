# 可恢复流式对话 Resources

## Knowledge

- [OpenAI Codex TypeScript SDK README](https://github.com/openai/codex/blob/main/sdk/typescript/README.md)
  官方 SDK 对 `runStreamed()`、结构化事件和 Thread 恢复的说明。用于理解工作台上游事件从哪里来。
- [MDN: Using server-sent events](https://developer.mozilla.org/en-US/docs/Web/API/Server-sent_events/Using_server-sent_events)
  SSE 与 `EventSource` 的权威参考，包括单向连接、事件格式、断线重连和关闭连接。用于理解为什么 SSE 适合发通知，却不等于持久状态。
- [MDN: requestAnimationFrame](https://developer.mozilla.org/en-US/docs/Web/API/Window/requestAnimationFrame)
  浏览器绘制前回调的权威参考。用于理解为什么把高频文本更新合并到一帧只提交一次。
- [React: memo](https://react.dev/reference/react/memo)
  React 官方的跳过无关重渲染说明。用于理解按消息 ID 订阅与稳定对象引用的价值。
- [`src/server/codex-adapter.ts`](./src/server/codex-adapter.ts)
  本项目将 Codex SDK 事件归一化，并为同一 Turn 内的 item 建立稳定 ID。
- [`src/server/workbench-conversation.ts`](./src/server/workbench-conversation.ts)
  本项目按 ID 替换累计消息和活动状态，并在完整协议与 Turn 完成后开放确认。
- [`src/store/conversation-store.ts`](./src/store/conversation-store.ts)
  本项目把权威累计快照安全转换为浏览器 delta，并按动画帧合并更新。
- [`tests/conversation-store.test.ts`](./tests/conversation-store.test.ts)
  可执行证据：累计快照转 delta、终态前 flush、历史消息引用不变和 Markdown 围栏处理。

## Wisdom (Communities)

- [OpenAI Codex Discussions](https://github.com/openai/codex/discussions)
  与 SDK 使用者和维护者核对事件语义、恢复策略及实际集成问题。
- [React Discussions](https://github.com/reactjs/react.dev/discussions)
  核对渲染性能、组件边界和外部状态订阅的工程取舍。
