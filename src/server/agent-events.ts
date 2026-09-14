import type { AgentEvent } from '../domain/workbench'

type Subscriber = (event: AgentEvent) => void
const subscribers = new Set<Subscriber>()

export function publishAgentEvent(event: AgentEvent) {
  for (const subscriber of subscribers) subscriber(event)
}

export function subscribeAgentEvents(subscriber: Subscriber) {
  subscribers.add(subscriber)
  return () => subscribers.delete(subscriber)
}
