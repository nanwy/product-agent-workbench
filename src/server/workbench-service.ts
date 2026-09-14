import { randomUUID } from 'node:crypto'

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
import {
  getTargetProcessSnapshot,
  startTargetProject,
  stopTargetProject,
} from './target-runtime'

export async function getWorkbenchSnapshot(): Promise<WorkbenchSnapshot> {
  const [requirement, target] = await Promise.all([
    requirementStore.load(),
    loadTargetProjectConfig().catch(() => null),
  ])

  return {
    requirement,
    target,
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
  const requirement: Requirement = {
    id: `REQ-${now.slice(0, 10).replaceAll('-', '')}-${randomUUID().slice(0, 8)}`,
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
      questions: createGrillingQuestions(input),
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
  return { stage, input, action, output, recordedAt: new Date().toISOString() }
}

function activateImplementationSkill(
  skills: Requirement['skills'],
  active: boolean,
  activationReason: string,
) {
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
  const requirement = await requirementStore.load()
  if (!requirement) return
  await requirementStore.save({
    ...requirement,
    processFacts,
    updatedAt: new Date().toISOString(),
    evidence: [
      ...requirement.evidence,
      evidence('project-adaptation', processFacts.command, action, output),
    ],
  })
}

function createGrillingQuestions(input: CreateRequirementInput) {
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
      prompt: '本次修改应限制在哪个页面和工程范围？',
      recommendation: `只修改目标页面 ${input.targetPage} 及配置范围内的必要代码`,
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
