import { describe, expect, it } from 'vitest'

import { createWorkbenchConversation } from '../src/server/workbench-conversation'
import { applyImplementationEvent, beginImplementationTurn, finishImplementationTurn } from '../src/server/workbench-implementation'

describe('implementation conversation', () => {
  it('keeps the planning thread separate and folds text and tool activity into one assistant turn', () => {
    const initial = { ...createWorkbenchConversation('REQ-TEST'), phase: 'implement' as const, planningThreadId: 'planning-1' }
    const started = beginImplementationTurn(initial, 'TICKET-01')
    const text = applyImplementationEvent(started, {
      id: 'text-1', type: 'message', message: '正在实现', recordedAt: '2026-09-18T00:00:00.000Z',
    }, 'implementation-1')
    const activity = applyImplementationEvent(text, {
      id: 'tool-1', type: 'tool', message: 'pnpm typecheck', activityStatus: 'completed', recordedAt: '2026-09-18T00:00:01.000Z',
    }, 'implementation-1')
    const finished = finishImplementationTurn(activity, 'completed', 'TICKET-01 已完成')

    expect(finished.planningThreadId).toBe('planning-1')
    expect(finished.implementationThreadId).toBe('implementation-1')
    expect(finished.messages.at(-1)?.status).toBe('completed')
    expect(finished.messages.at(-1)?.segments).toEqual(expect.arrayContaining([
      expect.objectContaining({ type: 'text', content: '正在实现' }),
      expect.objectContaining({ type: 'activity', detail: 'pnpm typecheck' }),
    ]))
  })

  it('preserves partial text when a turn is cancelled', () => {
    const started = beginImplementationTurn({ ...createWorkbenchConversation('REQ-TEST'), phase: 'implement' }, 'TICKET-01')
    const partial = applyImplementationEvent(started, {
      id: 'text-1', type: 'message', message: '部分结果', recordedAt: '2026-09-18T00:00:00.000Z',
    }, 'implementation-1')
    const cancelled = finishImplementationTurn(partial, 'cancelled', '用户停止')
    expect(cancelled.status).toBe('cancelled')
    expect(cancelled.messages.at(-1)?.segments).toEqual(expect.arrayContaining([
      expect.objectContaining({ type: 'text', content: '部分结果' }),
      expect.objectContaining({ type: 'activity', label: '执行已取消' }),
    ]))
  })
})
