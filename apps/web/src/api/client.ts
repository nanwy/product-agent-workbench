import type {
  ConfirmGrillingRequest,
  ConfirmSpecRequest,
  ConfirmTicketsRequest,
  CreateRequirementRequest,
  ErrorResponse,
  HealthResponse,
  GenerateSpecRequest,
  GenerateTicketsRequest,
  PlanningResponse,
  ReconcileTaskRunsRequest,
  ReconcileTaskRunsResponse,
  RequirementResponse,
  TaskRunResponse,
  TargetRuntimeResponse,
  StartTargetRuntimeRequest,
  VerificationReportResponse,
  VerifyRequirementRequest,
  VerifyRequirementResponse,
} from './contracts'

/**
 * 练习 19A：浏览器到 Go 控制面的唯一 HTTP 入口。
 *
 * 前端视角：把它理解成项目自己的 axios instance。组件只调用业务名称明确的方法，
 * 不再到处重复 fetch、状态码判断和 JSON 解析。19A 只建立这一层，尚不替换页面调用。
 */

const defaultBaseUrl = '/api/v1'

type FetchLike = (input: RequestInfo | URL, init?: RequestInit) => Promise<Response>

export type RequestOptions = {
  /** React effect 卸载或用户取消时，把 AbortSignal 原样交给 fetch。 */
  signal?: AbortSignal
}

export type WorkbenchApiClientOptions = {
  /** 浏览器默认使用同源 /api/v1；测试可以传入绝对地址或自定义前缀。 */
  baseUrl?: string
  /** 注入 fetch 后，无需真的启动 Go 服务也能测试请求协议。 */
  fetch?: FetchLike
}

/**
 * WorkbenchApiError 表示“服务器已经返回一个非 2xx 响应”。
 *
 * 它不同于断网产生的 TypeError，也不同于 AbortError。页面以后可以分别展示：
 * 表单错误、版本冲突、服务不可达和用户主动取消。
 */
export class WorkbenchApiError extends Error {
  readonly status: number
  readonly code: string
  readonly requestId?: string

  constructor(status: number, response: ErrorResponse) {
    super(response.message)
    this.name = 'WorkbenchApiError'
    this.status = status
    this.code = response.code
    this.requestId = response.requestId
  }
}

export type WorkbenchApiClient = ReturnType<typeof createWorkbenchApiClient>

/**
 * 创建一份可复用的 API client。
 *
 * 第一步：固定 baseUrl 和 fetch 依赖，类似 React Provider 初始化 SDK。
 * 第二步：所有公开方法只收业务参数，不让组件手写 method、path 和 body。
 * 第三步：统一进入 requestJSON，成功和失败只解析一次响应体。
 */
export function createWorkbenchApiClient(options: WorkbenchApiClientOptions = {}) {
  const baseUrl = normalizeBaseUrl(options.baseUrl ?? defaultBaseUrl)
  const fetchRequest = options.fetch ?? globalThis.fetch.bind(globalThis)

  // 前端类比：下面每个方法类似一个 typed mutation/query function，但尚未绑定 React Query。
  return {
    getHealth: (requestOptions?: RequestOptions) =>
      requestJSON<HealthResponse>(fetchRequest, baseUrl, '/health', {
        signal: requestOptions?.signal,
      }),

    getTargetRuntime: (requestOptions?: RequestOptions) =>
      requestJSON<TargetRuntimeResponse>(fetchRequest, baseUrl, '/target-runtime', {
        signal: requestOptions?.signal,
      }),

    startTargetRuntime: (input: StartTargetRuntimeRequest, requestOptions?: RequestOptions) =>
      requestJSON<TargetRuntimeResponse>(fetchRequest, baseUrl, '/target-runtime/start', {
        method: 'POST',
        body: input,
        signal: requestOptions?.signal,
      }),

    stopTargetRuntime: (requestOptions?: RequestOptions) =>
      requestJSON<TargetRuntimeResponse>(fetchRequest, baseUrl, '/target-runtime/stop', {
        method: 'POST',
        signal: requestOptions?.signal,
      }),

    createRequirement: (input: CreateRequirementRequest, requestOptions?: RequestOptions) =>
      requestJSON<RequirementResponse>(fetchRequest, baseUrl, '/requirements', {
        method: 'POST',
        body: input,
        signal: requestOptions?.signal,
      }),

    getRequirement: (requirementId: string, requestOptions?: RequestOptions) =>
      requestJSON<RequirementResponse>(
        fetchRequest,
        baseUrl,
        `/requirements/${encodePathSegment(requirementId)}`,
        { signal: requestOptions?.signal },
      ),

    getPlanning: (requirementId: string, requestOptions?: RequestOptions) =>
      requestJSON<PlanningResponse>(
        fetchRequest,
        baseUrl,
        `/requirements/${encodePathSegment(requirementId)}/planning`,
        { signal: requestOptions?.signal },
      ),

    confirmGrilling: (
      requirementId: string,
      input: ConfirmGrillingRequest,
      requestOptions?: RequestOptions,
    ) => requestJSON<PlanningResponse>(
      fetchRequest,
      baseUrl,
      `/requirements/${encodePathSegment(requirementId)}/grilling/confirm`,
      { method: 'POST', body: input, signal: requestOptions?.signal },
    ),

    generateSpec: (
      requirementId: string,
      input: GenerateSpecRequest,
      requestOptions?: RequestOptions,
    ) => requestJSON<PlanningResponse>(
      fetchRequest,
      baseUrl,
      `/requirements/${encodePathSegment(requirementId)}/specs/generate`,
      { method: 'POST', body: input, signal: requestOptions?.signal },
    ),

    confirmSpec: (
      requirementId: string,
      versionId: string,
      input: ConfirmSpecRequest,
      requestOptions?: RequestOptions,
    ) => requestJSON<PlanningResponse>(
      fetchRequest,
      baseUrl,
      `/requirements/${encodePathSegment(requirementId)}/specs/${encodePathSegment(versionId)}/confirm`,
      { method: 'POST', body: input, signal: requestOptions?.signal },
    ),

    generateTickets: (
      requirementId: string,
      input: GenerateTicketsRequest,
      requestOptions?: RequestOptions,
    ) => requestJSON<PlanningResponse>(
      fetchRequest,
      baseUrl,
      `/requirements/${encodePathSegment(requirementId)}/tickets/generate`,
      { method: 'POST', body: input, signal: requestOptions?.signal },
    ),

    confirmTickets: (
      requirementId: string,
      versionId: string,
      input: ConfirmTicketsRequest,
      requestOptions?: RequestOptions,
    ) => requestJSON<PlanningResponse>(
      fetchRequest,
      baseUrl,
      `/requirements/${encodePathSegment(requirementId)}/tickets/${encodePathSegment(versionId)}/confirm`,
      { method: 'POST', body: input, signal: requestOptions?.signal },
    ),

    reconcileTaskRuns: (input: ReconcileTaskRunsRequest, requestOptions?: RequestOptions) =>
      requestJSON<ReconcileTaskRunsResponse>(fetchRequest, baseUrl, '/task-runs', {
        method: 'POST',
        body: input,
        signal: requestOptions?.signal,
      }),

    getTaskRun: (taskRunId: string, requestOptions?: RequestOptions) =>
      requestJSON<TaskRunResponse>(fetchRequest, baseUrl, `/task-runs/${encodePathSegment(taskRunId)}`, {
        signal: requestOptions?.signal,
      }),

    cancelTaskRun: (taskRunId: string, requestOptions?: RequestOptions) =>
      requestJSON<TaskRunResponse>(
        fetchRequest,
        baseUrl,
        `/task-runs/${encodePathSegment(taskRunId)}/cancel`,
        { method: 'POST', signal: requestOptions?.signal },
      ),

    verifyRequirement: (
      requirementId: string,
      input: VerifyRequirementRequest,
      requestOptions?: RequestOptions,
    ) =>
      requestJSON<VerifyRequirementResponse>(
        fetchRequest,
        baseUrl,
        `/requirements/${encodePathSegment(requirementId)}/verify`,
        { method: 'POST', body: input, signal: requestOptions?.signal },
      ),

    getLatestVerification: (
      requirementId: string,
      ticketVersionId: string,
      requestOptions?: RequestOptions,
    ) => {
      const query = new URLSearchParams({ ticketVersionId })
      return requestJSON<VerificationReportResponse>(
        fetchRequest,
        baseUrl,
        `/requirements/${encodePathSegment(requirementId)}/verification?${query}`,
        { signal: requestOptions?.signal },
      )
    },
  }
}

type JSONRequest = {
  method?: 'GET' | 'POST'
  body?: unknown
  signal?: AbortSignal
}

/**
 * requestJSON 集中处理 HTTP 的机械工作。
 *
 * 前端视角：它相当于 axios 的 response interceptor：
 * 1. 有 body 时设置 JSON Content-Type；GET 不发送空 body。
 * 2. 先读取 text，再解析 JSON，避免错误响应被读取两次。
 * 3. 非 2xx 转成 WorkbenchApiError；网络错误和取消错误保持 fetch 原样。
 */
async function requestJSON<ResponseBody>(
  fetchRequest: FetchLike,
  baseUrl: string,
  path: string,
  request: JSONRequest,
): Promise<ResponseBody> {
  const hasBody = request.body !== undefined
  const response = await fetchRequest(`${baseUrl}${path}`, {
    method: request.method ?? 'GET',
    headers: {
      Accept: 'application/json',
      ...(hasBody ? { 'Content-Type': 'application/json' } : {}),
    },
    body: hasBody ? JSON.stringify(request.body) : undefined,
    signal: request.signal,
  })
  const payload = await parseJSON(response)

  if (!response.ok) {
    throw new WorkbenchApiError(response.status, toErrorResponse(payload))
  }
  return payload as ResponseBody
}

async function parseJSON(response: Response): Promise<unknown> {
  const text = await response.text()
  try {
    return JSON.parse(text) as unknown
  } catch {
    throw new WorkbenchApiError(response.status, {
      code: 'invalid_response',
      message: 'Go 服务返回了无法解析的 JSON',
    })
  }
}

function toErrorResponse(payload: unknown): ErrorResponse {
  if (isRecord(payload) && typeof payload.code === 'string' && typeof payload.message === 'string') {
    return {
      code: payload.code,
      message: payload.message,
      requestId: typeof payload.requestId === 'string' ? payload.requestId : undefined,
    }
  }
  return { code: 'invalid_response', message: 'Go 服务返回了未知错误' }
}

function normalizeBaseUrl(value: string) {
  const normalized = value.trim().replace(/\/+$/, '')
  if (!normalized) throw new Error('Workbench API baseUrl 不能为空')
  return normalized
}

function encodePathSegment(value: string) {
  const normalized = value.trim()
  if (!normalized) throw new Error('API 资源 ID 不能为空')
  return encodeURIComponent(normalized)
}

function isRecord(value: unknown): value is Record<string, unknown> {
  return typeof value === 'object' && value !== null && !Array.isArray(value)
}
