// 需求状态机是整个项目的主轴。failed/cancelled 是旁路终态，因此阶段轨道只展示前八步。
export const requirementStages = [
  'project-adaptation',
  'intent-drafting',
  'awaiting-spec-confirmation',
  'awaiting-boundary-confirmation',
  'implementation',
  'verification',
  'awaiting-product-acceptance',
  'accepted',
  'failed',
  'cancelled',
] as const

export type RequirementStage = (typeof requirementStages)[number]

export type EvidenceRecord = {
  // 一条面向产品和工程师的审计事实，而不是可重新执行的命令日志。
  stage: RequirementStage
  input: string
  action: string
  output: string
  recordedAt: string
}

export type ExecutionTicket = {
  id: string
  title: string
  status: 'queued' | 'active' | 'completed' | 'blocked'
  acceptanceCriteria: string[]
}

export type ResolvedRule = {
  source: string
  scope: string
  summary: string
  active: boolean
  activationReason: string
  content: string
}

/** 工作台自身的 SDD 流程步骤，只用于解释当前工作流，不会发送给执行 Agent。 */
export type WorkflowStep = {
  name: string
  description: string
  active: boolean
  activationReason: string
}

/** 从目标仓库发现、且可能真正进入 Codex 执行上下文的 Skill。 */
export type DiscoveredSkill = {
  name: string
  description: string
  // 相对目标仓库根目录的入口；执行 Agent 按此路径自行读取，Requirement 不缓存全文。
  source: string
  active: boolean
  activationReason: string
}

export type GrillingQuestion = {
  id: string
  title: string
  prompt: string
  recommendation: string
  answer: string | null
}

export type GrillingSession = {
  status: 'pending' | 'confirmed'
  questions: GrillingQuestion[]
  confirmedAt: string | null
}

export type WorkingTreeEntry = {
  status: string
  path: string
}

export type BranchFacts = {
  // 创建需求分支前固定的 Git 基线；用于证明候选实现从哪里开始、改了什么。
  baseBranch: string
  baseRevision: string
  requirementBranch: string | null
  startingWorkingTree: WorkingTreeEntry[]
  createdAt: string | null
  blockedReason: string | null
}

export type AgentEvent = {
  id: string
  type: 'session' | 'message' | 'tool' | 'file' | 'completed' | 'failed' | 'cancelled' | 'unknown'
  message: string
  recordedAt: string
}

export type CodexSession = {
  // 工作台只保存对 UI 有意义的归一化状态，Codex CLI 原始 JSONL 不进入领域层。
  status: 'idle' | 'running' | 'completed' | 'failed' | 'cancelled'
  sessionId: string | null
  activeTicketId: string | null
  changedFiles: string[]
  events: AgentEvent[]
  error: string | null
}

export type BoundaryItem = {
  id: string
  label: string
  reason: string
  confirmed: boolean
}

export type RequirementSpec = {
  title: string
  desiredBehavior: string
  targetPage: string
  acceptanceCriteria: string[]
  proposedScope: string
  exploration: {
    inspectedFileCount: number
    candidateCodeLocations: string[]
    existingBehaviorEvidence: string[]
  }
}

export type Requirement = {
  // 聚合根同时承载产品契约、执行进度和交付证据，避免多个文件各自维护一套状态机。
  id: string
  specId: string
  title: string
  targetPage: string
  requestedOutcome: string
  stage: RequirementStage
  createdAt: string
  updatedAt: string
  modificationRounds: number
  grilling: GrillingSession
  spec: RequirementSpec | null
  tickets: ExecutionTicket[]
  rules: ResolvedRule[]
  workflow: WorkflowStep[]
  skills: DiscoveredSkill[]
  boundaryItems: BoundaryItem[]
  evidence: EvidenceRecord[]
  targetSnapshot: TargetProjectConfig
  processFacts: TargetProcessSnapshot
  branch: BranchFacts | null
  codex: CodexSession
  validation: ValidationRecord[]
  delivery: DeliveryCard | null
}

export type TargetProjectConfig = {
  // 这是仓库内受信任配置，不由表单输入或 Agent 输出动态改写。
  name: string
  repositoryRoot: string
  modificationScope: string
  startCommand: {
    command: string
    args: string[]
  }
  healthUrl: string
  previewUrl: string
  previewGatewayPort: number
  startupTimeoutMs: number
  validationCommands?: ValidationCommand[]
}

export type ValidationCommand = {
  // 验证命令来自受信任的目标项目配置，不接受产品输入或 Agent 输出拼接。
  id: string
  label: string
  command: string
  args: string[]
  timeoutMs: number
}

/**
 * 验证矩阵的一行。它不只保存“通过/失败”，还保存证据来源和可追溯的执行事实。
 *
 * verified：已有正向证据；failed：已有反向证据并阻止验收；
 * not-run：尚无证据，必须附原因，不能冒充 verified。
 */
export type ValidationRecord = {
  id: string
  criterion: string
  result: 'verified' | 'failed' | 'not-run'
  source: 'command' | 'git-scope' | 'runtime' | 'product'
  detail: string
  command: string | null
  exitCode: number | null
  startedAt: string
  completedAt: string
  notRunReason: string | null
}

/** 产品验收后冻结的交付快照，指向一个不再变动的 Git revision。 */
export type DeliveryCard = {
  branch: string
  revision: string
  changedFiles: string[]
  diffStat: string
  acceptedAt: string
  handoff: string
}

export type TargetProcessStatus =
  | 'unconfigured'
  | 'stopped'
  | 'starting'
  | 'ready'
  | 'failed'

export type TargetProcessSnapshot = {
  // 进程快照用于展示和审计；真正的 ChildProcess 句柄只存在于 target-runtime 模块内。
  status: TargetProcessStatus
  pid: number | null
  command: string
  healthUrl: string
  previewUrl: string
  gatewayUrl: string
  startedAt: string | null
  error: string | null
  recentOutput: string[]
}

export type WorkbenchSnapshot = {
  // 前端唯一读取模型：持久化需求 + 固定目标配置 + 当前易失运行态。
  requirement: Requirement | null
  target: TargetProjectConfig | null
  process: TargetProcessSnapshot
}

export type CreateRequirementInput = {
  title: string
  targetPage: string
  requestedOutcome: string
}

/**
 * 规范化并校验创建需求的产品输入。
 *
 * 确保标题、目标页面、期望结果均非空，并要求目标页面是以 `/` 开头的站内路径。
 * 该函数不访问文件系统，也不修改任何状态。
 *
 * @returns 去除首尾空白后的安全输入。
 * @throws 字段缺失或目标页面不是站内路径时抛出可展示给用户的错误。
 */
export function assertCreateRequirementInput(
  input: CreateRequirementInput,
): CreateRequirementInput {
  const title = input.title.trim()
  const targetPage = input.targetPage.trim()
  const requestedOutcome = input.requestedOutcome.trim()

  if (!title || !targetPage || !requestedOutcome) {
    throw new Error('需求标题、目标页面和期望结果均为必填项')
  }

  if (!targetPage.startsWith('/')) {
    throw new Error('目标页面必须是以 / 开头的站内路径')
  }

  return { title, targetPage, requestedOutcome }
}
