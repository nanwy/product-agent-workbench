import { describe, expect, it } from 'vitest'

import type { TargetProjectConfig, ValidationCommand } from '../src/domain/workbench'
import { runValidationCommand, validateRuntime } from '../src/server/verification'

// 验证命令测试覆盖成功、非零退出和超时，确保三种结果都能形成确定性证据。
describe('host validation commands', () => {
  it('records success and non-zero exits without losing the actionable output', async () => {
    const success = await runValidationCommand(command('success', ['-e', "console.log('checked')"]), target())
    const failure = await runValidationCommand(command('failure', ['-e', "console.error('error: first actionable failure'); console.log('x'.repeat(5000)); process.exit(3)"]), target())

    expect(success).toMatchObject({ result: 'verified', exitCode: 0, detail: 'checked' })
    expect(failure).toMatchObject({ result: 'failed', exitCode: 3 })
    expect(failure.detail).toContain('first actionable failure')
  })

  it('replaces a non-standard NODE_ENV and omits inherited proxy variables', async () => {
    const previousNodeEnv = process.env.NODE_ENV
    const previousProxy = process.env.HTTP_PROXY
    process.env.NODE_ENV = 'staging'
    process.env.HTTP_PROXY = 'http://127.0.0.1:7897'
    try {
      const result = await runValidationCommand(command('environment', ['-e', "console.log(`${process.env.NODE_ENV}:${Boolean(process.env.HTTP_PROXY)}`)"], 2000, 'production'), target())
      expect(result).toMatchObject({ result: 'verified', detail: 'production:false' })
    } finally {
      if (previousNodeEnv === undefined) delete process.env.NODE_ENV
      else process.env.NODE_ENV = previousNodeEnv
      if (previousProxy === undefined) delete process.env.HTTP_PROXY
      else process.env.HTTP_PROXY = previousProxy
    }
  })

  it('force-terminates and records a command that ignores graceful timeout', async () => {
    const result = await runValidationCommand(command('timeout', ['-e', "process.on('SIGTERM', () => {}); setInterval(() => {}, 5000)"], 100), target())

    expect(result.result).toBe('failed')
    expect(result.exitCode).toBe(124)
    expect(result.detail).toContain('验证超时')
  })

  it('cancels the owned validation process group through AbortSignal', async () => {
    const controller = new AbortController()
    setTimeout(() => controller.abort(), 50)
    const result = await runValidationCommand(command('cancel', ['-e', 'setInterval(() => {}, 5000)']), target(), controller.signal)

    expect(result).toMatchObject({ result: 'failed', exitCode: 130 })
    expect(result.detail).toContain('验证已取消')
  })
})

describe('runtime validation', () => {
  it('uses the configured page response as truth even when no process handle exists', async () => {
    const originalFetch = globalThis.fetch
    globalThis.fetch = async (input) => {
      expect(String(input)).toBe('http://127.0.0.1:3003/home')
      return new Response('', { status: 200, statusText: 'OK' })
    }
    try {
      const result = await validateRuntime('/home', target())
      expect(result).toMatchObject({ id: 'runtime-preview', result: 'verified', detail: 'HTTP 200 OK' })
    } finally {
      globalThis.fetch = originalFetch
    }
  })
})

function command(id: string, args: string[], timeoutMs = 2000, environment: ValidationCommand['environment'] = 'test'): ValidationCommand {
  return { id, label: id, command: process.execPath, args, timeoutMs, environment, proxyPolicy: 'omit' }
}

function target(): TargetProjectConfig {
  return {
    name: 'validation fixture', repositoryRoot: process.cwd(), modificationScope: '.',
    startCommand: { command: process.execPath, args: [] }, healthUrl: 'http://127.0.0.1:1', previewUrl: 'http://127.0.0.1:3003',
    previewGatewayPort: 4174, startupTimeoutMs: 1000,
  }
}
