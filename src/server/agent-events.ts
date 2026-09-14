import type { AgentEvent } from '../domain/workbench'

type Subscriber = (event: AgentEvent) => void
// MVP 是单机单进程，因此使用进程内发布订阅；Requirement JSON 才是可恢复的持久真值。
const subscribers = new Set<Subscriber>()

export function publishAgentEvent(event: AgentEvent) {
  for (const subscriber of subscribers) {
    try {
      subscriber(event)
    } catch {
      // 一个断开的 SSE 客户不应该阻断 Codex 执行，失效订阅者直接移除。
      subscribers.delete(subscriber)
    }
  }
}

export function subscribeAgentEvents(subscriber: Subscriber) {
  subscribers.add(subscriber)
  return () => subscribers.delete(subscriber)
}
