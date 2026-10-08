import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'

import { createWorkbenchApiClient } from './client'
import type {
  CreateRequirementRequest,
  ReconcileTaskRunsRequest,
  RequirementResponse,
  TaskRunResponse,
  VerificationReportResponse,
  VerifyRequirementRequest,
} from './contracts'

/**
 * 练习 19B：把 19A 的普通 Promise 函数接入 React Query。
 *
 * 前端视角：API Client 只负责“怎样发请求”，本文件负责“响应放在哪个缓存槽、什么时候更新”。
 * 页面不应该同时维护一份 Requirement useState，否则服务端事实会出现两个真值来源。
 */

const api = createWorkbenchApiClient()

// 第一步：集中 Query Key。数组层级让以后可以按 requirement 或整个 Go API 范围失效缓存。
export const goQueryKeys = {
  all: ['go-control-plane'] as const,
  health: () => [...goQueryKeys.all, 'health'] as const,
  requirement: (requirementId: string) =>
    [...goQueryKeys.all, 'requirement', requirementId] as const,
  taskRun: (taskRunId: string) => [...goQueryKeys.all, 'task-run', taskRunId] as const,
  verification: (requirementId: string, ticketVersionId: string) =>
    [...goQueryKeys.all, 'verification', requirementId, ticketVersionId] as const,
}

/** 第二步：query hook 只读取服务端事实；enabled 防止拿空 ID 拼出无意义请求。 */
export function useGoHealthQuery() {
  return useQuery({
    queryKey: goQueryKeys.health(),
    queryFn: ({ signal }) => api.getHealth({ signal }),
    staleTime: 10_000,
    retry: false,
  })
}

export function useGoRequirementQuery(requirementId: string) {
  return useQuery({
    queryKey: goQueryKeys.requirement(requirementId),
    queryFn: ({ signal }) => api.getRequirement(requirementId, { signal }),
    enabled: requirementId.length > 0,
    retry: false,
  })
}

export function useGoTaskRunQuery(taskRunId: string) {
  return useQuery({
    queryKey: goQueryKeys.taskRun(taskRunId),
    queryFn: ({ signal }) => api.getTaskRun(taskRunId, { signal }),
    enabled: taskRunId.length > 0,
    refetchInterval: (query) => {
      const status = query.state.data?.status
      return status === 'queued' || status === 'claimed' || status === 'running' ? 1_000 : false
    },
    retry: false,
  })
}

export function useGoVerificationQuery(requirementId: string, ticketVersionId: string) {
  return useQuery({
    queryKey: goQueryKeys.verification(requirementId, ticketVersionId),
    queryFn: ({ signal }) =>
      api.getLatestVerification(requirementId, ticketVersionId, { signal }),
    enabled: requirementId.length > 0 && ticketVersionId.length > 0,
    retry: false,
  })
}

/**
 * 第三步：mutation 成功后，把响应直接写入它对应的 Query Cache。
 *
 * 这类似 Redux 中用服务端响应更新 normalized entity，而不是再请求整张页面快照。响应已经
 * 是 Go 刚保存的权威事实，因此这里 setQueryData 比无条件 invalidate + refetch 少一次瀑布请求。
 */
export function useCreateRequirementMutation() {
  const queryClient = useQueryClient()
  return useMutation({
    mutationFn: (input: CreateRequirementRequest) => api.createRequirement(input),
    onSuccess: (requirement) => writeRequirementCache(queryClient, requirement),
  })
}

export function useReconcileTaskRunsMutation() {
  const queryClient = useQueryClient()
  return useMutation({
    mutationFn: (input: ReconcileTaskRunsRequest) => api.reconcileTaskRuns(input),
    onSuccess: (response) => {
      for (const run of response.enqueued) writeTaskRunCache(queryClient, run)
    },
  })
}

export function useCancelTaskRunMutation() {
  const queryClient = useQueryClient()
  return useMutation({
    mutationFn: (taskRunId: string) => api.cancelTaskRun(taskRunId),
    onSuccess: (run) => writeTaskRunCache(queryClient, run),
  })
}

export function useVerifyRequirementMutation() {
  const queryClient = useQueryClient()
  return useMutation({
    mutationFn: (input: {
      requirementId: string
      request: VerifyRequirementRequest
    }) => api.verifyRequirement(input.requirementId, input.request),
    onSuccess: (response) => {
      writeRequirementCache(queryClient, response.requirement)
      writeVerificationCache(queryClient, response.report)
    },
  })
}

// 缓存写入函数保持为小而纯的 adapter，测试无需渲染 React 组件。
type QueryCacheWriter = {
  setQueryData: (queryKey: readonly unknown[], value: unknown) => unknown
}

export function writeRequirementCache(cache: QueryCacheWriter, requirement: RequirementResponse) {
  cache.setQueryData(goQueryKeys.requirement(requirement.id), requirement)
}

export function writeTaskRunCache(cache: QueryCacheWriter, run: TaskRunResponse) {
  cache.setQueryData(goQueryKeys.taskRun(run.id), run)
}

export function writeVerificationCache(
  cache: QueryCacheWriter,
  report: VerificationReportResponse,
) {
  cache.setQueryData(
    goQueryKeys.verification(report.requirementId, report.ticketVersionId),
    report,
  )
}

