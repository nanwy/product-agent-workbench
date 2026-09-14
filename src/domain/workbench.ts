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

export type DiscoveredSkill = {
  name: string
  description: string
  source: string
  kind: 'workflow' | 'target-project'
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
  id: string
  label: string
  command: string
  args: string[]
  timeoutMs: number
}

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
  requirement: Requirement | null
  target: TargetProjectConfig | null
  process: TargetProcessSnapshot
}

export type CreateRequirementInput = {
  title: string
  targetPage: string
  requestedOutcome: string
}

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
