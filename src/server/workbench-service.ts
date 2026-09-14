import { randomUUID } from 'node:crypto'
import path from 'node:path'

import {
  assertCreateRequirementInput,
  assertContinueRefinementInput,
  type CreateRequirementInput,
  type ContinueRefinementInput,
  type EvidenceRecord,
  type GrillingQuestion,
  type Requirement,
  type WorkbenchSnapshot,
} from '../domain/workbench'
import { loadTargetProjectConfig } from './config'
import { exploreRequirement, workflowSteps } from './exploration'
import { requirementStore } from './requirement-store'
import { reconcileInterruptedExecution } from './workbench-execution'
import { commitAcceptedCandidate } from './git-workspace'
import {
  getTargetProcessSnapshot,
  startTargetProject,
  stopTargetProject,
} from './target-runtime'

/**
 * 组合当前工作台的权威读取模型。
 *
 * 并行读取 Requirement 与目标配置，再从进程管理器取得易失运行态。
 * 路由和 UI 应读取这个快照，不应分别访问底层模块后自行拼装状态。
 */
export async function getWorkbenchSnapshot(): Promise<WorkbenchSnapshot> {
  // 首次读取也承担服务重启恢复：不会重放写动作，只把失去进程的 running 变为可继续状态。
  await reconcileInterruptedExecution()
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

/**
 * 创建新的 Requirement 聚合并原子保存。
 *
 * 初始化 Grilling 问题、工作流步骤、目标配置快照、Codex 空闲态和首批审计证据。
 * 此时不会探索目标代码、创建 Git 分支或启动 Codex。
 */
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
    metrics: { firstPreviewAt: null, acceptedAt: null },
    refinements: [],
    grilling: {
      status: 'pending',
      confirmedAt: null,
      questions: createGrillingQuestions(input, target),
    },
    spec: null,
    tickets: [],
    rules: [],
    workflow: workflowSteps(false),
    skills: [],
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

  // UI 只显示一个表单不是安全边界；服务端同样禁止 API 覆盖仍可继续的活动需求。
  await requirementStore.update((current) => {
    assertCanCreateRequirement(current)
    return requirement
  })
  return getWorkbenchSnapshot()
}

/**
 * 对 Grilling 已确认的当前需求执行只读探索。
 *
 * 解析代码、规则与 Skills，生成 Spec、执行票和边界项，然后推进到规格确认阶段。
 */
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
    workflow: result.workflow,
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

/**
 * 确认 Grilling 回答并冻结本轮共同理解。
 *
 * 要求所有问题都有答案，并验证用户确认的写入目录没有扩大配置授权范围。
 */
export async function confirmCurrentGrilling(answers: Record<string, string>): Promise<WorkbenchSnapshot> {
  const requirement = await requireCurrentRequirement()
  if (requirement.grilling.status === 'confirmed') return getWorkbenchSnapshot()
  const questions = confirmGrillingAnswers(requirement.grilling.questions, answers)
  const confirmedScope = questions.find((question) => question.id === 'change-boundary')?.answer ?? ''
  // 用户可以收窄修改范围，但不能借回答问题扩大配置文件授权的目录。
  assertConfirmedScope(confirmedScope, requirement.targetSnapshot)
  const now = new Date().toISOString()
  await requirementStore.save({
    ...requirement,
    grilling: { status: 'confirmed', questions, confirmedAt: now },
    workflow: workflowSteps(true),
    updatedAt: now,
    evidence: [...requirement.evidence, evidence('intent-drafting', questions.map((question) => `${question.title}: ${question.answer}`).join('；'), '完成 grilling 设计树并确认共同理解', '需求可以进入只读探索与 Spec 生成')],
  })
  return getWorkbenchSnapshot()
}

/**
 * 确认候选 Spec 与执行票。
 *
 * 无边界风险时直接开放实现；有未确认边界时先进入边界确认阶段。
 */
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
    workflow: activateImplementationStep(
      requirement.workflow,
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

/**
 * 确认全部工程边界项并开放候选实现。
 *
 * 这里只记录产品决定和激活 implement 工作流步骤，不会直接修改目标仓库。
 */
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
    workflow: activateImplementationStep(
      requirement.workflow,
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

/**
 * 按受信任配置启动目标开发服务并保存运行证据。
 *
 * 实际进程生命周期由 target-runtime 管理，本函数负责把结果接回 Requirement 聚合。
 */
export async function startConfiguredTarget(): Promise<WorkbenchSnapshot> {
  const config = await loadTargetProjectConfig()
  const processFacts = await startTargetProject(config)
  await saveProcessFacts(processFacts, '启动目标项目', `运行状态：${processFacts.status}，PID：${processFacts.pid ?? '—'}`)
  return getWorkbenchSnapshot()
}

/**
 * 停止工作台拥有的目标进程和预览网关，并保存停止证据。
 */
export async function stopConfiguredTarget(): Promise<WorkbenchSnapshot> {
  const config = await loadTargetProjectConfig()
  const processFacts = await stopTargetProject(config)
  await saveProcessFacts(processFacts, '停止目标项目', '已回收工作台拥有的目标进程与预览网关')
  return getWorkbenchSnapshot()
}

/**
 * 在同一 Requirement、Git 分支和 Codex session 中追加一轮产品返修。
 *
 * 产品文本只进入 Ticket 和后续 Codex prompt，不参与 Shell 命令构造。提交时保存当前
 * 验证矩阵作为上一版候选证据，然后恢复 implementation；真正执行仍由原执行队列负责。
 */
export async function continueCurrentRefinement(rawInput: ContinueRefinementInput): Promise<WorkbenchSnapshot> {
  const input = assertContinueRefinementInput(rawInput)
  const requirement = await requireCurrentRequirement()
  if (requirement.stage !== 'awaiting-product-acceptance') throw new Error('只有待产品验收的候选实现可以继续返修')
  if (!requirement.branch?.requirementBranch || !requirement.codex.sessionId) throw new Error('返修必须复用已建立的需求分支和 Codex session')
  const now = new Date().toISOString()
  const nextNumber = requirement.tickets.reduce((largest, ticket) => {
    const value = Number(ticket.id.match(/(\d+)$/)?.[1] ?? 0)
    return Math.max(largest, value)
  }, 0) + 1
  const ticketId = `TICKET-${String(nextNumber).padStart(2, '0')}`
  await requirementStore.save({
    ...requirement,
    stage: 'implementation',
    updatedAt: now,
    delivery: null,
    tickets: [...requirement.tickets, {
      id: ticketId,
      title: input.requestedOutcome.length > 32 ? `${input.requestedOutcome.slice(0, 31)}…` : input.requestedOutcome,
      status: 'queued',
      acceptanceCriteria: [`在 ${requirement.targetPage} 验证返修结果：${input.requestedOutcome}`, '继续遵守原 Spec、修改范围、生效规则和生效 Skill'],
    }],
    // sessionId、branch、Requirement ID、Spec ID、Spec 与规则均通过展开原聚合保持不变。
    codex: { ...requirement.codex, status: 'idle', activeTicketId: null, error: null },
    refinements: [...requirement.refinements, {
      id: `REFINEMENT-${String(requirement.refinements.length + 1).padStart(2, '0')}`,
      ticketId,
      requestedOutcome: input.requestedOutcome,
      requestedAt: now,
      status: 'queued',
      baselineValidation: requirement.validation,
    }],
    evidence: [...requirement.evidence, evidence('implementation', input.requestedOutcome, `追加返修执行票 ${ticketId}`, '保留原分支、Session、范围、规则和上一版验证证据')],
  })
  return getWorkbenchSnapshot()
}

/**
 * 对通过自动验证的候选实现执行产品验收。
 *
 * 服务端重新执行阶段、失败项、分支和修改范围门禁；全部通过后创建稳定 commit，
 * 将产品验证项标记为 verified，并生成交付 Handoff。不会 push、merge 或发布。
 */
export async function acceptCurrentRequirement(): Promise<WorkbenchSnapshot> {
  const requirement = await requireCurrentRequirement()
  // 第一层门禁：只有验证编排已结束并转入待验收阶段，按钮才能生效。
  // 第二层门禁：服务端再次检查 failed，不信任前端的 disabled 状态。
  if (requirement.stage !== 'awaiting-product-acceptance') throw new Error('只有自动验证完成的候选实现可以验收')
  if (requirement.validation.some((item) => item.required && item.result !== 'verified')) {
    throw new Error('存在失败或未运行的必要验证项，不能验收')
  }
  const configuredTarget = await loadTargetProjectConfig()
  const target = { ...configuredTarget, modificationScope: requirement.spec?.proposedScope ?? configuredTarget.modificationScope }
  // 验收的副作用是“冻结候选实现”：先重查当前分支/范围，再创建稳定 commit。
  // 只有 commit 成功后才会把 Requirement 写成 accepted，中途失败不会产生假验收。
  const committed = await commitAcceptedCandidate(requirement, target)
  const acceptedAt = new Date().toISOString()
  const validation = requirement.validation.map((item) => item.source === 'product' && item.result === 'not-run' ? {
    ...item,
    result: 'verified' as const,
    detail: item.id === 'product-acceptance' ? '产品已在工作台显式确认候选实现' : '产品已结合真实预览与 agent-browser 证据确认该验收条件',
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
    metrics: { ...requirement.metrics, acceptedAt },
    evidence: [...requirement.evidence, evidence('accepted', '产品显式验收候选实现', '创建稳定提交并生成前端交付 Handoff', `${committed.branch} @ ${committed.revision.slice(0, 8)}`)],
  })
  return getWorkbenchSnapshot()
}

/** 读取当前 Requirement；没有活动需求时统一抛出领域错误。 */
async function requireCurrentRequirement() {
  const requirement = await requirementStore.load()
  if (!requirement) throw new Error('请先创建产品需求')
  return requirement
}

/** 创建一条只追加的阶段证据记录。 */
function evidence(
  stage: EvidenceRecord['stage'],
  input: string,
  action: string,
  output: string,
): EvidenceRecord {
  return { stage, input, action, output, recordedAt: new Date().toISOString() }
}

/** 切换 implement 工作流步骤的激活状态，同时保留其他步骤原样。 */
function activateImplementationStep(
  workflow: Requirement['workflow'],
  active: boolean,
  activationReason: string,
) {
  return workflow.map((step) =>
    step.name === 'implement'
      ? { ...step, active, activationReason }
      : step,
  )
}

/** 把易失的目标进程快照同步进 Requirement，并追加对应审计证据。 */
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
      metrics: {
        ...current.metrics,
        firstPreviewAt: processFacts.status === 'ready' ? current.metrics.firstPreviewAt ?? new Date().toISOString() : current.metrics.firstPreviewAt,
      },
      updatedAt: new Date().toISOString(),
      evidence: [
        ...current.evidence,
        evidence('project-adaptation', processFacts.command, action, output),
      ],
    }
  })
}

/** 根据产品输入和配置边界建立 MVP 的三道固定 Grilling 问题。 */
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

/** 阻止新请求覆盖仍可继续的唯一活动 Requirement；accepted 是唯一可替换终态。 */
export function assertCanCreateRequirement(current: Pick<Requirement, 'stage'> | null) {
  if (current && current.stage !== 'accepted') {
    throw new Error('当前产品需求尚未验收，不能建立第二条活动需求')
  }
}

/** 校验每道 Grilling 问题都有非空回答，并返回已去除首尾空白的确认结果。 */
export function confirmGrillingAnswers(questions: GrillingQuestion[], answers: Record<string, string>) {
  return questions.map((question) => {
    const answer = answers[question.id]?.trim()
    if (!answer) throw new Error(`请回答：${question.title}`)
    return { ...question, answer }
  })
}

/** 校验确认范围只能等于或位于配置范围之下，并阻止绝对路径与目录穿越。 */
export function assertConfirmedScope(scope: string, target: Requirement['targetSnapshot']) {
  // resolve 后再比较，防止 `../` 等路径穿越绕过仓库相对目录限制。
  const configuredRoot = path.resolve(target.repositoryRoot, target.modificationScope)
  const confirmedRoot = path.resolve(target.repositoryRoot, scope)
  if (!scope || path.isAbsolute(scope) || (confirmedRoot !== configuredRoot && !confirmedRoot.startsWith(`${configuredRoot}${path.sep}`))) {
    throw new Error(`修改边界必须是 ${target.modificationScope} 或其子目录`)
  }
}
