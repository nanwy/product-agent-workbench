/**
 * 练习 19A：Go HTTP 合约在前端的 TypeScript 镜像。
 *
 * 前端视角：这里和根据 OpenAPI 生成的 types.ts 很像，只描述“网络上传输什么”，
 * 不保存 React 状态，也不包含按钮点击后的业务编排。字段名必须与 Go struct 的 json tag
 * 一致；Go 的 time.Time 到浏览器后是 ISO 8601 字符串，而不是 JavaScript Date。
 */

// 第一步：把后端枚举写成联合类型。这样组件不能随手拼出不存在的状态。
export type RequirementStage =
  | 'grilling'
  | 'spec'
  | 'tickets'
  | 'implementation'
  | 'acceptance'
  | 'delivered'

export type TaskStatus =
  | 'queued'
  | 'claimed'
  | 'running'
  | 'completed'
  | 'failed'
  | 'cancelled'

export type VerificationStatus = 'verified' | 'failed' | 'not-run'
export type VerificationSource = 'command' | 'candidate'
export type VerificationReportStatus = 'passed' | 'blocked'

// 第二步：分别声明请求 DTO 和响应 DTO。请求里没有的字段，浏览器就不应该伪造。
export type CreateRequirementRequest = {
  projectId: string
  title: string
}

export type RequirementResponse = {
  id: string
  projectId: string
  title: string
  stage: RequirementStage
  revision: number
}

export type ConfirmGrillingRequest = {
  expectedRevision: number
  summary: string
  desiredOutcome: string
  changeBoundary: string
  acceptanceSignal: string
}

export type GenerateSpecRequest = {
  expectedRevision: number
  instruction: string
}

export type ConfirmSpecRequest = {
  expectedRevision: number
}

export type GenerateTicketsRequest = {
  expectedRevision: number
  instruction: string
}

export type ConfirmTicketsRequest = {
  expectedRevision: number
}

export type GrillingUnderstandingResponse = {
  summary: string
  desiredOutcome: string
  changeBoundary: string
  acceptanceSignal: string
  confirmedAt: string
}

export type SpecDocumentResponse = {
  problem: string
  solution: string
  userStories: string[]
  implementationDecisions: string[]
  testingDecisions: string[]
  outOfScope: string[]
  furtherNotes: string[]
}

export type SpecVersionResponse = {
  id: string
  version: number
  status: 'draft' | 'confirmed' | 'superseded'
  document: SpecDocumentResponse
  createdAt: string
  confirmedAt?: string
}

export type TicketResponse = {
  id: string
  title: string
  blockedBy: string[]
  deliverable: string
  acceptanceCriteria: string[]
  modificationScopes: string[]
}

export type TicketVersionResponse = {
  id: string
  specVersionId: string
  version: number
  status: 'draft' | 'confirmed' | 'superseded'
  baseRevision: string
  requirementBranch: string
  tickets: TicketResponse[]
  createdAt: string
  confirmedAt?: string
}

export type PlanningResponse = {
  requirement: RequirementResponse
  understanding?: GrillingUnderstandingResponse
  specVersions: SpecVersionResponse[]
  ticketVersions: TicketVersionResponse[]
}

export type ReconcileTaskRunsRequest = {
  requirementId: string
  ticketVersionId: string
}

export type TaskResultResponse = {
  summary: string
  codexThreadId?: string
  worktreePath?: string
  revision?: string
}

export type TaskRunResponse = {
  id: string
  requirementId: string
  ticketVersionId: string
  ticketId: string
  attempt: number
  status: TaskStatus
  createdAt: string
  claimedAt?: string
  startedAt?: string
  finishedAt?: string
  result?: TaskResultResponse
  failureReason?: string
}

export type ReconcileTaskRunsResponse = {
  ticketVersionId: string
  enqueued: TaskRunResponse[]
}

export type VerifyRequirementRequest = {
  ticketVersionId: string
}

export type VerificationEvidenceResponse = {
  id: string
  label: string
  status: VerificationStatus
  detail: string
  reason?: string
  required: boolean
  source: VerificationSource
  command?: string
  exitCode?: number
  startedAt: string
  completedAt: string
}

export type VerificationReportResponse = {
  id: string
  requirementId: string
  ticketVersionId: string
  candidateRevision?: string
  status: VerificationReportStatus
  records: VerificationEvidenceResponse[]
  startedAt: string
  completedAt: string
}

export type VerifyRequirementResponse = {
  requirement: RequirementResponse
  report: VerificationReportResponse
  deliverable: boolean
}

export type HealthResponse = {
  status: string
}

// 练习 19C：SSE 外层事件固定叫 task-event，真正的 Codex 业务类型放在 type 字段。
export type TaskEventResponse = {
  taskRunId: string
  sequence: number
  type: string
  payload: string
  createdAt: string
}

export type ReplayResetResponse = {
  reason: 'cursor_outside_retained_window' | string
  oldestEventId: number
  latestEventId: number
}

// 第三步：错误也属于 API 合约。页面应判断 code，而不是解析中文 message。
export type ErrorResponse = {
  code: 'invalid_request' | 'not_found' | 'conflict' | 'internal_error' | string
  message: string
  requestId?: string
}
