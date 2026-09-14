import { readFile, realpath } from 'node:fs/promises'
import path from 'node:path'

import type { TargetProjectConfig } from '../domain/workbench'

type TargetProjectConfigFile = Omit<TargetProjectConfig, 'repositoryRoot'> & {
  repositoryRoot: string
}

// 目标项目是受控配置，不从产品输入或终端输出猜测仓库、命令和端口。
// 测试可用环境变量指向一次性 fixture，生产默认读取 config/target-project.json。
export async function loadTargetProjectConfig(): Promise<TargetProjectConfig> {
  const configPath = path.resolve(
    process.env.WORKBENCH_TARGET_CONFIG ?? 'config/target-project.json',
  )
  const content = await readFile(configPath, 'utf8')
  const parsed = JSON.parse(content) as TargetProjectConfigFile
  // realpath 消除 .. 和符号链接差异，后续所有路径边界判断都基于这个规范根目录。
  const repositoryRoot = await realpath(
    process.env.WORKBENCH_TARGET_ROOT ??
      path.resolve(path.dirname(configPath), parsed.repositoryRoot),
  )

  validateConfig(parsed)
  const scopeRoot = path.resolve(repositoryRoot, parsed.modificationScope)
  if (scopeRoot !== repositoryRoot && !scopeRoot.startsWith(`${repositoryRoot}${path.sep}`)) {
    throw new Error('目标项目 modificationScope 超出仓库根目录')
  }

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

  for (const validation of config.validationCommands ?? []) {
    if (!validation.id.trim() || !validation.label.trim() || !validation.command.trim()) {
      throw new Error('验证命令缺少 id、label 或 command')
    }
    if (!Number.isInteger(validation.timeoutMs) || validation.timeoutMs < 1000) {
      throw new Error(`验证命令 ${validation.id} 的 timeoutMs 无效`)
    }
  }

  // 首版明确是本地工作台，禁止把预览网关变成任意远程代理。
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
