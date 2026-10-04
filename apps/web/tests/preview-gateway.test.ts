import { createServer } from 'node:net'

import { afterEach, describe, expect, it } from 'vitest'

import {
  removeEmbeddingRestrictions,
  resolvePreviewTarget,
  startPreviewGateway,
  stopPreviewGateway,
} from '../src/server/preview-gateway'

afterEach(stopPreviewGateway)

// 网关只放宽 iframe 嵌入限制，同时必须把所有请求锁死在配置的上游 origin。
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

  it('returns a deterministic 502 when the configured upstream is unavailable', async () => {
    const upstreamPort = await freePort()
    const gatewayPort = await freePort()
    await startPreviewGateway(`http://127.0.0.1:${upstreamPort}`, gatewayPort)

    const response = await fetch(`http://127.0.0.1:${gatewayPort}/competition`)

    expect(response.status).toBe(502)
    await expect(response.text()).resolves.toContain('Preview upstream unavailable')
  })
})

function freePort(): Promise<number> {
  return new Promise((resolve, reject) => {
    const server = createServer()
    server.once('error', reject)
    server.listen(0, '127.0.0.1', () => {
      const address = server.address()
      if (!address || typeof address === 'string') return reject(new Error('无法分配测试端口'))
      server.close(() => resolve(address.port))
    })
  })
}
