import { QueryClient } from '@tanstack/react-query'
import { describe, expect, it } from 'vitest'

import type { ConversationMessage, Requirement, WorkbenchSnapshot } from '../src/domain/workbench'
import { applyAgentStreamBatch, applyWorkbenchSnapshot, projectConversation, workbenchQueryKeys } from '../src/store/workbench-query'
import { partitionStreamingMarkdown } from '../src/store/streaming-markdown'

const first: ConversationMessage = {
  id: 'assistant-1', role: 'assistant', phase: 'grilling', status: 'completed',
  recordedAt: '2026-09-15T00:00:00.000Z', segments: [{ id: 'text-1', type: 'text', content: '旧消息' }],
}

describe('workbench Query projection', () => {
  it('preserves unchanged message references', () => {
    const requirement = requirementWith('REQ-1', [first])
    const initial = projectConversation(requirement)
    const next = projectConversation({
      ...requirement,
      conversation: { ...requirement.conversation, messages: [structuredClone(first)] },
    }, initial)

    expect(next.messagesById[first.id]).toBe(initial.messagesById[first.id])
    expect(next.messageIds).toBe(initial.messageIds)
  })

  it('isolates conversations by Requirement query key', () => {
    const queryClient = new QueryClient()
    const current = requirementWith('REQ-1', [first])
    const historical = requirementWith('REQ-2', [])
    const snapshot = {
      requirement: current,
      history: [current, historical],
      target: null,
      process: { status: 'stopped' },
    } as unknown as WorkbenchSnapshot

    applyWorkbenchSnapshot(queryClient, snapshot)

    expect(queryClient.getQueryData(workbenchQueryKeys.conversation('REQ-1'))).toMatchObject({ messageIds: ['assistant-1'] })
    expect(queryClient.getQueryData(workbenchQueryKeys.conversation('REQ-2'))).toMatchObject({ messageIds: [] })
  })

  it('applies ordered batches once and rejects a sequence gap', () => {
    const queryClient = new QueryClient()
    const streaming = { ...first, status: 'streaming' as const, segments: [] }
    const requirement = requirementWith('REQ-1', [streaming])
    queryClient.setQueryData(workbenchQueryKeys.conversation('REQ-1'), projectConversation(requirement))
    const batch = {
      type: 'stream-batch' as const,
      requirementId: 'REQ-1', target: 'conversation' as const, targetId: streaming.id,
      turnId: 'turn-1', itemId: 'item-1', sequenceStart: 1, sequenceEnd: 1,
      revision: '2026-09-15T00:01:00.000Z', delta: '你好',
    }

    expect(applyAgentStreamBatch(queryClient, batch)).toBe(true)
    expect(applyAgentStreamBatch(queryClient, batch)).toBe(true)
    expect(applyAgentStreamBatch(queryClient, { ...batch, sequenceStart: 3, sequenceEnd: 3 })).toBe(false)
    expect(queryClient.getQueryData(workbenchQueryKeys.conversation('REQ-1'))).toMatchObject({
      messagesById: { [streaming.id]: { segments: [{ content: '你好' }] } },
    })
  })

  it('keeps an unfinished code fence out of the Markdown parser until completion', () => {
    expect(partitionStreamingMarkdown('说明\n\n```ts\nconst value = 1', true)).toEqual({ stable: '说明\n\n', streamingCode: '```ts\nconst value = 1' })
    expect(partitionStreamingMarkdown('```ts\nconst value = 1\n```', true)).toEqual({ stable: '```ts\nconst value = 1\n```', streamingCode: null })
  })
})

function requirementWith(id: string, messages: ConversationMessage[]) {
  return {
    id,
    specVersions: [],
    conversation: { messages },
  } as unknown as Requirement
}
