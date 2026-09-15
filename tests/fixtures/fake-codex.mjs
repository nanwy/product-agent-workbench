#!/usr/bin/env node
import { spawn } from 'node:child_process'
import { appendFile, rm, stat, writeFile } from 'node:fs/promises'
import os from 'node:os'
import path from 'node:path'
import readline from 'node:readline'
import { fileURLToPath } from 'node:url'

// 假 SDK 底层可执行文件复刻最小事件协议，并通过真实文件改动模拟 Agent 实现。
// grilling 的 updated -> completed 累计正文是工作台的兼容性场景，不代表 codex exec 0.154
// 会按 token 推送 assistant 文本；真实 exec 当前通常只给 item.completed 完整正文。
if (process.argv.includes('--version')) {
  console.log('codex-cli fixture-1.0.0')
  process.exit(0)
}

if (process.argv.includes('app-server')) {
  await runAppServer()
  process.exit(0)
}

const prompt = await new Promise((resolve) => {
  let input = ''
  process.stdin.setEncoding('utf8')
  process.stdin.on('data', (chunk) => { input += chunk })
  process.stdin.on('end', () => resolve(input))
})

if (prompt.includes('VALIDATE_EXEC_ARGS')) {
  if (process.argv.includes('-a')) {
    console.error("error: unexpected argument '-a' found")
    process.exit(2)
  }
  console.log(JSON.stringify({ type: 'thread.started', thread_id: '00000000-0000-4000-8000-000000000001' }))
  console.log(JSON.stringify({ type: 'item.completed', item: { id: 'sandbox', type: 'agent_message', text: process.argv[process.argv.indexOf('--sandbox') + 1] } }))
  console.log(JSON.stringify({ type: 'turn.completed', usage: { input_tokens: 1, cached_input_tokens: 0, output_tokens: 1, reasoning_output_tokens: 0 } }))
  process.exit(0)
}

if (prompt.includes('$handoff')) {
  const outputPath = prompt.match(/必须写入这个精确文件：([^\n]+)/)?.[1]?.trim()
  if (!outputPath) process.exit(2)
  await writeFile(outputPath, '# Agent Session Handoff\n\n## suggested skills\n\n- implement\n')
  console.log(JSON.stringify({ type: 'turn.completed', usage: { input_tokens: 1, cached_input_tokens: 0, output_tokens: 1, reasoning_output_tokens: 0 } }))
  process.exit(0)
}
const fixtureSessionId = process.argv.includes('resume')
  ? process.argv[process.argv.indexOf('resume') + 1]
  : '00000000-0000-4000-8000-000000000001'
if (prompt.includes('Product Agent Workbench 的 grilling 阶段')) {
  console.log(JSON.stringify({ type: 'thread.started', thread_id: fixtureSessionId }))
  console.log(JSON.stringify({ type: 'item.updated', item: { id: 'grilling-message', type: 'agent_message', text: '❓ **Q1** - **成功结果**：用户最终应该观察到什么？' } }))
  console.log(JSON.stringify({ type: 'item.completed', item: { id: 'grilling-message', type: 'agent_message', text: '❓ **Q1** - **成功结果**：用户最终应该观察到什么？\n\n➡️ 使用需求中的初始期望作为成功结果。' } }))
  console.log(JSON.stringify({ type: 'turn.completed', usage: { input_tokens: 1, cached_input_tokens: 0, output_tokens: 1, reasoning_output_tokens: 0 } }))
  process.exit(0)
}
if (prompt.includes('用户对上一轮 frontier 的回答') || prompt.includes('继续刚才中断的 Grilling')) {
  if (!process.argv.includes('resume')) {
    console.log(JSON.stringify({ type: 'turn.failed', error: { message: 'grilling reply did not resume its planning thread' } }))
    process.exit(2)
  }
  const slowGrillingMarker = path.join(os.tmpdir(), 'product-agent-workbench-slow-grilling')
  if (await stat(slowGrillingMarker).then(() => true).catch(() => false)) {
    await rm(slowGrillingMarker, { force: true })
    await new Promise((resolve) => setTimeout(resolve, 5000))
  }
  console.log(JSON.stringify({ type: 'thread.started', thread_id: fixtureSessionId }))
  console.log(JSON.stringify({ type: 'item.completed', item: { id: 'grilling-summary', type: 'agent_message', text: '共同理解：按已确认结果，在配置范围内完成修改，并通过真实预览验收。\n[WORKBENCH:UNDERSTANDING]{"summary":"按已确认结果在配置范围内修改","desiredOutcome":"用户应看到更明确的空状态文案","changeBoundary":"只修改 /competition，不修改其他页面","acceptanceSignal":"真实预览可见且无新增控制台错误"}[/WORKBENCH:UNDERSTANDING]\n[WORKBENCH:READY_FOR_CONFIRMATION]' } }))
  console.log(JSON.stringify({ type: 'turn.completed', usage: { input_tokens: 1, cached_input_tokens: 0, output_tokens: 1, reasoning_output_tokens: 0 } }))
  process.exit(0)
}
if (prompt.includes('Product Agent Workbench 的 to-spec 阶段')) {
  console.log(JSON.stringify({ type: 'thread.started', thread_id: fixtureSessionId }))
  const failSpecMarker = path.join(os.tmpdir(), 'product-agent-workbench-fail-spec')
  if (await stat(failSpecMarker).then(() => true).catch(() => false)) {
    await rm(failSpecMarker, { force: true })
    console.log(JSON.stringify({ type: 'turn.failed', error: { message: 'fixture Spec failure' } }))
    process.exit(2)
  }
  const slowSpecMarker = path.join(os.tmpdir(), 'product-agent-workbench-slow-spec')
  if (await stat(slowSpecMarker).then(() => true).catch(() => false)) {
    await rm(slowSpecMarker, { force: true })
    await new Promise((resolve) => setTimeout(resolve, 5000))
  }
  const revised = prompt.includes('用户对当前 Spec 的修订要求')
  const document = {
    problem: '当前空状态没有说明用户下一步可以做什么',
    solution: revised ? '使用更具体的空状态文案，并补充下一步引导' : '使用更明确的空状态文案',
    userStories: ['作为赛事页用户，我能理解当前状态和下一步操作'],
    implementationDecisions: ['保持现有页面结构，只调整配置范围内的展示'],
    testingDecisions: ['在真实预览中验证文案可见且没有新增控制台错误'],
    outOfScope: ['不修改认证、数据库或远程接口'],
    furtherNotes: ['Spec 仅保存在工作台本地数据目录'],
  }
  if (prompt.includes('修改已确认的共同理解')) document.understanding = {
    summary: '范围不变，验收信号已更新',
    desiredOutcome: '用户看到明确文案和下一步引导',
    changeBoundary: '只修改 /competition，不修改其他页面',
    acceptanceSignal: '真实预览可见、无新增控制台错误且引导可理解',
  }
  console.log(JSON.stringify({ type: 'item.completed', item: { id: 'spec-message', type: 'agent_message', text: `Spec 已按共同理解整理。\n[WORKBENCH:SPEC]${JSON.stringify(document)}[/WORKBENCH:SPEC]` } }))
  console.log(JSON.stringify({ type: 'turn.completed', usage: { input_tokens: 1, cached_input_tokens: 0, output_tokens: 1, reasoning_output_tokens: 0 } }))
  process.exit(0)
}
const ticket = prompt.match(/当前执行票 (TICKET-\d+)/)?.[1] ?? 'TICKET-UNKNOWN'
const sessionId = fixtureSessionId

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
console.log(JSON.stringify({ type: 'item.completed', item: { id: 'agent-message', type: 'agent_message', text: `正在实现 ${ticket}` } }))
const workingDirectory = process.argv.includes('--cd') ? process.argv[process.argv.indexOf('--cd') + 1] : process.cwd()
const targetFile = workingDirectory.endsWith(path.join('apps', 'front'))
  ? path.join(workingDirectory, 'app/competition/page.tsx')
  : path.join(workingDirectory, 'apps/front/app/competition/page.tsx')
await appendFile(targetFile, `\n// implemented ${ticket}\n`)
console.log(JSON.stringify({ type: 'item.completed', item: { id: 'file-change', type: 'file_change', changes: [{ path: 'apps/front/app/competition/page.tsx', kind: 'update' }], status: 'completed' } }))
console.log(JSON.stringify({ type: 'turn.completed', usage: { input_tokens: 1, cached_input_tokens: 0, output_tokens: 1, reasoning_output_tokens: 0 } }))

async function runAppServer() {
  const lines = readline.createInterface({ input: process.stdin, crlfDelay: Infinity })
  const mode = process.env.WORKBENCH_FAKE_APP_SERVER_MODE ?? 'success'
  let threadId = null
  let turnId = null
  let runningChild = null
  let resumed = false

  const send = (value) => process.stdout.write(`${JSON.stringify(value)}\n`)
  const respond = (id, result) => send({ jsonrpc: '2.0', id, result })
  const notify = (method, params) => send({ jsonrpc: '2.0', method, params })

  for await (const line of lines) {
    if (!line.trim()) continue
    const request = JSON.parse(line)
    if (request.method === 'initialize') {
      if (mode === 'no-response') continue
      if (mode === 'initialize-failure') {
        send({ jsonrpc: '2.0', id: request.id, error: { code: -32000, message: 'fixture initialize failure' } })
      } else {
        respond(request.id, { userAgent: 'fake-codex/1.0' })
      }
      continue
    }
    if (request.method === 'initialized') continue
    if (request.method === 'thread/start') {
      threadId = '00000000-0000-4000-8000-000000000001'
      resumed = false
      respond(request.id, { thread: { id: threadId } })
      continue
    }
    if (request.method === 'thread/resume') {
      threadId = request.params.threadId
      resumed = true
      respond(request.id, { thread: { id: threadId } })
      continue
    }
    if (request.method === 'turn/start') {
      turnId = crypto.randomUUID()
      respond(request.id, { turn: { id: turnId } })
      notify('turn/started', { threadId, turn: { id: turnId, status: 'inProgress' } })
      if (mode === 'process-exit') process.exit(23)
      if (mode === 'invalid-json') {
        process.stdout.write('this is not json-rpc\n')
        continue
      }
      if (mode === 'turn-failure') {
        notify('turn/completed', {
          threadId,
          turn: { id: turnId, status: 'failed', error: { message: 'fixture turn failure' } },
        })
        continue
      }
      const prompt = request.params.input?.[0]?.text ?? ''
      if (prompt.includes('VALIDATE_APP_SERVER') || prompt.includes('SLOW_TURN') || prompt === 'continue') {
        const sandbox = request.params.sandboxPolicy?.type === 'readOnly' ? 'read-only' : 'workspace-write'
        notify('item/completed', {
          threadId,
          turnId,
          item: {
            id: 'message-0001',
            type: 'agentMessage',
            text: prompt.includes('SLOW_TURN') ? 'partial before cancel' : `${sandbox}:${prompt}`,
            phase: 'final_answer',
          },
        })
        if (!prompt.includes('SLOW_TURN')) {
          notify('turn/completed', { threadId, turn: { id: turnId, status: 'completed' } })
        }
        continue
      }
      runningChild = runExecFixture(request.params, threadId, turnId, resumed, notify)
      continue
    }
    if (request.method === 'turn/interrupt') {
      if (mode === 'ignore-interrupt') continue
      respond(request.id, {})
      runningChild?.kill('SIGTERM')
      runningChild = null
      // Cancellation may beat the nested exec fixture to its one-shot delay
      // marker. Consume it here so the retry represents a recovered turn.
      await Promise.all([
        'product-agent-workbench-slow-turn',
        'product-agent-workbench-slow-grilling',
        'product-agent-workbench-slow-spec',
      ].map((name) => rm(path.join(os.tmpdir(), name), { force: true })))
      notify('turn/completed', { threadId, turn: { id: turnId, status: 'cancelled' } })
      continue
    }
    if (request.id !== undefined) respond(request.id, {})
  }

  runningChild?.kill('SIGTERM')
}

function runExecFixture(params, threadId, turnId, resumed, notify) {
  const sandbox = params.sandboxPolicy?.type === 'readOnly' ? 'read-only' : 'workspace-write'
  const args = [
    fileURLToPath(import.meta.url),
    'exec',
    '--json',
    '--sandbox', sandbox,
    '--cd', params.cwd,
  ]
  if (resumed) args.push('resume', threadId)
  args.push('-')
  const child = spawn(process.execPath, args, { cwd: params.cwd, env: process.env, stdio: ['pipe', 'pipe', 'pipe'] })
  let completed = false
  child.stdin.end(params.input?.[0]?.text ?? '')
  const output = readline.createInterface({ input: child.stdout, crlfDelay: Infinity })
  output.on('line', (line) => {
    if (!line.trim()) return
    const event = JSON.parse(line)
    if (event.type === 'thread.started') return
    if (event.type === 'turn.completed') {
      completed = true
      notify('turn/completed', { threadId, turn: { id: turnId, status: 'completed', usage: event.usage } })
      return
    }
    if (event.type === 'turn.failed' || event.type === 'error') {
      completed = true
      notify('turn/completed', {
        threadId,
        turn: { id: turnId, status: 'failed', error: event.error ?? { message: event.message ?? 'fixture turn failed' } },
      })
      return
    }
    if (event.type.startsWith('item.')) {
      notify(event.type.replace('.', '/'), { threadId, turnId, item: toAppServerItem(event.item) })
    }
  })
  child.stderr.on('data', (chunk) => process.stderr.write(chunk))
  child.on('error', (error) => {
    completed = true
    notify('turn/completed', {
      threadId,
      turn: { id: turnId, status: 'failed', error: { message: error.message } },
    })
  })
  child.on('close', (code, signal) => {
    if (!completed && code !== 0 && signal === null) {
      completed = true
      notify('turn/completed', {
        threadId,
        turn: { id: turnId, status: 'failed', error: { message: `fixture exec exited with ${code}` } },
      })
    }
  })
  return child
}

function toAppServerItem(item) {
  if (!item) return item
  const type = item.type === 'agent_message'
    ? 'agentMessage'
    : item.type === 'command_execution'
      ? 'commandExecution'
      : item.type === 'file_change'
        ? 'fileChange'
        : item.type
  return {
    ...item,
    type,
    aggregatedOutput: item.aggregated_output,
    exitCode: item.exit_code,
  }
}
