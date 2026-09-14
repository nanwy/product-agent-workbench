import { describe, expect, it } from 'vitest'

import { removeEmbeddingRestrictions } from '../src/server/preview-gateway'

describe('preview gateway response policy', () => {
  it('removes only embedding restrictions', () => {
    const headers = removeEmbeddingRestrictions({
      'content-security-policy': "default-src 'self'; frame-ancestors 'none'; script-src 'self'",
      'x-frame-options': 'DENY',
      'x-content-type-options': 'nosniff',
    })

    expect(headers).toEqual({
      'content-security-policy': "default-src 'self'; script-src 'self'",
      'x-content-type-options': 'nosniff',
    })
  })
})
