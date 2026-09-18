import { randomUUID } from 'node:crypto'

import type { AgentEvent, WorkbenchConversation } from '../domain/workbench'

/** 在同一逻辑对话中开启一张执行票；只有实现 Thread 与规划 Thread 分离。 */
export function beginImplementationTurn(conversation: WorkbenchConversation, ticketId: string): WorkbenchConversation {
  return {
    ...conversation,
    status: 'running',
    error: null,
    messages: [...conversation.messages, {
      id: randomUUID(), role: 'assistant', phase: 'implement', status: 'streaming',
      recordedAt: new Date().toISOString(),
      segments: [{ id: randomUUID(), type: 'gate', label: `开始 ${ticketId}`, detail: '独立 workspace-write Thread · 目标范围受控' }],
    }],
  }
}

/** 把执行事件按 item ID 折叠到对话；命令和文件事件显示为可展开活动。 */
export function applyImplementationEvent(conversation: WorkbenchConversation, item: AgentEvent, sessionId: string | null): WorkbenchConversation {
  const messages = [...conversation.messages]
  const index = messages.map((message) => message.phase === 'implement' && message.role === 'assistant' && message.status === 'streaming').lastIndexOf(true)
  if (index < 0) return { ...conversation, implementationThreadId: sessionId ?? conversation.implementationThreadId }
  const active = messages[index]
  if (item.type === 'message' || item.type === 'tool' || item.type === 'file') {
    const segment = item.type === 'message'
      ? { id: item.id, type: 'text' as const, content: item.message }
      : { id: item.id, type: 'activity' as const, label: item.type === 'file' ? '文件变更' : 'Agent 活动', detail: item.message, status: item.activityStatus ?? 'completed' }
    const segments = [...active.segments]
    const existing = segments.findIndex((candidate) => candidate.id === item.id)
    if (existing >= 0) segments[existing] = segment
    else segments.push(segment)
    messages[index] = { ...active, segments }
  }
  return { ...conversation, implementationThreadId: sessionId ?? conversation.implementationThreadId, messages }
}

/** 先结束当前流式消息，再保留可重试的失败或取消终态。 */
export function finishImplementationTurn(conversation: WorkbenchConversation, status: 'completed' | 'failed' | 'cancelled', detail: string): WorkbenchConversation {
  const messages = [...conversation.messages]
  const index = messages.map((message) => message.phase === 'implement' && message.role === 'assistant' && message.status === 'streaming').lastIndexOf(true)
  if (index >= 0) messages[index] = {
    ...messages[index], status,
    segments: [...messages[index].segments, {
      id: randomUUID(), type: 'activity', label: status === 'completed' ? '执行完成' : status === 'cancelled' ? '执行已取消' : '执行失败',
      detail, status: status === 'failed' ? 'failed' : 'completed',
    }],
  }
  return { ...conversation, messages, status: status === 'completed' ? 'awaiting-user' : status, error: status === 'failed' ? detail : null }
}
