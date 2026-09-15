---
id: SPEC-20260916-002-app-server-streaming-react-query
title: 使用 app-server 和 React Query 建立可恢复的实时对话链路
status: ready-for-agent
created: 2026-09-16
updated: 2026-09-16
owner: 产品
labels:
  - ready-for-agent
---

# 使用 app-server 和 React Query 建立可恢复的实时对话链路

## Problem Statement

工作台当前通过 TypeScript Codex SDK 的流式接口运行执行 Agent，但该接口实际包装 Codex Exec 的结构化 JSON 事件。它能陆续报告会话、工具、文件和完成状态，却通常只在 Agent 消息完成时提供整段正文。前端虽然为累计快照准备了增量缓冲和按动画帧合并，但真实 Codex 正文没有细粒度 delta，这些机制无法降低首段正文的等待时间。

实时数据链路还存在多份状态。服务端返回完整工作台快照，页面将其复制到 React 状态，再把对话投影到 Zustand；SSE 到达时不消费事件内容，而是重新请求整份快照。同时，三秒轮询始终运行。mutation、SSE 刷新和轮询可能并发返回，使同一服务端事实进入多套状态更新路径。

现有 SSE 是单进程内通知，不携带可恢复序号，也不支持重放。它依靠完整 JSON 快照和轮询实现最终收敛。这种恢复原则适合本地 MVP，但需要明确区分实时增量与权威快照，并避免为了逐字效果引入数据库、事件日志或复杂的常驻进程池。

## Solution

将所有 Codex 阶段迁移到一个 app-server Adapter。每个 turn 启动一个项目固定版本的 `codex app-server` 子进程，通过标准输入输出上的 JSON-RPC 完成初始化、Thread 启动或恢复、Turn 启动、通知消费、取消和清理。Adapter 对调用方继续提供统一的运行、取消、活动状态和可恢复 Thread 身份，不让 app-server 的协议形状泄漏到规格驱动开发流程。

Adapter 消费 Agent 消息 delta，并在后端按 Turn 和 Item 聚合。面向页面的批次每 50 毫秒或累计 4KB 时发送；面向本地 JSON 真值的累计正文每 500 毫秒或累计 32KB 时持久化。工具开始、完成、失败、取消和 Turn 终态会先清空此前正文缓冲，再立即处理，保证时间线顺序和最终内容完整。

继续使用 SSE 作为服务端到浏览器的单向实时通道，产品回复、确认和取消仍通过普通 HTTP mutation 完成。SSE 发送带 Requirement、Turn、Item 和单调序号的有序事件批次。正文 delta 直接投影到 React Query；完成事件携带权威全文并覆盖临时拼接结果。断线、序号缺口或重连时重新获取完整快照，不为 MVP 建立可重放事件日志。

React Query 成为浏览器中唯一的服务端状态来源。低频工作台状态和高频对话状态使用不同 Query Key；loader 只预取并水合 Query Cache，不再创建独立快照状态；Zustand 对话 Store 被移除。输入框、历史选择、面板开关等纯界面状态继续由组件本地管理。

本次改造参考 Multica 的 app-server Adapter、后端批处理和 Query Cache 实时投影方式，但缩小为本地单用户 MVP：每次 Turn 一个 app-server 进程、全局最多一个活动 Turn、没有数据库、没有多实例协调，也不伪造打字机动画。当上游只产生一个大 delta 时，页面立即显示整段内容，并保持最终快照正确。

## User Stories

1. As a 产品经理, I want to see Agent text appear as app-server supplies it, so that I can follow useful progress before a long turn completes.
2. As a 产品经理, I want tool calls and file changes interleaved with Agent text in their real order, so that I can understand what the execution Agent is doing.
3. As a 产品经理, I want a complete response to appear immediately when the upstream sends one large delta, so that the interface does not delay content with a fake typing animation.
4. As a 产品经理, I want planning, Grilling, Spec and implementation to use the same Codex transport, so that different stages do not exhibit incompatible streaming and recovery behavior.
5. As a 产品经理, I want a page refresh to recover the complete conversation, so that transient streaming state is never the only copy of important content.
6. As a 产品经理, I want an interrupted SSE connection to recover automatically, so that a temporary connection failure does not require restarting the Requirement.
7. As a 产品经理, I want missing real-time events to converge to the server snapshot, so that a visually smooth stream cannot leave the final conversation incomplete.
8. As a 产品经理, I want completed messages to be checked against the authoritative full text, so that duplicate, delayed or missing delta events cannot corrupt the answer.
9. As a 产品经理, I want cancelling a Turn to stop new output promptly, so that an incorrect execution does not continue modifying the target project.
10. As a 产品经理, I want cancellation to preserve text already received, so that useful context is not erased when I stop a Turn.
11. As a 产品经理, I want retry and continuation to resume the correct planning or implementation Thread, so that the established project context remains available.
12. As a 产品经理, I want the current single-Turn behavior to remain predictable, so that this transport migration does not introduce competing Agents.
13. As a 产品经理, I want a clear failure when app-server cannot start or complete its handshake, so that environment problems are not mistaken for Agent reasoning failures.
14. As a 使用者, I want SSE to stop continuous polling while it is healthy, so that the workbench avoids redundant full-snapshot requests.
15. As a 使用者, I want degraded polling while SSE is unavailable, so that the page still reaches the correct state.
16. As a 使用者, I want reconnecting SSE to trigger a fresh authoritative snapshot, so that events missed during disconnection are recovered.
17. As a 使用者, I want old query responses rejected when a newer revision is visible, so that a slow poll cannot overwrite recent output.
18. As a 使用者, I want historical Requirements isolated by Query Key, so that selecting history cannot overwrite the active Requirement conversation.
19. As a 使用者, I want only the changing conversation message to update during streaming, so that a long history does not repeatedly rerender.
20. As a 使用者, I want unfinished Markdown code fences displayed safely during streaming, so that code blocks do not repeatedly change structure or disappear.
21. As a 使用者, I want automatic scrolling only while I remain near the bottom, so that reading older messages is not disrupted by new delta events.
22. As a 系统维护者, I want one stable internal Codex event model, so that app-server protocol changes remain local to its Adapter.
23. As a 系统维护者, I want small text fragments batched before broadcast, so that high-frequency model output does not cause excessive network messages or renders.
24. As a 系统维护者, I want cumulative text persisted less frequently than it is displayed, so that real-time output does not rewrite the full Requirement JSON for every token fragment.
25. As a 系统维护者, I want every event within a Turn to have a monotonic sequence, so that duplicates and gaps can be detected deterministically.
26. As a 系统维护者, I want non-text events to flush preceding text, so that batching never reorders explanations and tool activity.
27. As a 系统维护者, I want terminal events to flush all buffers, so that completion and failure cannot leave an unpersisted tail.
28. As a 系统维护者, I want the project to use a pinned local Codex executable, so that behavior does not depend on an arbitrary global installation.
29. As a 系统维护者, I want an explicit temporary SDK fallback, so that app-server rollout can be reversed without making fallback behavior automatic or invisible.
30. As a 系统维护者, I want the existing JSON snapshot to remain the durable truth, so that this MVP does not require a database or distributed event system.
31. As a 求职者, I want the implementation to distinguish lifecycle streaming from text streaming, so that I can explain the technical trade-off accurately in an interview.
32. As a 前端工程师, I want React Query to own all server state while component state owns only UI concerns, so that the data flow has one understandable source of truth.

## Implementation Decisions

- Preserve the existing Codex execution seam while replacing its default Adapter. Callers continue to run a Turn with working directory, prompt, resumable Thread identity, sandbox, cancellation signal and normalized event callback.
- Implement the app-server Adapter as a deep module. It owns child-process startup, JSON-RPC request correlation, initialization, Thread start or resume, Turn start, notification normalization, interruption, process cleanup and protocol failure reporting.
- Start one app-server process per Turn and terminate it after completion, failure or bounded cancellation. Do not build a persistent process pool.
- Preserve the global single-active-Turn invariant and the serial execution-ticket queue. app-server multiplexing is not exposed in this MVP.
- Start or resume a Thread before starting its Turn and surface the Thread identity immediately so it can be persisted before completion.
- Continue using the planning Thread identity for Grilling and Spec work and the existing Codex session identity for implementation and refinement. Do not perform a historical data migration for the unused implementation Thread field.
- On cancellation, persist the cancelled workflow state first, flush pending output, send a targeted Turn interrupt, wait up to two seconds, and then terminate the child process if completion is not acknowledged.
- Treat process exit, malformed JSON-RPC, failed initialization and broken protocol transport as a failed Turn. They must not leave the Requirement in a running state.
- Keep sandbox and repository constraints equivalent to the current Adapter: the target project is the working directory, planning may be read-only, implementation may use workspace write, network remains disabled by default, and interactive approval is not introduced.
- Depend directly on a pinned project-local Codex CLI runtime. Allow an explicit command override for controlled testing or local repair; do not silently fall back to a global binary.
- Make app-server the default backend. Keep the old TypeScript SDK Adapter temporarily behind an explicit backend environment setting. An app-server error remains visible and never triggers automatic fallback.
- Normalize app-server notifications into application-owned events containing Requirement identity, Turn identity, Item identity, Turn-wide sequence, event type, content or delta, activity status, and recorded time.
- Treat an Agent message delta as append-only transport data. Treat the completed Agent message text and persisted conversation snapshot as authoritative state.
- Aggregate text independently per active Item while retaining one global event order within the Turn. Flush a pending text aggregate before any following non-text event.
- Broadcast a stream batch every 50 milliseconds, when its encoded content reaches 4KB, or when a terminal or ordering boundary occurs.
- Persist cumulative message text every 500 milliseconds, when pending content reaches 32KB, or when a terminal or ordering boundary occurs.
- Preserve the existing redaction rule before content is made visible or durable. Message text remains untruncated; activity details retain their existing bounded history behavior.
- Extend SSE from an invalidation signal into a typed batch transport. A batch declares its Requirement and Turn scope, inclusive sequence range, snapshot revision and ordered events.
- Continue using HTTP mutations for replies, confirmations, retries and cancellation. SSE remains server-to-browser only.
- Add a periodic SSE heartbeat and retain response headers that prevent transformation and buffering by the local server path.
- Do not implement durable SSE replay. On connection, reconnection or a detected sequence gap, refetch the authoritative Query and resume from its revision.
- Replace constant three-second polling with degraded polling while SSE is disconnected. Add a low-frequency health reconciliation while connected for state changes that do not publish events.
- Add a monotonic revision to authoritative workbench and conversation snapshots. Query updates never replace a cached revision with an older revision.
- Use separate React Query entries for low-frequency workbench state and high-frequency Requirement conversation state. Both remain server state under one Query Client.
- Seed the Query Cache during server rendering and read it through queries from the first browser render. Do not retain a parallel React snapshot state.
- Apply each SSE batch to the conversation Query Cache with one immutable update. Preserve references for unaffected messages and specification versions.
- Deduplicate events at or below the last applied Turn sequence. A sequence gap marks the cache stale and triggers an authoritative refetch instead of guessing missing text.
- Apply message completion by replacing temporary content with the full completed text and setting terminal status. Never append the completed full text to accumulated deltas.
- Remove the Zustand conversation Store after its useful behavior has moved into Query Cache projection: batching compatibility, prefix-safe snapshot adoption, stable references, terminal flush, Markdown protection and Requirement isolation.
- Keep component-local state for input drafts, selected history, panel visibility, hydration-only presentation and preview refresh controls. These are not server state.
- Do not fake character-by-character output. The UI renders the batching granularity actually received from app-server and the upstream model backend.

## Testing Decisions

- The primary backend test seam remains the existing Codex Turn interface. A deterministic fake app-server process exercises the public Turn behavior without exposing JSON-RPC helpers to callers.
- Backend contract coverage includes initialization, Thread start, Thread resume, Turn start, multiple text deltas, complete Agent text, tool ordering, failed Turn, malformed response, process exit and bounded cancellation.
- The fake app-server remains alive for a complete Turn and supports correlated requests, notifications and interruption. The existing fake Exec fixture remains only for the temporary SDK fallback.
- Batching tests use controllable time rather than real sleeps. They verify the 50ms and 4KB broadcast thresholds, the 500ms and 32KB persistence thresholds, independent Item buffers, ordering-boundary flush and terminal flush.
- Event projection tests assert external cache behavior: ordered append, duplicate rejection, sequence-gap detection, completed-full-text replacement, stale-revision rejection and stable references for unaffected messages.
- SSE route coverage verifies typed event names or envelopes, heartbeat behavior, disconnect cleanup and disabled proxy buffering. It does not assert internal subscriber collection details.
- Query integration coverage verifies that loader hydration, mutation success, SSE batches, degraded polling and reconnect refetch all converge on the same Query Cache.
- Tests verify that an older in-flight refetch cannot overwrite a newer mutation response or stream revision.
- Conversation rendering coverage verifies progressive visible text, safe unfinished code fences, message-level rerender isolation, stick-to-bottom behavior and respect for manual upward scrolling.
- Existing Grilling, Spec generation, execution-ticket sequencing, refinement, cancellation, page refresh and Requirement recovery scenarios remain regression requirements.
- Browser-level acceptance uses the real control service with a fake app-server and observes the UI rather than private stores. It verifies progressive content followed by a stable final message and a correct refresh result.
- A real app-server smoke test is required when the pinned local runtime is available. It records whether the upstream produced multiple deltas or one aggregate delta; either is valid if the final content and lifecycle are correct.
- Type checking, unit tests, the existing end-to-end suite and a production build must pass before the migration is considered complete.

## Out of Scope

- Guaranteeing one character or token per event.
- Synthesizing a typing animation after complete text is received.
- Multiple simultaneous Codex Turns or parallel execution tickets.
- A persistent or pooled app-server process.
- Durable SSE event replay or a permanent delta event log.
- Introducing PostgreSQL, SQLite or another database.
- Multi-instance, serverless or distributed pub/sub deployment.
- Changing the target-project branch, validation, preview, acceptance or delivery ownership model.
- Migrating or deleting historical unused Thread fields.
- Automatically falling back to another Codex backend after app-server failure.
- Replacing SSE with WebSocket.
- Exposing raw app-server notifications directly to React components.
- Retaining every intermediate Markdown parse or visual frame as evidence.

## Further Notes

- This Spec is an incremental architecture change to the Product Agent Workbench MVP. Where the original Spec says chat delta persistence is not required or describes Codex CLI generically, this Spec takes precedence for the real-time conversation path.
- “Streaming” must be qualified in documentation and interviews. Lifecycle events may be incremental while Agent text is aggregated; the implementation can faithfully display only the granularity supplied by the selected backend.
- The app-server protocol supports Agent message delta notifications, but a particular model backend may still emit one large delta. That outcome is not a frontend defect if the full response appears immediately and final reconciliation succeeds.
- Multica is reference implementation material for process-per-Turn app-server usage, backend batching, ordered task messages and Query Cache updates. Its 500ms task-timeline cadence is not copied for visible chat text; the workbench uses a shorter broadcast window.
- The MVP deliberately spends complexity on correctness at four points: stable identity, event order, final full-text reconciliation and disconnect recovery. It avoids complexity that belongs to distributed production infrastructure.
- This Spec is stored locally only. It must not create, update or publish an issue.
