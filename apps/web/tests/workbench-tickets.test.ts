import { describe, expect, it } from 'vitest'

import type { Requirement } from '../src/domain/workbench'
import {
  buildTicketsPrompt,
  confirmTicketVersion,
  nextTicketVersionIdentity,
  parseTicketsArtifact,
  recordTicketVersion,
  stripTicketsProtocol,
} from '../src/server/workbench-tickets'

describe('conversational Tickets gate', () => {
  it('loads the local Skill and requires read-only local publication', () => {
    const requirement = fixtureRequirement()
    const prompt = buildTicketsPrompt(requirement, 'tickets/REQ-1/v001')

    expect(prompt).toContain('.agents/skills/to-tickets/SKILL.md')
    expect(prompt).toContain('只读')
    expect(prompt).toContain('不得修改目标项目')
    expect(prompt).toContain('不得修改目标项目、创建外部 Issue')
    expect(prompt).toContain('tickets/REQ-1/v001')
  })

  it('accepts ordered blocking edges and rejects forward or missing dependencies', () => {
    const valid = protocol([
      { id: 'TICKET-01', title: '第一票', blockedBy: [], deliverable: '可演示结果一', acceptanceCriteria: ['标准一'] },
      { id: 'TICKET-02', title: '第二票', blockedBy: ['TICKET-01'], deliverable: '可演示结果二', acceptanceCriteria: ['标准二'] },
    ])
    const invalid = protocol([
      { id: 'TICKET-01', title: '第一票', blockedBy: ['TICKET-02'], deliverable: '结果', acceptanceCriteria: ['标准'] },
      { id: 'TICKET-02', title: '第二票', blockedBy: [], deliverable: '结果', acceptanceCriteria: ['标准'] },
    ])

    expect(parseTicketsArtifact(valid)?.[1].blockedBy).toEqual(['TICKET-01'])
    expect(parseTicketsArtifact(invalid)).toBeNull()
    expect(stripTicketsProtocol(`说明\n${valid}`)).toBe('说明\n')
  })

  it('versions drafts and opens Implement only after the active version is confirmed', () => {
    const requirement = fixtureRequirement()
    const parsed = parseTicketsArtifact(protocol([
      { id: 'TICKET-01', title: '交付结果', blockedBy: [], deliverable: '用户能看到结果', acceptanceCriteria: ['预览可见'] },
    ]))!
    const identity = nextTicketVersionIdentity(requirement)
    const draft = recordTicketVersion(
      requirement,
      parsed.map((ticket) => ({ ...ticket, path: `${identity.directory}/01-ticket.md` })),
      identity,
    )
    const confirmed = confirmTicketVersion(draft, identity.id)

    expect(draft.conversation).toMatchObject({ phase: 'to-tickets', status: 'awaiting-confirmation' })
    expect(draft.conversation.messages.at(-1)?.segments.at(-1)).toMatchObject({ type: 'tickets', versionId: identity.id })
    expect(confirmed.conversation).toMatchObject({ phase: 'implement', activeSkill: 'implement', status: 'idle' })
    expect(confirmed.tickets).toEqual([expect.objectContaining({ id: 'TICKET-01', status: 'queued' })])
    expect(confirmTicketVersion(confirmed, identity.id)).toBe(confirmed)
    expect(() => confirmTicketVersion(draft, 'REQ-1-tickets-v0')).toThrow('当前有效')
    expect(() => confirmTicketVersion({ ...draft, conversation: { ...draft.conversation, phase: 'to-spec' } }, identity.id)).toThrow('当前阶段')
    expect(() => confirmTicketVersion({ ...draft, conversation: { ...draft.conversation, status: 'running' } }, identity.id)).toThrow('当前阶段')
  })
})

function protocol(tickets: unknown[]) {
  return `[WORKBENCH:TICKETS]${JSON.stringify({ tickets })}[/WORKBENCH:TICKETS]`
}

function fixtureRequirement(): Requirement {
  const now = '2026-09-16T00:00:00.000Z'
  return {
    id: 'REQ-1', specId: 'SPEC-1', title: '优化空状态', targetPage: '/competition', requestedOutcome: '文案更明确',
    stage: 'ticket-drafting', createdAt: now, updatedAt: now, modificationRounds: 0,
    metrics: { firstPreviewAt: null, acceptedAt: null }, refinements: [],
    grilling: { status: 'confirmed', questions: [], confirmedAt: now },
    conversation: {
      id: 'CONVERSATION-REQ-1', phase: 'to-tickets', status: 'running', planningThreadId: 'thread-1', implementationThreadId: null,
      activeSkill: 'to-tickets', understanding: { summary: '摘要', desiredOutcome: '结果', changeBoundary: 'apps/front', acceptanceSignal: '预览' },
      pendingUnderstandingRevision: null, turnReadyForConfirmation: false,
      messages: [{ id: 'assistant', role: 'assistant', phase: 'to-tickets', status: 'streaming', segments: [], recordedAt: now }], error: null,
    },
    spec: null,
    specVersions: [{ id: 'SPEC-1-v1', version: 1, status: 'confirmed', path: 'specs/REQ-1/v001.md', createdAt: now, confirmedAt: now, document: {
      problem: '问题', solution: '方案', userStories: ['故事'], implementationDecisions: ['决定'], testingDecisions: ['测试'], outOfScope: ['非目标'], furtherNotes: ['说明'],
    } }],
    activeSpecVersionId: 'SPEC-1-v1', ticketVersions: [], activeTicketVersionId: null, tickets: [],
    rules: [], workflow: [], skills: [], boundaryItems: [], evidence: [],
    targetSnapshot: { name: 'Fixture', repositoryRoot: process.cwd(), modificationScope: 'apps/front', startCommand: { command: 'node', args: [] }, healthUrl: 'http://127.0.0.1:1', previewUrl: 'http://127.0.0.1:1', previewGatewayPort: 4174, startupTimeoutMs: 1000 },
    processFacts: { status: 'stopped', ownership: 'none', pid: null, command: '', healthUrl: '', previewUrl: '', gatewayUrl: '', startedAt: null, error: null, recentOutput: [] },
    branch: null, codex: { status: 'idle', sessionId: null, activeTicketId: null, changedFiles: [], events: [], error: null },
    validation: [], delivery: null, agentSessionHandoff: null,
  }
}
