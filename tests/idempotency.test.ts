import { describe, expect, it } from 'vitest'

import { runIdempotentMutation } from '../src/server/idempotency'

describe('workflow mutation idempotency', () => {
  it('runs concurrent requests with the same key exactly once', async () => {
    let calls = 0
    const operation = () => runIdempotentMutation('same-request', async () => ++calls)

    const results = await Promise.all([operation(), operation(), operation()])

    expect(results).toEqual([1, 1, 1])
    expect(calls).toBe(1)
  })
})
