import { QueryClient, type QueryClientConfig } from '@tanstack/react-query'

import type { ConversationMessage, Requirement, SpecVersion, WorkbenchSnapshot } from '../domain/workbench'
import type { AgentStreamBatch } from '../server/agent-events'

export const workbenchQueryKeys = {
  snapshot: ['workbench', 'snapshot'] as const,
  conversation: (requirementId: string) => ['workbench', 'conversation', requirementId] as const,
}

export type ConversationProjection = {
  requirementId: string
  messageIds: string[]
  messagesById: Record<string, ConversationMessage>
  specVersionsById: Record<string, SpecVersion>
  lastSequenceByTurn: Record<string, number>
  revision: string
}

const queryClientConfig: QueryClientConfig = {
  defaultOptions: {
    queries: {
      retry: false,
      refetchOnWindowFocus: false,
    },
  },
}

let browserQueryClient: QueryClient | undefined

/** SSR 请求使用独立实例；浏览器只创建一个 Query Client。 */
export function getWorkbenchQueryClient() {
  if (typeof window === 'undefined') return new QueryClient(queryClientConfig)
  browserQueryClient ??= new QueryClient(queryClientConfig)
  return browserQueryClient
}

/** HTTP、SSE 刷新和轮询全部通过这里写入同一份 Query Cache。 */
export function applyWorkbenchSnapshot(queryClient: QueryClient, snapshot: WorkbenchSnapshot) {
  const previous = queryClient.getQueryData<WorkbenchSnapshot>(workbenchQueryKeys.snapshot)
  const accepted = previous ? rejectStaleRequirements(previous, snapshot) : snapshot
  queryClient.setQueryData(workbenchQueryKeys.snapshot, accepted)
  const requirements = [accepted.requirement, ...accepted.history].filter(
    (requirement): requirement is Requirement => Boolean(requirement),
  )
  for (const requirement of requirements) {
    queryClient.setQueryData(
      workbenchQueryKeys.conversation(requirement.id),
      (previous: ConversationProjection | undefined) => projectConversation(requirement, previous),
    )
  }
  return accepted
}

function rejectStaleRequirements(current: WorkbenchSnapshot, incoming: WorkbenchSnapshot) {
  const known = new Map(
    [current.requirement, ...current.history]
      .filter((requirement): requirement is Requirement => Boolean(requirement))
      .map((requirement) => [requirement.id, requirement]),
  )
  const accept = (requirement: Requirement | null) => {
    if (!requirement) return null
    const existing = known.get(requirement.id)
    return existing && Date.parse(existing.updatedAt) > Date.parse(requirement.updatedAt)
      ? existing
      : requirement
  }
  return {
    ...incoming,
    requirement: accept(incoming.requirement),
    history: incoming.history.map((requirement) => accept(requirement) ?? requirement),
  }
}

/** 按 Requirement 隔离对话，并复用没有变化的消息和 Spec 引用。 */
export function projectConversation(
  requirement: Requirement,
  previous?: ConversationProjection,
): ConversationProjection {
  if (previous && Date.parse(previous.revision) > Date.parse(requirement.updatedAt))
    return previous
  const messagesById = Object.fromEntries(
    requirement.conversation.messages.map((message) => {
      const existing = previous?.messagesById[message.id]
      return [message.id, existing && sameValue(existing, message) ? existing : message]
    }),
  )
  const specVersionsById = Object.fromEntries(
    requirement.specVersions.map((version) => {
      const existing = previous?.specVersionsById[version.id]
      return [version.id, existing && sameValue(existing, version) ? existing : version]
    }),
  )
  const nextIds = requirement.conversation.messages.map(({ id }) => id)
  const messageIds = previous && sameStringArray(previous.messageIds, nextIds)
    ? previous.messageIds
    : nextIds
  return {
    requirementId: requirement.id,
    messageIds,
    messagesById,
    specVersionsById,
    lastSequenceByTurn: previous?.lastSequenceByTurn ?? {},
    revision: newerRevision(previous?.revision, requirement.updatedAt),
  }
}

/** 将一个有序 SSE 批次投影进 Cache；false 表示序号缺口，调用方必须拉取快照。 */
export function applyAgentStreamBatch(queryClient: QueryClient, batch: AgentStreamBatch) {
  const key = workbenchQueryKeys.conversation(batch.requirementId)
  const projection = queryClient.getQueryData<ConversationProjection>(key)
  if (!projection) return false
  const lastSequence = projection.lastSequenceByTurn[batch.turnId] ?? 0
  if (batch.sequenceEnd <= lastSequence) return true
  if (batch.sequenceStart > lastSequence + 1) return false

  queryClient.setQueryData<ConversationProjection>(key, (current) => {
    if (!current) return current
    if (batch.target !== 'conversation') return {
      ...current,
      revision: batch.revision,
      lastSequenceByTurn: { ...current.lastSequenceByTurn, [batch.turnId]: batch.sequenceEnd },
    }
    const message = current.messagesById[batch.targetId]
    if (!message) return current
    const segments = [...message.segments]
    const textIndex = segments.findIndex(
      (segment) => segment.type === 'text' && segment.id === batch.itemId,
    )
    if (textIndex >= 0 && segments[textIndex].type === 'text') {
      const text = segments[textIndex]
      segments[textIndex] = { ...text, content: `${text.content}${batch.delta}` }
    } else {
      segments.push({ id: batch.itemId, type: 'text', content: batch.delta })
    }
    return {
      ...current,
      revision: batch.revision,
      lastSequenceByTurn: { ...current.lastSequenceByTurn, [batch.turnId]: batch.sequenceEnd },
      messagesById: {
        ...current.messagesById,
        [batch.targetId]: { ...message, segments },
      },
    }
  })

  if (batch.target === 'execution') {
    queryClient.setQueryData<WorkbenchSnapshot>(workbenchQueryKeys.snapshot, (snapshot) => {
      if (!snapshot?.requirement || snapshot.requirement.id !== batch.requirementId) return snapshot
      const events = [...snapshot.requirement.codex.events]
      const index = events.findIndex((event) => event.id === batch.itemId)
      if (index >= 0) events[index] = { ...events[index], message: `${events[index].message}${batch.delta}` }
      else events.push({ id: batch.itemId, type: 'message', message: batch.delta, recordedAt: batch.revision })
      return {
        ...snapshot,
        requirement: {
          ...snapshot.requirement,
          updatedAt: batch.revision,
          codex: { ...snapshot.requirement.codex, events },
        },
      }
    })
  }
  return true
}

function sameValue(left: unknown, right: unknown) {
  return left === right || JSON.stringify(left) === JSON.stringify(right)
}

function sameStringArray(left: string[], right: string[]) {
  return left.length === right.length && left.every((value, index) => value === right[index])
}

function newerRevision(left: string | undefined, right: string) {
  return left && Date.parse(left) > Date.parse(right) ? left : right
}
