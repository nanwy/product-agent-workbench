import path from 'node:path'

import { describe, expect, it } from 'vitest'

import type { Requirement, TargetProjectConfig } from '../src/domain/workbench'
import { exploreRequirement } from '../src/server/exploration'

describe('target project exploration', () => {
  it('resolves the root rule, closest scoped rule, and matching Skill', async () => {
    const target: TargetProjectConfig = {
      name: 'Fixture Product / front',
      repositoryRoot: path.resolve('tests/fixtures/target-project'),
      modificationScope: 'apps/front',
      startCommand: { command: 'node', args: ['server.mjs'] },
      healthUrl: 'http://127.0.0.1:4173/health',
      previewUrl: 'http://127.0.0.1:4173',
      previewGatewayPort: 4174,
      startupTimeoutMs: 5000,
    }
    const requirement = requirementFixture('更新首页空状态文案')

    const result = await exploreRequirement(requirement, target)

    expect(result.rules.filter((rule) => rule.active).map((rule) => rule.source)).toEqual([
      'AGENTS.md',
      'apps/front/AGENTS.md',
    ])
    expect(result.rules.find((rule) => rule.source === 'apps/admin/AGENTS.md')?.active).toBe(false)
    expect(result.skills.find((skill) => skill.name === 'product-copy-change')).toMatchObject({
      active: true,
      activationReason: '需求类型确定性匹配',
    })
  })

  it('raises a boundary item for authentication changes', async () => {
    const target: TargetProjectConfig = {
      name: 'Fixture Product / front',
      repositoryRoot: path.resolve('tests/fixtures/target-project'),
      modificationScope: 'apps/front',
      startCommand: { command: 'node', args: ['server.mjs'] },
      healthUrl: 'http://127.0.0.1:4173/health',
      previewUrl: 'http://127.0.0.1:4173',
      previewGatewayPort: 4174,
      startupTimeoutMs: 5000,
    }

    const result = await exploreRequirement(requirementFixture('调整登录鉴权流程'), target)

    expect(result.boundaryItems).toEqual([
      expect.objectContaining({ id: 'authentication', confirmed: false }),
    ])
  })
})

function requirementFixture(requestedOutcome: string): Requirement {
  return {
    id: 'REQ-FIXTURE',
    title: 'Fixture requirement',
    targetPage: '/',
    requestedOutcome,
    stage: 'intent-drafting',
    createdAt: '2026-09-14T00:00:00.000Z',
    updatedAt: '2026-09-14T00:00:00.000Z',
    modificationRounds: 0,
    spec: null,
    tickets: [],
    rules: [],
    skills: [],
    boundaryItems: [],
    evidence: [],
  }
}
