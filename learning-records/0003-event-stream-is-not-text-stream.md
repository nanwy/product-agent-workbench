# 事件流不等于文字流

用户通过追问事件语义并亲自抓取运行日志，确认当前 `codex exec --experimental-json` 会流式返回结构化生命周期事件，但 assistant 正文由单个 `item.completed` 整段返回。后续可以直接进入“如何接入 app-server delta”或“如何诚实描述现有实现”，不再把测试夹具的增长快照误认为 SDK 的真实 token 流。

## Evidence

用户提供的日志中，一份完整 Spec 全部位于同一个 `item.completed.item.text`，随后立即出现 `turn.completed`，没有 assistant `item.updated` 或 delta 事件。
