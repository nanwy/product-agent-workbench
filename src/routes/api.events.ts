import { createFileRoute } from '@tanstack/react-router'

import { subscribeAgentEvents } from '../server/agent-events'

// SSE 只承载 Codex 进度通知，不承载状态变更命令。
// 浏览器收到事件后会重新拉取 Requirement 快照，因此断线重连不会重放副作用。
export const Route = createFileRoute('/api/events')({
  server: {
    handlers: {
      GET: () => {
        const encoder = new TextEncoder()
        let unsubscribe: () => void = () => undefined
        const stream = new ReadableStream({
          start(controller) {
            // 先发送 SSE 注释行，让客户立即确认连接已建立。
            controller.enqueue(encoder.encode(': connected\n\n'))
            unsubscribe = subscribeAgentEvents((event) => {
              controller.enqueue(encoder.encode(`data: ${JSON.stringify(event)}\n\n`))
            })
          },
          cancel() {
            // 浏览器关闭 EventSource 时解除订阅，避免热更后累积失效回调。
            unsubscribe()
          },
        })
        return new Response(stream, {
          headers: {
            'Cache-Control': 'no-cache, no-transform',
            Connection: 'keep-alive',
            'Content-Type': 'text/event-stream',
          },
        })
      },
    },
  },
})
