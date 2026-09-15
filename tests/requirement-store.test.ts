import { mkdtemp, readFile, rm, writeFile } from 'node:fs/promises'
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

  it('publishes each Spec version as a separate local workbench artifact', async () => {
    const directory = await mkdtemp(path.join(os.tmpdir(), 'workbench-store-'))
    temporaryDirectories.push(directory)
    const store = new RequirementStore(directory)

    const first = await store.writeSpecArtifact('REQ-STORE', 1, '# Spec v1')
    const second = await store.writeSpecArtifact('REQ-STORE', 2, '# Spec v2')

    expect(first).toBe('specs/REQ-STORE/v001.md')
    expect(second).toBe('specs/REQ-STORE/v002.md')
    await expect(readFile(path.join(directory, first), 'utf8')).resolves.toBe('# Spec v1\n')
    await expect(readFile(path.join(directory, second), 'utf8')).resolves.toBe('# Spec v2\n')
  })

  it('migrates legacy mixed Skills into separate workflow and target capabilities', async () => {
    const directory = await mkdtemp(path.join(os.tmpdir(), 'workbench-store-'))
    temporaryDirectories.push(directory)
    const { workflow: _newWorkflowField, metrics: _newMetrics, refinements: _newRefinements, ...legacy } = fixtureRequirement()
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
    expect(restored?.metrics).toEqual({ firstPreviewAt: null, acceptedAt: null })
    expect(restored?.refinements).toEqual([])
  })

  it('migrates fixed Grilling questions into a resumable logical conversation', async () => {
    const directory = await mkdtemp(path.join(os.tmpdir(), 'workbench-store-'))
    temporaryDirectories.push(directory)
    const { conversation: _conversation, ...legacy } = fixtureRequirement()
    legacy.grilling = { status: 'pending', confirmedAt: null, questions: [{ id: 'outcome', title: '成功结果', prompt: '用户看到什么？', recommendation: '新结果', answer: null }] }
    await writeFile(path.join(directory, 'current-requirement.json'), JSON.stringify(legacy))

    const restored = await new RequirementStore(directory).load()

    expect(restored?.conversation).toMatchObject({ phase: 'grilling', status: 'awaiting-user', planningThreadId: null })
    expect(restored?.conversation.messages.map((message) => message.role)).toEqual(['system', 'assistant'])
    expect(restored?.conversation.messages[1].segments[0]).toMatchObject({ type: 'text', content: expect.stringContaining('成功结果') })
  })

  it('keeps a pre-Grilling legacy requirement internally confirmed after migration', async () => {
    const directory = await mkdtemp(path.join(os.tmpdir(), 'workbench-store-'))
    temporaryDirectories.push(directory)
    const { conversation: _conversation, grilling: _grilling, ...legacy } = fixtureRequirement()
    await writeFile(path.join(directory, 'current-requirement.json'), JSON.stringify(legacy))

    const restored = await new RequirementStore(directory).load()

    expect(restored?.grilling.status).toBe('confirmed')
    expect(restored?.conversation).toMatchObject({ phase: 'to-spec', status: 'idle' })
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
    stage: 'intent-drafting', createdAt: now, updatedAt: now, modificationRounds: 0, metrics: { firstPreviewAt: null, acceptedAt: null }, refinements: [],
    grilling: { status: 'pending', questions: [], confirmedAt: null }, conversation: { id: 'CONVERSATION-REQ-STORE', phase: 'grilling', status: 'idle', planningThreadId: null, implementationThreadId: null, activeSkill: 'grilling', understanding: null, pendingUnderstandingRevision: null, turnReadyForConfirmation: false, messages: [], error: null }, spec: null, specVersions: [], activeSpecVersionId: null, tickets: [], rules: [], workflow: [], skills: [], boundaryItems: [], evidence: [],
    targetSnapshot: target,
    processFacts: { status: 'stopped', ownership: 'none', pid: null, command: '', healthUrl: target.healthUrl, previewUrl: target.previewUrl, gatewayUrl: '', startedAt: null, error: null, recentOutput: [] },
    branch: null, codex: { status: 'idle', sessionId: null, activeTicketId: null, changedFiles: [], events: [], error: null }, validation: [], delivery: null, agentSessionHandoff: null,
  }
}
