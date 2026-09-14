import { randomUUID } from 'node:crypto'
import path from 'node:path'

import {
  assertCreateRequirementInput,
  type CreateRequirementInput,
  type EvidenceRecord,
  type Requirement,
  type WorkbenchSnapshot,
} from '../domain/workbench'
import { loadTargetProjectConfig } from './config'
import { exploreRequirement, workflowSkills } from './exploration'
import { requirementStore } from './requirement-store'
import { commitAcceptedCandidate } from './git-workspace'
import {
  getTargetProcessSnapshot,
  startTargetProject,
  stopTargetProject,
} from './target-runtime'

// 用例服务层：把领域状态、持久化、Git 和目标进程串成一次完整的产品动作。
// 路由层只能调用这里暴露的动作，不应自行拼装 Requirement 的下一状态。
export async function getWorkbenchSnapshot(): Promise<WorkbenchSnapshot> {
  const [requirement, target] = await Promise.all([
    requirementStore.load(),
    loadTargetProjectConfig().catch(() => null),
  ])

  return {
    requirement,
    target,
    // 进程是易失的运行时事实，每次读取快照时都以进程管理器为准。
    process: getTargetProcessSnapshot(target ?? undefined),
  }
}

export async function createRequirement(
  rawInput: CreateRequirementInput,
): Promise<WorkbenchSnapshot> {
  const input = assertCreateRequirementInput(rawInput)
  const target = await loadTargetProjectConfig()
  const now = new Date().toISOString()
  const processFacts = getTargetProcessSnapshot(target)
  // Requirement 是工作台的聚合根：一次需求从澄清到交付的事实都挂在它下面。
  const requirement: Requirement = {
    id: `REQ-${now.slice(0, 10).replaceAll('-', '')}-${randomUUID().slice(0, 8)}`,
    specId: `SPEC-${now.slice(0, 10).replaceAll('-', '')}-${randomUUID().slice(0, 8)}`,
    title: input.title,
    targetPage: input.targetPage,
    requestedOutcome: input.requestedOutcome,
    stage: 'intent-drafting',
    createdAt: now,
    updatedAt: now,
    modificationRounds: 0,
    grilling: {
      status: 'pending',
      confirmedAt: null,
      questions: createGrillingQuestions(input, target),
    },
    spec: null,
    tickets: [],
    rules: [],
    skills: workflowSkills(false),
    boundaryItems: [],
    targetSnapshot: target,
    processFacts,
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
    delivery: null,
    evidence: [
      evidence(
        'project-adaptation',
        `固定目标配置：${target.name}`,
        '校验仓库根目录、启动命令、健康地址和预览地址',
        `目标项目已适配，拟修改范围为 ${target.modificationScope}`,
      ),
      evidence(
        'intent-drafting',
        `${input.title} · ${input.targetPage}`,
        '把产品输入保存为产品需求，而不是作为可执行命令',
        input.requestedOutcome,
      ),
    ],
  }

  await requirementStore.save(requirement)
  return getWorkbenchSnapshot()
}

export async function analyzeCurrentRequirement(): Promise<WorkbenchSnapshot> {
  const requirement = await requireCurrentRequirement()
  // 状态门禁写在服务端，不能依赖前端是否显示按钮。
  if (requirement.grilling.status !== 'confirmed') {
    throw new Error('请先完成 grilling 并确认共同理解')
  }
  const target = await loadTargetProjectConfig()
  const result = await exploreRequirement(requirement, target)
  const now = new Date().toISOString()
  const next: Requirement = {
    ...requirement,
    stage: 'awaiting-spec-confirmation',
    updatedAt: now,
    spec: result.spec,
    tickets: result.tickets,
    rules: result.rules,
    skills: result.skills,
    boundaryItems: result.boundaryItems,
    evidence: [
      ...requirement.evidence,
      evidence(
        'awaiting-spec-confirmation',
        `目标页面 ${requirement.targetPage}，拟修改范围 ${target.modificationScope}`,
        `只读检查 ${result.spec.exploration.inspectedFileCount} 个代码文件、${result.rules.length} 条生效规则和 ${result.skills.length} 个 Skill，并形成 Spec 与执行票`,
        result.boundaryItems.length
          ? `发现 ${result.boundaryItems.length} 个需要边界确认的工程影响`
          : `Spec 与 ${result.tickets.length} 张执行票已准备确认`,
      ),
    ],
  }
  await requirementStore.save(next)
  return getWorkbenchSnapshot()
}

export async function confirmCurrentGrilling(answers: Record<string, string>): Promise<WorkbenchSnapshot> {
  const requirement = await requireCurrentRequirement()
  if (requirement.grilling.status === 'confirmed') return getWorkbenchSnapshot()
  const questions = requirement.grilling.questions.map((question) => {
    const answer = answers[question.id]?.trim()
    if (!answer) throw new Error(`请回答：${question.title}`)
    return { ...question, answer }
  })
  const confirmedScope = questions.find((question) => question.id === 'change-boundary')?.answer ?? ''
  // 用户可以收窄修改范围，但不能借回答问题扩大配置文件授权的目录。
  assertConfirmedScope(confirmedScope, requirement.targetSnapshot)
  const now = new Date().toISOString()
  await requirementStore.save({
    ...requirement,
    grilling: { status: 'confirmed', questions, confirmedAt: now },
    skills: workflowSkills(true),
    updatedAt: now,
    evidence: [...requirement.evidence, evidence('intent-drafting', questions.map((question) => `${question.title}: ${question.answer}`).join('；'), '完成 grilling 设计树并确认共同理解', '需求可以进入只读探索与 Spec 生成')],
  })
  return getWorkbenchSnapshot()
}

export async function confirmCurrentPlan(): Promise<WorkbenchSnapshot> {
  const requirement = await requireCurrentRequirement()
  if (requirement.stage !== 'awaiting-spec-confirmation') {
    throw new Error('只有待确认的 Spec 和执行票可以被确认')
  }
  const requiresBoundary = requirement.boundaryItems.some((item) => !item.confirmed)
  // 普通需求直接进入实现；发现跨边界影响时插入一次显式产品确认。
  const stage = requiresBoundary
    ? 'awaiting-boundary-confirmation'
    : 'implementation'
  await requirementStore.save({
    ...requirement,
    stage,
    updatedAt: new Date().toISOString(),
    skills: activateImplementationSkill(
      requirement.skills,
      !requiresBoundary,
      requiresBoundary ? '等待边界确认' : 'Spec 与执行票已确认',
    ),
    evidence: [
      ...requirement.evidence,
      evidence(
        stage,
        '产品确认 Spec 与执行票',
        requiresBoundary ? '暂停执行并请求边界确认' : '开放候选实现阶段',
        requiresBoundary ? '存在未确认的工程边界' : '需求已准备进入执行',
      ),
    ],
  })
  return getWorkbenchSnapshot()
}

export async function confirmCurrentBoundary(): Promise<WorkbenchSnapshot> {
  const requirement = await requireCurrentRequirement()
  if (requirement.stage !== 'awaiting-boundary-confirmation') {
    throw new Error('当前产品需求不在边界确认阶段')
  }
  await requirementStore.save({
    ...requirement,
    stage: 'implementation',
    updatedAt: new Date().toISOString(),
    boundaryItems: requirement.boundaryItems.map((item) => ({
      ...item,
      confirmed: true,
    })),
    skills: activateImplementationSkill(
      requirement.skills,
      true,
      'Spec、执行票与工程边界均已确认',
    ),
    evidence: [
      ...requirement.evidence,
      evidence(
        'implementation',
        '产品确认全部工程边界',
        '记录边界确认并开放候选实现阶段',
        '需求已准备进入执行',
      ),
    ],
  })
  return getWorkbenchSnapshot()
}

export async function startConfiguredTarget(): Promise<WorkbenchSnapshot> {
  const config = await loadTargetProjectConfig()
  const processFacts = await startTargetProject(config)
  await saveProcessFacts(processFacts, '启动目标项目', `运行状态：${processFacts.status}，PID：${processFacts.pid ?? '—'}`)
  return getWorkbenchSnapshot()
}

export async function stopConfiguredTarget(): Promise<WorkbenchSnapshot> {
  const config = await loadTargetProjectConfig()
  const processFacts = await stopTargetProject(config)
  await saveProcessFacts(processFacts, '停止目标项目', '已回收工作台拥有的目标进程与预览网关')
  return getWorkbenchSnapshot()
}

export async function acceptCurrentRequirement(): Promise<WorkbenchSnapshot> {
  const requirement = await requireCurrentRequirement()
  // 第一层门禁：只有验证编排已结束并转入待验收阶段，按钮才能生效。
  // 第二层门禁：服务端再次检查 failed，不信任前端的 disabled 状态。
  if (requirement.stage !== 'awaiting-product-acceptance') throw new Error('只有自动验证完成的候选实现可以验收')
  if (requirement.validation.some((item) => item.result === 'failed')) throw new Error('存在失败的验证项，不能验收')
  const configuredTarget = await loadTargetProjectConfig()
  const target = { ...configuredTarget, modificationScope: requirement.spec?.proposedScope ?? configuredTarget.modificationScope }
  // 验收的副作用是“冻结候选实现”：先重查当前分支/范围，再创建稳定 commit。
  // 只有 commit 成功后才会把 Requirement 写成 accepted，中途失败不会产生假验收。
  const committed = await commitAcceptedCandidate(requirement, target)
  const acceptedAt = new Date().toISOString()
  const validation = requirement.validation.map((item) => item.id === 'product-acceptance' ? {
    ...item,
    result: 'verified' as const,
    detail: '产品已在工作台显式确认候选实现',
    notRunReason: null,
    completedAt: acceptedAt,
  } : item)
  // Handoff 是前端审查的摘要，不是 PR 或发布单；所以只记录可审查事实，不执行 push/merge。
  const handoff = [
    `# Frontend Delivery Handoff · ${requirement.specId}`,
    '',
    `- 需求：${requirement.title}`,
    `- 分支：${committed.branch}`,
    `- 提交：${committed.revision}`,
    `- 改动文件：${committed.changedFiles.join(', ')}`,
    `- 验证：${validation.map((item) => `${item.criterion}=${item.result}`).join('；')}`,
    '- 责任边界：产品验收已完成；生产合并与发布仍由前端工程师负责。',
  ].join('\n')
  await requirementStore.save({
    ...requirement,
    stage: 'accepted',
    validation,
    delivery: { ...committed, acceptedAt, handoff },
    updatedAt: acceptedAt,
    evidence: [...requirement.evidence, evidence('accepted', '产品显式验收候选实现', '创建稳定提交并生成前端交付 Handoff', `${committed.branch} @ ${committed.revision.slice(0, 8)}`)],
  })
  return getWorkbenchSnapshot()
}

async function requireCurrentRequirement() {
  const requirement = await requirementStore.load()
  if (!requirement) throw new Error('请先创建产品需求')
  return requirement
}

function evidence(
  stage: EvidenceRecord['stage'],
  input: string,
  action: string,
  output: string,
): EvidenceRecord {
  // 证据只追加、不覆盖，用于回答“谁在什么阶段依据什么做了什么”。
  return { stage, input, action, output, recordedAt: new Date().toISOString() }
}

function activateImplementationSkill(
  skills: Requirement['skills'],
  active: boolean,
  activationReason: string,
) {
  // Skill 是否生效属于需求快照的一部分，页面才能解释本轮执行采用了哪些流程规则。
  return skills.map((skill) =>
    skill.name === 'implement'
      ? { ...skill, active, activationReason }
      : skill,
  )
}

async function saveProcessFacts(
  processFacts: Requirement['processFacts'],
  action: string,
  output: string,
) {
  // 目标进程可以脱离需求存在；有需求时再把当时的运行事实同步进审计记录。
  const requirement = await requirementStore.load()
  if (!requirement) return
  await requirementStore.update((current) => {
    if (!current) throw new Error('产品需求已不存在')
    return {
      ...current,
      processFacts,
      updatedAt: new Date().toISOString(),
      evidence: [
        ...current.evidence,
        evidence('project-adaptation', processFacts.command, action, output),
      ],
    }
  })
}

function createGrillingQuestions(input: CreateRequirementInput, target: Requirement['targetSnapshot']) {
  // MVP 固定三类问题：结果、写入边界、验收信号。它们共同构成执行前的最小契约。
  return [
    {
      id: 'desired-outcome',
      title: '成功结果',
      prompt: '用户最终应该观察到什么结果？',
      recommendation: input.requestedOutcome,
      answer: null,
    },
    {
      id: 'change-boundary',
      title: '修改边界',
      prompt: '填写本次唯一可写的仓库相对目录；可以等于或收窄已配置范围。',
      recommendation: target.modificationScope,
      answer: null,
    },
    {
      id: 'acceptance-signal',
      title: '验收信号',
      prompt: '用什么可观察证据判断需求完成？',
      recommendation: `真实预览中可以验证“${input.requestedOutcome}”，且没有新增控制台错误`,
      answer: null,
    },
  ]
}

function assertConfirmedScope(scope: string, target: Requirement['targetSnapshot']) {
  // resolve 后再比较，防止 `../` 等路径穿越绕过仓库相对目录限制。
  const configuredRoot = path.resolve(target.repositoryRoot, target.modificationScope)
  const confirmedRoot = path.resolve(target.repositoryRoot, scope)
  if (!scope || path.isAbsolute(scope) || (confirmedRoot !== configuredRoot && !confirmedRoot.startsWith(`${configuredRoot}${path.sep}`))) {
    throw new Error(`修改边界必须是 ${target.modificationScope} 或其子目录`)
  }
}
