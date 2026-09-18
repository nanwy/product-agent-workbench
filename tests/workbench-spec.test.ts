import { describe, expect, it } from 'vitest'

import type { Requirement, SpecDocument } from '../src/domain/workbench'
import { confirmSpecVersion, nextSpecVersionIdentity, recordSpecVersion, reviseConfirmedUnderstanding } from '../src/server/workbench-spec'

const document: SpecDocument = {
  problem: '空状态含义不清', solution: '改为明确文案', userStories: ['作为用户，我能理解下一步'],
  implementationDecisions: ['保持现有结构'], testingDecisions: ['通过真实预览检查'], outOfScope: ['不修改接口'],
  furtherNotes: ['版本只保存在工作台'],
}

describe('conversational Spec gate', () => {
  it('retains an old draft as superseded when a revision becomes current', () => {
    const initial = fixtureRequirement()
    const first = recordSpecVersion(initial, document, nextSpecVersionIdentity(initial), '2026-09-15T01:00:00.000Z')
    const second = recordSpecVersion(first, { ...document, solution: '改为更具体的引导文案' }, nextSpecVersionIdentity(first), '2026-09-15T02:00:00.000Z')

    expect(second.specVersions.map(({ version, status }) => ({ version, status }))).toEqual([
      { version: 1, status: 'superseded' }, { version: 2, status: 'draft' },
    ])
    expect(second.activeSpecVersionId).toBe('SPEC-1-v2')
    expect(second.conversation.messages.at(-1)?.segments.at(-1)).toEqual(expect.objectContaining({ type: 'spec', versionId: 'SPEC-1-v2' }))
  })

  it('confirms only the active draft and advances to Tickets without creating them', () => {
    const initial = fixtureRequirement()
    const draft = recordSpecVersion(initial, document, nextSpecVersionIdentity(initial), '2026-09-15T01:00:00.000Z')
    const confirmed = confirmSpecVersion(draft, 'SPEC-1-v1', '2026-09-15T02:00:00.000Z')

    expect(confirmed.specVersions[0].status).toBe('confirmed')
    expect(confirmed.conversation).toMatchObject({ phase: 'to-tickets', activeSkill: 'to-tickets', status: 'idle' })
    expect(confirmed.stage).toBe('ticket-drafting')
    expect(confirmed.tickets).toEqual([])
    expect(() => confirmSpecVersion(draft, 'SPEC-1-stale')).toThrow('当前有效')
    expect(confirmSpecVersion(confirmed, 'SPEC-1-v1')).toBe(confirmed)
  })

  it('invalidates downstream planning when confirmed understanding changes', () => {
    const initial = fixtureRequirement()
    const confirmed = confirmSpecVersion(recordSpecVersion(initial, document, nextSpecVersionIdentity(initial)), 'SPEC-1-v1')
    confirmed.tickets = [{ id: 'TICKET-01', title: '旧票', status: 'queued', acceptanceCriteria: ['旧标准'] }]

    const revised = reviseConfirmedUnderstanding(confirmed, { summary: '新摘要', desiredOutcome: '新结果', changeBoundary: 'apps/front', acceptanceSignal: '新验收' })

    expect(revised).toMatchObject({ stage: 'spec-drafting', activeSpecVersionId: null, tickets: [], spec: null })
    expect(revised.specVersions[0].status).toBe('superseded')
    expect(revised.conversation).toMatchObject({ phase: 'to-spec', activeSkill: 'to-spec', understanding: { desiredOutcome: '新结果' } })
  })
})

function fixtureRequirement(): Requirement {
  const now = '2026-09-15T00:00:00.000Z'
  return {
    id: 'REQ-1', specId: 'SPEC-1', title: '优化空状态', targetPage: '/competition', requestedOutcome: '文案更明确', stage: 'awaiting-spec-confirmation', createdAt: now, updatedAt: now,
    modificationRounds: 0, metrics: { firstPreviewAt: null, acceptedAt: null }, refinements: [],
    grilling: { status: 'confirmed', questions: [], confirmedAt: now },
    conversation: { id: 'CONVERSATION-REQ-1', phase: 'to-spec', status: 'running', planningThreadId: 'thread-1', implementationThreadId: null, activeSkill: 'to-spec', understanding: { summary: '摘要', desiredOutcome: '文案更明确', changeBoundary: 'apps/front', acceptanceSignal: '预览可见' }, pendingUnderstandingRevision: null, turnReadyForConfirmation: false, messages: [{ id: 'assistant', role: 'assistant', phase: 'to-spec', status: 'streaming', recordedAt: now, segments: [] }], error: null },
    spec: null, specVersions: [], activeSpecVersionId: null, ticketVersions: [], activeTicketVersionId: null, tickets: [], rules: [], workflow: [], skills: [], boundaryItems: [], evidence: [],
    targetSnapshot: { name: 'Fixture', repositoryRoot: '/tmp/fixture', modificationScope: 'apps/front', startCommand: { command: 'pnpm', args: [] }, healthUrl: 'http://127.0.0.1:1', previewUrl: 'http://127.0.0.1:1', previewGatewayPort: 4174, startupTimeoutMs: 1000 },
    processFacts: { status: 'stopped', ownership: 'none', pid: null, command: '', healthUrl: '', previewUrl: '', gatewayUrl: '', startedAt: null, error: null, recentOutput: [] },
    branch: null, codex: { status: 'idle', sessionId: null, activeTicketId: null, changedFiles: [], events: [], error: null }, validation: [], delivery: null, agentSessionHandoff: null,
  }
}
