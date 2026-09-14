import { randomUUID } from 'node:crypto'
import { spawn } from 'node:child_process'
import { createConnection } from 'node:net'

import type { Requirement, TargetProjectConfig, ValidationCommand, ValidationRecord } from '../domain/workbench'
import { loadTargetProjectConfig } from './config'
import { changedFilesSince, filesOutsideScope } from './git-workspace'
import { requirementStore } from './requirement-store'
import { getTargetProcessSnapshot, startTargetProject } from './target-runtime'

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
  // 机器证据串行生成：配置命令、Git 边界、健康端点、真实路由和 agent-browser。
  // 串行使页面展示顺序稳定，也避免多个重型构建命令抢占同一仓库。
  const records: ValidationRecord[] = []
  for (const command of target.validationCommands ?? []) records.push(await runValidationCommand(command, target, signal))
  const scopeRecord = await validateScope(requirement.branch.baseRevision, scopedTarget)
  records.push(scopeRecord)
  // 候选实现完成后由宿主确保运行时就绪；执行 Agent 无需在沙箱中监听端口或操作浏览器。
  if (!signal?.aborted && getTargetProcessSnapshot(target).status !== 'ready') await startTargetProject(target)
  await captureFirstPreviewMetric(target)
  records.push(await validateHealth(target))
  records.push(await validateRuntime(requirement.targetPage, target))
  const browserRecord = await runAgentBrowserValidation(requirement.targetPage, target, signal)
  records.push(browserRecord)
  // Spec 的字符串顺序在确认后冻结，因此由 specId + 序号形成稳定条件 ID。
  // 每条条件单独映射到最贴近的机器证据，不能用一条“页面可访问”覆盖全部产品行为。
  if (requirement.spec) {
    records.push(...requirement.spec.acceptanceCriteria.map((criterion, index) =>
      criterionRecord(criterion, index, scopeRecord, browserRecord, requirement),
    ))
  }
  records.push(record({
    id: 'product-acceptance',
    criterionId: null,
    criterion: '产品在真实预览中确认候选实现符合意图',
    required: false,
    result: 'not-run',
    source: 'product',
    detail: '等待产品显式验收；产品验收不等于生产批准',
    notRunReason: '尚未点击产品验收',
  }))

  // 必要项必须全部 verified；环境导致的 not-run 也不能被产品点击绕过。
  const blocking = records.some((item) => item.required && item.result !== 'verified')
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

/** 自动验证启动目标项目时也记录首次可用预览，避免指标依赖用户是否手动点过“启动”。 */
async function captureFirstPreviewMetric(target: TargetProjectConfig) {
  const processFacts = getTargetProcessSnapshot(target)
  if (processFacts.status !== 'ready') return
  await requirementStore.update((current) => {
    if (!current) throw new Error('产品需求已不存在')
    if (current.metrics.firstPreviewAt) return current
    return { ...current, processFacts, metrics: { ...current.metrics, firstPreviewAt: new Date().toISOString() } }
  })
}

export async function runValidationCommand(command: ValidationCommand, target: TargetProjectConfig, signal?: AbortSignal): Promise<ValidationRecord> {
  const startedAt = new Date().toISOString()
  const displayedCommand = [command.command, ...command.args].join(' ')
  const result = await run(command, target.repositoryRoot, signal)
  return {
    id: command.id,
    criterionId: null,
    criterion: command.label,
    required: true,
    result: result.exitCode === 0 ? 'verified' : 'failed',
    source: 'command',
    detail: result.output || (result.exitCode === 0 ? '命令执行成功' : `命令退出码 ${result.exitCode}`),
    command: displayedCommand,
    exitCode: result.exitCode,
    startedAt,
    completedAt: new Date().toISOString(),
    notRunReason: null,
    browserEvidence: null,
  }
}

async function validateScope(baseRevision: string, target: TargetProjectConfig): Promise<ValidationRecord> {
  const files = await changedFilesSince(baseRevision, target)
  const outside = filesOutsideScope(files, target)
  return record({
    id: 'git-scope',
    criterionId: null,
    criterion: `所有改动位于确认范围 ${target.modificationScope}`,
    required: true,
    result: outside.length ? 'failed' : 'verified',
    source: 'git-scope',
    detail: outside.length ? `发现范围外改动：${outside.join(', ')}` : `已检查 ${files.length} 个改动文件`,
  })
}

/** 健康端点只证明目标服务存活，与“目标页面可渲染”分别留证。 */
async function validateHealth(target: TargetProjectConfig): Promise<ValidationRecord> {
  const startedAt = new Date().toISOString()
  try {
    const response = await fetch(target.healthUrl, { signal: AbortSignal.timeout(5000) })
    return record({
      id: 'runtime-health', criterionId: null, criterion: '目标项目健康检查通过', required: true,
      result: response.ok ? 'verified' : 'failed', source: 'runtime', detail: `HTTP ${response.status} ${response.statusText}`,
      command: `GET ${target.healthUrl}`, startedAt, completedAt: new Date().toISOString(),
    })
  } catch (error) {
    return record({
      id: 'runtime-health', criterionId: null, criterion: '目标项目健康检查通过', required: true,
      result: 'not-run', source: 'runtime', detail: error instanceof Error ? error.message : String(error),
      command: `GET ${target.healthUrl}`, startedAt, completedAt: new Date().toISOString(),
      notRunReason: 'environment: 宿主无法连接目标项目健康端点',
    })
  }
}

async function validateRuntime(targetPage: string, target: TargetProjectConfig): Promise<ValidationRecord> {
  const snapshot = getTargetProcessSnapshot(target)
  if (snapshot.status !== 'ready') return record({
    id: 'runtime-preview', criterionId: null, criterion: `目标页面 ${targetPage} 可访问`, required: true, result: 'not-run', source: 'runtime',
    detail: '目标项目尚未就绪，可启动后重跑验证', notRunReason: '目标运行时未就绪',
  })
  try {
    const response = await fetch(new URL(targetPage, snapshot.gatewayUrl), { signal: AbortSignal.timeout(5000) })
    return record({
      id: 'runtime-preview', criterionId: null, criterion: `目标页面 ${targetPage} 可访问`, required: true, result: response.ok ? 'verified' : 'failed', source: 'runtime',
      detail: `HTTP ${response.status} ${response.statusText}`,
    })
  } catch (error) {
    return record({
      id: 'runtime-preview', criterionId: null, criterion: `目标页面 ${targetPage} 可访问`, required: true, result: 'not-run', source: 'runtime',
      detail: error instanceof Error ? error.message : String(error), notRunReason: '宿主无法访问目标页面',
    })
  }
}

/**
 * 使用 agent-browser 对托管预览做第一层 UI 验证。
 *
 * 它依次打开真实路由、设置固定 viewport、读取 Accessibility Tree（其中包含 @eN
 * 语义引用），再读取 page error 与 console。这里不使用 Computer Use，也不把
 * Playwright 当探索工具；Playwright 只固化已经明确的稳定断言。
 */
export async function runAgentBrowserValidation(targetPage: string, target: TargetProjectConfig, signal?: AbortSignal): Promise<ValidationRecord> {
  const startedAt = new Date().toISOString()
  const snapshot = getTargetProcessSnapshot(target)
  const viewport = { width: 1440, height: 900 }
  const targetUrl = snapshot.gatewayUrl ? new URL(targetPage, snapshot.gatewayUrl).toString() : new URL(targetPage, target.previewUrl).toString()
  if (snapshot.status !== 'ready') return record({
    id: 'agent-browser-route', criterionId: null, criterion: `agent-browser 验证 ${targetPage}`, required: true,
    result: 'not-run', source: 'browser', detail: '目标运行时未就绪，未启动浏览器探索',
    command: `agent-browser open ${targetUrl}`, startedAt, completedAt: new Date().toISOString(),
    notRunReason: 'environment: 目标运行时未就绪',
  })

  const executable = process.env.WORKBENCH_AGENT_BROWSER_COMMAND ?? 'agent-browser'
  const session = `workbench-${randomUUID().slice(0, 8)}`
  const invoke = (args: string[]) => run({
    id: 'agent-browser', label: 'agent-browser', command: executable,
    args: ['--session', session, ...args], timeoutMs: 30000, environment: 'test', proxyPolicy: 'inherit',
  }, target.repositoryRoot, signal)

  const openResult = await invoke(['open', targetUrl])
  if (openResult.exitCode !== 0) return browserNotRun(openResult, targetUrl, viewport, startedAt)
  try {
    const viewportResult = await invoke(['set', 'viewport', String(viewport.width), String(viewport.height)])
    const treeResult = viewportResult.exitCode === 0 ? await invoke(['snapshot', '-c', '--json']) : viewportResult
    const pageErrors = treeResult.exitCode === 0 ? await invoke(['errors', '--json']) : treeResult
    const consoleOutput = pageErrors.exitCode === 0 ? await invoke(['console', '--json']) : pageErrors
    const failedStep = [viewportResult, treeResult, pageErrors, consoleOutput].find((item) => item.exitCode !== 0)
    if (failedStep) return browserNotRun(failedStep, targetUrl, viewport, startedAt)

    const accessibilityTree = treeResult.output.slice(-4000)
    const semanticRefs = [...new Set(accessibilityTree.match(/@e\d+/g) ?? [])].slice(0, 100)
    const consoleErrors = extractBrowserErrors(pageErrors.output, consoleOutput.output)
    return record({
      id: 'agent-browser-route', criterionId: null, criterion: `agent-browser 验证 ${targetPage}`, required: true,
      result: consoleErrors.length ? 'failed' : 'verified', source: 'browser',
      detail: consoleErrors.length
        ? `发现 ${consoleErrors.length} 条新增 browser console/page error`
        : `已记录 Accessibility Tree 和 ${semanticRefs.length} 个语义引用；未发现新增 console/page error`,
      command: `agent-browser open ${targetUrl}; set viewport ${viewport.width} ${viewport.height}; snapshot; errors; console`,
      exitCode: consoleErrors.length ? 1 : 0, startedAt, completedAt: new Date().toISOString(), notRunReason: null,
      browserEvidence: { targetUrl, viewport, accessibilityTree, semanticRefs, consoleErrors },
    })
  } finally {
    // close 失败不覆盖已经采集的证据；session 名唯一，不会关闭其他 Agent 的浏览器。
    await invoke(['close']).catch(() => undefined)
  }
}

function browserNotRun(result: { exitCode: number; output: string }, targetUrl: string, viewport: { width: number; height: number }, startedAt: string) {
  return record({
    id: 'agent-browser-route', criterionId: null, criterion: 'agent-browser 真实页面探索', required: true,
    result: 'not-run', source: 'browser', detail: result.output || `agent-browser 退出码 ${result.exitCode}`,
    command: `agent-browser open ${targetUrl}`, exitCode: result.exitCode, startedAt, completedAt: new Date().toISOString(),
    notRunReason: classifyEnvironmentReason(result.output),
    browserEvidence: { targetUrl, viewport, accessibilityTree: '', semanticRefs: [], consoleErrors: [] },
  })
}

/** 把已确认 Spec 的每条条件映射到独立记录，并保留稳定的 SPEC-AC-NN 编号。 */
function criterionRecord(criterion: string, index: number, scope: ValidationRecord, browser: ValidationRecord, requirement: Requirement): ValidationRecord {
  const criterionId = `SPEC-AC-${String(index + 1).padStart(2, '0')}`
  if (/(修改边界|修改范围)/.test(criterion)) return { ...scope, id: criterionId, criterionId, criterion }
  if (/(目标页面|控制台|console)/i.test(criterion)) return { ...browser, id: criterionId, criterionId, criterion }
  if (/(规则|Skill)/i.test(criterion)) return record({
    id: criterionId, criterionId, criterion, required: true, source: 'workflow', result: 'verified',
    detail: `执行完成；已加载 ${requirement.rules.filter((item) => item.active).length} 条规则和 ${requirement.skills.filter((item) => item.active).length} 个生效 Skill`,
  })
  // 任意产品行为不能仅凭“页面打开了”自动判定。agent-browser 树作为观察材料留存，
  // 最终语义判断由产品验收完成；它不是机器门禁，因此不会形成无法解除的死锁。
  return record({
    id: criterionId, criterionId, criterion, required: false, source: 'product', result: 'not-run',
    detail: 'agent-browser 已采集真实页面证据，等待产品确认该行为是否符合意图',
    notRunReason: '需要产品基于真实预览作语义判断', browserEvidence: browser.browserEvidence,
    startedAt: browser.startedAt, completedAt: browser.completedAt,
  })
}

function extractBrowserErrors(pageErrors: string, consoleOutput: string) {
  const candidates = `${pageErrors}\n${consoleOutput}`.split(/\r?\n/).map((line) => line.trim()).filter(Boolean)
  return [...new Set(candidates.filter((line) => {
    if (/no (page |console )?errors?|"errors"\s*:\s*\[\]/i.test(line)) return false
    return /(\[error\]|"type"\s*:\s*"error"|pageerror|console\.error|uncaught|unhandled)/i.test(line)
  }))].slice(0, 20)
}

function classifyEnvironmentReason(output: string) {
  if (/ENOENT|not found|not recognized/i.test(output)) return 'environment: agent-browser 未安装或不在 PATH 中'
  if (/browser.*not installed|chromium.*missing/i.test(output)) return 'environment: agent-browser 浏览器运行时未安装'
  if (/ECONNREFUSED|ERR_CONNECTION_REFUSED|timeout|timed out/i.test(output)) return 'environment: 托管预览当前不可连接'
  return 'environment: agent-browser 无法完成页面证据采集'
}

function record(input: Pick<ValidationRecord, 'id' | 'criterion' | 'result' | 'source' | 'detail' | 'required'> & Partial<ValidationRecord>): ValidationRecord {
  const now = new Date().toISOString()
  const result = { criterionId: null, command: null, exitCode: null, startedAt: now, completedAt: now, notRunReason: null, browserEvidence: null, ...input }
  // 构造函数是 JSON 入口之外的最后防线：not-run 不允许没有原因。
  if (result.result === 'not-run' && !result.notRunReason?.trim()) throw new Error(`验证项 ${result.id} 的 not-run 缺少原因`)
  if (result.result !== 'not-run') result.notRunReason = null
  return result
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
