import type { AgentEvent, Requirement } from '../domain/workbench'
import { cancelCodexTurn, event, runCodexTurn } from './codex-adapter'
import { loadTargetProjectConfig } from './config'
import { changedFilesSince, createRequirementBranch, filesOutsideScope, inspectGitWorkspace } from './git-workspace'
import { publishAgentEvent } from './agent-events'
import { requirementStore } from './requirement-store'

const globalExecution = globalThis as typeof globalThis & { __workbenchExecution?: Promise<void> | null }

export async function startRequirementExecution() {
  let requirement = await requireRequirement()
  if (requirement.stage !== 'implementation') throw new Error('只有已确认规格与边界的需求可以执行')
  const target = await loadTargetProjectConfig()

  if (!requirement.branch?.requirementBranch) {
    const branch = await createRequirementBranch(requirement, target)
    if (branch.blockedReason) {
      requirement = {
        ...requirement,
        branch,
        codex: { ...requirement.codex, status: 'failed', error: branch.blockedReason },
        updatedAt: new Date().toISOString(),
      }
      await requirementStore.save(requirement)
      return
    }
    requirement = { ...requirement, branch, updatedAt: new Date().toISOString() }
  } else {
    const unsafeReason = await executionWorkspaceProblem(requirement, target)
    if (unsafeReason) {
      await requirementStore.save({ ...requirement, codex: { ...requirement.codex, status: 'failed', error: unsafeReason }, updatedAt: new Date().toISOString() })
      return
    }
  }

  if (globalExecution.__workbenchExecution) return
  const activeIndex = requirement.tickets.findIndex((ticket) => ticket.status === 'active')
  const nextIndex = activeIndex >= 0 ? activeIndex : requirement.tickets.findIndex((ticket) => ticket.status === 'queued')
  if (nextIndex < 0) return
  const tickets = requirement.tickets.map((ticket, index) => index === nextIndex ? { ...ticket, status: 'active' as const } : ticket)
  await requirementStore.save({
    ...requirement,
    tickets,
    codex: { ...requirement.codex, status: 'running', activeTicketId: tickets[nextIndex].id, error: null },
    updatedAt: new Date().toISOString(),
  })
  globalExecution.__workbenchExecution = executeQueue().finally(() => { globalExecution.__workbenchExecution = null })
}

export async function cancelRequirementExecution() {
  const requirement = await requireRequirement()
  if (requirement.codex.status !== 'running') return
  cancelCodexTurn()
  const cancelled = event('cancelled', `已停止 ${requirement.codex.activeTicketId ?? '当前执行票'} 的 Codex turn`)
  await requirementStore.save({
    ...requirement,
    codex: { ...requirement.codex, status: 'cancelled', error: null, events: [...requirement.codex.events, cancelled].slice(-200) },
    updatedAt: new Date().toISOString(),
  })
  publishAgentEvent(cancelled)
}

async function executeQueue() {
  const target = await loadTargetProjectConfig()
  while (true) {
    const requirement = await requireRequirement()
    if (requirement.codex.status !== 'running' || !requirement.branch) return
    const ticket = requirement.tickets.find((candidate) => candidate.status === 'active')
    if (!ticket) return
    const result = await runCodexTurn({
      repositoryRoot: target.repositoryRoot,
      prompt: executionPrompt(requirement, ticket),
      sessionId: requirement.codex.sessionId,
      onEvent: persistAgentEvent,
    })
    const latest = await requireRequirement()
    if (latest.codex.status === 'cancelled') return
    const changedFiles = await changedFilesSince(requirement.branch.baseRevision, target)
    const outside = filesOutsideScope(changedFiles, target)
    const workspaceProblem = await executionWorkspaceProblem(latest, target)
    if (result.exitCode !== 0 || outside.length || workspaceProblem) {
      const reason = outside.length ? `Codex 修改越出范围：${outside.join(', ')}` : workspaceProblem ?? `Codex turn 退出码为 ${result.exitCode}`
      const failed = event('failed', reason)
      await requirementStore.save({
        ...latest,
        tickets: latest.tickets.map((candidate) => candidate.id === ticket.id ? { ...candidate, status: 'blocked' } : candidate),
        codex: { ...latest.codex, status: 'failed', sessionId: result.sessionId, changedFiles, error: reason, events: [...latest.codex.events, failed].slice(-200) },
        updatedAt: new Date().toISOString(),
      })
      publishAgentEvent(failed)
      return
    }
    const completedTickets = latest.tickets.map((candidate) => candidate.id === ticket.id ? { ...candidate, status: 'completed' as const } : candidate)
    const nextIndex = completedTickets.findIndex((candidate) => candidate.status === 'queued')
    if (nextIndex >= 0) completedTickets[nextIndex] = { ...completedTickets[nextIndex], status: 'active' }
    const completed = event('completed', `${ticket.id} 已完成${nextIndex >= 0 ? `，继续 ${completedTickets[nextIndex].id}` : '，执行队列结束'}`)
    await requirementStore.save({
      ...latest,
      tickets: completedTickets,
      modificationRounds: latest.modificationRounds + 1,
      codex: {
        ...latest.codex,
        status: nextIndex >= 0 ? 'running' : 'completed',
        sessionId: result.sessionId,
        activeTicketId: nextIndex >= 0 ? completedTickets[nextIndex].id : null,
        changedFiles,
        error: null,
        events: [...latest.codex.events, completed].slice(-200),
      },
      updatedAt: new Date().toISOString(),
    })
    publishAgentEvent(completed)
    if (nextIndex < 0) return
  }
}

async function persistAgentEvent(agentEvent: AgentEvent, sessionId: string | null) {
  const requirement = await requireRequirement()
  await requirementStore.save({
    ...requirement,
    codex: { ...requirement.codex, sessionId, events: [...requirement.codex.events, agentEvent].slice(-200) },
    updatedAt: new Date().toISOString(),
  })
  publishAgentEvent(agentEvent)
}

function executionPrompt(requirement: Requirement, ticket: Requirement['tickets'][number]) {
  return [
    `实现已确认需求：${requirement.spec?.title}`,
    `当前执行票 ${ticket.id}：${ticket.title}`,
    `目标页面：${requirement.targetPage}`,
    `允许修改范围：${requirement.targetSnapshot.modificationScope}`,
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
