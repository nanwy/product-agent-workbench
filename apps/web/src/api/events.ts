import type { QueryClient } from '@tanstack/react-query'
import { useEffect, useState } from 'react'

import type { ReplayResetResponse, TaskEventResponse } from './contracts'
import { goQueryKeys } from './queries'

/**
 * 练习 19C：把 Go 持久化 TaskEvent 投影成 React Query 缓存失效信号。
 *
 * 前端视角：SSE 类似 WebSocket 推送的 entityChanged 通知。事件 payload 用于展示日志，
 * 但不能代替完整 TaskRun；收到通知后仍由 Query 通过普通 HTTP 读取权威资源。
 */

const cursorStorageKey = 'product-agent-workbench.go-events.cursor.v1'
const defaultEventsUrl = '/api/v1/events'

export type GoEventStreamStatus = 'connecting' | 'connected' | 'reconnecting' | 'error'

type CursorStorage = Pick<Storage, 'getItem' | 'setItem' | 'removeItem'>

/**
 * 页面刷新后原生 EventSource 不记得上一个实例的 Last-Event-ID，因此把已确认游标放入 URL。
 * 同一个 EventSource 后续自动重连时，浏览器会发送标准 Last-Event-ID 请求头。
 */
export function buildGoEventsUrl(storage: CursorStorage, baseUrl = defaultEventsUrl) {
  const cursor = readEventCursor(storage)
  if (cursor === 0) return baseUrl
  const separator = baseUrl.includes('?') ? '&' : '?'
  return `${baseUrl}${separator}after=${cursor}`
}

export function readEventCursor(storage: CursorStorage) {
  const value = storage.getItem(cursorStorageKey)
  if (value === null) return 0
  const cursor = Number(value)
  return Number.isSafeInteger(cursor) && cursor > 0 ? cursor : 0
}

/** 收到单条任务事件后只失效对应 TaskRun，不做整页 refetch。 */
export function applyTaskEvent(
  queryClient: QueryClient,
  storage: CursorStorage,
  eventId: string,
  payload: TaskEventResponse,
) {
  const cursor = parseEventID(eventId)
  assertTaskEvent(payload)
  storage.setItem(cursorStorageKey, String(cursor))
  return queryClient.invalidateQueries({
    queryKey: goQueryKeys.taskRun(payload.taskRunId),
    exact: true,
  })
}

/**
 * replay-reset 表示游标与数据库历史已经断开，不能靠局部事件猜状态。
 * 保存服务端给出的最新边界，并让所有正在观察的 Go Query 回源。
 */
export function applyReplayReset(
  queryClient: QueryClient,
  storage: CursorStorage,
  eventId: string,
  payload: ReplayResetResponse,
) {
  const cursor = parseEventID(eventId)
  assertReplayReset(payload)
  if (cursor !== payload.latestEventId) throw new Error('SSE reset 游标与 payload 不一致')
  if (cursor === 0) storage.removeItem(cursorStorageKey)
  else storage.setItem(cursorStorageKey, String(cursor))
  return queryClient.invalidateQueries({ queryKey: goQueryKeys.all })
}

/**
 * React hook 只管理连接生命周期，不保存业务事件数组。
 *
 * 原生 EventSource 自带退避重连；onerror 只更新展示状态，不能再 new 第二个连接。
 * effect cleanup 关闭连接，避免路由切换或 StrictMode 重挂载后留下重复订阅。
 */
export function useGoTaskEventStream(queryClient: QueryClient) {
  const [status, setStatus] = useState<GoEventStreamStatus>('connecting')
  const [error, setError] = useState<string | null>(null)

  useEffect(() => {
    const storage = window.localStorage
    const source = new EventSource(buildGoEventsUrl(storage))

    source.onopen = () => {
      setStatus('connected')
      setError(null)
    }
    source.onerror = () => setStatus('reconnecting')

    const handleTaskEvent = (event: Event) => {
      const message = event as MessageEvent<string>
      try {
        const payload = parseTaskEvent(message.data)
        void applyTaskEvent(queryClient, storage, message.lastEventId, payload)
      } catch (streamError) {
        setStatus('error')
        setError(errorMessage(streamError))
      }
    }
    const handleReplayReset = (event: Event) => {
      const message = event as MessageEvent<string>
      try {
        const payload = parseReplayReset(message.data)
        void applyReplayReset(queryClient, storage, message.lastEventId, payload)
      } catch (streamError) {
        setStatus('error')
        setError(errorMessage(streamError))
      }
    }

    source.addEventListener('task-event', handleTaskEvent)
    source.addEventListener('replay-reset', handleReplayReset)
    return () => source.close()
  }, [queryClient])

  return { status, error }
}

function parseTaskEvent(value: string): TaskEventResponse {
  const payload = parseObject(value)
  assertTaskEvent(payload)
  return payload
}

function parseReplayReset(value: string): ReplayResetResponse {
  const payload = parseObject(value)
  assertReplayReset(payload)
  return payload
}

function parseObject(value: string): Record<string, unknown> {
  const payload: unknown = JSON.parse(value)
  if (typeof payload !== 'object' || payload === null || Array.isArray(payload)) {
    throw new Error('SSE data 不是 JSON 对象')
  }
  return payload as Record<string, unknown>
}

function assertTaskEvent(value: unknown): asserts value is TaskEventResponse {
  if (
    typeof value !== 'object' ||
    value === null ||
    typeof (value as TaskEventResponse).taskRunId !== 'string' ||
    !(value as TaskEventResponse).taskRunId.trim() ||
    typeof (value as TaskEventResponse).sequence !== 'number' ||
    typeof (value as TaskEventResponse).type !== 'string' ||
    typeof (value as TaskEventResponse).payload !== 'string' ||
    typeof (value as TaskEventResponse).createdAt !== 'string'
  ) {
    throw new Error('SSE task-event 结构无效')
  }
}

function assertReplayReset(value: unknown): asserts value is ReplayResetResponse {
  if (
    typeof value !== 'object' ||
    value === null ||
    typeof (value as ReplayResetResponse).reason !== 'string' ||
    !Number.isSafeInteger((value as ReplayResetResponse).oldestEventId) ||
    !Number.isSafeInteger((value as ReplayResetResponse).latestEventId)
  ) {
    throw new Error('SSE replay-reset 结构无效')
  }
}

function parseEventID(value: string) {
  const eventID = Number(value)
  if (!Number.isSafeInteger(eventID) || eventID < 0) throw new Error('SSE event ID 无效')
  return eventID
}

function errorMessage(error: unknown) {
  return error instanceof Error ? error.message : String(error)
}

