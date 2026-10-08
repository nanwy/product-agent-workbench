import { QueryClient } from '@tanstack/react-query'
import { describe, expect, it } from 'vitest'

import {
  applyReplayReset,
  applyTaskEvent,
  buildGoEventsUrl,
  readEventCursor,
} from '../src/api/events'
import { goQueryKeys } from '../src/api/queries'

describe('Go SSE cache convergence', () => {
  it('restores a persisted cursor in the initial EventSource URL', () => {
    const storage = new MemoryStorage()
    expect(buildGoEventsUrl(storage)).toBe('/api/v1/events')

    storage.setItem('product-agent-workbench.go-events.cursor.v1', '42')
    expect(readEventCursor(storage)).toBe(42)
    expect(buildGoEventsUrl(storage)).toBe('/api/v1/events?after=42')
  })

  it('persists an accepted event and invalidates only its TaskRun', async () => {
    const queryClient = new QueryClient()
    const storage = new MemoryStorage()
    queryClient.setQueryData(goQueryKeys.taskRun('run-1'), { status: 'running' })
    queryClient.setQueryData(goQueryKeys.taskRun('run-2'), { status: 'running' })

    await applyTaskEvent(queryClient, storage, '7', {
      taskRunId: 'run-1', sequence: 2, type: 'codex.text.delta', payload: '{}',
      createdAt: '2026-10-08T00:00:00Z',
    })

    expect(readEventCursor(storage)).toBe(7)
    expect(queryClient.getQueryState(goQueryKeys.taskRun('run-1'))?.isInvalidated).toBe(true)
    expect(queryClient.getQueryState(goQueryKeys.taskRun('run-2'))?.isInvalidated).toBe(false)
  })

  it('moves to the server reset boundary and invalidates all Go resources', async () => {
    const queryClient = new QueryClient()
    const storage = new MemoryStorage()
    queryClient.setQueryData(goQueryKeys.requirement('req-1'), { revision: 1 })

    await applyReplayReset(queryClient, storage, '12', {
      reason: 'cursor_outside_retained_window', oldestEventId: 8, latestEventId: 12,
    })

    expect(readEventCursor(storage)).toBe(12)
    expect(queryClient.getQueryState(goQueryKeys.requirement('req-1'))?.isInvalidated).toBe(true)
  })

  it('rejects a mismatched reset cursor without advancing local storage', () => {
    const queryClient = new QueryClient()
    const storage = new MemoryStorage()

    expect(() => applyReplayReset(queryClient, storage, '11', {
      reason: 'cursor_outside_retained_window', oldestEventId: 8, latestEventId: 12,
    })).toThrow('游标与 payload 不一致')
    expect(readEventCursor(storage)).toBe(0)
  })
})

class MemoryStorage {
  private readonly values = new Map<string, string>()

  getItem(key: string) {
    return this.values.get(key) ?? null
  }

  setItem(key: string, value: string) {
    this.values.set(key, value)
  }

  removeItem(key: string) {
    this.values.delete(key)
  }
}
