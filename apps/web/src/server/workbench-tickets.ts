import type { ExecutionTicket, Requirement, TicketDraft } from '../domain/workbench'
import { resolveWorkbenchSkillPath } from './workbench-skills'

export const TICKETS_ARTIFACT_START = '[WORKBENCH:TICKETS]'
export const TICKETS_ARTIFACT_END = '[/WORKBENCH:TICKETS]'

export type TicketVersionIdentity = { id: string; version: number; directory: string }
export type ParsedTicket = Omit<TicketDraft, 'path'>

export function ticketsArtifactDirectory(requirementId: string, version: number) {
  return `tickets/${requirementId}/v${String(version).padStart(3, '0')}`
}

/** Tickets 与 Spec 分别编号；修订 Tickets 不会伪造新的 Spec 版本。 */
export function nextTicketVersionIdentity(requirement: Requirement): TicketVersionIdentity {
  const versions = requirement.ticketVersions
  const version = versions.reduce((largest, item) => Math.max(largest, item.version), 0) + 1
  return {
    id: `${requirement.id}-tickets-v${version}`,
    version,
    directory: ticketsArtifactDirectory(requirement.id, version),
  }
}

/** 每个 Turn 重申 Skill、本地发布目录和只读权限，避免依赖 Thread 的隐式记忆。 */
export function buildTicketsPrompt(requirement: Requirement, directory: string, revision = '') {
  const spec = requirement.specVersions.find((item) => item.id === requirement.activeSpecVersionId)
  if (!spec || spec.status !== 'confirmed') throw new Error('生成 Tickets 前必须存在已确认的 Spec')
  return [
    '你正在 Product Agent Workbench 的 to-tickets 阶段。',
    `完整读取并严格执行 Skill：${resolveWorkbenchSkillPath('to-tickets')}`,
    '目标项目权限为只读。可以检查仓库事实，但不得修改目标项目、创建外部 Issue、标签、评论、PR 或远程依赖关系。',
    `工作台本地发布目录：${directory}。不要自行写文件；工作台会把结构化 artifact 原子写成一票一文件。`,
    `已确认 Spec：${JSON.stringify(spec.document)}`,
    revision ? `用户对当前 Tickets 的修订要求：${revision}` : '把当前 Spec 拆成最小但完整、可独立演示或验证的 tracer-bullet Tickets。',
    'blocking edges 只能引用本次列表中排在当前票之前的 Ticket ID；没有真实依赖时必须使用空数组。',
    `最终必须输出 ${TICKETS_ARTIFACT_START}{"tickets":[{"id":"TICKET-01","title":"...","blockedBy":[],"deliverable":"...","acceptanceCriteria":["..."]}]}${TICKETS_ARTIFACT_END}。至少一票，所有字段非空；控制块之外可以给出简短说明。`,
  ].join('\n\n')
}

function protocolPayload(message: string): Record<string, unknown> | null {
  const start = message.indexOf(TICKETS_ARTIFACT_START)
  const end = message.indexOf(TICKETS_ARTIFACT_END, start + TICKETS_ARTIFACT_START.length)
  if (start < 0 || end < 0) return null
  try {
    return JSON.parse(message.slice(start + TICKETS_ARTIFACT_START.length, end)) as Record<string, unknown>
  } catch {
    return null
  }
}

/** 严格校验顺序和依赖边，避免把循环依赖或不存在的票带入执行队列。 */
export function parseTicketsArtifact(message: string): ParsedTicket[] | null {
  const payload = protocolPayload(message)
  if (!payload || !Array.isArray(payload.tickets) || payload.tickets.length === 0) return null
  const parsed: ParsedTicket[] = []
  for (const [index, value] of payload.tickets.entries()) {
    if (!value || typeof value !== 'object') return null
    const item = value as Record<string, unknown>
    const expectedId = `TICKET-${String(index + 1).padStart(2, '0')}`
    if (item.id !== expectedId || typeof item.title !== 'string' || !item.title.trim()) return null
    if (typeof item.deliverable !== 'string' || !item.deliverable.trim()) return null
    if (!Array.isArray(item.acceptanceCriteria) || item.acceptanceCriteria.length === 0
      || !item.acceptanceCriteria.every((criterion) => typeof criterion === 'string' && criterion.trim())) return null
    if (!Array.isArray(item.blockedBy) || !item.blockedBy.every((blocker) => typeof blocker === 'string')) return null
    const earlier = new Set(parsed.map(({ id }) => id))
    if (!(item.blockedBy as string[]).every((blocker) => earlier.has(blocker))) return null
    parsed.push({
      id: expectedId,
      title: item.title.trim(),
      blockedBy: [...new Set(item.blockedBy as string[])],
      deliverable: item.deliverable.trim(),
      acceptanceCriteria: (item.acceptanceCriteria as string[]).map((criterion) => criterion.trim()),
    })
  }
  return parsed
}

/** 隐藏控制协议永不作为聊天 Markdown 展示，未闭合时也不会闪出 JSON。 */
export function stripTicketsProtocol(message: string) {
  const start = message.indexOf(TICKETS_ARTIFACT_START)
  const end = message.indexOf(TICKETS_ARTIFACT_END, start + TICKETS_ARTIFACT_START.length)
  if (start < 0) return message
  if (end < 0) return message.slice(0, start)
  return `${message.slice(0, start)}${message.slice(end + TICKETS_ARTIFACT_END.length)}`
}

export function renderTicketMarkdown(ticket: ParsedTicket) {
  const blockers = ticket.blockedBy.length ? ticket.blockedBy.join(', ') : 'None — can start immediately'
  return [
    `# ${ticket.id} — ${ticket.title}`,
    '', `**What to build:** ${ticket.deliverable}`,
    '', `**Blocked by:** ${blockers}`,
    '', '**Status:** ready-for-agent',
    '', ...ticket.acceptanceCriteria.map((criterion) => `- [ ] ${criterion}`),
  ].join('\n')
}

/**
 * 把确认后的不可变 Ticket artifact 转成第一批执行状态。
 *
 * 没有前置依赖的根 Ticket 可以立即进入队列；其余 Ticket 只保存为 pending，不能因为它们
 * 在 artifact 中排得靠前就提前执行。当前 Node MVP 仍由一个执行循环消费 queued，Go
 * Scheduler 接管后也必须保持相同语义。
 */
export function initializeTicketQueue(tickets: TicketDraft[]): ExecutionTicket[] {
  return tickets.map((ticket) => ({
    ...ticket,
    status: ticket.blockedBy.length === 0 ? 'queued' as const : 'pending' as const,
  }))
}

/**
 * 根据已经完成的 Ticket 解锁所有新就绪节点。
 *
 * 这是 Ticket DAG 的纯计算 seam：只把 pending 变为 queued，不修改 active、失败待重试或
 * 已完成状态。依赖失败/取消时不会出现在 completed 集合中，因此后继票会继续 pending。
 */
export function releaseReadyTickets(tickets: ExecutionTicket[]): ExecutionTicket[] {
  const completed = new Set(
    tickets.filter((ticket) => ticket.status === 'completed').map((ticket) => ticket.id),
  )
  return tickets.map((ticket) => {
    if (ticket.status !== 'pending') return ticket
    const blockers = ticket.blockedBy ?? []
    return blockers.length > 0 && blockers.every((blocker) => completed.has(blocker))
      ? { ...ticket, status: 'queued' as const }
      : ticket
  })
}

/** 固化当前草稿并把卡片挂到最后一条 assistant 消息；旧版本只标记失效。 */
export function recordTicketVersion(
  requirement: Requirement,
  tickets: TicketDraft[],
  identity: TicketVersionIdentity,
  recordedAt = new Date().toISOString(),
): Requirement {
  if (requirement.conversation.phase !== 'to-tickets') throw new Error('当前需求不在 to-tickets 阶段')
  const versions = requirement.ticketVersions
  const messages = requirement.conversation.messages.map((message, index, all) =>
    index === all.length - 1 && message.role === 'assistant'
      ? { ...message, status: 'completed' as const, segments: [...message.segments, { id: `${identity.id}-segment`, type: 'tickets' as const, versionId: identity.id }] }
      : message,
  )
  return {
    ...requirement,
    stage: 'ticket-drafting',
    updatedAt: recordedAt,
    tickets: [],
    ticketVersions: [
      ...versions.map((version) => version.status === 'superseded' ? version : { ...version, status: 'superseded' as const, confirmedAt: null }),
      { ...identity, status: 'draft', createdAt: recordedAt, confirmedAt: null, tickets },
    ],
    activeTicketVersionId: identity.id,
    conversation: { ...requirement.conversation, status: 'awaiting-confirmation', turnReadyForConfirmation: true, messages, error: null },
  }
}

/** 只有当前草稿能开放 Implement；重复确认同一版本保持幂等。 */
export function confirmTicketVersion(
  requirement: Requirement,
  versionId: string,
  confirmedAt = new Date().toISOString(),
): Requirement {
  const versions = requirement.ticketVersions
  const active = versions.find((version) => version.id === requirement.activeTicketVersionId)
  if (requirement.conversation.phase === 'implement' && active?.id === versionId && active.status === 'confirmed') return requirement
  if (requirement.conversation.phase !== 'to-tickets' || requirement.conversation.status !== 'awaiting-confirmation')
    throw new Error('当前阶段不能确认 Tickets')
  if (!active || active.id !== versionId || active.status !== 'draft') throw new Error('只能确认当前有效的 Tickets 草稿')
  return {
    ...requirement,
    stage: 'implementation',
    updatedAt: confirmedAt,
    ticketVersions: versions.map((version) => version.id === versionId ? { ...version, status: 'confirmed' as const, confirmedAt } : version),
    tickets: initializeTicketQueue(active.tickets),
    conversation: {
      ...requirement.conversation,
      phase: 'implement',
      activeSkill: 'implement',
      status: 'idle',
      turnReadyForConfirmation: false,
      messages: [...requirement.conversation.messages, {
        id: `${versionId}-confirmed-message`, role: 'system', phase: 'to-tickets', status: 'completed', recordedAt: confirmedAt,
        segments: [{ id: `${versionId}-confirmed-gate`, type: 'gate', label: 'Tickets 已确认', detail: `v${active.version} 已冻结 · 可以进入 implement` }],
      }],
      error: null,
    },
  }
}
