import { readFile, readdir } from 'node:fs/promises'
import path from 'node:path'

import type {
  BoundaryItem,
  DiscoveredSkill,
  Requirement,
  RequirementSpec,
  ResolvedRule,
  TargetProjectConfig,
} from '../domain/workbench'

const ignoredDirectories = new Set([
  '.git',
  '.next',
  '.output',
  'dist',
  'node_modules',
])

const workflowSkills: DiscoveredSkill[] = [
  {
    name: 'to-spec',
    description: '把产品意图整理为可确认的规格。',
    source: 'workflow',
    kind: 'workflow',
    active: true,
    activationReason: 'SDD 规格阶段要求',
  },
  {
    name: 'to-tickets',
    description: '把规格拆成可独立执行和验证的执行票。',
    source: 'workflow',
    kind: 'workflow',
    active: true,
    activationReason: 'SDD 执行票阶段要求',
  },
  {
    name: 'implement',
    description: '根据已确认规格和执行票形成候选实现。',
    source: 'workflow',
    kind: 'workflow',
    active: false,
    activationReason: '等待 Spec 与执行票确认',
  },
]

export type ExplorationResult = {
  rules: ResolvedRule[]
  skills: DiscoveredSkill[]
  spec: RequirementSpec
  boundaryItems: BoundaryItem[]
}

export async function exploreRequirement(
  requirement: Requirement,
  target: TargetProjectConfig,
): Promise<ExplorationResult> {
  const rules = await discoverRules(target)
  const skills = [
    ...workflowSkills,
    ...(await discoverTargetSkills(requirement, target, rules)),
  ]

  return {
    rules,
    skills,
    spec: {
      title: requirement.title,
      desiredBehavior: requirement.requestedOutcome,
      targetPage: requirement.targetPage,
      proposedScope: target.modificationScope,
      acceptanceCriteria: [
        `在 ${requirement.targetPage} 可以观察到：${requirement.requestedOutcome}`,
        '目标页面可以通过工作台预览，并且没有新增浏览器控制台错误',
        '实现遵守页面修改范围内的生效规则和生效 Skill',
      ],
    },
    boundaryItems: detectBoundaryItems(requirement.requestedOutcome),
  }
}

async function discoverRules(
  target: TargetProjectConfig,
): Promise<ResolvedRule[]> {
  const sources = await findNamedFiles(target.repositoryRoot, 'AGENTS.md')
  const scopeDirectory = path.resolve(
    target.repositoryRoot,
    target.modificationScope,
  )
  assertWithinRoot(scopeDirectory, target.repositoryRoot)

  const candidates = sources.map((source) => {
    const directory = path.dirname(source)
    const applies =
      directory === target.repositoryRoot ||
      scopeDirectory === directory ||
      scopeDirectory.startsWith(`${directory}${path.sep}`)
    return { source, directory, applies }
  })
  const closestNested = candidates
    .filter(
      (candidate) =>
        candidate.applies && candidate.directory !== target.repositoryRoot,
    )
    .sort((left, right) => right.directory.length - left.directory.length)[0]

  return Promise.all(
    candidates.map(async (candidate) => {
      const content = await readFile(candidate.source, 'utf8')
      const isRoot = candidate.directory === target.repositoryRoot
      const active = isRoot || candidate.source === closestNested?.source
      return {
        source: normalizeRelative(target.repositoryRoot, candidate.source),
        scope: normalizeRelative(target.repositoryRoot, candidate.directory),
        summary: summarizeRule(content),
        active,
        activationReason: active
          ? isRoot
            ? '目标项目根规则始终生效'
            : `最接近拟修改范围 ${target.modificationScope} 的目录规则`
          : candidate.applies
            ? '存在更接近拟修改范围的目录规则'
            : '不适用于当前拟修改范围',
        content,
      }
    }),
  )
}

async function discoverTargetSkills(
  requirement: Requirement,
  target: TargetProjectConfig,
  rules: ResolvedRule[],
): Promise<DiscoveredSkill[]> {
  const manifests = (await findNamedFiles(target.repositoryRoot, 'SKILL.md')).filter(
    (source) => source.includes(`${path.sep}.agents${path.sep}skills${path.sep}`) ||
      source.includes(`${path.sep}.codex${path.sep}skills${path.sep}`),
  )
  const activeRuleText = rules
    .filter((rule) => rule.active)
    .map((rule) => rule.content)
    .join('\n')
  const requirementText = `${requirement.title}\n${requirement.requestedOutcome}`.toLowerCase()

  return Promise.all(
    manifests.map(async (source) => {
      const content = await readFile(source, 'utf8')
      const name = frontmatterValue(content, 'name') ?? path.basename(path.dirname(source))
      const description = frontmatterValue(content, 'description') ?? '未提供说明'
      const namedByProduct =
        requirementText.includes(`$${name.toLowerCase()}`) ||
        requirementText.includes(name.toLowerCase())
      const requiredByRule =
        activeRuleText.includes(`$${name}`) || activeRuleText.includes(`/${name}`)
      const copyMatch =
        name === 'product-copy-change' &&
        /(文案|文字|标题|空状态|copy|text)/i.test(requirementText)
      const active = namedByProduct || requiredByRule || copyMatch

      return {
        name,
        description,
        source: normalizeRelative(target.repositoryRoot, source),
        kind: 'target-project' as const,
        active,
        activationReason: namedByProduct
          ? '产品需求点名'
          : requiredByRule
            ? '生效规则要求'
            : copyMatch
              ? '需求类型确定性匹配'
              : '已发现，但与当前需求无确定关联',
      }
    }),
  )
}

function detectBoundaryItems(outcome: string): BoundaryItem[] {
  const categories = [
    ['database', '数据库', /(数据库|数据表|迁移|schema|prisma)/i],
    ['authentication', '认证与权限', /(认证|鉴权|权限|登录|auth)/i],
    ['shared-contract', '共享契约', /(共享契约|公共接口|api contract)/i],
    ['shared-ui', '公共组件', /(公共组件|共享组件|design system)/i],
    ['cross-application', '跨应用联动', /(跨应用|前后台联动|多应用)/i],
    ['delivery', '发布配置', /(发布|部署|delivery|deploy)/i],
  ] as const

  return categories
    .filter(([, , pattern]) => pattern.test(outcome))
    .map(([id, label]) => ({
      id,
      label,
      reason: `需求涉及${label}，实现前必须确认工程影响范围`,
      confirmed: false,
    }))
}

async function findNamedFiles(root: string, filename: string) {
  const results: string[] = []

  async function visit(directory: string): Promise<void> {
    const entries = await readdir(directory, { withFileTypes: true })
    await Promise.all(
      entries.map(async (entry) => {
        const source = path.join(directory, entry.name)
        if (entry.isDirectory()) {
          if (!ignoredDirectories.has(entry.name)) {
            await visit(source)
          }
        } else if (entry.isFile() && entry.name === filename) {
          results.push(source)
        }
      }),
    )
  }

  await visit(root)
  return results.sort()
}

function frontmatterValue(content: string, key: string) {
  const match = content.match(new RegExp(`^${key}:\\s*["']?(.+?)["']?\\s*$`, 'm'))
  return match?.[1]?.trim()
}

function summarizeRule(content: string) {
  const lines = content
    .split('\n')
    .map((line) => line.replace(/^#+\s*/, '').trim())
    .filter((line) => line && !line.startsWith('---'))
  return lines.slice(0, 2).join(' · ').slice(0, 180) || '空规则文件'
}

function normalizeRelative(root: string, source: string) {
  const relative = path.relative(root, source)
  return relative || '.'
}

function assertWithinRoot(candidate: string, root: string) {
  if (candidate !== root && !candidate.startsWith(`${root}${path.sep}`)) {
    throw new Error('拟修改范围超出目标项目根目录')
  }
}
