import { QueryClient, type QueryClientConfig } from '@tanstack/react-query'

import type { ConversationMessage, Requirement, SpecVersion, WorkbenchSnapshot } from '../domain/workbench'

export const workbenchQueryKeys = {
  snapshot: ['workbench', 'snapshot'] as const,
  conversation: (requirementId: string) => ['workbench', 'conversation', requirementId] as const,
}

export type ConversationProjection = {
  requirementId: string
  messageIds: string[]
  messagesById: Record<string, ConversationMessage>
  specVersionsById: Record<string, SpecVersion>
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
  return { requirementId: requirement.id, messageIds, messagesById, specVersionsById }
}

function sameValue(left: unknown, right: unknown) {
  return left === right || JSON.stringify(left) === JSON.stringify(right)
}

function sameStringArray(left: string[], right: string[]) {
  return left.length === right.length && left.every((value, index) => value === right[index])
}
