import path from 'node:path'

import { describe, expect, it } from 'vitest'

import type { Requirement, TargetProjectConfig } from '../src/domain/workbench'
import { exploreRequirement } from '../src/server/exploration'
import { buildExecutionPrompt, executionWritableRoot, recoverInterruptedRequirement } from '../src/server/workbench-execution'

// 探索测试固定规则覆盖、Skill 激活、边界识别和执行票拆分这四个纯只读行为。
describe('target project exploration', () => {
  it('resolves the root rule, closest scoped rule, and matching Skill', async () => {
    const target = targetFixture()
    const requirement = requirementFixture('更新首页空状态文案')

    const result = await exploreRequirement(requirement, target)

    expect(result.rules.map((rule) => rule.source)).toEqual([
      'AGENTS.md',
      'apps/front/AGENTS.md',
    ])
    expect(result.skills.find((skill) => skill.name === 'product-copy-change')).toMatchObject({
      active: true,
      activationReason: '需求类型确定性匹配',
      source: '.agents/skills/product-copy-change/SKILL.md',
    })
    expect(result.workflow.map((step) => step.name)).toEqual(['grilling', 'to-spec', 'to-tickets', 'implement'])
    expect(result.spec.exploration.candidateCodeLocations).toContain(
      'apps/front/app/competition/page.tsx',
    )
    expect(result.spec.exploration.existingBehaviorEvidence[0]).toContain(
      '当前没有可展示的赛事',
    )
    expect(result.tickets).toHaveLength(1)
  })

  it('keeps unrelated target Skills available without activating them', async () => {
    const result = await exploreRequirement(requirementFixture('调整比赛筛选逻辑'), targetFixture())
    const skill = result.skills.find((candidate) => candidate.name === 'product-copy-change')

    expect(skill).toMatchObject({ active: false })
  })

  it('asks Codex to read only active target-project Skills by path', async () => {
    const requirement = requirementFixture('更新首页空状态文案')
    const result = await exploreRequirement(requirement, targetFixture())
    const executable = { ...requirement, spec: result.spec, tickets: result.tickets, rules: result.rules, workflow: result.workflow, skills: result.skills }

    const prompt = buildExecutionPrompt(executable, result.tickets[0])

    expect(prompt).toContain('product-copy-change')
    expect(prompt).toContain('.agents/skills/product-copy-change/SKILL.md')
    expect(prompt).toContain(path.resolve('.agents/skills/implement/SKILL.md'))
    expect(prompt).not.toContain('Keep the change narrow and verify it in the browser.')
    expect(prompt).toContain('不采用 TDD')
    expect(prompt).toContain('中文多行 JSDoc')
    expect(prompt).not.toContain('to-spec')
    expect(prompt).not.toContain('to-tickets')
  })

  it('narrows the Codex writable root to the confirmed modification scope', () => {
    const target = targetFixture()
    expect(executionWritableRoot(target)).toBe(path.resolve(target.repositoryRoot, 'apps/front'))
    expect(() => executionWritableRoot({ ...target, modificationScope: '../outside' })).toThrow('超出目标仓库')
  })

  it('recovers a persisted running ticket after the owning service process disappears', () => {
    const requirement = requirementFixture('更新文案')
    requirement.tickets = [{ id: 'TICKET-01', title: '更新文案', status: 'active', acceptanceCriteria: [] }]
    requirement.codex = { ...requirement.codex, status: 'running', activeTicketId: 'TICKET-01', sessionId: 'session-1' }

    const recovered = recoverInterruptedRequirement(requirement, '服务已重启')

    expect(recovered.tickets[0].status).toBe('blocked')
    expect(recovered.codex).toMatchObject({ status: 'failed', sessionId: 'session-1', activeTicketId: 'TICKET-01', error: '服务已重启' })
  })

  it('raises a boundary item for authentication changes', async () => {
    const target = targetFixture()

    const result = await exploreRequirement(requirementFixture('调整登录鉴权流程'), target)

    expect(result.boundaryItems).toEqual(expect.arrayContaining([
      expect.objectContaining({ id: 'authentication', confirmed: false }),
      expect.objectContaining({ label: '规则冲突', confirmed: false }),
    ]))
  })

  it('splits a compound outcome into independently verifiable tickets', async () => {
    const requirement = requirementFixture('原始模糊描述')
    requirement.grilling.questions = [
      { id: 'desired-outcome', title: '成功结果', prompt: '', recommendation: '', answer: '更新赛事空状态文案，同时增加返回入口；保留现有筛选条件' },
      { id: 'change-boundary', title: '修改边界', prompt: '', recommendation: '', answer: 'apps/front/app/competition' },
      { id: 'acceptance-signal', title: '验收信号', prompt: '', recommendation: '', answer: '预览出现新文案与返回入口' },
    ]
    const result = await exploreRequirement(requirement, targetFixture())

    expect(result.tickets).toHaveLength(3)
    expect(result.tickets.map((ticket) => ticket.id)).toEqual([
      'TICKET-01',
      'TICKET-02',
      'TICKET-03',
    ])
    expect(result.spec.desiredBehavior).toContain('增加返回入口')
    expect(result.spec.acceptanceCriteria).toContain('预览出现新文案与返回入口')
    expect(result.spec.acceptanceCriteria).toContain('确认的修改边界：apps/front/app/competition')
  })
})

function targetFixture(): TargetProjectConfig {
  return {
    name: 'Fixture Product / front',
    repositoryRoot: path.resolve('tests/fixtures/target-project'),
    modificationScope: 'apps/front',
    startCommand: { command: 'node', args: ['server.mjs'] },
    healthUrl: 'http://127.0.0.1:4173/health',
    previewUrl: 'http://127.0.0.1:4173',
    previewGatewayPort: 4174,
    startupTimeoutMs: 5000,
  }
}

function requirementFixture(requestedOutcome: string): Requirement {
  const targetSnapshot = targetFixture()
  return {
    id: 'REQ-FIXTURE',
    specId: 'SPEC-FIXTURE',
    title: 'Fixture requirement',
    targetPage: '/',
    requestedOutcome,
    stage: 'intent-drafting',
    createdAt: '2026-09-14T00:00:00.000Z',
    updatedAt: '2026-09-14T00:00:00.000Z',
    modificationRounds: 0, metrics: { firstPreviewAt: null, acceptedAt: null }, refinements: [],
    grilling: { status: 'confirmed', questions: [], confirmedAt: '2026-09-14T00:00:00.000Z' },
    conversation: { id: 'CONVERSATION-REQ-FIXTURE', phase: 'to-spec', status: 'idle', planningThreadId: null, implementationThreadId: null, activeSkill: 'to-spec', understanding: null, pendingUnderstandingRevision: null, turnReadyForConfirmation: false, messages: [], error: null },
    spec: null,
    specVersions: [],
    activeSpecVersionId: null,
    ticketVersions: [],
    activeTicketVersionId: null,
    tickets: [],
    rules: [],
    workflow: [],
    skills: [],
    boundaryItems: [],
    evidence: [],
    targetSnapshot,
    processFacts: {
      status: 'stopped',
      ownership: 'none',
      pid: null,
      command: 'node server.mjs',
      healthUrl: targetSnapshot.healthUrl,
      previewUrl: targetSnapshot.previewUrl,
      gatewayUrl: 'http://127.0.0.1:4174',
      startedAt: null,
      error: null,
      recentOutput: [],
    },
    branch: null,
    codex: {
      status: 'idle',
      sessionId: null,
      activeTicketId: null,
      changedFiles: [],
      events: [],
      error: null,
    },
    validation: [],
    delivery: null, agentSessionHandoff: null,
  }
}
