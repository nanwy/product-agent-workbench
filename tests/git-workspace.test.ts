import { execFile } from 'node:child_process'
import { mkdtemp, rm, writeFile } from 'node:fs/promises'
import os from 'node:os'
import path from 'node:path'
import { promisify } from 'node:util'

import { afterEach, describe, expect, it } from 'vitest'

import type { TargetProjectConfig } from '../src/domain/workbench'
import { commitAcceptedCandidate, createRequirementBranch, filesOutsideScope, inspectGitWorkspace } from '../src/server/git-workspace'

const execFileAsync = promisify(execFile)
const temporaryDirectories: string[] = []

afterEach(async () => {
  await Promise.all(temporaryDirectories.splice(0).map((directory) => rm(directory, { force: true, recursive: true })))
})

describe('safe requirement branches', () => {
  it('creates from a clean revision and blocks a dirty working tree', async () => {
    const root = await mkdtemp(path.join(os.tmpdir(), 'workbench-git-'))
    temporaryDirectories.push(root)
    await execFileAsync('git', ['init', '-b', 'main'], { cwd: root })
    await execFileAsync('git', ['config', 'user.name', 'Fixture'], { cwd: root })
    await execFileAsync('git', ['config', 'user.email', 'fixture@example.test'], { cwd: root })
    await writeFile(path.join(root, 'README.md'), 'fixture\n')
    await execFileAsync('git', ['add', '.'], { cwd: root })
    await execFileAsync('git', ['commit', '-m', 'baseline'], { cwd: root })
    const target = targetFixture(root)

    const branch = await createRequirementBranch({ specId: 'SPEC-001', title: 'Copy update' }, target)

    expect(branch.requirementBranch).toBe('product/spec-001-copy-update')
    expect(branch.startingWorkingTree).toEqual([])
    await writeFile(path.join(root, 'README.md'), 'accepted candidate\n')
    const delivery = await commitAcceptedCandidate({ specId: 'SPEC-001', title: 'Copy update', branch }, target)
    expect(delivery.branch).toBe('product/spec-001-copy-update')
    expect(delivery.changedFiles).toEqual(['README.md'])
    expect(delivery.diffStat).toContain('README.md')
    await writeFile(path.join(root, 'dirty.txt'), 'do not touch\n')
    const dirty = await inspectGitWorkspace(target)
    expect(dirty.blockedReason).toBe('工作区存在 1 条未提交变更')
    expect(dirty.startingWorkingTree).toEqual([{ status: '??', path: 'dirty.txt' }])
    expect(filesOutsideScope(['apps/front/page.tsx', 'apps/admin/page.tsx'], { ...target, modificationScope: 'apps/front' })).toEqual(['apps/admin/page.tsx'])
    expect(filesOutsideScope(['README.md', 'apps/front/page.tsx'], target)).toEqual([])
  })
})

function targetFixture(repositoryRoot: string): TargetProjectConfig {
  return {
    name: 'Git fixture',
    repositoryRoot,
    modificationScope: '.',
    startCommand: { command: 'node', args: [] },
    healthUrl: 'http://127.0.0.1:4173',
    previewUrl: 'http://127.0.0.1:4173',
    previewGatewayPort: 4174,
    startupTimeoutMs: 5000,
  }
}
