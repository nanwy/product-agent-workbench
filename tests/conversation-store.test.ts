import { describe, expect, it, vi } from 'vitest'

import { createConversationStore } from '../src/store/conversation-store'
import { partitionStreamingMarkdown } from '../src/store/streaming-markdown'

const first = {
  id: 'assistant-1', role: 'assistant' as const, phase: 'grilling' as const, status: 'completed' as const,
  recordedAt: '2026-09-15T00:00:00.000Z', segments: [{ id: 'text-1', type: 'text' as const, content: '旧消息' }],
}
const streaming = {
  id: 'assistant-2', role: 'assistant' as const, phase: 'grilling' as const, status: 'streaming' as const,
  recordedAt: '2026-09-15T00:01:00.000Z', segments: [{ id: 'text-2', type: 'text' as const, content: '' }],
}

describe('conversation projection', () => {
  it('batches deltas and preserves completed message references', () => {
    const scheduled: Array<() => void> = []
    const store = createConversationStore((flush) => { scheduled.push(flush); return scheduled.length })
    store.getState().hydrate({ requirementId: 'REQ-1', messages: [first, streaming] })
    const historicalReference = store.getState().messagesById[first.id]

    store.getState().appendDelta(streaming.id, '你好')
    store.getState().appendDelta(streaming.id, '，世界')

    expect(scheduled).toHaveLength(1)
    expect(store.getState().messagesById[streaming.id].segments[0]).toMatchObject({ content: '' })
    scheduled[0]()
    expect(store.getState().messagesById[streaming.id].segments[0]).toMatchObject({ content: '你好，世界' })
    expect(store.getState().messagesById[first.id]).toBe(historicalReference)
  })

  it('turns cumulative server snapshots into frame-buffered deltas', () => {
    const scheduled: Array<() => void> = []
    const store = createConversationStore((flush) => { scheduled.push(flush); return scheduled.length })
    const started = { ...streaming, segments: [{ ...streaming.segments[0], content: '你' }] }
    const advanced = { ...streaming, segments: [{ ...streaming.segments[0], content: '你好，世界' }] }

    store.getState().hydrate({ requirementId: 'REQ-1', messages: [first, started] })
    store.getState().hydrate({ requirementId: 'REQ-1', messages: [first, advanced] })

    expect(scheduled).toHaveLength(1)
    expect(store.getState().messagesById[streaming.id].segments[0]).toMatchObject({ content: '你' })
    scheduled[0]()
    expect(store.getState().messagesById[streaming.id].segments[0]).toMatchObject({ content: '你好，世界' })
  })

  it('flushes into the existing text segment when an activity follows it', () => {
    const scheduled: Array<() => void> = []
    const store = createConversationStore((flush) => { scheduled.push(flush); return scheduled.length })
    const withActivity = {
      ...streaming,
      segments: [
        { ...streaming.segments[0], content: '你' },
        { id: 'activity-1', type: 'activity' as const, label: '检查', detail: '完成', status: 'completed' as const },
      ],
    }
    store.getState().hydrate({ requirementId: 'REQ-1', messages: [withActivity] })
    store.getState().appendDelta(streaming.id, '好')
    scheduled[0]()

    expect(store.getState().messagesById[streaming.id].segments).toEqual([
      expect.objectContaining({ type: 'text', content: '你好' }),
      expect.objectContaining({ type: 'activity', id: 'activity-1' }),
    ])
  })

  it('flushes pending text before recording a terminal state', () => {
    const store = createConversationStore(() => 1)
    store.getState().hydrate({ requirementId: 'REQ-1', messages: [streaming] })
    store.getState().appendDelta(streaming.id, '最后一段')
    store.getState().finishMessage(streaming.id, 'cancelled')

    expect(store.getState().messagesById[streaming.id]).toMatchObject({
      status: 'cancelled',
      segments: [{ content: '最后一段' }],
    })
  })

  it('replaces the projection when a different requirement is hydrated', () => {
    const store = createConversationStore(vi.fn(() => 1))
    store.getState().hydrate({ requirementId: 'REQ-1', messages: [first] })
    store.getState().hydrate({ requirementId: 'REQ-2', messages: [] })

    expect(store.getState().requirementId).toBe('REQ-2')
    expect(store.getState().messageIds).toEqual([])
    expect(store.getState().messagesById).toEqual({})
  })

  it('keeps an unfinished code fence out of the Markdown parser until completion', () => {
    expect(partitionStreamingMarkdown('说明\n\n```ts\nconst value = 1', true)).toEqual({ stable: '说明\n\n', streamingCode: '```ts\nconst value = 1' })
    expect(partitionStreamingMarkdown('```ts\nconst value = 1\n```', true)).toEqual({ stable: '```ts\nconst value = 1\n```', streamingCode: null })
    expect(partitionStreamingMarkdown('```ts\nconst value = 1', false)).toEqual({ stable: '```ts\nconst value = 1', streamingCode: null })
  })
})
