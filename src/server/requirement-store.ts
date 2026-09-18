import { randomUUID } from 'node:crypto'
import { mkdir, readFile, readdir, rename, writeFile } from 'node:fs/promises'
import path from 'node:path'

import type { ConversationMessage, DiscoveredSkill, Requirement, TicketDraft, WorkbenchConversation, WorkflowStep } from '../domain/workbench'
import { specArtifactPath } from './workbench-spec'
import { renderTicketMarkdown, ticketsArtifactDirectory, type ParsedTicket } from './workbench-tickets'

export class RequirementStore {
  readonly #filePath: string
  readonly #historyDirectory: string
  readonly #specDirectory: string
  readonly #ticketsDirectory: string
  #queue: Promise<unknown> = Promise.resolve()

  constructor(dataDirectory = process.env.WORKBENCH_DATA_DIR ?? '.data') {
    this.#filePath = path.resolve(dataDirectory, 'current-requirement.json')
    this.#historyDirectory = path.resolve(dataDirectory, 'requirements')
    this.#specDirectory = path.resolve(dataDirectory, 'specs')
    this.#ticketsDirectory = path.resolve(dataDirectory, 'tickets')
  }

  /** 把 Spec 发布到工作台数据目录；返回可持久化、与机器无关的相对路径。 */
  async writeSpecArtifact(requirementId: string, version: number, content: string) {
    if (!/^REQ-[A-Za-z0-9-]+$/.test(requirementId) || !Number.isSafeInteger(version) || version < 1)
      throw new Error('Spec artifact 标识或版本无效')
    const relativePath = specArtifactPath(requirementId, version)
    const filePath = path.join(this.#specDirectory, requirementId, path.basename(relativePath))
    await mkdir(path.dirname(filePath), { recursive: true })
    const temporaryPath = `${filePath}.${process.pid}.${randomUUID()}.tmp`
    await writeFile(temporaryPath, `${content.trimEnd()}\n`, { mode: 0o600 })
    await rename(temporaryPath, filePath)
    return relativePath
  }

  /** 按依赖顺序把一个 Tickets 版本发布成一票一文件，目标项目始终不受影响。 */
  async writeTicketArtifacts(requirementId: string, version: number, tickets: ParsedTicket[]): Promise<TicketDraft[]> {
    if (!/^REQ-[A-Za-z0-9-]+$/.test(requirementId) || !Number.isSafeInteger(version) || version < 1 || tickets.length === 0)
      throw new Error('Tickets artifact 标识、版本或内容无效')
    const directory = ticketsArtifactDirectory(requirementId, version)
    const outputDirectory = path.join(this.#ticketsDirectory, requirementId, path.basename(directory))
    await mkdir(outputDirectory, { recursive: true })
    return Promise.all(tickets.map(async (ticket, index) => {
      const filename = `${String(index + 1).padStart(2, '0')}-ticket.md`
      const relativePath = `${directory}/${filename}`
      const filePath = path.join(outputDirectory, filename)
      const temporaryPath = `${filePath}.${process.pid}.${randomUUID()}.tmp`
      await writeFile(temporaryPath, `${renderTicketMarkdown(ticket).trimEnd()}\n`, { mode: 0o600 })
      await rename(temporaryPath, filePath)
      return { ...ticket, path: relativePath }
    }))
  }

  async load(): Promise<Requirement | null> {
    // 等待先前的写入队列，防止读到 rename 前的旧快照。
    await this.#queue.catch(() => undefined)
    return this.#loadDirect()
  }

  /**
   * 返回每条需求最后一次持久化的完整聚合快照。
   *
   * 当前需求也参与合并，保证升级旧数据后无需先触发一次写操作就能在历史列表看到；
   * 相同 ID 以 current 文件为准，结果按最近更新时间倒序排列。
   */
  async list(): Promise<Requirement[]> {
    await this.#queue.catch(() => undefined)
    const current = await this.#loadDirect()
    const archived = await this.#loadHistoryDirect()
    const byId = new Map(archived.map((requirement) => [requirement.id, requirement]))
    if (current) byId.set(current.id, current)
    return [...byId.values()].sort((left, right) => right.updatedAt.localeCompare(left.updatedAt))
  }

  save(requirement: Requirement): Promise<Requirement> {
    return this.#enqueue(async () => {
      await this.#writeDirect(requirement)
      return requirement
    })
  }

  update(mutator: (current: Requirement | null) => Requirement | Promise<Requirement>): Promise<Requirement> {
    // 把“读当前值 → 计算新值 → 原子写入”放进同一队列，避免 SSE 事件和按钮请求互相覆盖。
    return this.#enqueue(async () => {
      const current = await this.#loadDirect()
      const next = await mutator(current)
      // 新需求替换 current 前先固化旧聚合；随后 #writeDirect 也会保存新聚合。
      if (current && current.id !== next.id) await this.#writeHistoryDirect(current)
      await this.#writeDirect(next)
      return next
    })
  }

  #enqueue<T>(operation: () => Promise<T>): Promise<T> {
    const result = this.#queue.then(operation, operation)
    this.#queue = result.then(() => undefined, () => undefined)
    return result
  }

  async #loadDirect(): Promise<Requirement | null> {
    try {
      return await migrateRequirement(JSON.parse(await readFile(this.#filePath, 'utf8')) as Requirement)
    } catch (error) {
      if (isMissingFile(error)) return null
      throw error
    }
  }

  async #writeDirect(requirement: Requirement) {
    await this.#writeJson(this.#filePath, requirement)
  }

  async #writeHistoryDirect(requirement: Requirement) {
    await this.#writeJson(path.join(this.#historyDirectory, `${requirement.id}.json`), requirement)
  }

  async #writeJson(filePath: string, requirement: Requirement) {
    await mkdir(path.dirname(filePath), { recursive: true })
    const temporaryPath = `${filePath}.${process.pid}.${randomUUID()}.tmp`
    // 0600 限制为当前用户可读写；同目录 rename 确保读取方只会看到旧或新完整 JSON。
    await writeFile(temporaryPath, `${JSON.stringify(requirement, null, 2)}\n`, { mode: 0o600 })
    await rename(temporaryPath, filePath)
  }

  async #loadHistoryDirect(): Promise<Requirement[]> {
    let names: string[]
    try {
      names = await readdir(this.#historyDirectory)
    } catch (error) {
      if (isMissingFile(error)) return []
      throw error
    }
    return Promise.all(names
      .filter((name) => name.endsWith('.json'))
      .map(async (name) => migrateRequirement(JSON.parse(await readFile(path.join(this.#historyDirectory, name), 'utf8')) as Requirement)))
  }
}

function isMissingFile(error: unknown): error is NodeJS.ErrnoException {
  return error instanceof Error && 'code' in error && error.code === 'ENOENT'
}

/** 读取时完成向前兼容迁移，磁盘中的旧 Requirement 无需先执行破坏性升级。 */
async function migrateRequirement(requirement: Requirement): Promise<Requirement> {
  // JSON 是持久合约：新版字段必须在读取时补默认值，而不要求用户删除当前需求。
  // 旧版把工作流步骤与目标项目 Skill 混在 skills 中；读取时拆开，写回后即使用新模型。
  const legacySkills = (requirement.skills ?? []) as Array<DiscoveredSkill & {
    kind?: 'workflow' | 'target-project'
    content?: string | null
  }>
  const workflow = requirement.workflow ?? legacySkills
    .filter((skill) => skill.kind === 'workflow')
    .map(({ name, description, active, activationReason }): WorkflowStep => ({
      name,
      description,
      active,
      activationReason,
    }))
  const skills = legacySkills
    .filter((skill) => skill.kind !== 'workflow')
    // 旧快照可能缓存 Skill 全文；迁移时丢弃缓存，只保留可审计的目标仓库相对路径。
    .map(({ kind: _legacyKind, content: _legacyContent, ...skill }) => skill)
  const grilling = requirement.grilling ?? { status: 'confirmed' as const, questions: [], confirmedAt: requirement.createdAt }
  const legacyConversation = requirement.conversation as WorkbenchConversation & { confirmationReady?: boolean }
  const conversation = requirement.conversation
    ? {
        ...requirement.conversation,
        pendingUnderstandingRevision: requirement.conversation.pendingUnderstandingRevision ?? null,
        turnReadyForConfirmation: requirement.conversation.turnReadyForConfirmation ?? legacyConversation.confirmationReady ?? requirement.conversation.status === 'awaiting-confirmation',
        understanding: requirement.conversation.understanding
          ?? (requirement.conversation.status === 'awaiting-confirmation' || grilling.status === 'confirmed'
            ? legacyUnderstanding(requirement, grilling, latestAssistantText(requirement.conversation.messages))
            : null),
      }
    : migrateLegacyConversation(requirement, grilling)
  return {
    ...requirement,
    specId: requirement.specId ?? requirement.id.replace(/^REQ-/, 'SPEC-'),
    metrics: requirement.metrics ?? { firstPreviewAt: null, acceptedAt: requirement.delivery?.acceptedAt ?? null },
    refinements: requirement.refinements ?? [],
    specVersions: requirement.specVersions ?? [],
    activeSpecVersionId: requirement.activeSpecVersionId ?? null,
    ticketVersions: requirement.ticketVersions ?? [],
    activeTicketVersionId: requirement.activeTicketVersionId ?? null,
    grilling,
    conversation,
    workflow,
    skills,
    processFacts: {
      ...requirement.processFacts,
      ownership: requirement.processFacts?.ownership ?? 'none',
    },
    branch: requirement.branch ?? null,
    codex: requirement.codex ?? { status: 'idle', sessionId: null, activeTicketId: null, changedFiles: [], events: [], error: null },
    validation: (requirement.validation ?? []).map((record) => ({
      // 兼容第五票补全前已经落盘的矩阵，避免要求用户删除当前需求。
      ...record,
      criterionId: record.criterionId ?? null,
      required: record.required ?? record.id !== 'product-acceptance',
      browserEvidence: record.browserEvidence ?? null,
    })),
    delivery: requirement.delivery ? { ...requirement.delivery, artifactPaths: requirement.delivery.artifactPaths ?? [] } : null,
    agentSessionHandoff: requirement.agentSessionHandoff ?? null,
  }
}

/** 把旧三题快照投影为可继续的对话，不改变其确认事实或覆盖原始答案。 */
function migrateLegacyConversation(requirement: Requirement, grilling: Requirement['grilling']): WorkbenchConversation {
  const confirmed = grilling.status === 'confirmed'
  const messages: ConversationMessage[] = [{
    id: `legacy-${requirement.id}-gate`, role: 'system', phase: 'grilling', status: 'completed', recordedAt: requirement.createdAt,
    segments: [{ id: `legacy-${requirement.id}-gate-segment`, type: 'gate', label: '迁移 Grilling', detail: '从旧版固定问题恢复' }],
  }]
  for (const [index, question] of grilling.questions.entries()) {
    messages.push({
      id: `legacy-${requirement.id}-question-${index}`, role: 'assistant', phase: 'grilling', status: 'completed', recordedAt: requirement.createdAt,
      segments: [{ id: `legacy-${requirement.id}-question-${index}-text`, type: 'text', content: `❓ **Q${index + 1}** - **${question.title}**：${question.prompt}\n\n➡️ ${question.recommendation}` }],
    })
    if (question.answer) messages.push({
      id: `legacy-${requirement.id}-answer-${index}`, role: 'user', phase: 'grilling', status: 'completed', recordedAt: grilling.confirmedAt ?? requirement.updatedAt,
      segments: [{ id: `legacy-${requirement.id}-answer-${index}-text`, type: 'text', content: question.answer }],
    })
  }
  if (confirmed) messages.push({
    id: `legacy-${requirement.id}-confirmed`, role: 'system', phase: 'grilling', status: 'completed', recordedAt: grilling.confirmedAt ?? requirement.updatedAt,
    segments: [{ id: `legacy-${requirement.id}-confirmed-gate`, type: 'gate', label: '共同理解已确认', detail: '旧版确认记录' }],
  })
  return {
    id: `CONVERSATION-${requirement.id}`,
    phase: confirmed ? 'to-spec' : 'grilling',
    status: confirmed ? 'idle' : 'awaiting-user',
    planningThreadId: null,
    implementationThreadId: requirement.codex?.sessionId ?? null,
    activeSkill: confirmed ? 'to-spec' : 'grilling',
    understanding: confirmed ? legacyUnderstanding(requirement, grilling) : null,
    pendingUnderstandingRevision: null,
    turnReadyForConfirmation: false,
    messages,
    error: null,
  }
}

/** 把旧三题答案转换成新版结构化共同理解，保持旧需求的确认语义。 */
function legacyUnderstanding(requirement: Requirement, grilling: Requirement['grilling'], summary: string | null = '') {
  const answer = (id: string, fallback: string) => grilling.questions.find((question) => question.id === id)?.answer
    ?? grilling.questions.find((question) => question.id === id)?.recommendation
    ?? fallback
  const fallbackSummary = summary || grilling.questions.map((question) => `${question.title}：${question.answer ?? question.recommendation}`).join('\n') || requirement.requestedOutcome
  return {
    summary: fallbackSummary,
    desiredOutcome: answer('desired-outcome', requirement.requestedOutcome),
    changeBoundary: answer('change-boundary', requirement.targetSnapshot?.modificationScope ?? '.'),
    acceptanceSignal: answer('acceptance-signal', `在 ${requirement.targetPage} 观察到预期结果`),
  }
}

/** 为早期已到确认态但没有结构化字段的对话寻找可展示摘要。 */
function latestAssistantText(messages: ConversationMessage[]) {
  for (let messageIndex = messages.length - 1; messageIndex >= 0; messageIndex -= 1) {
    const message = messages[messageIndex]
    if (message.role !== 'assistant') continue
    for (let segmentIndex = message.segments.length - 1; segmentIndex >= 0; segmentIndex -= 1) {
      const segment = message.segments[segmentIndex]
      if (segment.type === 'text' && segment.content.trim()) return segment.content
    }
  }
  return null
}

export const requirementStore = new RequirementStore()
