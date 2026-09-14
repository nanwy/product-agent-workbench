import { spawn } from 'node:child_process'
import { createConnection } from 'node:net'

import type { TargetProjectConfig, ValidationCommand, ValidationRecord } from '../domain/workbench'
import { loadTargetProjectConfig } from './config'
import { changedFilesSince, filesOutsideScope } from './git-workspace'
import { requirementStore } from './requirement-store'
import { getTargetProcessSnapshot } from './target-runtime'

/**
 * 第五票的验证编排入口。
 *
 * “验证命令”是一次真实的宿主子进程（例如 typecheck）；
 * “验证矩阵”是把命令、Git 边界、运行时和人工判断统一成 ValidationRecord[]。
 * 这样 UI 和验收门禁只消费一种结构，不需要猜测日志文本。
 */
export async function verifyCurrentRequirement(signal?: AbortSignal) {
  const requirement = await requirementStore.load()
  if (!requirement?.branch) throw new Error('验证前必须完成需求分支上的候选实现')
  if (!requirement.tickets.length || requirement.tickets.some((ticket) => ticket.status !== 'completed')) {
    throw new Error('所有执行票完成后才能进入验证')
  }

  await requirementStore.update((current) => {
    if (!current) throw new Error('产品需求已不存在')
    return { ...current, stage: 'verification', validation: [], updatedAt: new Date().toISOString() }
  })

  const target = await loadTargetProjectConfig()
  const scopedTarget = { ...target, modificationScope: requirement.spec?.proposedScope ?? target.modificationScope }
  // 三类机器证据串行生成：配置命令、改动范围、真实路由。
  // 串行使页面展示顺序稳定，也避免多个重型构建命令抢占同一仓库。
  const records: ValidationRecord[] = []
  for (const command of target.validationCommands ?? []) records.push(await runValidationCommand(command, target, signal))
  records.push(await validateScope(requirement.branch.baseRevision, scopedTarget))
  records.push(await validateRuntime(requirement.targetPage, target))
  records.push(record({
    id: 'product-acceptance',
    criterion: '产品在真实预览中确认候选实现符合意图',
    result: 'not-run',
    source: 'product',
    detail: '等待产品显式验收；产品验收不等于生产批准',
    notRunReason: '尚未点击产品验收',
  }))

  // failed 是硬门禁；not-run 保留为未验证风险，最终由产品显式验收补齐。
  const blocking = records.some((item) => item.result === 'failed')
  return requirementStore.update((current) => {
    if (!current) throw new Error('产品需求已不存在')
    const now = new Date().toISOString()
    return {
      ...current,
      stage: blocking ? 'verification' : 'awaiting-product-acceptance',
      validation: records,
      updatedAt: now,
      evidence: [...current.evidence, {
        stage: blocking ? 'verification' : 'awaiting-product-acceptance',
        input: `${records.length} 项验证`,
        action: '在工作台宿主运行确定性检查并记录证据',
        output: blocking ? '存在失败项，已阻止产品验收' : '自动验证已完成，等待产品验收',
        recordedAt: now,
      }],
    }
  })
}

export async function runValidationCommand(command: ValidationCommand, target: TargetProjectConfig, signal?: AbortSignal): Promise<ValidationRecord> {
  const startedAt = new Date().toISOString()
  const displayedCommand = [command.command, ...command.args].join(' ')
  const result = await run(command, target.repositoryRoot, signal)
  return {
    id: command.id,
    criterion: command.label,
    result: result.exitCode === 0 ? 'verified' : 'failed',
    source: 'command',
    detail: result.output || (result.exitCode === 0 ? '命令执行成功' : `命令退出码 ${result.exitCode}`),
    command: displayedCommand,
    exitCode: result.exitCode,
    startedAt,
    completedAt: new Date().toISOString(),
    notRunReason: null,
  }
}

async function validateScope(baseRevision: string, target: TargetProjectConfig): Promise<ValidationRecord> {
  const files = await changedFilesSince(baseRevision, target)
  const outside = filesOutsideScope(files, target)
  return record({
    id: 'git-scope',
    criterion: `所有改动位于确认范围 ${target.modificationScope}`,
    result: outside.length ? 'failed' : 'verified',
    source: 'git-scope',
    detail: outside.length ? `发现范围外改动：${outside.join(', ')}` : `已检查 ${files.length} 个改动文件`,
  })
}

async function validateRuntime(targetPage: string, target: TargetProjectConfig): Promise<ValidationRecord> {
  const snapshot = getTargetProcessSnapshot(target)
  if (snapshot.status !== 'ready') return record({
    id: 'runtime-preview', criterion: `目标页面 ${targetPage} 可访问`, result: 'not-run', source: 'runtime',
    detail: '目标项目尚未就绪，可启动后重跑验证', notRunReason: '目标运行时未就绪',
  })
  try {
    const response = await fetch(new URL(targetPage, target.previewUrl), { signal: AbortSignal.timeout(5000) })
    return record({
      id: 'runtime-preview', criterion: `目标页面 ${targetPage} 可访问`, result: response.ok ? 'verified' : 'failed', source: 'runtime',
      detail: `HTTP ${response.status} ${response.statusText}`,
    })
  } catch (error) {
    return record({
      id: 'runtime-preview', criterion: `目标页面 ${targetPage} 可访问`, result: 'not-run', source: 'runtime',
      detail: error instanceof Error ? error.message : String(error), notRunReason: '宿主无法访问目标页面',
    })
  }
}

function record(input: Pick<ValidationRecord, 'id' | 'criterion' | 'result' | 'source' | 'detail'> & Partial<ValidationRecord>): ValidationRecord {
  const now = new Date().toISOString()
  return { command: null, exitCode: null, startedAt: now, completedAt: now, notRunReason: null, ...input }
}

async function run(command: ValidationCommand, cwd: string, signal?: AbortSignal): Promise<{ exitCode: number; output: string }> {
  const env = await buildValidationEnvironment(command)
  if (signal?.aborted) return { exitCode: 130, output: '验证已取消' }
  return new Promise((resolve) => {
    // 不使用 shell，避免把参数再次解析成管道、重定向或命令替换。
    // detached 让超时时可以终止整个验证进程组，不留下 pnpm/tsc 子进程。
    const child = spawn(command.command, command.args, { cwd, detached: process.platform !== 'win32', env, stdio: ['ignore', 'pipe', 'pipe'] })
    let output = ''
    let firstActionableError = ''
    let scanRemainder = ''
    // 只保留末尾 4000 字符：通常包含最可操作的错误，同时防止 Requirement JSON 无限增长。
    const append = (chunk: Buffer) => {
      const text = chunk.toString()
      output = `${output}${text}`.slice(-4000)
      const lines = `${scanRemainder}${text}`.split(/\r?\n/)
      scanRemainder = lines.pop() ?? ''
      firstActionableError ||= lines.find(isActionableError)?.trim().slice(0, 1200) ?? ''
    }
    child.stdout?.on('data', append)
    child.stderr?.on('data', append)
    let termination: 'exit' | 'timeout' | 'cancelled' = 'exit'
    let forceTimer: NodeJS.Timeout | null = null
    const terminate = (reason: 'timeout' | 'cancelled') => {
      if (termination !== 'exit' || !child.pid) return
      termination = reason
      stopProcessGroup(child.pid, 'SIGTERM')
      forceTimer = setTimeout(() => stopProcessGroup(child.pid!, 'SIGKILL'), 1000)
      forceTimer.unref()
    }
    const timer = setTimeout(() => {
      terminate('timeout')
    }, command.timeoutMs)
    const abort = () => terminate('cancelled')
    signal?.addEventListener('abort', abort, { once: true })
    child.once('error', (error) => {
      clearTimeout(timer)
      signal?.removeEventListener('abort', abort)
      resolve({ exitCode: 1, output: redact(error.message) })
    })
    child.once('close', (code) => {
      clearTimeout(timer)
      if (forceTimer) clearTimeout(forceTimer)
      signal?.removeEventListener('abort', abort)
      firstActionableError ||= isActionableError(scanRemainder) ? scanRemainder.trim().slice(0, 1200) : ''
      const evidence = preserveFirstError(firstActionableError, output.trim())
      if (termination === 'timeout') resolve({ exitCode: 124, output: redact(`验证超时（${command.timeoutMs}ms）\n${evidence}`.trim()) })
      else if (termination === 'cancelled') resolve({ exitCode: 130, output: redact(`验证已取消\n${evidence}`.trim()) })
      else resolve({ exitCode: code ?? 1, output: redact(evidence) })
    })
  })
}

/** 构造可复现的验证环境，并在 auto 模式下移除宿主无法连接的本机代理。 */
export async function buildValidationEnvironment(command: ValidationCommand): Promise<NodeJS.ProcessEnv> {
  const env = { ...process.env }
  env.NODE_ENV = command.environment
  if (command.proxyPolicy === 'omit') removeProxyVariables(env)
  if (command.proxyPolicy === 'auto') {
    const availability = new Map<string, boolean>()
    for (const key of proxyKeys) {
      const value = env[key]
      if (!value) continue
      let url: URL
      try { url = new URL(value) } catch { delete env[key]; continue }
      if (!isLoopback(url.hostname)) continue
      const proxyHost = url.hostname.replace(/^\[|\]$/g, '')
      const endpoint = `${proxyHost}:${url.port || (url.protocol === 'https:' ? '443' : '80')}`
      if (!availability.has(endpoint)) availability.set(endpoint, await canConnect(proxyHost, Number(url.port || (url.protocol === 'https:' ? 443 : 80))))
      if (!availability.get(endpoint)) delete env[key]
    }
  }
  return env
}

const proxyKeys = ['HTTP_PROXY', 'HTTPS_PROXY', 'ALL_PROXY', 'http_proxy', 'https_proxy', 'all_proxy'] as const

function removeProxyVariables(env: NodeJS.ProcessEnv) {
  for (const key of proxyKeys) delete env[key]
}

function isLoopback(hostname: string) {
  return hostname === 'localhost' || hostname === '127.0.0.1' || hostname === '::1' || hostname === '[::1]'
}

function canConnect(host: string, port: number) {
  return new Promise<boolean>((resolve) => {
    const socket = createConnection({ host, port })
    let settled = false
    const finish = (available: boolean) => {
      if (settled) return
      settled = true
      socket.destroy()
      resolve(available)
    }
    socket.setTimeout(300)
    socket.once('connect', () => finish(true))
    socket.once('timeout', () => finish(false))
    socket.once('error', () => finish(false))
  })
}

function stopProcessGroup(pid: number, signal: NodeJS.Signals) {
  try {
    if (process.platform === 'win32') process.kill(pid, signal)
    else process.kill(-pid, signal)
  } catch (error) {
    if (!(error instanceof Error && 'code' in error && error.code === 'ESRCH')) throw error
  }
}

function isActionableError(line: string) {
  return /(^|\s)(error|exception|failed|failure|err!)(\s|:|$)/i.test(line)
}

function preserveFirstError(firstError: string, tail: string) {
  if (!firstError || tail.includes(firstError)) return tail
  return `首个错误：${firstError}\n…\n${tail}`
}

function redact(value: string) {
  return value.replace(/(token|password|secret|authorization)(\s*[:=]\s*)([^\s,;]+)/gi, '$1$2[REDACTED]').replace(/Bearer\s+[A-Za-z0-9._~+/-]+=*/gi, 'Bearer [REDACTED]')
}
