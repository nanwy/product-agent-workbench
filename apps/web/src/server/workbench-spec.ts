import type { Requirement, SpecDocument } from '../domain/workbench'
import { resolveWorkbenchSkillPath } from './workbench-skills'

export const SPEC_ARTIFACT_START = '[WORKBENCH:SPEC]'
export const SPEC_ARTIFACT_END = '[/WORKBENCH:SPEC]'

export type SpecVersionIdentity = { id: string; version: number; path: string }

export function specArtifactPath(requirementId: string, version: number) {
  return `specs/${requirementId}/v${String(version).padStart(3, '0')}.md`
}

/** 一次分配版本号、领域 ID 和文件路径，所有调用方共享同一身份。 */
export function nextSpecVersionIdentity(requirement: Requirement): SpecVersionIdentity {
  const version = requirement.specVersions.reduce((largest, item) => Math.max(largest, item.version), 0) + 1
  return { id: `${requirement.specId}-v${version}`, version, path: specArtifactPath(requirement.id, version) }
}

function parseSpecProtocolPayload(message: string): Record<string, unknown> | null {
  const start = message.indexOf(SPEC_ARTIFACT_START)
  const end = message.indexOf(SPEC_ARTIFACT_END, start + SPEC_ARTIFACT_START.length)
  if (start < 0 || end < 0) return null
  try {
    return JSON.parse(message.slice(start + SPEC_ARTIFACT_START.length, end)) as Record<string, unknown>
  } catch {
    return null
  }
}

/** 每个 Spec Turn 都重新声明 Skill、只读边界和本地发布目标，不能依赖上一轮隐式记忆。 */
export function buildSpecPrompt(requirement: Requirement, artifactPath: string, revision = '') {
  const understanding = requirement.conversation.understanding
  if (!understanding) throw new Error('生成 Spec 前必须存在已确认的共同理解')
  return [
    '你正在 Product Agent Workbench 的 to-spec 阶段。',
    `完整读取并严格执行 Skill：${resolveWorkbenchSkillPath('to-spec')}`,
    '目标项目权限为只读。你可以检查仓库事实，但不得修改目标项目，不得生成 Tickets，也不得进入 implement。',
    `工作台本地发布路径：${artifactPath}。不要自行写文件；工作台会把你返回的结构化 artifact 原子写入该路径。`,
    `已确认共同理解：${JSON.stringify(understanding)}`,
    `需求：${requirement.title} · ${requirement.targetPage} · ${requirement.requestedOutcome}`,
    revision ? `用户对当前 Spec 的修订要求：${revision}` : '根据已确认对话与仓库事实形成第一版 Spec。',
    `最终必须输出 ${SPEC_ARTIFACT_START}{"problem":"...","solution":"...","userStories":["..."],"implementationDecisions":["..."],"testingDecisions":["..."],"outOfScope":["..."],"furtherNotes":["..."]}${SPEC_ARTIFACT_END}。所有字段必须非空；若修订改变了已确认共同理解，再加入 "understanding":{"summary":"...","desiredOutcome":"...","changeBoundary":"...","acceptanceSignal":"..."}。控制块之外可以给出简短说明。`,
  ].join('\n\n')
}

/** 解析 Agent 的隐藏 Spec 协议；任一章节缺失都会阻止产生可确认版本。 */
export function parseSpecArtifact(message: string): SpecDocument | null {
  const value = parseSpecProtocolPayload(message)
  if (!value) return null
  try {
    const textFields = ['problem', 'solution'] as const
    const listFields = ['userStories', 'implementationDecisions', 'testingDecisions', 'outOfScope', 'furtherNotes'] as const
    if (!textFields.every((field) => typeof value[field] === 'string' && value[field].trim())) return null
    if (!listFields.every((field) => Array.isArray(value[field]) && value[field].length > 0 && value[field].every((item) => typeof item === 'string' && item.trim()))) return null
    return {
      problem: (value.problem as string).trim(),
      solution: (value.solution as string).trim(),
      userStories: (value.userStories as string[]).map((item) => item.trim()),
      implementationDecisions: (value.implementationDecisions as string[]).map((item) => item.trim()),
      testingDecisions: (value.testingDecisions as string[]).map((item) => item.trim()),
      outOfScope: (value.outOfScope as string[]).map((item) => item.trim()),
      furtherNotes: (value.furtherNotes as string[]).map((item) => item.trim()),
    }
  } catch {
    return null
  }
}

/** 当 Spec 协议显式携带更新后的共同理解时，校验四个稳定字段后交给失效逻辑。 */
export function parseRevisedUnderstanding(message: string): Requirement['conversation']['understanding'] {
  const value = parseSpecProtocolPayload(message)
  if (!value) return null
  try {
    const understanding = value.understanding as Record<string, unknown> | undefined
    const fields = ['summary', 'desiredOutcome', 'changeBoundary', 'acceptanceSignal'] as const
    if (!understanding || !fields.every((field) => typeof understanding[field] === 'string' && understanding[field].trim())) return null
    return Object.fromEntries(fields.map((field) => [field, (understanding[field] as string).trim()])) as NonNullable<Requirement['conversation']['understanding']>
  } catch {
    return null
  }
}

/** 控制块供服务端消费，不进入用户可见 Markdown。 */
export function stripSpecProtocol(message: string) {
  const start = message.indexOf(SPEC_ARTIFACT_START)
  const end = message.indexOf(SPEC_ARTIFACT_END, start + SPEC_ARTIFACT_START.length)
  if (start < 0) return message
  // 消息正文可能以未完成快照到达；控制块未闭合时，从起始标记起暂存，避免内部 JSON 短暂闪现。
  if (end < 0) return message.slice(0, start)
  return `${message.slice(0, start)}${message.slice(end + SPEC_ARTIFACT_END.length)}`
}

export function renderSpecMarkdown(requirement: Requirement, document: SpecDocument) {
  const list = (items: string[]) => items.map((item, index) => `${index + 1}. ${item}`).join('\n')
  return [
    `# ${requirement.title}`,
    '', '## Problem Statement', '', document.problem,
    '', '## Solution', '', document.solution,
    '', '## User Stories', '', list(document.userStories),
    '', '## Implementation Decisions', '', list(document.implementationDecisions),
    '', '## Testing Decisions', '', list(document.testingDecisions),
    '', '## Out of Scope', '', list(document.outOfScope),
    '', '## Further Notes', '', list(document.furtherNotes),
  ].join('\n')
}

/** 将一个成功 Turn 固化为新草稿；旧草稿只改变状态，不删除内容或本地文件。 */
export function recordSpecVersion(
  requirement: Requirement,
  document: SpecDocument,
  identity: SpecVersionIdentity,
  recordedAt = new Date().toISOString(),
): Requirement {
  if (requirement.conversation.phase !== 'to-spec') throw new Error('当前需求不在 to-spec 阶段')
  const { id: versionId, version, path: artifactPath } = identity
  const messages = requirement.conversation.messages.map((message, index, all) =>
    index === all.length - 1 && message.role === 'assistant'
      ? { ...message, status: 'completed' as const, segments: [...message.segments, { id: `${versionId}-segment`, type: 'spec' as const, versionId }] }
      : message,
  )
  return {
    ...requirement,
    stage: 'awaiting-spec-confirmation',
    updatedAt: recordedAt,
    spec: null,
    tickets: [],
    ticketVersions: requirement.ticketVersions.map((item) => item.status === 'superseded' ? item : { ...item, status: 'superseded' as const, confirmedAt: null }),
    activeTicketVersionId: null,
    boundaryItems: [],
    specVersions: [
      ...requirement.specVersions.map((item) => item.status === 'superseded' ? item : { ...item, status: 'superseded' as const, confirmedAt: null }),
      { id: versionId, version, status: 'draft', path: artifactPath, createdAt: recordedAt, confirmedAt: null, document },
    ],
    activeSpecVersionId: versionId,
    conversation: { ...requirement.conversation, status: 'awaiting-confirmation', turnReadyForConfirmation: true, pendingUnderstandingRevision: null, messages, error: null },
  }
}

/** 服务端 Spec 门禁：只接受当前草稿，重复确认同一已确认版本保持幂等。 */
export function confirmSpecVersion(
  requirement: Requirement,
  versionId: string,
  confirmedAt = new Date().toISOString(),
): Requirement {
  const active = requirement.specVersions.find((item) => item.id === requirement.activeSpecVersionId)
  if (requirement.conversation.phase === 'to-tickets' && active?.id === versionId && active.status === 'confirmed') return requirement
  if (requirement.conversation.phase !== 'to-spec' || requirement.conversation.status !== 'awaiting-confirmation')
    throw new Error('当前阶段不能确认 Spec')
  if (!active || active.id !== versionId || active.status !== 'draft') throw new Error('只能确认当前有效的 Spec 草稿')
  return {
    ...requirement,
    stage: 'ticket-drafting',
    updatedAt: confirmedAt,
    tickets: [],
    ticketVersions: requirement.ticketVersions.map((item) => item.status === 'superseded' ? item : { ...item, status: 'superseded' as const, confirmedAt: null }),
    activeTicketVersionId: null,
    specVersions: requirement.specVersions.map((item) => item.id === versionId ? { ...item, status: 'confirmed' as const, confirmedAt } : item),
    conversation: {
      ...requirement.conversation,
      phase: 'to-tickets',
      activeSkill: 'to-tickets',
      status: 'idle',
      turnReadyForConfirmation: false,
      pendingUnderstandingRevision: null,
      messages: [...requirement.conversation.messages, {
        id: `${versionId}-confirmed-message`, role: 'system', phase: 'to-spec', status: 'completed', recordedAt: confirmedAt,
        segments: [{ id: `${versionId}-confirmed-gate`, type: 'gate', label: 'Spec 已确认', detail: `v${active.version} 已冻结 · 等待进入 to-tickets` }],
      }],
    },
  }
}

/** 共同理解发生变化时，所有依赖旧事实的规划产物立即失效，再从 to-spec 继续。 */
export function reviseConfirmedUnderstanding(
  requirement: Requirement,
  understanding: NonNullable<Requirement['conversation']['understanding']>,
  recordedAt = new Date().toISOString(),
): Requirement {
  return {
    ...requirement,
    stage: 'spec-drafting',
    updatedAt: recordedAt,
    spec: null,
    tickets: [],
    boundaryItems: [],
    activeSpecVersionId: null,
    activeTicketVersionId: null,
    specVersions: requirement.specVersions.map((version) => version.status === 'superseded' ? version : { ...version, status: 'superseded' as const, confirmedAt: null }),
    ticketVersions: requirement.ticketVersions.map((version) => version.status === 'superseded' ? version : { ...version, status: 'superseded' as const, confirmedAt: null }),
    conversation: {
      ...requirement.conversation,
      phase: 'to-spec',
      activeSkill: 'to-spec',
      status: 'idle',
      turnReadyForConfirmation: false,
      understanding,
      pendingUnderstandingRevision: null,
      error: null,
    },
  }
}
