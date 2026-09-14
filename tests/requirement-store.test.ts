import { mkdtemp, rm, writeFile } from 'node:fs/promises'
import os from 'node:os'
import path from 'node:path'

import { afterEach, describe, expect, it } from 'vitest'

import type { Requirement } from '../src/domain/workbench'
import { RequirementStore } from '../src/server/requirement-store'

const temporaryDirectories: string[] = []

afterEach(async () => {
  await Promise.all(temporaryDirectories.splice(0).map((directory) => rm(directory, { force: true, recursive: true })))
})

describe('requirement persistence', () => {
  it('restores the same aggregate from a newly constructed store adapter', async () => {
    const directory = await mkdtemp(path.join(os.tmpdir(), 'workbench-store-'))
    temporaryDirectories.push(directory)
    const beforeRestart = new RequirementStore(directory)
    const requirement = fixtureRequirement()
    await beforeRestart.save(requirement)

    // 新实例没有前一个 adapter 的内存队列，等价验证服务重启后只依赖磁盘恢复。
    const afterRestart = new RequirementStore(directory)
    await expect(afterRestart.load()).resolves.toEqual(requirement)
  })

  it('migrates legacy mixed Skills into separate workflow and target capabilities', async () => {
    const directory = await mkdtemp(path.join(os.tmpdir(), 'workbench-store-'))
    temporaryDirectories.push(directory)
    const { workflow: _newWorkflowField, ...legacy } = fixtureRequirement()
    const legacyDocument = { ...legacy, skills: [
      { name: 'to-spec', description: 'workflow', source: 'workflow', active: true, activationReason: 'legacy', content: null, kind: 'workflow' },
      { name: 'product-copy-change', description: 'target', source: 'tests/fixtures/target-project/.agents/skills/product-copy-change/SKILL.md', active: true, activationReason: 'legacy', content: null, kind: 'target-project' },
    ] }
    await writeFile(path.join(directory, 'current-requirement.json'), JSON.stringify(legacyDocument))

    const restored = await new RequirementStore(directory).load()

    expect(restored?.workflow.map((step) => step.name)).toEqual(['to-spec'])
    expect(restored?.skills.map((skill) => skill.name)).toEqual(['product-copy-change'])
    expect(restored?.skills[0]).toEqual({
      name: 'product-copy-change',
      description: 'target',
      source: 'tests/fixtures/target-project/.agents/skills/product-copy-change/SKILL.md',
      active: true,
      activationReason: 'legacy',
    })
  })
})

function fixtureRequirement(): Requirement {
  const now = '2026-09-15T00:00:00.000Z'
  const target = {
    name: 'Fixture', repositoryRoot: process.cwd(), modificationScope: '.',
    startCommand: { command: 'node', args: [] }, healthUrl: 'http://127.0.0.1:1', previewUrl: 'http://127.0.0.1:1',
    previewGatewayPort: 4174, startupTimeoutMs: 1000,
  }
  return {
    id: 'REQ-STORE', specId: 'SPEC-STORE', title: '持久化需求', targetPage: '/', requestedOutcome: '刷新后仍存在',
    stage: 'intent-drafting', createdAt: now, updatedAt: now, modificationRounds: 0,
    grilling: { status: 'pending', questions: [], confirmedAt: null }, spec: null, tickets: [], rules: [], workflow: [], skills: [], boundaryItems: [], evidence: [],
    targetSnapshot: target,
    processFacts: { status: 'stopped', ownership: 'none', pid: null, command: '', healthUrl: target.healthUrl, previewUrl: target.previewUrl, gatewayUrl: '', startedAt: null, error: null, recentOutput: [] },
    branch: null, codex: { status: 'idle', sessionId: null, activeTicketId: null, changedFiles: [], events: [], error: null }, validation: [], delivery: null,
  }
}
