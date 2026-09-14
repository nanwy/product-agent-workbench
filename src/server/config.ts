import { readFile, realpath } from 'node:fs/promises'
import path from 'node:path'

import type { TargetProjectConfig } from '../domain/workbench'

type TargetProjectConfigFile = Omit<TargetProjectConfig, 'repositoryRoot'> & {
  repositoryRoot: string
}

export async function loadTargetProjectConfig(): Promise<TargetProjectConfig> {
  const configPath = path.resolve(
    process.env.WORKBENCH_TARGET_CONFIG ?? 'config/target-project.json',
  )
  const content = await readFile(configPath, 'utf8')
  const parsed = JSON.parse(content) as TargetProjectConfigFile
  const repositoryRoot = await realpath(
    process.env.WORKBENCH_TARGET_ROOT ??
      path.resolve(path.dirname(configPath), parsed.repositoryRoot),
  )

  validateConfig(parsed)

  return {
    ...parsed,
    repositoryRoot,
  }
}

function validateConfig(config: TargetProjectConfigFile) {
  if (!config.name.trim() || !config.modificationScope.trim()) {
    throw new Error('目标项目配置缺少 name 或 modificationScope')
  }

  if (!config.startCommand.command.trim()) {
    throw new Error('目标项目配置缺少 startCommand.command')
  }

  for (const value of [config.healthUrl, config.previewUrl]) {
    const url = new URL(value)
    if (!isLoopback(url.hostname)) {
      throw new Error(`目标项目 URL 必须监听本机：${value}`)
    }
  }

  if (
    !Number.isInteger(config.previewGatewayPort) ||
    config.previewGatewayPort < 1024 ||
    config.previewGatewayPort > 65535
  ) {
    throw new Error('previewGatewayPort 必须是 1024 到 65535 之间的端口')
  }
}

function isLoopback(hostname: string) {
  return hostname === 'localhost' || hostname === '127.0.0.1' || hostname === '::1'
}
