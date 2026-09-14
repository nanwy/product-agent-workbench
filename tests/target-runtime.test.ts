import { createServer } from 'node:net'

import { afterEach, describe, expect, it } from 'vitest'

import type { TargetProjectConfig } from '../src/domain/workbench'
import { startTargetProject, stopTargetProject } from '../src/server/target-runtime'

let activeConfig: TargetProjectConfig | null = null

afterEach(async () => {
  if (activeConfig) await stopTargetProject(activeConfig)
  activeConfig = null
})

// runtime 是进程级单例，所以这些测试必须串行，避免一个用例停止另一个用例的子进程。
describe('owned target runtime lifecycle', () => {
  it('reports a child that exits before becoming healthy', async () => {
    const upstreamPort = await freePort()
    const gatewayPort = await freePort()
    activeConfig = config(upstreamPort, gatewayPort, ['-e', 'process.exit(2)'], 600)

    const snapshot = await startTargetProject(activeConfig)

    expect(snapshot.status).toBe('failed')
    expect(snapshot.error).toMatch(/退出|就绪/)
    expect(snapshot.pid).toBeNull()

    // 同一个 runtime 在修正启动条件后可以重试，不需要重启工作台服务。
    const serverCode = `require('node:http').createServer((request,response)=>response.end('retry-ready')).listen(${upstreamPort},'127.0.0.1')`
    activeConfig = config(upstreamPort, gatewayPort, ['-e', serverCode], 2000)
    await expect(startTargetProject(activeConfig)).resolves.toMatchObject({ status: 'ready' })
  })

  it('times out and reclaims an unhealthy child', async () => {
    activeConfig = await targetConfig(['-e', 'setInterval(() => {}, 1000)'], 250)

    const snapshot = await startTargetProject(activeConfig)

    expect(snapshot.status).toBe('failed')
    expect(snapshot.error).toContain('启动超时')
    expect(snapshot.pid).toBeNull()
  })

  it('stops the entire owned process group, including its server child', async () => {
    const upstreamPort = await freePort()
    const gatewayPort = await freePort()
    const serverCode = `require('node:http').createServer((request,response)=>response.end('fixture:'+request.url)).listen(${upstreamPort},'127.0.0.1')`
    const parentCode = `const {spawn}=require('node:child_process');spawn(process.execPath,['-e',${JSON.stringify(serverCode)}],{stdio:'ignore'});setInterval(()=>{},1000)`
    activeConfig = config(upstreamPort, gatewayPort, ['-e', parentCode], 3000)

    const ready = await startTargetProject(activeConfig)
    expect(ready.status).toBe('ready')
    await expect(fetch(`${ready.gatewayUrl}/competition`).then((response) => response.text())).resolves.toBe('fixture:/competition')

    const stopped = await stopTargetProject(activeConfig)
    activeConfig = null
    expect(stopped.status).toBe('stopped')
    await expect(waitUntilUnavailable(`http://127.0.0.1:${upstreamPort}`)).resolves.toBeUndefined()
  })
})

async function targetConfig(args: string[], startupTimeoutMs: number) {
  return config(await freePort(), await freePort(), args, startupTimeoutMs)
}

function config(upstreamPort: number, gatewayPort: number, args: string[], startupTimeoutMs: number): TargetProjectConfig {
  return {
    name: 'Runtime fixture',
    repositoryRoot: process.cwd(),
    modificationScope: '.',
    startCommand: { command: process.execPath, args },
    healthUrl: `http://127.0.0.1:${upstreamPort}`,
    previewUrl: `http://127.0.0.1:${upstreamPort}`,
    previewGatewayPort: gatewayPort,
    startupTimeoutMs,
  }
}

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

async function waitUntilUnavailable(url: string) {
  const deadline = Date.now() + 2000
  while (Date.now() < deadline) {
    const available = await fetch(url, { signal: AbortSignal.timeout(150) }).then(() => true).catch(() => false)
    if (!available) return
    await new Promise((resolve) => setTimeout(resolve, 50))
  }
  throw new Error(`自有子进程仍在响应：${url}`)
}
