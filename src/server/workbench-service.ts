import { randomUUID } from 'node:crypto'

import {
  assertCreateRequirementInput,
  type CreateRequirementInput,
  type EvidenceRecord,
  type Requirement,
  type WorkbenchSnapshot,
} from '../domain/workbench'
import { loadTargetProjectConfig } from './config'
import { exploreRequirement } from './exploration'
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
    spec: null,
    tickets: [],
    rules: [],
    skills: [],
    boundaryItems: [],
    targetSnapshot: target,
    processFacts,
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
