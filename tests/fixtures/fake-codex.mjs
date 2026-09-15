#!/usr/bin/env node
import { appendFile, rm, stat, writeFile } from 'node:fs/promises'
import os from 'node:os'
import path from 'node:path'

// 假 CLI 复刻工作台依赖的最小 JSONL 协议，并通过真实文件改动模拟 Agent 实现。
if (process.argv.includes('--version')) {
  console.log('codex-cli fixture-1.0.0')
  process.exit(0)
}

if (process.argv.includes('VALIDATE_EXEC_ARGS')) {
  if (process.argv.includes('-a')) {
    console.error("error: unexpected argument '-a' found")
    process.exit(2)
  }
  console.log(JSON.stringify({ type: 'turn.completed' }))
  process.exit(0)
}

const prompt = process.argv.at(-1) ?? ''
if (prompt.includes('$handoff')) {
  const outputPath = prompt.match(/必须写入这个精确文件：([^\n]+)/)?.[1]?.trim()
  if (!outputPath) process.exit(2)
  await writeFile(outputPath, '# Agent Session Handoff\n\n## suggested skills\n\n- implement\n')
  console.log(JSON.stringify({ type: 'turn.completed' }))
  process.exit(0)
}
const ticket = prompt.match(/当前执行票 (TICKET-\d+)/)?.[1] ?? 'TICKET-UNKNOWN'
const sessionId = process.argv.includes('resume')
  ? process.argv[process.argv.indexOf('--json') + 1]
  : '00000000-0000-4000-8000-000000000001'

console.log(JSON.stringify({ type: 'thread.started', thread_id: sessionId }))
const failMarker = path.join(os.tmpdir(), 'product-agent-workbench-fail-turn')
if (await stat(failMarker).then(() => true).catch(() => false)) {
  await rm(failMarker, { force: true })
  console.error(JSON.stringify({ type: 'turn.failed', error: { message: 'fixture refinement failure' } }))
  process.exit(2)
}
const slowMarker = path.join(os.tmpdir(), 'product-agent-workbench-slow-turn')
if (prompt.includes('SLOW_TURN') || await stat(slowMarker).then(() => true).catch(() => false)) {
  await rm(slowMarker, { force: true })
  await new Promise((resolve) => setTimeout(resolve, 5000))
}
console.log(JSON.stringify({ type: 'item.completed', item: { type: 'agent_message', text: `正在实现 ${ticket}` } }))
const targetFile = process.cwd().endsWith(path.join('apps', 'front'))
  ? path.join(process.cwd(), 'app/competition/page.tsx')
  : path.join(process.cwd(), 'apps/front/app/competition/page.tsx')
await appendFile(targetFile, `\n// implemented ${ticket}\n`)
console.log(JSON.stringify({ type: 'item.completed', item: { type: 'file_change', name: 'apps/front/app/competition/page.tsx' } }))
console.log(JSON.stringify({ type: 'turn.completed' }))
