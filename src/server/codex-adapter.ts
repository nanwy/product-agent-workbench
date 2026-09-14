import { execFile, spawn, type ChildProcess } from 'node:child_process'
import { access } from 'node:fs/promises'
import { createInterface } from 'node:readline'
import { promisify } from 'node:util'

import type { AgentEvent } from '../domain/workbench'

const execFileAsync = promisify(execFile)
const bundledCodex = '/Applications/ChatGPT.app/Contents/Resources/codex'
const globalCodex = globalThis as typeof globalThis & { __workbenchCodexChild?: ChildProcess | null }

export type CodexTurnResult = { exitCode: number; sessionId: string | null }

export async function runCodexTurn(input: {
  repositoryRoot: string
  prompt: string
  sessionId: string | null
  onEvent: (event: AgentEvent, sessionId: string | null) => Promise<void>
}): Promise<CodexTurnResult> {
  if (globalCodex.__workbenchCodexChild) throw new Error('已有 Codex turn 正在运行')
  const command = await resolveCodexCommand()
  await verifyCodexCommand(command)
  const args = input.sessionId
    ? ['exec', 'resume', '--json', input.sessionId, input.prompt]
    : ['exec', '--json', '--color', 'never', '-C', input.repositoryRoot, '-s', 'workspace-write', '-a', 'never', input.prompt]
  const child = spawn(command, args, { cwd: input.repositoryRoot, env: process.env, stdio: ['ignore', 'pipe', 'pipe'] })
  globalCodex.__workbenchCodexChild = child
  const completion = new Promise<number>((resolve, reject) => {
    child.once('error', reject)
    child.once('close', (code) => resolve(code ?? 1))
  })
  let sessionId = input.sessionId
  let stderr = ''
  child.stderr?.on('data', (chunk: Buffer) => { stderr = `${stderr}${chunk.toString()}`.slice(-4000) })
  const lines = createInterface({ input: child.stdout! })
  try {
    for await (const line of lines) {
      const parsed = parseJson(line)
      sessionId = extractSessionId(parsed) ?? sessionId
      await input.onEvent(normalizeEvent(parsed, line), sessionId)
    }
  } catch (error) {
    child.kill('SIGTERM')
    await completion.catch(() => undefined)
    globalCodex.__workbenchCodexChild = null
    throw error
  }
  const exitCode = await completion.finally(() => {
    if (globalCodex.__workbenchCodexChild === child) globalCodex.__workbenchCodexChild = null
  })
  if (exitCode !== 0 && stderr) {
    await input.onEvent(event('failed', redact(stderr.trim())), sessionId)
  }
  return { exitCode, sessionId }
}

export function cancelCodexTurn() {
  const child = globalCodex.__workbenchCodexChild
  if (!child?.pid || child.exitCode !== null) return false
  child.kill('SIGTERM')
  return true
}

async function resolveCodexCommand() {
  if (process.env.WORKBENCH_CODEX_COMMAND) return process.env.WORKBENCH_CODEX_COMMAND
  try {
    await access(bundledCodex)
    return bundledCodex
  } catch {
    return 'codex'
  }
}

async function verifyCodexCommand(command: string) {
  try {
    await execFileAsync(command, ['--version'], { timeout: 5000 })
  } catch (error) {
    throw new Error(`Codex CLI 不可用（${command}）：${error instanceof Error ? error.message : String(error)}`)
  }
}

function parseJson(line: string): unknown {
  try { return JSON.parse(line) } catch { return { type: 'unknown', message: line } }
}

function extractSessionId(value: unknown) {
  if (!value || typeof value !== 'object') return null
  const record = value as Record<string, unknown>
  const candidate = record.thread_id ?? record.session_id ?? (record.thread as Record<string, unknown> | undefined)?.id
  return typeof candidate === 'string' ? candidate : null
}

function normalizeEvent(value: unknown, raw: string): AgentEvent {
  if (!value || typeof value !== 'object') return event('unknown', raw)
  const record = value as Record<string, unknown>
  const rawType = String(record.type ?? 'unknown')
  const item = record.item && typeof record.item === 'object' ? record.item as Record<string, unknown> : null
  const message = String(record.message ?? item?.text ?? item?.name ?? rawType)
  if (/thread|session/.test(rawType)) return event('session', message)
  if (/message|agent_message/.test(`${rawType} ${item?.type ?? ''}`)) return event('message', message)
  if (/tool|command/.test(`${rawType} ${item?.type ?? ''}`)) return event('tool', message)
  if (/file/.test(`${rawType} ${item?.type ?? ''}`)) return event('file', message)
  if (/completed/.test(rawType)) return event('completed', message)
  if (/failed|error/.test(rawType)) return event('failed', message)
  return event('unknown', message)
}

export function event(type: AgentEvent['type'], message: string): AgentEvent {
  return { id: crypto.randomUUID(), type, message: redact(message).slice(0, 2000), recordedAt: new Date().toISOString() }
}

function redact(value: string) {
  return value.replace(/(token|password|secret|authorization)(\s*[:=]\s*)([^\s,;]+)/gi, '$1$2[REDACTED]').replace(/Bearer\s+[A-Za-z0-9._~+/-]+=*/gi, 'Bearer [REDACTED]')
}
