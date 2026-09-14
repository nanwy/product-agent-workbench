import { spawn } from 'node:child_process'

import type { TargetProjectConfig, ValidationCommand, ValidationRecord } from '../domain/workbench'
import { loadTargetProjectConfig } from './config'
import { changedFilesSince, filesOutsideScope } from './git-workspace'
import { requirementStore } from './requirement-store'
import { getTargetProcessSnapshot } from './target-runtime'

export async function verifyCurrentRequirement() {
  const requirement = await requirementStore.load()
  if (!requirement?.branch) throw new Error('验证前必须完成需求分支上的候选实现')
  if (!requirement.tickets.length || requirement.tickets.some((ticket) => ticket.status !== 'completed')) {
    throw new Error('所有执行票完成后才能进入验证')
  }

  await requirementStore.update((current) => {
    if (!current) throw new Error('产品需求已不存在')
    return { ...current, stage: 'verification', validation: [], updatedAt: new Date().toISOString() }
  })

  const target = await loadTargetProjectConfig()
  const scopedTarget = { ...target, modificationScope: requirement.spec?.proposedScope ?? target.modificationScope }
  const records: ValidationRecord[] = []
  for (const command of target.validationCommands ?? []) records.push(await runValidationCommand(command, target))
  records.push(await validateScope(requirement.branch.baseRevision, scopedTarget))
  records.push(await validateRuntime(requirement.targetPage, target))
  records.push(record({
    id: 'product-acceptance',
    criterion: '产品在真实预览中确认候选实现符合意图',
    result: 'not-run',
    source: 'product',
    detail: '等待产品显式验收；产品验收不等于生产批准',
    notRunReason: '尚未点击产品验收',
  }))

  const blocking = records.some((item) => item.result === 'failed')
  return requirementStore.update((current) => {
    if (!current) throw new Error('产品需求已不存在')
    const now = new Date().toISOString()
    return {
      ...current,
      stage: blocking ? 'verification' : 'awaiting-product-acceptance',
      validation: records,
      updatedAt: now,
      evidence: [...current.evidence, {
        stage: blocking ? 'verification' : 'awaiting-product-acceptance',
        input: `${records.length} 项验证`,
        action: '在工作台宿主运行确定性检查并记录证据',
        output: blocking ? '存在失败项，已阻止产品验收' : '自动验证已完成，等待产品验收',
        recordedAt: now,
      }],
    }
  })
}

export async function runValidationCommand(command: ValidationCommand, target: TargetProjectConfig): Promise<ValidationRecord> {
  const startedAt = new Date().toISOString()
  const displayedCommand = [command.command, ...command.args].join(' ')
  const result = await run(command, target.repositoryRoot)
  return {
    id: command.id,
    criterion: command.label,
    result: result.exitCode === 0 ? 'verified' : 'failed',
    source: 'command',
    detail: result.output || (result.exitCode === 0 ? '命令执行成功' : `命令退出码 ${result.exitCode}`),
    command: displayedCommand,
    exitCode: result.exitCode,
    startedAt,
    completedAt: new Date().toISOString(),
    notRunReason: null,
  }
}

async function validateScope(baseRevision: string, target: TargetProjectConfig): Promise<ValidationRecord> {
  const files = await changedFilesSince(baseRevision, target)
  const outside = filesOutsideScope(files, target)
  return record({
    id: 'git-scope',
    criterion: `所有改动位于确认范围 ${target.modificationScope}`,
    result: outside.length ? 'failed' : 'verified',
    source: 'git-scope',
    detail: outside.length ? `发现范围外改动：${outside.join(', ')}` : `已检查 ${files.length} 个改动文件`,
  })
}

async function validateRuntime(targetPage: string, target: TargetProjectConfig): Promise<ValidationRecord> {
  const snapshot = getTargetProcessSnapshot(target)
  if (snapshot.status !== 'ready') return record({
    id: 'runtime-preview', criterion: `目标页面 ${targetPage} 可访问`, result: 'not-run', source: 'runtime',
    detail: '目标项目尚未就绪，可启动后重跑验证', notRunReason: '目标运行时未就绪',
  })
  try {
    const response = await fetch(new URL(targetPage, target.previewUrl), { signal: AbortSignal.timeout(5000) })
    return record({
      id: 'runtime-preview', criterion: `目标页面 ${targetPage} 可访问`, result: response.ok ? 'verified' : 'failed', source: 'runtime',
      detail: `HTTP ${response.status} ${response.statusText}`,
    })
  } catch (error) {
    return record({
      id: 'runtime-preview', criterion: `目标页面 ${targetPage} 可访问`, result: 'not-run', source: 'runtime',
      detail: error instanceof Error ? error.message : String(error), notRunReason: '宿主无法访问目标页面',
    })
  }
}

function record(input: Pick<ValidationRecord, 'id' | 'criterion' | 'result' | 'source' | 'detail'> & Partial<ValidationRecord>): ValidationRecord {
  const now = new Date().toISOString()
  return { command: null, exitCode: null, startedAt: now, completedAt: now, notRunReason: null, ...input }
}

function run(command: ValidationCommand, cwd: string): Promise<{ exitCode: number; output: string }> {
  return new Promise((resolve) => {
    const child = spawn(command.command, command.args, { cwd, detached: process.platform !== 'win32', env: process.env, stdio: ['ignore', 'pipe', 'pipe'] })
    let output = ''
    const append = (chunk: Buffer) => { output = `${output}${chunk.toString()}`.slice(-4000) }
    child.stdout?.on('data', append)
    child.stderr?.on('data', append)
    let timedOut = false
    const timer = setTimeout(() => {
      timedOut = true
      if (!child.pid) return
      try {
        if (process.platform === 'win32') child.kill('SIGTERM')
        else process.kill(-child.pid, 'SIGTERM')
      } catch (error) {
        if (!(error instanceof Error && 'code' in error && error.code === 'ESRCH')) throw error
      }
    }, command.timeoutMs)
    child.once('error', (error) => { clearTimeout(timer); resolve({ exitCode: 1, output: redact(error.message) }) })
    child.once('close', (code) => {
      clearTimeout(timer)
      resolve({ exitCode: timedOut ? 124 : code ?? 1, output: redact(timedOut ? `验证超时（${command.timeoutMs}ms）\n${output}` : output.trim()) })
    })
  })
}

function redact(value: string) {
  return value.replace(/(token|password|secret|authorization)(\s*[:=]\s*)([^\s,;]+)/gi, '$1$2[REDACTED]').replace(/Bearer\s+[A-Za-z0-9._~+/-]+=*/gi, 'Bearer [REDACTED]')
}
