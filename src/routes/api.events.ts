import { createFileRoute } from '@tanstack/react-router'

import { subscribeAgentEvents } from '../server/agent-events'

export const Route = createFileRoute('/api/events')({
  server: {
    handlers: {
      GET: () => {
        const encoder = new TextEncoder()
        let unsubscribe: () => void = () => undefined
        const stream = new ReadableStream({
          start(controller) {
            controller.enqueue(encoder.encode(': connected\n\n'))
            unsubscribe = subscribeAgentEvents((event) => {
              controller.enqueue(encoder.encode(`data: ${JSON.stringify(event)}\n\n`))
            })
          },
          cancel() {
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
