#!/usr/bin/env node

// E2E 只替换 agent-browser 的进程边界；输出仍保持真实 CLI 的语义树/console 证据形状。
const args = process.argv.slice(2)
const commandIndex = args.findIndex((value) => ['open', 'set', 'snapshot', 'errors', 'console', 'close'].includes(value))
const command = args[commandIndex]

if (command === 'snapshot') {
  console.log(JSON.stringify({ success: true, data: { snapshot: '@e1 [heading] "Fixture target"\n@e2 [paragraph] "/competition"' } }))
} else if (command === 'errors') {
  console.log(JSON.stringify({ success: true, data: { errors: [] } }))
} else if (command === 'console') {
  console.log(JSON.stringify({ success: true, data: { messages: [] } }))
} else {
  console.log(JSON.stringify({ success: true }))
}
