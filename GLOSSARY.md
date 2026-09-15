# 可恢复流式对话 Glossary

本词汇表固定当前项目和面试讲解中的用词，避免把服务端真值、网络通知与浏览器显示状态混在一起。

## Terms

**累计全文（cumulative text）**:
Adapter 将同一个 item 已收到的原始 delta 按顺序拼接后的正文。它会定期持久化，但仍要由 `item.completed` 全文做最终校准。
_Avoid_: 单次 delta、最终全文

**原始 delta（text delta）**:
app-server 的 `item/agentMessage/delta` 携带的可追加文字片段。同一 item 的 delta 必须按到达顺序拼接，粒度可能是一个字，也可能是一整段。
_Avoid_: 累计快照、逐 token 保证

**SSE 批次（stream batch）**:
后端在 50ms 或 4KB 窗口内合并的同 item delta。批次带 `sequenceStart` 和 `sequenceEnd`，供浏览器去重和发现缺口。
_Avoid_: 权威快照、持久事件日志

**Revision**:
一次批次或 Requirement 更新的新旧标记。浏览器拒绝比当前缓存更旧的快照，避免慢请求让页面倒退。
_Avoid_: SSE sequence；两者解决的问题不同

**权威快照（authoritative snapshot）**:
Requirement 中持久化的完整领域状态，是刷新、断线或客户端投影失效时的恢复来源。
_Avoid_: SSE 事件、浏览器缓存

**最终全文（completed text）**:
`item/completed` 中同一个 Agent message item 的完整正文。它按稳定 item ID 替换临时累计内容，负责最终校准。
_Avoid_: 再追加一次、普通 delta
