import path from 'node:path'

import { afterEach, describe, expect, it } from 'vitest'

import { cancelCodexTurn, runCodexTurn } from '../src/server/codex-adapter'

const originalCommand = process.env.WORKBENCH_CODEX_COMMAND

afterEach(() => {
  if (originalCommand === undefined) delete process.env.WORKBENCH_CODEX_COMMAND
  else process.env.WORKBENCH_CODEX_COMMAND = originalCommand
})

describe('Codex adapter', () => {
  it('cancels only the active turn process', async () => {
    process.env.WORKBENCH_CODEX_COMMAND = path.resolve('tests/fixtures/fake-codex.mjs')
    let cancelled = false
    const result = await runCodexTurn({
      repositoryRoot: process.cwd(),
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
