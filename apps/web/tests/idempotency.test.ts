import { describe, expect, it } from 'vitest'
import { randomUUID } from 'node:crypto'

import { runIdempotentMutation } from '../src/server/idempotency'

// 并发调用同一动作时，只有首个请求能产生副作用，其他请求读取已有结果。
describe('workflow mutation idempotency', () => {
  it('runs concurrent requests with the same key exactly once', async () => {
    let calls = 0
    const key = randomUUID()
    const operation = () => runIdempotentMutation('test-operation', key, async () => -1, async () => ++calls)

    const results = await Promise.all([operation(), operation(), operation()])

    expect(results).toEqual([1, -1, -1])
    expect(calls).toBe(1)

    const other = await runIdempotentMutation('other-operation', key, async () => -1, async () => ++calls)
    expect(other).toBe(2)
  })

  it('does not queue an interrupt behind an unrelated long mutation', async () => {
    let release!: () => void
    const blocked = new Promise<void>((resolve) => { release = resolve })
    const longMutation = runIdempotentMutation('long-operation', randomUUID(), async () => 'duplicate', async () => {
      await blocked
      return 'finished'
    })
    const interrupt = await runIdempotentMutation('cancel-operation', randomUUID(), async () => 'duplicate', async () => 'cancelled')

    expect(interrupt).toBe('cancelled')
    release()
    await expect(longMutation).resolves.toBe('finished')
  })
})
