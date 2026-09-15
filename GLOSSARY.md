# 可恢复流式对话 Glossary

本词汇表固定当前项目和面试讲解中的用词，避免把服务端真值、网络通知与浏览器显示状态混在一起。

## Terms

**累计快照（cumulative snapshot）**:
同一消息截至当前时刻的完整正文；后一次内容包含前一次内容时，服务端按稳定 ID 替换原消息。
_Avoid_: 字符流、单次增量

**Delta**:
在确认新快照以客户端有效内容开头后，从快照尾部截取出的新增文本；它是可丢弃的显示优化，不是业务真值。
_Avoid_: 新快照、唯一真值

**Pending delta**:
客户端已经接收、但尚未在下一动画帧 flush 到 Store 的 delta。计算后续 delta 时，它也属于客户端已有内容。
_Avoid_: 未接收内容、丢失内容

**Effective content**:
页面已提交正文与 pending delta 的拼接结果，即客户端实际已经拥有的完整内容。新快照必须以它开头，才能继续安全截取 delta。
_Avoid_: 仅页面可见正文

**权威快照（authoritative snapshot）**:
Requirement 中持久化的完整领域状态，是刷新、断线或客户端投影失效时的恢复来源。
_Avoid_: SSE 事件、浏览器缓存
