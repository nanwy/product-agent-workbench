import path from 'node:path'

import type { AgentEvent, Requirement } from '../domain/workbench'
import { publishAgentEvent } from './agent-events'
import { cancelCodexTurn, event, hasActiveCodexTurn, runCodexTurn } from './codex-adapter'
import { loadTargetProjectConfig } from './config'
import { changedFilesSince, createRequirementBranch, filesOutsideScope, inspectGitWorkspace } from './git-workspace'
import { requirementStore } from './requirement-store'
import { getTargetProcessSnapshot, startTargetProject, stopTargetProject } from './target-runtime'
import { verifyCurrentRequirement } from './verification'
import { assertWorkbenchSkillBundle, resolveWorkbenchSkillPath } from './workbench-skills'

const globalExecution = globalThis as typeof globalThis & {
  __workbenchExecution?: Promise<void> | null
  __workbenchExecutionStart?: Promise<void> | null
  __workbenchExecutionAbort?: AbortController | null
  __workbenchExecutionHeartbeat?: ReturnType<typeof setInterval> | null
}

// Vite 开发态可能让后台 Server Function 与轮询请求落在不同模块实例中，后者看不到
// 前者的内存 Promise。持久化心跳才是跨模块判断“执行仍活着”的共同事实。
const EXECUTION_HEARTBEAT_INTERVAL_MS = 5_000
const EXECUTION_HEARTBEAT_GRACE_MS = 15_000

/**
 * 准备并启动当前 Requirement 的后台执行队列。
 *
 * start promise 防止双击在分支尚未记录时并发进入准备阶段；execution promise
 * 表示后台队列的完整生命周期。函数在分支与首张活动票可靠落盘后返回，不等待 Codex 完成。
 */
export function startRequirementExecution() {
  if (globalExecution.__workbenchExecutionStart) return globalExecution.__workbenchExecutionStart
  const start = prepareExecution()
  globalExecution.__workbenchExecutionStart = start
  return start.finally(() => { globalExecution.__workbenchExecutionStart = null })
}

/**
 * 把服务重启遗留的 running 状态恢复为可重试失败态。
 *
 * Requirement 会持久化，而 Promise 与 ChildProcess 只属于旧 Node 进程。若内存中已无
 * 执行事实，当前票改回 blocked；分支和 session 保留，用户可以继续而不必重建需求。
 */
export async function reconcileInterruptedExecution() {
  const requirement = await requirementStore.load()
  if (!requirement || requirement.codex.status !== 'running') return requirement
  if (globalExecution.__workbenchExecution || hasActiveCodexTurn()) return requirement
  // 刚启动或仍在写心跳的任务不能被另一个 SSR 模块实例误判为服务重启。只有共同
  // 存储超过宽限期没有任何更新，才把不可恢复的内存进程状态降级为可重试失败态。
  const lastHeartbeatAt = Date.parse(requirement.updatedAt)
  if (Number.isFinite(lastHeartbeatAt) && Date.now() - lastHeartbeatAt < EXECUTION_HEARTBEAT_GRACE_MS) return requirement
  const reason = '工作台服务在 Codex 执行期间重启；原进程已不存在，可从当前 Session 继续'
  const interrupted = event('failed', reason)
  const recovered = await requirementStore.update((current) => {
    if (!current || current.codex.status !== 'running') return current ?? requirement
    return recoverInterruptedRequirement(current, reason, interrupted)
  })
  publishAgentEvent(interrupted)
  return recovered
}

/** 纯状态转换，便于验证重启恢复不会丢失分支、session 或已完成票。 */
export function recoverInterruptedRequirement(requirement: Requirement, reason: string, interrupted = event('failed', reason)): Requirement {
  const activeTicketId = requirement.codex.activeTicketId
  return {
    ...requirement,
    tickets: requirement.tickets.map((ticket) => ticket.id === activeTicketId ? { ...ticket, status: 'blocked' as const } : ticket),
    refinements: setRefinementStatus(requirement.refinements, activeTicketId, 'failed'),
    codex: { ...requirement.codex, status: 'failed', error: reason, events: [...requirement.codex.events, interrupted].slice(-200) },
    updatedAt: new Date().toISOString(),
  }
}

async function prepareExecution() {
  if (globalExecution.__workbenchExecution) return
  let requirement = await requireRequirement()
  if (requirement.stage !== 'implementation') throw new Error('只有已确认规格与边界的需求可以执行')
  // 产品流程 Skill 必须随工作台仓库完整交付；缺失时在修改目标 Git 状态前失败。
  await assertWorkbenchSkillBundle()
  const configuredTarget = await loadTargetProjectConfig()
  const target = { ...configuredTarget, modificationScope: requirement.spec?.proposedScope ?? configuredTarget.modificationScope }
  // Session 可能执行目标项目自己的 build。先暂停工作台拥有的 dev server，避免两个
  // Next.js 进程同时写 `.next`；外部进程没有所有权，工作台不会擅自终止。
  const processBeforeExecution = getTargetProcessSnapshot(configuredTarget)
  const restoreOwnedRuntime = processBeforeExecution.status === 'ready' && processBeforeExecution.ownership === 'workbench'
  if (restoreOwnedRuntime) await stopTargetProject(configuredTarget)

  // 首次执行创建需求分支；重试/续接不再建分支，而是校验记录的 Git 起点仍然成立。
  if (!requirement.branch?.requirementBranch) {
    const branch = await createRequirementBranch(requirement, target)
    if (branch.blockedReason) {
      const blocked = event('failed', `未能创建需求分支：${branch.blockedReason}`)
      await requirementStore.update((current) => {
        if (!current) throw new Error('产品需求已不存在')
        return { ...current, branch, codex: { ...current.codex, status: 'failed', error: branch.blockedReason, events: [...current.codex.events, blocked].slice(-200) }, updatedAt: new Date().toISOString() }
      })
      publishAgentEvent(blocked)
      return
    }
    requirement = await requirementStore.update((current) => {
      if (!current) throw new Error('产品需求已不存在')
      return { ...current, branch, updatedAt: new Date().toISOString() }
    })
  } else {
    const unsafeReason = await executionWorkspaceProblem(requirement, target)
    if (unsafeReason) {
      await recordExecutionFailure(unsafeReason)
      return
    }
  }

  // 优先恢复 active，其次重试 blocked，最后取下一张 queued，从而保持单票串行语义。
  const activeIndex = requirement.tickets.findIndex((ticket) => ticket.status === 'active')
  const blockedIndex = requirement.tickets.findIndex((ticket) => ticket.status === 'blocked')
  const queuedIndex = requirement.tickets.findIndex((ticket) => ticket.status === 'queued')
  const nextIndex = activeIndex >= 0 ? activeIndex : blockedIndex >= 0 ? blockedIndex : queuedIndex
  if (nextIndex < 0) return
  await requirementStore.update((current) => {
    if (!current) throw new Error('产品需求已不存在')
    // 即使旧快照异常留下多张 active，也在启动前收敛为唯一 active Ticket。
    const tickets = current.tickets.map((ticket, index) => index === nextIndex
      ? { ...ticket, status: 'active' as const }
      : ticket.status === 'active' ? { ...ticket, status: 'queued' as const } : ticket)
    return {
      ...current,
      tickets,
      refinements: setRefinementStatus(current.refinements, tickets[nextIndex].id, 'running'),
      codex: { ...current.codex, status: 'running', activeTicketId: tickets[nextIndex].id, error: null },
      updatedAt: new Date().toISOString(),
    }
  })
  const abortController = new AbortController()
  globalExecution.__workbenchExecutionAbort = abortController
  startExecutionHeartbeat(abortController.signal)
  // 请求只等待“启动完成”，长时间 Codex 队列留在后台，进度通过 JSON 快照 + SSE 展示。
  globalExecution.__workbenchExecution = executeQueue(abortController.signal)
    .catch(async (error) => {
      const current = await requireRequirement()
      if (current.codex.status !== 'cancelled') await recordExecutionFailure(error instanceof Error ? error.message : String(error))
    })
    .finally(async () => {
      stopExecutionHeartbeat()
      globalExecution.__workbenchExecutionAbort = null
      // 成功路径的验证编排通常已经重启运行时；失败/取消路径则在这里恢复用户原先
      // 打开的预览。先看当前状态，避免重复启动同一个目标进程。
      if (restoreOwnedRuntime && getTargetProcessSnapshot(configuredTarget).status !== 'ready') {
        await startTargetProject(configuredTarget)
      }
      // 恢复动作结束后才开放下一次执行，避免新 Session 与旧预览重启发生竞态。
      globalExecution.__workbenchExecution = null
    })
}

/**
 * 在 Codex 沉默执行长命令时刷新持久化时间戳。
 *
 * 心跳不写证据事件，避免污染用户可读时间线；它只更新 `updatedAt`，供其他服务端
 * 模块实例区分“正常运行但暂时没有 JSONL”与“Node 进程已经重启”。
 */
function startExecutionHeartbeat(signal: AbortSignal) {
  stopExecutionHeartbeat()
  const heartbeat = setInterval(() => {
    void requirementStore.update((current) => {
      if (!current) throw new Error('产品需求已不存在')
      if (current.codex.status !== 'running') return current
      return { ...current, updatedAt: new Date().toISOString() }
    }).catch(() => undefined)
  }, EXECUTION_HEARTBEAT_INTERVAL_MS)
  heartbeat.unref()
  globalExecution.__workbenchExecutionHeartbeat = heartbeat
  signal.addEventListener('abort', stopExecutionHeartbeat, { once: true })
}

/** 停止当前进程拥有的执行心跳；重复调用安全。 */
function stopExecutionHeartbeat() {
  if (globalExecution.__workbenchExecutionHeartbeat) clearInterval(globalExecution.__workbenchExecutionHeartbeat)
  globalExecution.__workbenchExecutionHeartbeat = null
}

/**
 * 取消当前 Codex turn 并等待后台队列退出。
 *
 * 同时发送 AbortSignal、终止 CLI 进程并持久化 cancelled 事件；保留分支和 session，
 * 使用户之后可以继续当前需求。
 */
export async function cancelRequirementExecution() {
  const requirement = await requireRequirement()
  if (requirement.codex.status !== 'running') return
  const cancelled = event('cancelled', `已停止 ${requirement.codex.activeTicketId ?? '当前执行票'} 的 Codex turn`)
  // 先持久化 cancelled，再终止进程。否则 CLI 退出可能先触发后台 catch，把用户取消
  // 竞态覆盖成 failed；session、分支和当前票都继续保留用于恢复。
  await requirementStore.update((current) => {
    if (!current) throw new Error('产品需求已不存在')
    return {
      ...current,
      refinements: setRefinementStatus(current.refinements, current.codex.activeTicketId, 'cancelled'),
      codex: { ...current.codex, status: 'cancelled', error: null, events: [...current.codex.events, cancelled].slice(-200) },
      updatedAt: new Date().toISOString(),
    }
  })
  // AbortSignal 停止 adapter 消费，cancelCodexTurn 终止实际进程；两者都不删除 session ID。
  globalExecution.__workbenchExecutionAbort?.abort()
  cancelCodexTurn()
  publishAgentEvent(cancelled)
  await globalExecution.__workbenchExecution?.catch(() => undefined)
}

async function executeQueue(signal: AbortSignal) {
  // 每轮都从 store 重读 Requirement，确保取消、热更和页面操作产生的最新状态能被看到。
  const target = await loadTargetProjectConfig()
  while (true) {
    const requirement = await requireRequirement()
    if (requirement.codex.status !== 'running' || !requirement.branch) return
    const ticket = requirement.tickets.find((candidate) => candidate.status === 'active')
    if (!ticket) return
    const scopedTarget = { ...target, modificationScope: requirement.spec?.proposedScope ?? target.modificationScope }
    const writableRoot = executionWritableRoot(scopedTarget)
    const result = await runCodexTurn({
      // -C 与 workspace-write 同时指向确认范围，越界写入会在发生前被 Codex 沙箱拒绝。
      workspaceRoot: writableRoot,
      prompt: buildExecutionPrompt(requirement, ticket),
      sessionId: requirement.codex.sessionId,
      signal,
      onEvent: persistAgentEvent,
    })
    const latest = await requireRequirement()
    if (latest.codex.status === 'cancelled') return
    // Codex 退出后不相信其文本声明，而是用 Git 真值检查改动文件、分支和 HEAD。
    const changedFiles = await changedFilesSince(requirement.branch.baseRevision, scopedTarget)
    const outside = filesOutsideScope(changedFiles, scopedTarget)
    const workspaceProblem = await executionWorkspaceProblem(latest, scopedTarget)
    if (result.exitCode !== 0 || outside.length || workspaceProblem) {
      const reason = outside.length ? `Codex 修改越出范围：${outside.join(', ')}` : workspaceProblem ?? `Codex turn 退出码为 ${result.exitCode}`
      await recordExecutionFailure(reason, result.sessionId, changedFiles, ticket.id)
      return
    }
    const completed = event('completed', `${ticket.id} 已完成`)
    const next = await requirementStore.update((current) => {
      if (!current) throw new Error('产品需求已不存在')
      const tickets = current.tickets.map((candidate) => candidate.id === ticket.id ? { ...candidate, status: 'completed' as const } : candidate)
      const nextIndex = tickets.findIndex((candidate) => candidate.status === 'queued')
      if (nextIndex >= 0) tickets[nextIndex] = { ...tickets[nextIndex], status: 'active' }
      completed.message += nextIndex >= 0 ? `，继续 ${tickets[nextIndex].id}` : '，执行队列结束'
      return {
        ...current,
        tickets,
        modificationRounds: current.modificationRounds + 1,
        refinements: setRefinementStatus(current.refinements, ticket.id, 'completed'),
        codex: { ...current.codex, status: nextIndex >= 0 ? 'running' as const : 'completed' as const, sessionId: result.sessionId, activeTicketId: nextIndex >= 0 ? tickets[nextIndex].id : null, changedFiles, error: null, events: [...current.codex.events, completed].slice(-200) },
        updatedAt: new Date().toISOString(),
      }
    })
    publishAgentEvent(completed)
    if (next.codex.status === 'completed') {
      // Agent 退出码为 0 只证明 turn 结束，不证明代码可交付。
      // 最后一张票完成后才进入独立验证阶段，避免 Agent 自我声明“已验证”。
      await verifyCurrentRequirement()
      return
    }
  }
}

async function persistAgentEvent(agentEvent: AgentEvent, sessionId: string | null) {
  await requirementStore.update((current) => {
    if (!current) throw new Error('产品需求已不存在')
    return { ...current, codex: { ...current.codex, sessionId, events: [...current.codex.events, agentEvent].slice(-200) }, updatedAt: new Date().toISOString() }
  })
  publishAgentEvent(agentEvent)
}

async function recordExecutionFailure(reason: string, sessionId?: string | null, changedFiles?: string[], ticketId?: string) {
  const failed = event('failed', reason)
  await requirementStore.update((current) => {
    if (!current) throw new Error('产品需求已不存在')
    const activeTicketId = ticketId ?? current.codex.activeTicketId
    return {
      ...current,
      tickets: current.tickets.map((ticket) => ticket.id === activeTicketId ? { ...ticket, status: 'blocked' as const } : ticket),
      refinements: setRefinementStatus(current.refinements, activeTicketId, 'failed'),
      codex: { ...current.codex, status: 'failed', sessionId: sessionId ?? current.codex.sessionId, changedFiles: changedFiles ?? current.codex.changedFiles, error: reason, events: [...current.codex.events, failed].slice(-200) },
      updatedAt: new Date().toISOString(),
    }
  })
  publishAgentEvent(failed)
}

/** 返修状态挂在对应 Ticket 上；普通初始 Ticket 不会命中，因此保持原数组不变。 */
function setRefinementStatus(refinements: Requirement['refinements'], ticketId: string | null, status: Requirement['refinements'][number]['status']) {
  if (!ticketId) return refinements
  return refinements.map((round) => round.ticketId === ticketId ? { ...round, status } : round)
}

export function buildExecutionPrompt(requirement: Requirement, ticket: Requirement['tickets'][number]) {
  const targetSkillSources = requirement.skills
    .filter((skill) => skill.active)
    .map((skill) => `- ${skill.name}：${path.resolve(requirement.targetSnapshot.repositoryRoot, skill.source)}`)
  // prompt 只传 Skill 入口路径，不复制正文；Codex 在自己的文件上下文中读取唯一事实源。
  return [
    `实现已确认需求：${requirement.spec?.title}`,
    `当前执行票 ${ticket.id}：${ticket.title}`,
    `目标页面：${requirement.targetPage}`,
    `唯一允许写入的范围：${requirement.spec?.proposedScope ?? requirement.targetSnapshot.modificationScope}`,
    `验收条件：\n${ticket.acceptanceCriteria.map((item) => `- ${item}`).join('\n')}`,
    `生效规则：\n${requirement.rules.map((rule) => `- ${rule.source} (${rule.scope})`).join('\n')}`,
    '实现方式：直接实现，不采用 TDD；完成后再运行与改动相关的回归验证。',
    '代码可读性：关键函数补充中文多行 JSDoc，解释职责、关键调用和返回结果，便于鼠标悬浮阅读。',
    [
      '执行前使用文件读取工具完整读取以下 Skill 入口，并遵循其中按需引用的资源。',
      '这些路径才是本次执行的指令来源，不要仅凭 Skill 名称假定它已加载：',
      `- 工作台实现流程：${resolveWorkbenchSkillPath('implement')}`,
      `- 目标项目生效 Skills：\n${targetSkillSources.join('\n') || '  无'}`,
    ].join('\n'),
    '直接实现并自行检查；不要提交、推送、创建 PR，也不要修改允许范围之外的文件。',
  ].join('\n\n')
}

/** 将确认范围解析成 Codex 唯一可写根，并拒绝通过绝对路径或 `..` 逃逸仓库。 */
export function executionWritableRoot(target: Pick<Awaited<ReturnType<typeof loadTargetProjectConfig>>, 'repositoryRoot' | 'modificationScope'>) {
  const root = path.resolve(target.repositoryRoot)
  const writableRoot = path.resolve(root, target.modificationScope)
  if (writableRoot !== root && !writableRoot.startsWith(`${root}${path.sep}`)) throw new Error('执行写入范围超出目标仓库')
  return writableRoot
}

async function requireRequirement() {
  const requirement = await requirementStore.load()
  if (!requirement) throw new Error('请先创建产品需求')
  return requirement
}

async function executionWorkspaceProblem(requirement: Requirement, target: Awaited<ReturnType<typeof loadTargetProjectConfig>>) {
  // 这是每个 turn 前后都可重用的安全不变式：分支、HEAD、Git 操作和修改范围必须与记录一致。
  if (!requirement.branch?.requirementBranch) return '需求分支尚未创建'
  const current = await inspectGitWorkspace(target)
  if (current.baseBranch !== requirement.branch.requirementBranch) return `目标仓库已切换到其他分支：${current.baseBranch}`
  if (current.baseRevision !== requirement.branch.baseRevision) return '目标仓库 HEAD 已改变，停止自动执行'
  if (current.blockedReason?.startsWith('Git 正在进行')) return current.blockedReason
  const outside = filesOutsideScope(current.startingWorkingTree.map((entry) => entry.path), target)
  return outside.length ? `工作区包含范围外修改：${outside.join(', ')}` : null
}
