# SPEC-20260916-002 执行票

| Ticket | Blocked by | 可验证结果 |
| --- | --- | --- |
| [TICKET-01](./TICKET-01-run-recoverable-codex-turns-through-app-server.md) | 无 | 所有 Codex 阶段通过 app-server 完成启动、恢复、取消与失败清理 |
| [TICKET-02](./TICKET-02-unify-browser-server-state-in-react-query.md) | 无 | React Query 成为浏览器唯一服务端状态来源 |
| [TICKET-03](./TICKET-03-stream-recoverable-agent-text-through-sse.md) | TICKET-01、TICKET-02 | app-server 正文经批处理和 SSE 实时投影，并在断线后恢复 |

TICKET-01 与 TICKET-02 可以独立推进；TICKET-03 需要两者都完成。
