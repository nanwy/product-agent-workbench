import { execFile, spawn, type ChildProcess } from 'node:child_process'
import { access } from 'node:fs/promises'
import { createInterface } from 'node:readline'
import { promisify } from 'node:util'

import type { AgentEvent } from '../domain/workbench'

const execFileAsync = promisify(execFile)
const bundledCodex = '/Applications/ChatGPT.app/Contents/Resources/codex'
// MVP 只允许一个 Codex turn 占用目标仓库；globalThis 让热更后仍能取消原 child。
const globalCodex = globalThis as typeof globalThis & { __workbenchCodexChild?: ChildProcess | null }

export type CodexTurnResult = { exitCode: number; sessionId: string | null }

export async function runCodexTurn(input: {
  workspaceRoot: string
  prompt: string
  sessionId: string | null
  signal?: AbortSignal
  // 会话转接在系统临时目录运行，不要求那里是 Git 仓库，并且不保留额外 Codex session。
  skipGitRepositoryCheck?: boolean
  ephemeral?: boolean
  onEvent: (event: AgentEvent, sessionId: string | null) => Promise<void>
}): Promise<CodexTurnResult> {
  if (globalCodex.__workbenchCodexChild) throw new Error('已有 Codex turn 正在运行')
  const command = await resolveCodexCommand()
  await verifyCodexCommand(command)
  if (input.signal?.aborted) throw new Error('Codex turn 已取消')
  // 首张票创建 session，后续票/返修用 resume 保留共同理解。
  // -C 和 workspace-write 把执行位置与写权限锁在目标仓库；实际文件范围还会在 turn 后二次检查。
  const transientArgs = [input.skipGitRepositoryCheck ? '--skip-git-repo-check' : '', input.ephemeral ? '--ephemeral' : ''].filter(Boolean)
  const args = input.sessionId
    ? ['exec', 'resume', '--json', input.sessionId, input.prompt]
    : ['exec', '--json', '--color', 'never', '-C', input.workspaceRoot, '-s', 'workspace-write', ...transientArgs, input.prompt]
  const child = spawn(command, args, { cwd: input.workspaceRoot, detached: process.platform !== 'win32', env: process.env, stdio: ['ignore', 'pipe', 'pipe'] })
  globalCodex.__workbenchCodexChild = child
  const abort = () => terminateCodexChild(child)
  input.signal?.addEventListener('abort', abort, { once: true })
  // stdout 是可解析 JSONL 事件，stderr 只在非零退出时作为失败证据。
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
      // CLI 事件形状可随版本变动，adapter 在这里归一化为应用稳定 AgentEvent。
      const parsed = parseJson(line)
      sessionId = extractSessionId(parsed) ?? sessionId
      await input.onEvent(normalizeCodexEvent(parsed, line), sessionId)
    }
  } catch (error) {
    terminateCodexChild(child)
    await completion.catch(() => undefined)
    globalCodex.__workbenchCodexChild = null
    throw error
  }
  const exitCode = await completion.finally(() => {
    input.signal?.removeEventListener('abort', abort)
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
  terminateCodexChild(child)
  return true
}

/** 服务重启恢复时用来区分“真实子进程仍在”与“只有 JSON 还写着 running”。 */
export function hasActiveCodexTurn() {
  const child = globalCodex.__workbenchCodexChild
  return Boolean(child?.pid && child.exitCode === null)
}

function terminateCodexChild(child: ChildProcess) {
  if (!child.pid || child.exitCode !== null) return
  try {
    if (process.platform === 'win32') child.kill('SIGTERM')
    else process.kill(-child.pid, 'SIGTERM')
  } catch (error) {
    if (!isMissingProcess(error)) throw error
  }
  const pid = child.pid
  // 先终止整个 turn 进程组；3 秒后仍未退出才强制清理。
  const timer = setTimeout(() => {
    if (child.exitCode !== null) return
    try {
      if (process.platform === 'win32') child.kill('SIGKILL')
      else process.kill(-pid, 'SIGKILL')
    } catch (error) {
      if (!isMissingProcess(error)) throw error
    }
  }, 3000)
  timer.unref()
}

function isMissingProcess(error: unknown): error is NodeJS.ErrnoException {
  return error instanceof Error && 'code' in error && error.code === 'ESRCH'
}

async function resolveCodexCommand() {
  // 测试可注入 fake CLI；macOS 优先桌面应用内置二进制，其他环境回退 PATH。
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

/** 把易变的 Codex JSONL 形状压缩为工作台稳定的事件类型。 */
export function normalizeCodexEvent(value: unknown, raw: string): AgentEvent {
  // 只暴露 UI 需要的少量语义类型，不让页面耦合 Codex CLI 的全部内部事件名。
  if (!value || typeof value !== 'object') return event('unknown', raw)
  const record = value as Record<string, unknown>
  const rawType = String(record.type ?? 'unknown')
  const item = record.item && typeof record.item === 'object' ? record.item as Record<string, unknown> : null
  const message = String(record.message ?? item?.text ?? item?.name ?? item?.command ?? item?.path ?? rawType)
  if (/thread|session/.test(rawType)) return event('session', message)
  if (/message|agent_message/.test(`${rawType} ${item?.type ?? ''}`)) return event('message', message)
  if (/tool|command/.test(`${rawType} ${item?.type ?? ''}`)) return event('tool', message)
  if (/file/.test(`${rawType} ${item?.type ?? ''}`)) return event('file', message)
  if (/completed/.test(rawType)) return event('completed', message)
  if (/failed|error/.test(rawType)) return event('failed', message)
  return event('unknown', message)
}

export function event(type: AgentEvent['type'], message: string): AgentEvent {
  // 入库/推送前统一脱敏和限长，避免终端输出把凭据或无界日志带到浏览器。
  return { id: crypto.randomUUID(), type, message: redact(message).slice(0, 2000), recordedAt: new Date().toISOString() }
}

function redact(value: string) {
  return value.replace(/(token|password|secret|authorization)(\s*[:=]\s*)([^\s,;]+)/gi, '$1$2[REDACTED]').replace(/Bearer\s+[A-Za-z0-9._~+/-]+=*/gi, 'Bearer [REDACTED]')
}
