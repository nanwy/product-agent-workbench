import type { AgentEvent } from '../domain/workbench'

export type AgentStreamBatch = {
  type: 'stream-batch'
  requirementId: string
  target: 'conversation' | 'execution'
  targetId: string
  turnId: string
  itemId: string
  sequenceStart: number
  sequenceEnd: number
  revision: string
  delta: string
}

export type AgentSseEvent =
  | { type: 'invalidate'; event: AgentEvent }
  | AgentStreamBatch

type Subscriber = (event: AgentSseEvent) => void
// MVP 是单机单进程，因此使用进程内发布订阅；Requirement JSON 才是可恢复的持久真值。
const subscribers = new Set<Subscriber>()
const pending = new Map<string, AgentStreamBatch>()
const sequences = new Map<string, number>()
let flushTimer: ReturnType<typeof setTimeout> | null = null

export function publishAgentEvent(event: AgentEvent) {
  flushAgentStreamBatches()
  publish({ type: 'invalidate', event })
}

export function publishAgentTextDelta(input: Omit<AgentStreamBatch, 'type' | 'sequenceStart' | 'sequenceEnd'>) {
  const sequenceKey = `${input.requirementId}:${input.turnId}`
  const sequence = (sequences.get(sequenceKey) ?? 0) + 1
  sequences.set(sequenceKey, sequence)
  const key = `${sequenceKey}:${input.itemId}`
  // 两个 Item 的正文不能跨过彼此合并，否则全局序号会失去真实顺序。
  if (pending.size && !pending.has(key)) flushAgentStreamBatches()
  const current = pending.get(key)
  pending.set(key, current
    ? { ...current, sequenceEnd: sequence, revision: input.revision, delta: `${current.delta}${input.delta}` }
    : { type: 'stream-batch', ...input, sequenceStart: sequence, sequenceEnd: sequence })
  if (Buffer.byteLength([...pending.values()].map((batch) => batch.delta).join(''), 'utf8') >= 4_096) {
    flushAgentStreamBatches()
    return
  }
  flushTimer ??= setTimeout(flushAgentStreamBatches, 50)
  flushTimer.unref?.()
}

export function flushAgentStreamBatches() {
  if (flushTimer) clearTimeout(flushTimer)
  flushTimer = null
  const batches = [...pending.values()].sort((left, right) => left.sequenceStart - right.sequenceStart)
  pending.clear()
  for (const batch of batches) publish(batch)
}

function publish(event: AgentSseEvent) {
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
