import path from 'node:path'

import { afterEach, describe, expect, it } from 'vitest'

import { cancelCodexTurn, runCodexTurn } from '../src/server/codex-adapter'

const originalCommand = process.env.WORKBENCH_CODEX_COMMAND

afterEach(() => {
  if (originalCommand === undefined) delete process.env.WORKBENCH_CODEX_COMMAND
  else process.env.WORKBENCH_CODEX_COMMAND = originalCommand
})

describe('Codex adapter', () => {
  it('fails clearly when the configured CLI cannot be started', async () => {
    process.env.WORKBENCH_CODEX_COMMAND = '/missing/workbench-codex'
    await expect(runCodexTurn({
      workspaceRoot: process.cwd(),
      prompt: 'never runs',
      sessionId: null,
      signal: undefined,
      onEvent: async () => undefined,
    })).rejects.toThrow('Codex CLI 不可用')
  })

  it('uses arguments accepted by the current non-interactive CLI', async () => {
    process.env.WORKBENCH_CODEX_COMMAND = path.resolve('tests/fixtures/fake-codex.mjs')
    const events: string[] = []
    const result = await runCodexTurn({
      workspaceRoot: process.cwd(),
      prompt: 'VALIDATE_EXEC_ARGS',
      sessionId: null,
      onEvent: async (event) => { events.push(event.message) },
    })

    expect(result.exitCode).toBe(0)
    expect(events).not.toContain("error: unexpected argument '-a' found")
  })

  it('cancels only the active turn process', async () => {
    process.env.WORKBENCH_CODEX_COMMAND = path.resolve('tests/fixtures/fake-codex.mjs')
    let cancelled = false
    const result = await runCodexTurn({
      workspaceRoot: process.cwd(),
      prompt: 'SLOW_TURN',
      sessionId: null,
      onEvent: async () => {
        if (!cancelled) {
          cancelled = true
          expect(cancelCodexTurn()).toBe(true)
        }
      },
    })

    expect(result.exitCode).not.toBe(0)
    expect(result.sessionId).toBe('00000000-0000-4000-8000-000000000001')
    expect(cancelCodexTurn()).toBe(false)
  })
})
