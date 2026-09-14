import { describe, expect, it } from 'vitest'

import type { TargetProjectConfig, ValidationCommand } from '../src/domain/workbench'
import { runValidationCommand } from '../src/server/verification'

describe('host validation commands', () => {
  it('records success and non-zero exits without losing the actionable output', async () => {
    const success = await runValidationCommand(command('success', ['-e', "console.log('checked')"]), target())
    const failure = await runValidationCommand(command('failure', ['-e', "console.error('actionable failure'); process.exit(3)"]), target())

    expect(success).toMatchObject({ result: 'verified', exitCode: 0, detail: 'checked' })
    expect(failure).toMatchObject({ result: 'failed', exitCode: 3, detail: 'actionable failure' })
  })

  it('terminates and records a timed-out command', async () => {
    const result = await runValidationCommand(command('timeout', ['-e', 'setTimeout(() => {}, 5000)'], 100), target())

    expect(result.result).toBe('failed')
    expect(result.exitCode).toBe(124)
    expect(result.detail).toContain('验证超时')
  })
})

function command(id: string, args: string[], timeoutMs = 2000): ValidationCommand {
  return { id, label: id, command: process.execPath, args, timeoutMs }
}

function target(): TargetProjectConfig {
  return {
    name: 'validation fixture', repositoryRoot: process.cwd(), modificationScope: '.',
    startCommand: { command: process.execPath, args: [] }, healthUrl: 'http://127.0.0.1:1', previewUrl: 'http://127.0.0.1:1',
    previewGatewayPort: 4174, startupTimeoutMs: 1000,
  }
}
