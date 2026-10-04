import { execFile } from 'node:child_process'
import { mkdir, readFile, rmdir, unlink, writeFile } from 'node:fs/promises'
import path from 'node:path'
import { promisify } from 'node:util'

import type { Requirement, TargetProjectConfig } from '../domain/workbench'
import { changedFilesSince } from './git-workspace'
import { renderSpecMarkdown } from './workbench-spec'
import { renderTicketMarkdown } from './workbench-tickets'

const execFileAsync = promisify(execFile)

export type DeliveryArtifactSet = {
  paths: string[]
  rollback: () => Promise<void>
}

/** commit 失败后只把工作台生成的交付文件移出暂存区，不触碰业务文件或工作区内容。 */
export async function unstageDeliveryArtifacts(target: TargetProjectConfig, paths: string[]) {
  if (!paths.length) return
  await git(target.repositoryRoot, ['restore', '--staged', '--', ...paths]).catch(() => undefined)
}

/**
 * 在已确认写入范围中生成前端可审查的交付文件。
 *
 * 文件先生成、后由 Git 模块与业务代码一起暂存。返回的 rollback 会精确恢复这四个
 * 文件的旧内容；如果 commit 失败，调用方可撤销本次生成而不删除用户原有文件。
 */
export async function writeDeliveryArtifacts(requirement: Requirement, target: TargetProjectConfig): Promise<DeliveryArtifactSet> {
  const confirmedSpec = requirement.specVersions.find((version) => version.id === requirement.activeSpecVersionId && version.status === 'confirmed')
  if ((!confirmedSpec && !requirement.spec) || !requirement.branch?.requirementBranch) throw new Error('生成交付文件前缺少已确认 Spec 或需求分支')
  const scopeRoot = path.resolve(target.repositoryRoot, target.modificationScope)
  const artifactRoot = path.join(scopeRoot, '.product-agent-workbench', requirement.specId)
  assertWithinScope(artifactRoot, scopeRoot)
  const confirmedTickets = requirement.ticketVersions.find((version) => version.id === requirement.activeTicketVersionId && version.status === 'confirmed')
  const filenames = ['spec.md', 'tickets.md', 'validation.md', 'frontend-delivery-handoff.md',
    ...(confirmedTickets?.tickets.map((_, index) => `tickets/${String(index + 1).padStart(2, '0')}-ticket.md`) ?? [])]
  const absolutePaths = filenames.map((filename) => path.join(artifactRoot, filename))
  const previous = new Map<string, string | null>()
  for (const source of absolutePaths) previous.set(source, await readExisting(source))

  const businessFiles = (await changedFilesSince(requirement.branch.baseRevision, target))
    .filter((source) => !source.includes('/.product-agent-workbench/'))
  const businessDiffStat = await git(target.repositoryRoot, ['diff', '--stat', requirement.branch.baseRevision, '--', target.modificationScope])
  const relativePaths = absolutePaths.map((source) => relative(target.repositoryRoot, source))
  const filesAfterPackaging = [...new Set([...businessFiles, ...relativePaths])].sort()
  const risks = requirement.validation.filter((item) => item.result !== 'verified')

  const contents = [
    renderSpec(requirement),
    renderTickets(requirement),
    renderValidation(requirement),
    renderFrontendHandoff(requirement, filesAfterPackaging, businessDiffStat.trim(), risks),
    ...(confirmedTickets?.tickets.map(renderTicketMarkdown) ?? []),
  ]

  await mkdir(artifactRoot, { recursive: true })
  if (confirmedTickets) await mkdir(path.join(artifactRoot, 'tickets'), { recursive: true })
  try {
    await Promise.all(absolutePaths.map((source, index) => writeFile(source, `${contents[index].trim()}\n`, { mode: 0o600 })))
  } catch (error) {
    await restoreFiles(previous, artifactRoot)
    throw error
  }
  return { paths: relativePaths, rollback: () => restoreFiles(previous, artifactRoot) }
}

function renderSpec(requirement: Requirement) {
  const confirmed = requirement.specVersions.find((version) => version.id === requirement.activeSpecVersionId && version.status === 'confirmed')
  if (confirmed) return renderSpecMarkdown(requirement, confirmed.document)
  const spec = requirement.spec!
  return [
    `# ${requirement.specId} · ${spec.title}`,
    '',
    `- Requirement: ${requirement.id}`,
    `- Target page: ${spec.targetPage}`,
    `- Confirmed modification scope: ${spec.proposedScope}`,
    '',
    '## Desired behavior', '', spec.desiredBehavior,
    '', '## Acceptance criteria', '',
    ...spec.acceptanceCriteria.map((criterion, index) => `- SPEC-AC-${String(index + 1).padStart(2, '0')}: ${criterion}`),
  ].join('\n')
}

function renderTickets(requirement: Requirement) {
  const confirmed = requirement.ticketVersions.find((version) => version.id === requirement.activeTicketVersionId && version.status === 'confirmed')
  if (confirmed) return [
    `# Confirmed Tickets · ${confirmed.id}`, '',
    ...confirmed.tickets.flatMap((ticket) => [renderTicketMarkdown(ticket), '']),
  ].join('\n')
  return [
    `# Execution Tickets · ${requirement.specId}`, '',
    ...requirement.tickets.flatMap((ticket) => [
      `## ${ticket.id} · ${ticket.title}`, '',
      `- Result: ${ticket.status}`, ...ticket.acceptanceCriteria.map((criterion) => `- Acceptance: ${criterion}`), '',
    ]),
  ].join('\n')
}

function renderValidation(requirement: Requirement) {
  return [
    `# Validation Matrix · ${requirement.specId}`, '',
    '| ID | Criterion | Result | Source | Required | Evidence |',
    '| --- | --- | --- | --- | --- | --- |',
    ...requirement.validation.map((item) => `| ${escapeCell(item.criterionId ?? item.id)} | ${escapeCell(item.criterion)} | ${item.result} | ${item.source} | ${item.required ? 'yes' : 'no'} | ${escapeCell(item.notRunReason ?? item.detail)} |`),
  ].join('\n')
}

function renderFrontendHandoff(requirement: Requirement, changedFiles: string[], businessDiffStat: string, risks: Requirement['validation']) {
  const confirmedSpec = requirement.specVersions.find((version) => version.id === requirement.activeSpecVersionId && version.status === 'confirmed')
  return [
    `# Frontend Delivery Handoff · ${requirement.specId}`, '',
    `- Requirement: ${requirement.id}`,
    `- Branch: ${requirement.branch!.requirementBranch}`,
    '- Revision: the commit containing this file（实际 hash 由工作台在提交后记录）',
    `- Target behavior: ${confirmedSpec?.document.solution ?? requirement.spec?.desiredBehavior ?? requirement.requestedOutcome}`,
    `- Target page: ${requirement.targetPage}`,
    `- Confirmed scope: ${requirement.spec?.proposedScope ?? requirement.targetSnapshot.modificationScope}`,
    '', '## Changed files', '', ...changedFiles.map((source) => `- ${source}`),
    '', '## Business diff stat before delivery files', '', '```text', businessDiffStat || 'uncollected', '```',
    '', '## Ticket results', '', ...requirement.tickets.map((ticket) => `- ${ticket.id}: ${ticket.status}`),
    '', '## Unverified or failed items', '', ...(risks.length ? risks.map((item) => `- ${item.criterionId ?? item.id}: ${item.result} — ${item.notRunReason ?? item.detail}`) : ['- None']),
    '', '## Ownership boundary', '',
    '产品验收确认候选实现符合产品意图；它不代表生产批准。push、PR、merge 与发布仍由前端工程师负责。',
  ].join('\n')
}

async function restoreFiles(previous: Map<string, string | null>, artifactRoot: string) {
  // 只恢复本次触碰的精确文件；目录非空时 rmdir 会安全失败，不做递归删除。
  await Promise.all([...previous].map(([source, content]) => content === null ? unlink(source).catch(() => undefined) : writeFile(source, content)))
  await rmdir(artifactRoot).catch(() => undefined)
}

async function readExisting(source: string) {
  try {
    return await readFile(source, 'utf8')
  } catch (error) {
    if (error instanceof Error && 'code' in error && error.code === 'ENOENT') return null
    throw error
  }
}

function assertWithinScope(candidate: string, scopeRoot: string) {
  if (candidate !== scopeRoot && !candidate.startsWith(`${scopeRoot}${path.sep}`)) throw new Error('交付文件目录超出确认修改范围')
}

function relative(root: string, source: string) {
  return path.relative(root, source).split(path.sep).join('/')
}

function escapeCell(value: string) {
  return value.replaceAll('|', '\\|').replace(/\r?\n/g, ' ')
}

async function git(cwd: string, args: string[]) {
  const { stdout } = await execFileAsync('git', args, { cwd, encoding: 'utf8', maxBuffer: 4 * 1024 * 1024 })
  return stdout
}
