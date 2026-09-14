import { describe, expect, it } from 'vitest'
import { randomUUID } from 'node:crypto'

import { runIdempotentMutation } from '../src/server/idempotency'

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
})
