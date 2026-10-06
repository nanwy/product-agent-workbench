// Package scheduler 把已确认的执行票依赖图转换成“当前可以入队”的执行票集合。
//
// 模块边界：
//   - scheduler 读取不可变 TicketVersion 和 TaskRun 执行历史；
//   - scheduler 只决定哪些执行票 ready，不领取任务，也不启动 Runner；
//   - store 负责持久化 TaskRun，runner 负责执行已经领取的 TaskRun；
//   - blockedBy 依赖来自版本化 artifact，不把 Ticket Markdown 重复保存进 SQLite。
//
// 对应旧 TypeScript：workbench-tickets.ts 中的 initializeTicketQueue 和
// releaseReadyTickets。Go 版本会把散落的 pending/queued 推进收敛到这个模块。
package scheduler
