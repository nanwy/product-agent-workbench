# 02 — 将浏览器服务端状态统一迁入 React Query

**What to build:** 让 loader、HTTP mutation、SSE 刷新和轮询都通过同一个 Query Client 管理工作台与产品需求对话，使页面刷新、历史切换和后台状态变化继续可见，同时删除 React 快照副本和 Zustand 对话真值。输入草稿、历史选择和面板开关仍保留为本地界面状态。

**Blocked by:** None — can start immediately

**Status:** complete

- [x] 工作台状态和按产品需求隔离的对话状态分别使用稳定 Query Key，并都由同一个 Query Client 管理。
- [x] 服务端首屏数据只用于预取和水合 Query Cache，页面不再维护平行的完整快照状态。
- [x] HTTP mutation 的返回结果、SSE 触发的刷新和轮询结果都汇入 Query Cache。
- [x] 历史产品需求与当前产品需求不会因全局 Store 水合而互相覆盖。
- [x] Zustand 对话 Store 被移除，消息级引用稳定、流式 Markdown 保护和自动滚动行为得到保留。
- [x] 现有需求提交、Grilling、Spec、执行、取消、返修、刷新恢复和历史浏览测试继续通过。
