import { open, readFile, readdir } from 'node:fs/promises'
import path from 'node:path'

import type {
  BoundaryItem,
  DiscoveredSkill,
  ExecutionTicket,
  Requirement,
  RequirementSpec,
  ResolvedRule,
  TargetProjectConfig,
  WorkflowStep,
} from '../domain/workbench'

const ignoredDirectories = new Set(['.git', '.next', '.output', 'coverage', 'dist', 'node_modules'])
const sourceExtensions = new Set(['.js', '.jsx', '.ts', '.tsx', '.vue'])

// workflow 描述工作台已经完成或正在执行的 SDD 步骤，不属于 Codex Skill 能力清单。
export function workflowSteps(grillingConfirmed: boolean): WorkflowStep[] {
  return [
    { name: 'grilling', description: '通过分轮追问消除产品需求中的隐含决策。', active: !grillingConfirmed, activationReason: grillingConfirmed ? '共同理解已确认' : '产品需求已建立，必须先完成需求追问' },
    { name: 'to-spec', description: '把产品意图整理为可确认的规格。', active: grillingConfirmed, activationReason: grillingConfirmed ? '需求追问已完成' : '等待 grilling 完成' },
    { name: 'to-tickets', description: '把规格拆成可独立执行和验证的执行票。', active: grillingConfirmed, activationReason: grillingConfirmed ? 'SDD 执行票阶段要求' : '等待 grilling 完成' },
    { name: 'implement', description: '根据已确认规格和执行票形成候选实现。', active: false, activationReason: '等待 Spec 与执行票确认' },
  ]
}

export type ExplorationResult = {
  rules: ResolvedRule[]
  workflow: WorkflowStep[]
  skills: DiscoveredSkill[]
  spec: RequirementSpec
  tickets: ExecutionTicket[]
  boundaryItems: BoundaryItem[]
}

export async function exploreRequirement(requirement: Requirement, target: TargetProjectConfig): Promise<ExplorationResult> {
  // grilling 的确认答案是 Spec 输入；原始需求只在答案缺失时兜底。
  const grillingAnswers = Object.fromEntries(requirement.grilling.questions.map((question) => [question.id, question.answer ?? question.recommendation]))
  const desiredBehavior = grillingAnswers['desired-outcome'] || requirement.requestedOutcome
  const proposedScope = grillingAnswers['change-boundary'] || target.modificationScope
  const scopedTarget = { ...target, modificationScope: proposedScope }
  // 先以最终确认范围解析规则/Skill，再做代码定位，保证探索与实施使用同一边界。
  const rules = await discoverRules(scopedTarget)
  const workflow = workflowSteps(requirement.grilling.status === 'confirmed')
  const skills = await discoverTargetSkills(requirement, scopedTarget, rules)
  const exploration = await inspectTargetCode(requirement, scopedTarget)
  const tickets = createExecutionTickets(requirement, desiredBehavior)

  return {
    rules,
    workflow,
    skills,
    tickets,
    spec: {
      title: requirement.title,
      desiredBehavior,
      targetPage: requirement.targetPage,
      proposedScope,
      exploration,
      acceptanceCriteria: [
        grillingAnswers['acceptance-signal'] || `在 ${requirement.targetPage} 可以观察到：${desiredBehavior}`,
        `确认的修改边界：${proposedScope}`,
        '目标页面可以通过工作台预览，并且没有新增浏览器控制台错误',
        '实现遵守页面修改范围内的生效规则和生效 Skill',
      ],
    },
    boundaryItems: [
      ...detectBoundaryItems(desiredBehavior),
      ...detectRuleConflicts(desiredBehavior, rules),
    ],
  }
}

async function discoverRules(target: TargetProjectConfig): Promise<ResolvedRule[]> {
  const sources = await findNamedFiles(target.repositoryRoot, 'AGENTS.md')
  const scopeDirectory = path.resolve(target.repositoryRoot, target.modificationScope)
  assertWithinRoot(scopeDirectory, target.repositoryRoot)

  const applicable = sources
    .map((source) => ({ source, directory: path.dirname(source) }))
    .filter(({ directory }) => directory === target.repositoryRoot || scopeDirectory === directory || scopeDirectory.startsWith(`${directory}${path.sep}`))
  // 根 AGENTS.md 始终生效；嵌套规则只取距离修改范围最近的一层，避免不相关子项目规则泄漏进上下文。
  const rootRule = applicable.find(({ directory }) => directory === target.repositoryRoot)
  const closestNested = applicable
    .filter(({ directory }) => directory !== target.repositoryRoot)
    .sort((left, right) => right.directory.length - left.directory.length)[0]
  const activeRules = [rootRule, closestNested].filter(
    (candidate): candidate is NonNullable<typeof candidate> => Boolean(candidate),
  )

  return Promise.all(activeRules.map(async ({ source, directory }) => {
    const content = await readFile(source, 'utf8')
    const isRoot = directory === target.repositoryRoot
    return {
      source: normalizeRelative(target.repositoryRoot, source),
      scope: normalizeRelative(target.repositoryRoot, directory),
      summary: summarizeRule(content),
      active: true,
      activationReason: isRoot ? '目标项目根规则始终生效' : `最接近拟修改范围 ${target.modificationScope} 的目录规则`,
      content,
    }
  }))
}

async function discoverTargetSkills(requirement: Requirement, target: TargetProjectConfig, rules: ResolvedRule[]): Promise<DiscoveredSkill[]> {
  const manifests = (await findNamedFiles(target.repositoryRoot, 'SKILL.md')).filter(
    (source) => source.includes(`${path.sep}.agents${path.sep}skills${path.sep}`) || source.includes(`${path.sep}.codex${path.sep}skills${path.sep}`),
  )
  const activeRuleText = rules.map((rule) => rule.content).join('\n')
  const requirementText = `${requirement.title}\n${requirement.requestedOutcome}`.toLowerCase()

  return Promise.all(manifests.map(async (source) => {
    // 探索阶段只读前置元数据；Skill 真正生效时才由执行 Agent 读取全文。
    const metadata = await readSkillMetadata(source)
    const name = metadata.name ?? path.basename(path.dirname(source))
    const description = metadata.description ?? '未提供说明'
    // Skill 生效必须有可解释来源：产品点名、规则要求或极少量确定性类型匹配。
    const namedByProduct = requirementText.includes(`$${name.toLowerCase()}`) || requirementText.includes(name.toLowerCase())
    const requiredByRule = activeRuleText.includes(`$${name}`) || activeRuleText.includes(`/${name}`)
    const copyMatch = name === 'product-copy-change' && /(文案|文字|标题|空状态|copy|text)/i.test(requirementText)
    const active = namedByProduct || requiredByRule || copyMatch
    return {
      name,
      description,
      source: normalizeRelative(target.repositoryRoot, source),
      active,
      activationReason: namedByProduct ? '产品需求点名' : requiredByRule ? '生效规则要求' : copyMatch ? '需求类型确定性匹配' : '已发现，但与当前需求无确定关联',
      // 只给当前需求真正激活的 Skill 读取全文，避免把无关 Skill 塞进 Requirement 和 prompt。
      content: active ? await readFile(source, 'utf8') : null,
    }
  }))
}

async function inspectTargetCode(requirement: Requirement, target: TargetProjectConfig): Promise<RequirementSpec['exploration']> {
  const scopeRoot = path.resolve(target.repositoryRoot, target.modificationScope)
  assertWithinRoot(scopeRoot, target.repositoryRoot)
  const files = (await findSourceFiles(scopeRoot)).slice(0, 600)
  const routeTokens = requirement.targetPage.split('/').filter(Boolean)
  const confirmedOutcome = requirement.grilling.questions.find((question) => question.id === 'desired-outcome')?.answer
  const terms = `${requirement.title} ${confirmedOutcome ?? requirement.requestedOutcome}`
    .toLowerCase()
    .split(/[\s，。；、,:：/]+/)
    .filter((term) => term.length >= 2)
    .slice(0, 12)
  // 这是只读启发式排名，目的是给 Spec 提供候选代码证据，不是在此处决定最终修改文件。
  const inspected = await Promise.all(files.map(async (source) => {
    const content = await readFile(source, 'utf8').catch(() => '')
    const relative = normalizeRelative(target.repositoryRoot, source)
    const lower = `${relative}\n${content}`.toLowerCase()
    const routeScore = routeTokens.filter((token) => lower.includes(token.toLowerCase())).length * 3
    const homeScore = routeTokens.length === 0 && /(^|\/)(page|index)\.(tsx?|jsx?)$/.test(relative) ? 4 : 0
    const termScore = terms.filter((term) => lower.includes(term)).length
    return { source: relative, content, score: routeScore + homeScore + termScore }
  }))
  const candidates = inspected
    .filter(({ score }) => score > 0)
    .sort((left, right) => right.score - left.score || left.source.localeCompare(right.source))
    .slice(0, 8)
  const fallback = inspected.filter(({ source }) => /(^|\/)(page|index)\.(tsx?|jsx?)$/.test(source)).slice(0, 3)
  const selected = candidates.length ? candidates : fallback
  return {
    inspectedFileCount: inspected.length,
    candidateCodeLocations: selected.map(({ source }) => source),
    existingBehaviorEvidence: selected.slice(0, 3).map(({ source, content }) => {
      const lines = content.split('\n').map((value) => value.trim())
      const line = lines.find((value) => /[\u3400-\u9fff]/.test(value)) ??
        lines.find((value) => value && !value.startsWith('import ') && !value.startsWith('//'))
      return `${source}: ${(line ?? '已定位候选页面入口').slice(0, 140)}`
    }),
  }
}

function createExecutionTickets(requirement: Requirement, desiredBehavior: string): ExecutionTicket[] {
  // MVP 用结果子句切成最多四张串行票；每张票都保留一个可在真实路由观察的验收点。
  const clauses = desiredBehavior
    .split(/[。；;]|，并|，同时|并且|以及/)
    .map((clause) => clause.trim().replace(/^[，,]+|[，,]+$/g, ''))
    .filter((clause) => clause.length >= 4)
    .slice(0, 4)
  const slices = clauses.length ? clauses : [desiredBehavior]
  return slices.map((slice, index) => ({
    id: `TICKET-${String(index + 1).padStart(2, '0')}`,
    title: slice.length > 32 ? `${slice.slice(0, 31)}…` : slice,
    status: 'queued',
    acceptanceCriteria: [`在 ${requirement.targetPage} 验证：${slice}`, '保留只读探索记录，并通过现有类型检查与构建验证'],
  }))
}

function detectBoundaryItems(outcome: string): BoundaryItem[] {
  // 高风险关键词只触发“需要人确认”，不替代真正的代码探索和工程审查。
  const categories = [
    ['database', '数据库', /(数据库|数据表|迁移|schema|prisma)/i],
    ['authentication', '认证与权限', /(认证|鉴权|权限|登录|auth)/i],
    ['shared-contract', '共享契约', /(共享契约|公共接口|api contract)/i],
    ['shared-ui', '公共组件', /(公共组件|共享组件|design system)/i],
    ['cross-application', '跨应用联动', /(跨应用|前后台联动|多应用)/i],
    ['delivery', '发布配置', /(发布|部署|delivery|deploy)/i],
  ] as const
  return categories.filter(([, , pattern]) => pattern.test(outcome)).map(([id, label]) => ({
    id,
    label,
    reason: `需求涉及${label}，实现前必须确认工程影响范围`,
    confirmed: false,
  }))
}

function detectRuleConflicts(outcome: string, rules: ResolvedRule[]): BoundaryItem[] {
  const topics = [
    ['authentication', /(认证|鉴权|权限|登录|auth)/i],
    ['database', /(数据库|数据表|迁移|schema|prisma)/i],
    ['shared', /(公共|共享|contract|design system)/i],
    ['delivery', /(发布|部署|delivery|deploy)/i],
  ] as const
  const conflicts: BoundaryItem[] = []
  for (const rule of rules) {
    const prohibitions = rule.content.split('\n').map((line) => line.trim()).filter((line) => /(禁止|不得|不可|must not|do not)/i.test(line))
    for (const [topic, pattern] of topics) {
      const line = prohibitions.find((candidate) => pattern.test(candidate))
      if (!line || !pattern.test(outcome)) continue
      conflicts.push({
        id: `rule-conflict-${topic}-${conflicts.length + 1}`,
        label: '规则冲突',
        reason: `需求可能与 ${rule.source} 冲突：${line.slice(0, 100)}`,
        confirmed: false,
      })
    }
  }
  return conflicts
}

async function findNamedFiles(root: string, filename: string) {
  // 规则/Skill 发现跳过依赖和构建产物，避免把第三方包的指令当成目标项目规则。
  const results: string[] = []
  async function visit(directory: string): Promise<void> {
    const entries = await readdir(directory, { withFileTypes: true })
    await Promise.all(entries.map(async (entry) => {
      const source = path.join(directory, entry.name)
      if (entry.isDirectory()) {
        if (!ignoredDirectories.has(entry.name)) await visit(source)
      } else if (entry.isFile() && entry.name === filename) results.push(source)
    }))
  }
  await visit(root)
  return results.sort()
}

async function findSourceFiles(root: string) {
  const results: string[] = []
  async function visit(directory: string): Promise<void> {
    const entries = await readdir(directory, { withFileTypes: true }).catch(() => [])
    for (const entry of entries) {
      const source = path.join(directory, entry.name)
      if (entry.isDirectory() && !ignoredDirectories.has(entry.name)) await visit(source)
      else if (entry.isFile() && sourceExtensions.has(path.extname(entry.name))) results.push(source)
      if (results.length >= 600) return
    }
  }
  await visit(root)
  return results.sort()
}

async function readSkillMetadata(source: string) {
  // 探索阶段最多读取 4 KiB frontmatter，不把所有 Skill 全文装入 Requirement。
  const handle = await open(source, 'r')
  try {
    const buffer = Buffer.alloc(4096)
    const { bytesRead } = await handle.read(buffer, 0, buffer.length, 0)
    const prefix = buffer.subarray(0, bytesRead).toString('utf8')
    const frontmatter = prefix.match(/^---\s*\n([\s\S]*?)\n---/)?.[1] ?? prefix
    return { name: frontmatterValue(frontmatter, 'name'), description: frontmatterValue(frontmatter, 'description') }
  } finally {
    await handle.close()
  }
}

function frontmatterValue(content: string, key: string) {
  const match = content.match(new RegExp(`^${key}:\\s*["']?(.+?)["']?\\s*$`, 'm'))
  return match?.[1]?.trim()
}

function summarizeRule(content: string) {
  const lines = content.split('\n').map((line) => line.replace(/^#+\s*/, '').trim()).filter((line) => line && !line.startsWith('---'))
  return lines.slice(0, 2).join(' · ').slice(0, 180) || '空规则文件'
}

function normalizeRelative(root: string, source: string) {
  const relative = path.relative(root, source)
  return relative || '.'
}

function assertWithinRoot(candidate: string, root: string) {
  if (candidate !== root && !candidate.startsWith(`${root}${path.sep}`)) throw new Error('拟修改范围超出目标项目根目录')
}
