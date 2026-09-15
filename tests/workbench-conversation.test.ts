import { describe, expect, it } from 'vitest'

import type { AgentEvent, Requirement } from '../src/domain/workbench'
import {
  applyPlanningEvent,
  beginAssistantTurn,
  buildGrillingPrompt,
  buildGrillingRecoveryPrompt,
  createWorkbenchConversation,
} from '../src/server/workbench-conversation'
import { buildSpecPrompt, parseSpecArtifact, stripSpecProtocol } from '../src/server/workbench-spec'

describe('workbench conversation', () => {
  it('starts every Spec turn with the local Skill, read-only boundary, and local artifact protocol', () => {
    const requirement = fixtureRequirement()
    requirement.grilling.status = 'confirmed'
    requirement.conversation.understanding = {
      summary: '保持结构，只优化文案', desiredOutcome: '空状态更明确', changeBoundary: 'apps/front', acceptanceSignal: '预览可见且无报错',
    }

    const prompt = buildSpecPrompt(requirement, 'specs/REQ-1/v001.md')

    expect(prompt).toContain('.agents/skills/to-spec/SKILL.md')
    expect(prompt).toContain('只读')
    expect(prompt).toContain('specs/REQ-1/v001.md')
    expect(prompt).toContain('[WORKBENCH:SPEC]')
    expect(prompt).toContain('不得生成 Tickets')
  })

  it('accepts only complete structured Spec artifacts', () => {
    const artifact = parseSpecArtifact('[WORKBENCH:SPEC]{"problem":"问题","solution":"方案","userStories":["故事"],"implementationDecisions":["决定"],"testingDecisions":["测试"],"outOfScope":["非目标"],"furtherNotes":["说明"]}[/WORKBENCH:SPEC]')

    expect(artifact).toEqual({ problem: '问题', solution: '方案', userStories: ['故事'], implementationDecisions: ['决定'], testingDecisions: ['测试'], outOfScope: ['非目标'], furtherNotes: ['说明'] })
    expect(parseSpecArtifact('[WORKBENCH:SPEC]{"problem":"问题"}[/WORKBENCH:SPEC]')).toBeNull()
  })

  it('never exposes a partially streamed Spec control block', () => {
    expect(stripSpecProtocol('可见说明\n[WORKBENCH:SPEC]{"problem":"尚未闭合')).toBe('可见说明\n')
  })

  it('starts a read-only Grilling conversation governed by the local Skill', () => {
    const requirement = fixtureRequirement()
    const conversation = createWorkbenchConversation(requirement.id, requirement.createdAt)
    const prompt = buildGrillingPrompt(requirement)

    expect(conversation).toMatchObject({ phase: 'grilling', status: 'idle', planningThreadId: null, activeSkill: 'grilling' })
    expect(prompt).toContain('.agents/skills/grilling/SKILL.md')
    expect(prompt).toContain('frontier')
    expect(prompt).toContain('[WORKBENCH:READY_FOR_CONFIRMATION]')
    expect(prompt).toContain('不要修改目标项目')
  })

  it('replaces a growing SDK assistant item and exposes the confirmation gate only after its marker', () => {
    const initial = beginAssistantTurn(createWorkbenchConversation('REQ-1', '2026-09-15T00:00:00.000Z'))
    const streamingId = initial.messages.at(-1)!.id
    const partial = applyPlanningEvent(initial, agentEvent('message', '❓ **Q1**'), 'thread-1')
    const grown = applyPlanningEvent(partial, agentEvent('message', '❓ **Q1** - **目标**：最终看到什么？'), 'thread-1')
    const ready = applyPlanningEvent(grown, agentEvent('message', '共同理解如下\n[WORKBENCH:UNDERSTANDING]{"summary":"摘要","desiredOutcome":"结果","changeBoundary":"apps/front","acceptanceSignal":"预览验收"}[/WORKBENCH:UNDERSTANDING]\n[WORKBENCH:READY_FOR_CONFIRMATION]'), 'thread-1')
    expect(ready.status).toBe('running')
    const completed = applyPlanningEvent(ready, agentEvent('completed', 'turn.completed'), 'thread-1')

    expect(completed.planningThreadId).toBe('thread-1')
    expect(completed.status).toBe('awaiting-confirmation')
    expect(completed.understanding).toEqual({ summary: '摘要', desiredOutcome: '结果', changeBoundary: 'apps/front', acceptanceSignal: '预览验收' })
    expect(completed.messages.find(({ id }) => id === streamingId)?.segments).toEqual([
      expect.objectContaining({ type: 'text', content: '共同理解如下' }),
    ])
  })

  it('records failure as a terminal message without discarding earlier conversation', () => {
    const initial = beginAssistantTurn(createWorkbenchConversation('REQ-1', '2026-09-15T00:00:00.000Z'))
    const failed = applyPlanningEvent(initial, agentEvent('failed', 'network unavailable'), null)

    expect(failed.status).toBe('failed')
    expect(failed.error).toBe('network unavailable')
    expect(failed.messages).toHaveLength(2)
    expect(failed.messages.at(-1)).toMatchObject({ status: 'failed' })
  })

  it('updates repeated SDK activity items instead of duplicating timeline keys', () => {
    const initial = beginAssistantTurn(createWorkbenchConversation('REQ-1', '2026-09-15T00:00:00.000Z'))
    const started = applyPlanningEvent(initial, { ...agentEvent('tool', 'reading'), id: 'item-1', activityStatus: 'running' }, 'thread-1')
    const completed = applyPlanningEvent(started, { ...agentEvent('tool', 'read complete'), id: 'item-1', activityStatus: 'completed' }, 'thread-1')
    const activities = completed.messages.at(-1)?.segments.filter(({ type }) => type === 'activity')

    expect(activities).toEqual([expect.objectContaining({ id: 'item-1', detail: 'read complete', status: 'completed' })])
  })

  it('preserves distinct assistant items within one turn', () => {
    const initial = beginAssistantTurn(createWorkbenchConversation('REQ-1', '2026-09-15T00:00:00.000Z'))
    const first = applyPlanningEvent(initial, { ...agentEvent('message', '先前说明'), id: 'message-1' }, 'thread-1')
    const second = applyPlanningEvent(first, { ...agentEvent('message', '最终问题'), id: 'message-2' }, 'thread-1')

    expect(second.messages.at(-1)?.segments).toEqual([
      expect.objectContaining({ id: 'message-1', content: '先前说明' }),
      expect.objectContaining({ id: 'message-2', content: '最终问题' }),
    ])
  })

  it('keeps earlier activities when the assistant text grows', () => {
    const initial = beginAssistantTurn(createWorkbenchConversation('REQ-1', '2026-09-15T00:00:00.000Z'))
    const active = applyPlanningEvent(initial, { ...agentEvent('tool', 'read complete'), id: 'item-1' }, 'thread-1')
    const withText = applyPlanningEvent(active, agentEvent('message', '问题内容'), 'thread-1')

    expect(withText.messages.at(-1)?.segments.map(({ type }) => type)).toEqual(['activity', 'text'])
  })

  it('rebuilds full Skill and transcript context when no SDK thread can be resumed', () => {
    const requirement = fixtureRequirement()
    requirement.conversation = appendAnswer(requirement.conversation, '我选择推荐范围')
    const prompt = buildGrillingRecoveryPrompt(requirement)

    expect(prompt).toContain('.agents/skills/grilling/SKILL.md')
    expect(prompt).toContain('产品需求：优化登录')
    expect(prompt).toContain('USER: 我选择推荐范围')
  })
})

function appendAnswer(conversation: Requirement['conversation'], content: string): Requirement['conversation'] {
  return {
    ...conversation,
    messages: [...conversation.messages, {
      id: 'answer-1', role: 'user', phase: 'grilling', status: 'completed', recordedAt: '2026-09-15T00:01:00.000Z',
      segments: [{ id: 'answer-text-1', type: 'text', content }],
    }],
  }
}

function agentEvent(type: AgentEvent['type'], message: string): AgentEvent {
  return { id: `event-${type}`, type, message, recordedAt: '2026-09-15T00:01:00.000Z' }
}

function fixtureRequirement(): Requirement {
  const now = '2026-09-15T00:00:00.000Z'
  const target = {
    name: 'Fixture', repositoryRoot: '/tmp/fixture', modificationScope: 'apps/front', startCommand: { command: 'pnpm', args: [] },
    healthUrl: 'http://127.0.0.1:1', previewUrl: 'http://127.0.0.1:1', previewGatewayPort: 4174, startupTimeoutMs: 1000,
  }
  return {
    id: 'REQ-1', specId: 'SPEC-1', title: '优化登录', targetPage: '/sign-in', requestedOutcome: '手机号登录', stage: 'intent-drafting',
    createdAt: now, updatedAt: now, modificationRounds: 0, metrics: { firstPreviewAt: null, acceptedAt: null }, refinements: [],
    grilling: { status: 'pending', questions: [], confirmedAt: null }, conversation: createWorkbenchConversation('REQ-1', now),
    spec: null, specVersions: [], activeSpecVersionId: null, tickets: [], rules: [], workflow: [], skills: [], boundaryItems: [], evidence: [], targetSnapshot: target,
    processFacts: { status: 'stopped', ownership: 'none', pid: null, command: '', healthUrl: target.healthUrl, previewUrl: target.previewUrl, gatewayUrl: '', startedAt: null, error: null, recentOutput: [] },
    branch: null, codex: { status: 'idle', sessionId: null, activeTicketId: null, changedFiles: [], events: [], error: null }, validation: [], delivery: null, agentSessionHandoff: null,
  }
}
