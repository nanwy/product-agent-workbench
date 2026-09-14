import { readFile, rm } from 'node:fs/promises'
import os from 'node:os'
import path from 'node:path'

// 无论 E2E 成功或失败，都回收夹具进程和跨用例的慢执行标记。
export default async function globalTeardown() {
  await rm(path.join(os.tmpdir(), 'product-agent-workbench-slow-turn'), { force: true })
  const pidFile = path.join(os.tmpdir(), 'product-agent-workbench-fixture-server.pid')
  try {
    const pid = Number(await readFile(pidFile, 'utf8'))
    if (Number.isInteger(pid) && pid > 1) process.kill(pid, 'SIGTERM')
  } catch (error) {
    if (!(error instanceof Error && 'code' in error && ['ENOENT', 'ESRCH'].includes(String(error.code)))) throw error
  }
  await rm(pidFile, { force: true })
}
