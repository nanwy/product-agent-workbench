import { mkdir, mkdtemp, rm, writeFile } from 'node:fs/promises'
import os from 'node:os'
import path from 'node:path'

import { afterEach, describe, expect, it } from 'vitest'

import { loadTargetProjectConfig } from '../src/server/config'

const temporaryDirectories: string[] = []
const originalConfig = process.env.WORKBENCH_TARGET_CONFIG
const originalRoot = process.env.WORKBENCH_TARGET_ROOT

afterEach(async () => {
  if (originalConfig === undefined) delete process.env.WORKBENCH_TARGET_CONFIG
  else process.env.WORKBENCH_TARGET_CONFIG = originalConfig
  if (originalRoot === undefined) delete process.env.WORKBENCH_TARGET_ROOT
  else process.env.WORKBENCH_TARGET_ROOT = originalRoot
  await Promise.all(temporaryDirectories.splice(0).map((directory) => rm(directory, { force: true, recursive: true })))
})

// 配置测试证明高权限参数只能来自受信任文件，并且不能逃逸仓库或代理远程地址。
describe('target project config boundaries', () => {
  it('resolves a valid repository and accepts a narrowed local configuration', async () => {
    const configPath = await writeConfig()

    await expect(loadTargetProjectConfig()).resolves.toMatchObject({
      modificationScope: 'apps/front',
      healthUrl: 'http://127.0.0.1:43101/health',
    })
    expect(configPath).toBe(process.env.WORKBENCH_TARGET_CONFIG)
  })

  it('rejects a modification scope outside the configured repository', async () => {
    await writeConfig({ modificationScope: '../outside' })

    await expect(loadTargetProjectConfig()).rejects.toThrow('modificationScope 超出仓库根目录')
  })

  it('rejects a non-loopback preview endpoint', async () => {
    await writeConfig({ previewUrl: 'https://example.com' })

    await expect(loadTargetProjectConfig()).rejects.toThrow('目标项目 URL 必须监听本机')
  })
})

async function writeConfig(overrides: Record<string, unknown> = {}) {
  const root = await mkdtemp(path.join(os.tmpdir(), 'workbench-config-'))
  temporaryDirectories.push(root)
  await mkdir(path.join(root, 'repo/apps/front'), { recursive: true })
  const configPath = path.join(root, 'target.json')
  const config = {
    name: 'Config fixture',
    repositoryRoot: './repo',
    modificationScope: 'apps/front',
    startCommand: { command: 'node', args: ['server.mjs'] },
    healthUrl: 'http://127.0.0.1:43101/health',
    previewUrl: 'http://127.0.0.1:43101',
    previewGatewayPort: 43102,
    startupTimeoutMs: 1000,
    ...overrides,
  }
  await writeFile(configPath, JSON.stringify(config))
  process.env.WORKBENCH_TARGET_CONFIG = configPath
  delete process.env.WORKBENCH_TARGET_ROOT
  return configPath
}

