import path from 'node:path'

import { afterEach, describe, expect, it } from 'vitest'

import { cancelCodexTurn, normalizeCodexEvent, runCodexTurn } from '../src/server/codex-adapter'

// 适配器测试通过假 app-server/SDK 可执行文件验证公共边界，不依赖本机真实 Codex。
const originalCommand = process.env.WORKBENCH_CODEX_COMMAND
const originalBackend = process.env.WORKBENCH_CODEX_BACKEND
const originalFixtureMode = process.env.WORKBENCH_FAKE_APP_SERVER_MODE
const originalHandshakeTimeout = process.env.WORKBENCH_CODEX_HANDSHAKE_TIMEOUT_MS

afterEach(() => {
  if (originalCommand === undefined) delete process.env.WORKBENCH_CODEX_COMMAND
  else process.env.WORKBENCH_CODEX_COMMAND = originalCommand
  if (originalBackend === undefined) delete process.env.WORKBENCH_CODEX_BACKEND
  else process.env.WORKBENCH_CODEX_BACKEND = originalBackend
  if (originalFixtureMode === undefined) delete process.env.WORKBENCH_FAKE_APP_SERVER_MODE
  else process.env.WORKBENCH_FAKE_APP_SERVER_MODE = originalFixtureMode
  if (originalHandshakeTimeout === undefined) delete process.env.WORKBENCH_CODEX_HANDSHAKE_TIMEOUT_MS
  else process.env.WORKBENCH_CODEX_HANDSHAKE_TIMEOUT_MS = originalHandshakeTimeout
})

describe('Codex adapter', () => {
  it('fails clearly when the configured app-server executable cannot be started', async () => {
    process.env.WORKBENCH_CODEX_COMMAND = '/missing/workbench-codex'
    await expect(runCodexTurn({
      workspaceRoot: process.cwd(),
      prompt: 'never runs',
      sessionId: null,
      signal: undefined,
      onEvent: async () => undefined,
    })).rejects.toThrow('Codex app-server 无法启动')
  })

  it('starts an app-server thread, forwards the sandbox, and keeps its thread id', async () => {
    process.env.WORKBENCH_CODEX_COMMAND = path.resolve('tests/fixtures/fake-codex.mjs')
    const events: string[] = []
    const result = await runCodexTurn({
      workspaceRoot: process.cwd(),
      prompt: 'VALIDATE_APP_SERVER',
      sessionId: null,
      sandboxMode: 'read-only',
      onEvent: async (event) => { events.push(event.message) },
    })

    expect(result.exitCode).toBe(0)
    expect(result.sessionId).toBe('00000000-0000-4000-8000-000000000001')
    expect(events).toContain('read-only:VALIDATE_APP_SERVER')
  })

  it('resumes the persisted app-server thread for the next turn', async () => {
    process.env.WORKBENCH_CODEX_COMMAND = path.resolve('tests/fixtures/fake-codex.mjs')
    const result = await runCodexTurn({
      workspaceRoot: process.cwd(),
      prompt: 'continue',
      sessionId: 'persisted-thread-id',
      onEvent: async () => undefined,
    })

    expect(result).toEqual({ exitCode: 0, sessionId: 'persisted-thread-id' })
  })

  it.each([
    ['initialize-failure', 'fixture initialize failure'],
    ['no-response', 'initialize 超时'],
    ['invalid-json', '非法 JSON-RPC'],
    ['turn-failure', 'fixture turn failure'],
    ['process-exit', '意外退出'],
  ])('settles failures instead of leaving a running turn: %s', async (mode, expected) => {
    process.env.WORKBENCH_CODEX_COMMAND = path.resolve('tests/fixtures/fake-codex.mjs')
    process.env.WORKBENCH_FAKE_APP_SERVER_MODE = mode
    process.env.WORKBENCH_CODEX_HANDSHAKE_TIMEOUT_MS = '50'
    const events: string[] = []
    const result = await runCodexTurn({
      workspaceRoot: process.cwd(),
      prompt: 'failure contract',
      sessionId: null,
      onEvent: async (event) => { events.push(`${event.type}:${event.message}`) },
    })

    expect(result.exitCode).toBe(1)
    expect(events.some((value) => value.includes(expected))).toBe(true)
  })

  it('force-cleans an app-server that ignores turn/interrupt', async () => {
    process.env.WORKBENCH_CODEX_COMMAND = path.resolve('tests/fixtures/fake-codex.mjs')
    process.env.WORKBENCH_FAKE_APP_SERVER_MODE = 'ignore-interrupt'
    let cancellationSent = false
    const result = await runCodexTurn({
      workspaceRoot: process.cwd(),
      prompt: 'SLOW_TURN',
      sessionId: null,
      onEvent: async (event) => {
        if (!cancellationSent && event.message === 'partial before cancel') {
          cancellationSent = true
          cancelCodexTurn()
        }
      },
    })

    expect(result.exitCode).toBe(130)
    expect(cancelCodexTurn()).toBe(false)
  }, 4_000)

  it('cancels only the active turn process', async () => {
    process.env.WORKBENCH_CODEX_COMMAND = path.resolve('tests/fixtures/fake-codex.mjs')
    let cancelled = false
    const events: string[] = []
    const result = await runCodexTurn({
      workspaceRoot: process.cwd(),
      prompt: 'SLOW_TURN',
      sessionId: null,
      onEvent: async (event) => {
        events.push(event.message)
        if (!cancelled && event.message === 'partial before cancel') {
          cancelled = true
          expect(cancelCodexTurn()).toBe(true)
        }
      },
    })

    expect(result.exitCode).not.toBe(0)
    expect(result.sessionId).toBe('00000000-0000-4000-8000-000000000001')
    expect(events).toContain('partial before cancel')
    expect(cancelCodexTurn()).toBe(false)
  })

  it('uses the SDK only when the fallback is selected explicitly', async () => {
    process.env.WORKBENCH_CODEX_BACKEND = 'sdk'
    process.env.WORKBENCH_CODEX_COMMAND = path.resolve('tests/fixtures/fake-codex.mjs')
    const result = await runCodexTurn({
      workspaceRoot: process.cwd(),
      prompt: 'VALIDATE_EXEC_ARGS',
      sessionId: null,
      sandboxMode: 'read-only',
      onEvent: async () => undefined,
    })

    expect(result.exitCode).toBe(0)
  })

  it('normalizes every SDK event category into the stable workbench contract', () => {
    const cases = [
      [{ type: 'thread.started', thread_id: 'session-1' }, 'session'],
      [{ type: 'item.completed', item: { type: 'agent_message', text: 'implemented' } }, 'message'],
      [{ type: 'item.started', item: { type: 'command_execution', command: 'pnpm test' } }, 'tool'],
      [{ type: 'item.completed', item: { type: 'file_change', changes: [{ path: 'src/page.tsx', kind: 'update' }], status: 'completed' } }, 'file'],
      [{ type: 'turn.completed' }, 'completed'],
      [{ type: 'turn.failed', message: 'token=private' }, 'failed'],
      [{ type: 'item.completed', item: { id: 'reasoning-1', type: 'reasoning', text: 'checked' } }, 'unknown'],
      // SDK item errors are explicitly non-fatal; only the top-level turn failure closes the turn.
      [{ type: 'item.completed', item: { id: 'error-1', type: 'error', message: 'tool failed' } }, 'tool'],
      [{ type: 'unknown', message: 'not-json' }, 'unknown'],
    ] as const

    for (const [input, expected] of cases) expect(normalizeCodexEvent(input, JSON.stringify(input)).type).toBe(expected)
    expect(normalizeCodexEvent(cases[5][0], '').message).toContain('token=[REDACTED]')
    expect(normalizeCodexEvent(cases[3][0], '').message).toBe('src/page.tsx')
    expect(normalizeCodexEvent({ type: 'item.updated', item: { id: 'tool-1', type: 'command_execution', command: 'pnpm test' } }, '').id).toBe('tool-1')
    expect(normalizeCodexEvent({ type: 'item.updated', item: { id: 'tool-1', type: 'command_execution', command: 'pnpm test' } }, '', 'turn-2').id).toBe('turn-2:tool-1')
    expect(normalizeCodexEvent({ type: 'item.started', item: { id: 'tool-1', type: 'command_execution', command: 'pnpm test', aggregated_output: '', status: 'in_progress' } }, '').activityStatus).toBe('running')
    expect(normalizeCodexEvent({ type: 'item.completed', item: { id: 'tool-1', type: 'command_execution', command: 'pnpm test', aggregated_output: 'ok', exit_code: 0, status: 'completed' } }, '').message).toBe('pnpm test\nok\nexit 0')
    const longReply = `${'x'.repeat(2500)}[WORKBENCH:READY_FOR_CONFIRMATION]`
    expect(normalizeCodexEvent({ type: 'item.updated', item: { id: 'message-1', type: 'agent_message', text: longReply } }, '').message).toContain('[WORKBENCH:READY_FOR_CONFIRMATION]')
  })
})
