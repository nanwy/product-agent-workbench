import { execFile } from 'node:child_process'
import { chmod, cp, mkdir, readFile, rm } from 'node:fs/promises'
import os from 'node:os'
import path from 'node:path'
import { promisify } from 'node:util'

const execFileAsync = promisify(execFile)

// 全局准备把夹具复制到临时目录并初始化 Git，模拟一个完全独立的目标项目。
export default async function globalSetup() {
  await stopPreviousFixture()
  await rm(path.join(os.tmpdir(), 'product-agent-workbench-slow-turn'), { force: true })
  await rm(path.join(os.tmpdir(), 'product-agent-workbench-fail-turn'), { force: true })
  const source = path.resolve('tests/fixtures/target-project')
  const target = path.join(os.tmpdir(), 'product-agent-workbench-e2e-target')
  await rm(path.join(os.tmpdir(), 'product-agent-workbench-e2e-data'), { force: true, recursive: true })
  await rm(target, { force: true, recursive: true })
  await mkdir(path.dirname(target), { recursive: true })
  await cp(source, target, { recursive: true })
  await chmod(path.resolve('tests/fixtures/fake-codex.mjs'), 0o755)
  await chmod(path.resolve('tests/fixtures/fake-agent-browser.mjs'), 0o755)
  await execFileAsync('git', ['init', '-b', 'main'], { cwd: target })
  await execFileAsync('git', ['config', 'user.name', 'Workbench Fixture'], { cwd: target })
  await execFileAsync('git', ['config', 'user.email', 'fixture@example.test'], { cwd: target })
  await execFileAsync('git', ['add', '.'], { cwd: target })
  await execFileAsync('git', ['commit', '-m', 'fixture baseline'], { cwd: target })
}

async function stopPreviousFixture() {
  const pidFile = path.join(os.tmpdir(), 'product-agent-workbench-fixture-server.pid')
  try {
    const pid = Number(await readFile(pidFile, 'utf8'))
    if (Number.isInteger(pid) && pid > 1) process.kill(pid, 'SIGTERM')
  } catch (error) {
    if (!(error instanceof Error && 'code' in error && ['ENOENT', 'ESRCH'].includes(String(error.code)))) throw error
  }
  await rm(pidFile, { force: true })
}
