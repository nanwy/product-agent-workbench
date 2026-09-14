import { spawn, type ChildProcess } from 'node:child_process'

import type {
  TargetProcessSnapshot,
  TargetProjectConfig,
} from '../domain/workbench'
import { startPreviewGateway, stopPreviewGateway } from './preview-gateway'

type RuntimeState = {
  child: ChildProcess | null
  snapshot: TargetProcessSnapshot
}

const emptySnapshot: TargetProcessSnapshot = {
  status: 'stopped',
  pid: null,
  command: '',
  healthUrl: '',
  previewUrl: '',
  gatewayUrl: '',
  startedAt: null,
  error: null,
  recentOutput: [],
}

const globalRuntime = globalThis as typeof globalThis & {
  __productAgentTargetRuntime?: RuntimeState
}

const runtime = (globalRuntime.__productAgentTargetRuntime ??= {
  child: null,
  snapshot: { ...emptySnapshot },
})

export function getTargetProcessSnapshot(config?: TargetProjectConfig) {
  if (!config) {
    return { ...emptySnapshot, status: 'unconfigured' as const }
  }

  return {
    ...runtime.snapshot,
    command: formatCommand(config),
    healthUrl: config.healthUrl,
    previewUrl: config.previewUrl,
    gatewayUrl: `http://127.0.0.1:${config.previewGatewayPort}`,
    recentOutput: [...runtime.snapshot.recentOutput],
  }
}

export async function startTargetProject(config: TargetProjectConfig) {
  if (runtime.child && ['starting', 'ready'].includes(runtime.snapshot.status)) {
    return getTargetProcessSnapshot(config)
  }

  runtime.snapshot = {
    ...getTargetProcessSnapshot(config),
    status: 'starting',
    pid: null,
    startedAt: new Date().toISOString(),
    error: null,
    recentOutput: [],
  }

  const child = spawn(config.startCommand.command, config.startCommand.args, {
    cwd: config.repositoryRoot,
    detached: process.platform !== 'win32',
    env: process.env,
    stdio: ['ignore', 'pipe', 'pipe'],
  })
  runtime.child = child
  runtime.snapshot.pid = child.pid ?? null

  child.stdout?.on('data', (chunk: Buffer) => appendOutput(chunk.toString()))
  child.stderr?.on('data', (chunk: Buffer) => appendOutput(chunk.toString()))
  child.once('exit', (code, signal) => {
    if (runtime.child !== child) return
    runtime.child = null
    if (runtime.snapshot.status !== 'stopped') {
      runtime.snapshot.status = 'failed'
      runtime.snapshot.error = `目标项目进程退出（code=${code ?? 'null'}, signal=${signal ?? 'null'}）`
    }
    runtime.snapshot.pid = null
    void stopPreviewGateway()
  })
  child.once('error', (error) => {
    runtime.snapshot.status = 'failed'
    runtime.snapshot.error = redact(error.message)
  })

  try {
    await waitUntilHealthy(config, child)
    await startPreviewGateway(config.previewUrl, config.previewGatewayPort)
    runtime.snapshot.status = 'ready'
    return getTargetProcessSnapshot(config)
  } catch (error) {
    const failure = redact(
      error instanceof Error ? error.message : String(error),
    )
    await stopTargetProject(config)
    runtime.snapshot.status = 'failed'
    runtime.snapshot.error = failure
    return getTargetProcessSnapshot(config)
  }
}

export async function stopTargetProject(config: TargetProjectConfig) {
  const child = runtime.child
  runtime.child = null
  await stopPreviewGateway()

  if (child?.pid && child.exitCode === null) {
    try {
      if (process.platform === 'win32') child.kill('SIGTERM')
      else process.kill(-child.pid, 'SIGTERM')
    } catch (error) {
      if (!isMissingProcess(error)) throw error
    }

    await Promise.race([
      child.exitCode === null
        ? new Promise<void>((resolve) => child.once('exit', () => resolve()))
        : Promise.resolve(),
      new Promise<void>((resolve) => setTimeout(resolve, 3000)),
    ])

    if (child.exitCode === null) {
      try {
        if (process.platform === 'win32') child.kill('SIGKILL')
        else process.kill(-child.pid, 'SIGKILL')
      } catch (error) {
        if (!isMissingProcess(error)) throw error
      }
    }
  }

  runtime.snapshot = {
    ...getTargetProcessSnapshot(config),
    status: 'stopped',
    pid: null,
    startedAt: null,
    error: null,
  }
  return getTargetProcessSnapshot(config)
}

async function waitUntilHealthy(
  config: TargetProjectConfig,
  child: ChildProcess,
) {
  const deadline = Date.now() + config.startupTimeoutMs
  let lastError = '尚未收到健康响应'

  while (Date.now() < deadline) {
    if (child.exitCode !== null) {
      throw new Error(`目标项目在就绪前退出：${lastError}`)
    }

    try {
      const response = await fetch(config.healthUrl, {
        signal: AbortSignal.timeout(1500),
      })
      if (response.ok) return
      lastError = `健康检查返回 ${response.status}`
    } catch (error) {
      lastError = error instanceof Error ? error.message : String(error)
    }
    await new Promise((resolve) => setTimeout(resolve, 250))
  }

  throw new Error(`目标项目启动超时：${lastError}`)
}

function appendOutput(output: string) {
  const lines = output
    .split(/\r?\n/)
    .filter(Boolean)
    .map(redact)
  runtime.snapshot.recentOutput = [...runtime.snapshot.recentOutput, ...lines].slice(
    -30,
  )
}

function redact(value: string) {
  return value
    .replace(
      /(token|password|secret|authorization)(\s*[:=]\s*)([^\s,;]+)/gi,
      '$1$2[REDACTED]',
    )
    .replace(/Bearer\s+[A-Za-z0-9._~+/-]+=*/gi, 'Bearer [REDACTED]')
}

function formatCommand(config: TargetProjectConfig) {
  return [config.startCommand.command, ...config.startCommand.args].join(' ')
}

function isMissingProcess(error: unknown): error is NodeJS.ErrnoException {
  return error instanceof Error && 'code' in error && error.code === 'ESRCH'
}
