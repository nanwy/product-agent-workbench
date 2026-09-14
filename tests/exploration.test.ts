import path from 'node:path'

import { describe, expect, it } from 'vitest'

import type { Requirement, TargetProjectConfig } from '../src/domain/workbench'
import { exploreRequirement } from '../src/server/exploration'

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
    })
    expect(result.spec.exploration.candidateCodeLocations).toContain(
      'apps/front/app/competition/page.tsx',
    )
    expect(result.spec.exploration.existingBehaviorEvidence[0]).toContain(
      '当前没有可展示的赛事',
    )
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
      { id: 'change-boundary', title: '修改边界', prompt: '', recommendation: '', answer: '仅修改赛事页' },
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
    expect(result.spec.acceptanceCriteria).toContain('确认的修改边界：仅修改赛事页')
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
    title: 'Fixture requirement',
    targetPage: '/',
    requestedOutcome,
    stage: 'intent-drafting',
    createdAt: '2026-09-14T00:00:00.000Z',
    updatedAt: '2026-09-14T00:00:00.000Z',
    modificationRounds: 0,
    grilling: { status: 'confirmed', questions: [], confirmedAt: '2026-09-14T00:00:00.000Z' },
    spec: null,
    tickets: [],
    rules: [],
    skills: [],
    boundaryItems: [],
    evidence: [],
    targetSnapshot,
    processFacts: {
      status: 'stopped',
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
  }
}
