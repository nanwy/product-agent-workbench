import { execFile } from 'node:child_process'
import { access } from 'node:fs/promises'
import path from 'node:path'
import { promisify } from 'node:util'

import type { BranchFacts, Requirement, TargetProjectConfig, WorkingTreeEntry } from '../domain/workbench'

const execFileAsync = promisify(execFile)

export async function inspectGitWorkspace(target: TargetProjectConfig): Promise<BranchFacts> {
  const [baseBranch, baseRevision, status, operation] = await Promise.all([
    git(target.repositoryRoot, ['branch', '--show-current']),
    git(target.repositoryRoot, ['rev-parse', 'HEAD']),
    git(target.repositoryRoot, ['status', '--porcelain=v1']),
    detectGitOperation(target.repositoryRoot),
  ])
  const entries = parsePorcelain(status)
  return {
    baseBranch: baseBranch.trim() || '(detached)',
    baseRevision: baseRevision.trim(),
    requirementBranch: null,
    startingWorkingTree: entries,
    createdAt: null,
    blockedReason: operation ?? (entries.length ? `工作区存在 ${entries.length} 条未提交变更` : null),
  }
}

export async function createRequirementBranch(requirement: Pick<Requirement, 'specId' | 'title'>, target: TargetProjectConfig) {
  const before = await inspectGitWorkspace(target)
  if (before.blockedReason) return before
  const branchName = `product/${requirement.specId.toLowerCase()}-${slug(requirement.title)}`.slice(0, 100)
  await git(target.repositoryRoot, ['switch', '-c', branchName, before.baseRevision])
  const after = await inspectGitWorkspace(target)
  if (after.baseRevision !== before.baseRevision || after.startingWorkingTree.length) {
    throw new Error('创建分支后 Git 状态与已记录起点不一致')
  }
  return {
    ...before,
    requirementBranch: branchName,
    createdAt: new Date().toISOString(),
  }
}

export async function changedFilesSince(baseRevision: string, target: TargetProjectConfig) {
  const output = await git(target.repositoryRoot, ['diff', '--name-only', baseRevision])
  const untracked = await git(target.repositoryRoot, ['ls-files', '--others', '--exclude-standard'])
  return [...new Set([...output.split('\n'), ...untracked.split('\n')].map((value) => value.trim()).filter(Boolean))].sort()
}

export function filesOutsideScope(files: string[], target: TargetProjectConfig) {
  const scope = path.posix.normalize(target.modificationScope.replaceAll(path.sep, '/'))
  return files.filter((file) => {
    const normalized = path.posix.normalize(file.replaceAll(path.sep, '/'))
    if (normalized.startsWith('../') || path.posix.isAbsolute(normalized)) return true
    if (scope === '.') return false
    const relative = path.posix.relative(scope, normalized)
    return relative === '..' || relative.startsWith('../') || path.posix.isAbsolute(relative)
  })
}

async function git(cwd: string, args: string[]) {
  const { stdout } = await execFileAsync('git', args, { cwd, encoding: 'utf8', maxBuffer: 4 * 1024 * 1024 })
  return stdout
}

function parsePorcelain(output: string): WorkingTreeEntry[] {
  return output.split('\n').filter(Boolean).map((entry) => ({ status: entry.slice(0, 2), path: entry.slice(3) }))
}

async function detectGitOperation(root: string) {
  const gitDirectory = (await git(root, ['rev-parse', '--git-dir'])).trim()
  const absolute = path.resolve(root, gitDirectory)
  for (const [entry, label] of [['MERGE_HEAD', 'merge'], ['rebase-merge', 'rebase'], ['rebase-apply', 'rebase'], ['CHERRY_PICK_HEAD', 'cherry-pick'], ['REVERT_HEAD', 'revert']] as const) {
    try {
      await access(path.join(absolute, entry))
      return `Git 正在进行 ${label}，不能创建需求分支`
    } catch {
      // Missing operation marker is the expected state.
    }
  }
  return null
}

function slug(value: string) {
  const normalized = value.toLowerCase().replace(/[^a-z0-9\u3400-\u9fff]+/g, '-').replace(/^-|-$/g, '')
  return normalized || 'requirement'
}
