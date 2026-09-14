import { describe, expect, it } from 'vitest'

import {
  removeEmbeddingRestrictions,
  resolvePreviewTarget,
} from '../src/server/preview-gateway'

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

  it('locks requests to the configured upstream origin', () => {
    const upstream = new URL('http://127.0.0.1:4173')

    expect(resolvePreviewTarget('/competition?tab=live', upstream).href).toBe(
      'http://127.0.0.1:4173/competition?tab=live',
    )
    expect(() => resolvePreviewTarget('http://evil.example/', upstream)).toThrow()
    expect(() => resolvePreviewTarget('//evil.example/', upstream)).toThrow()
  })
})
