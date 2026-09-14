import type { AgentEvent, Requirement } from '../domain/workbench'
import { publishAgentEvent } from './agent-events'
import { cancelCodexTurn, event, runCodexTurn } from './codex-adapter'
import { loadTargetProjectConfig } from './config'
import { changedFilesSince, createRequirementBranch, filesOutsideScope, inspectGitWorkspace } from './git-workspace'
import { requirementStore } from './requirement-store'
import { verifyCurrentRequirement } from './verification'

const globalExecution = globalThis as typeof globalThis & {
  __workbenchExecution?: Promise<void> | null
  __workbenchExecutionStart?: Promise<void> | null
  __workbenchExecutionAbort?: AbortController | null
}

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

async function prepareExecution() {
  if (globalExecution.__workbenchExecution) return
  let requirement = await requireRequirement()
  if (requirement.stage !== 'implementation') throw new Error('只有已确认规格与边界的需求可以执行')
  const configuredTarget = await loadTargetProjectConfig()
  const target = { ...configuredTarget, modificationScope: requirement.spec?.proposedScope ?? configuredTarget.modificationScope }

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
    const tickets = current.tickets.map((ticket, index) => index === nextIndex ? { ...ticket, status: 'active' as const } : ticket)
    return { ...current, tickets, codex: { ...current.codex, status: 'running', activeTicketId: tickets[nextIndex].id, error: null }, updatedAt: new Date().toISOString() }
  })
  const abortController = new AbortController()
  globalExecution.__workbenchExecutionAbort = abortController
  // 请求只等待“启动完成”，长时间 Codex 队列留在后台，进度通过 JSON 快照 + SSE 展示。
  globalExecution.__workbenchExecution = executeQueue(abortController.signal)
    .catch(async (error) => {
      const current = await requireRequirement()
      if (current.codex.status !== 'cancelled') await recordExecutionFailure(error instanceof Error ? error.message : String(error))
    })
    .finally(() => {
      globalExecution.__workbenchExecution = null
      globalExecution.__workbenchExecutionAbort = null
    })
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
  // AbortSignal 停止 adapter 消费，cancelCodexTurn 终止实际进程；两者都不删除 session ID。
  globalExecution.__workbenchExecutionAbort?.abort()
  cancelCodexTurn()
  const cancelled = event('cancelled', `已停止 ${requirement.codex.activeTicketId ?? '当前执行票'} 的 Codex turn`)
  await requirementStore.update((current) => {
    if (!current) throw new Error('产品需求已不存在')
    return { ...current, codex: { ...current.codex, status: 'cancelled', error: null, events: [...current.codex.events, cancelled].slice(-200) }, updatedAt: new Date().toISOString() }
  })
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
    const result = await runCodexTurn({
      workspaceRoot: target.repositoryRoot,
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
      codex: { ...current.codex, status: 'failed', sessionId: sessionId ?? current.codex.sessionId, changedFiles: changedFiles ?? current.codex.changedFiles, error: reason, events: [...current.codex.events, failed].slice(-200) },
      updatedAt: new Date().toISOString(),
    }
  })
  publishAgentEvent(failed)
}

export function buildExecutionPrompt(requirement: Requirement, ticket: Requirement['tickets'][number]) {
  // prompt 由已确认 Spec、当前票和解析后规则组成，不包含可执行 shell 片段。
  return [
    `实现已确认需求：${requirement.spec?.title}`,
    `当前执行票 ${ticket.id}：${ticket.title}`,
    `目标页面：${requirement.targetPage}`,
    `唯一允许写入的范围：${requirement.spec?.proposedScope ?? requirement.targetSnapshot.modificationScope}`,
    `验收条件：\n${ticket.acceptanceCriteria.map((item) => `- ${item}`).join('\n')}`,
    `生效规则：\n${requirement.rules.map((rule) => `- ${rule.source} (${rule.scope})`).join('\n')}`,
    `目标项目生效 Skills：\n${requirement.skills.filter((skill) => skill.active).map((skill) => [
      `## ${skill.name}（${skill.source}）`,
      skill.content ?? 'Skill 指令未加载，停止执行并报告配置错误。',
    ].join('\n')).join('\n\n') || '无'}`,
    '直接实现并自行检查；不要提交、推送、创建 PR，也不要修改允许范围之外的文件。',
  ].join('\n\n')
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
