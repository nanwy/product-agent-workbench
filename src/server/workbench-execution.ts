import path from 'node:path'

import type { AgentEvent, Requirement } from '../domain/workbench'
import { publishAgentEvent } from './agent-events'
import { cancelCodexTurn, event, runCodexTurn } from './codex-adapter'
import { loadTargetProjectConfig } from './config'
import { changedFilesSince, createRequirementBranch, filesOutsideScope, inspectGitWorkspace } from './git-workspace'
import { requirementStore } from './requirement-store'

const globalExecution = globalThis as typeof globalThis & {
  __workbenchExecution?: Promise<void> | null
  __workbenchExecutionStart?: Promise<void> | null
  __workbenchExecutionAbort?: AbortController | null
}

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
  const target = await loadTargetProjectConfig()

  if (!requirement.branch?.requirementBranch) {
    const branch = await createRequirementBranch(requirement, target)
    if (branch.blockedReason) {
      await requirementStore.update((current) => {
        if (!current) throw new Error('产品需求已不存在')
        return { ...current, branch, codex: { ...current.codex, status: 'failed', error: branch.blockedReason }, updatedAt: new Date().toISOString() }
      })
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

export async function cancelRequirementExecution() {
  const requirement = await requireRequirement()
  if (requirement.codex.status !== 'running') return
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
  const target = await loadTargetProjectConfig()
  const workspaceRoot = path.resolve(target.repositoryRoot, target.modificationScope)
  while (true) {
    const requirement = await requireRequirement()
    if (requirement.codex.status !== 'running' || !requirement.branch) return
    const ticket = requirement.tickets.find((candidate) => candidate.status === 'active')
    if (!ticket) return
    const result = await runCodexTurn({
      workspaceRoot,
      prompt: executionPrompt(requirement, ticket),
      sessionId: requirement.codex.sessionId,
      signal,
      onEvent: persistAgentEvent,
    })
    const latest = await requireRequirement()
    if (latest.codex.status === 'cancelled') return
    const changedFiles = await changedFilesSince(requirement.branch.baseRevision, target)
    const outside = filesOutsideScope(changedFiles, target)
    const workspaceProblem = await executionWorkspaceProblem(latest, target)
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
    if (next.codex.status === 'completed') return
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

function executionPrompt(requirement: Requirement, ticket: Requirement['tickets'][number]) {
  return [
    `实现已确认需求：${requirement.spec?.title}`,
    `当前执行票 ${ticket.id}：${ticket.title}`,
    `目标页面：${requirement.targetPage}`,
    `唯一允许写入的范围：${requirement.targetSnapshot.modificationScope}`,
    `验收条件：\n${ticket.acceptanceCriteria.map((item) => `- ${item}`).join('\n')}`,
    `生效规则：\n${requirement.rules.map((rule) => `- ${rule.source} (${rule.scope})`).join('\n')}`,
    `生效 Skills：${requirement.skills.filter((skill) => skill.active).map((skill) => skill.name).join(', ')}`,
    '直接实现并自行检查；不要提交、推送、创建 PR，也不要修改允许范围之外的文件。',
  ].join('\n\n')
}

async function requireRequirement() {
  const requirement = await requirementStore.load()
  if (!requirement) throw new Error('请先创建产品需求')
  return requirement
}

async function executionWorkspaceProblem(requirement: Requirement, target: Awaited<ReturnType<typeof loadTargetProjectConfig>>) {
  if (!requirement.branch?.requirementBranch) return '需求分支尚未创建'
  const current = await inspectGitWorkspace(target)
  if (current.baseBranch !== requirement.branch.requirementBranch) return `目标仓库已切换到其他分支：${current.baseBranch}`
  if (current.baseRevision !== requirement.branch.baseRevision) return '目标仓库 HEAD 已改变，停止自动执行'
  if (current.blockedReason?.startsWith('Git 正在进行')) return current.blockedReason
  const outside = filesOutsideScope(current.startingWorkingTree.map((entry) => entry.path), target)
  return outside.length ? `工作区包含范围外修改：${outside.join(', ')}` : null
}
