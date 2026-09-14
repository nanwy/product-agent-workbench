#!/usr/bin/env node
import { appendFile } from 'node:fs/promises'
import path from 'node:path'

if (process.argv.includes('--version')) {
  console.log('codex-cli fixture-1.0.0')
  process.exit(0)
}

const prompt = process.argv.at(-1) ?? ''
const ticket = prompt.match(/当前执行票 (TICKET-\d+)/)?.[1] ?? 'TICKET-UNKNOWN'
const sessionId = process.argv.includes('resume')
  ? process.argv[process.argv.indexOf('--json') + 1]
  : '00000000-0000-4000-8000-000000000001'

console.log(JSON.stringify({ type: 'thread.started', thread_id: sessionId }))
if (prompt.includes('SLOW_TURN')) await new Promise((resolve) => setTimeout(resolve, 5000))
console.log(JSON.stringify({ type: 'item.completed', item: { type: 'agent_message', text: `正在实现 ${ticket}` } }))
await appendFile(path.join(process.cwd(), 'apps/front/app/competition/page.tsx'), `\n// implemented ${ticket}\n`)
console.log(JSON.stringify({ type: 'item.completed', item: { type: 'file_change', name: 'apps/front/app/competition/page.tsx' } }))
console.log(JSON.stringify({ type: 'turn.completed' }))
