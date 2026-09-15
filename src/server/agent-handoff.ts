import { access, mkdtemp, readFile, rm } from 'node:fs/promises'
import os from 'node:os'
import path from 'node:path'

import type { AgentSessionHandoff, Requirement } from '../domain/workbench'
import { runCodexTurn } from './codex-adapter'
import { resolveWorkbenchSkillPath } from './workbench-skills'

/**
 * 调用仓库内 `$handoff` Skill 生成 Agent 会话转接文件。
 *
 * 该 Codex turn 使用独立、临时、非持久 session，唯一可写根是系统临时目录；它不会
 * 恢复候选实现 session，也没有目标仓库写权限，因此文件不可能混入业务 commit。
 */
export async function createAgentSessionHandoff(requirement: Requirement, focus: string): Promise<AgentSessionHandoff> {
  if (!requirement.codex.sessionId) throw new Error('尚无可转接的 Codex session')
  const skillPath = resolveWorkbenchSkillPath('handoff')
  await access(skillPath)
  const temporaryRoot = await mkdtemp(path.join(os.tmpdir(), 'product-agent-handoff-'))
  const outputPath = path.join(temporaryRoot, 'agent-session-handoff.md')
  const prompt = [
    '$handoff',
    `请完整读取并严格执行 Skill：${skillPath}`,
    `本次转接用途：${focus}`,
    `当前 Requirement：${requirement.id}`,
    `当前 Spec：${requirement.specId}`,
    `目标页面：${requirement.targetPage}`,
    `需求阶段：${requirement.stage}`,
    `候选实现 Session：${requirement.codex.sessionId}`,
    `需求分支：${requirement.branch?.requirementBranch ?? '尚未创建'}`,
    `目标仓库中的交付文件：${requirement.delivery?.artifactPaths.join(', ') || '尚未生成'}`,
    `必须写入这个精确文件：${outputPath}`,
    '不要修改其他文件；不要复制已有 Spec/Ticket/Handoff 正文，只引用相对路径；包含 suggested skills 小节并脱敏。',
  ].join('\n\n')
  try {
    const result = await runCodexTurn({
      workspaceRoot: temporaryRoot,
      prompt,
      sessionId: null,
      skipGitRepositoryCheck: true,
      ephemeral: true,
      onEvent: async () => undefined,
    })
    if (result.exitCode !== 0) throw new Error(`$handoff Skill 执行失败，退出码 ${result.exitCode}`)
    const content = await readFile(outputPath, 'utf8')
    if (!content.trim()) throw new Error('$handoff Skill 没有生成有效转接内容')
    // 新文件确认可读后再清理旧临时文件，失败时仍保留上一次可用转接。
    await cleanupPreviousHandoff(requirement.agentSessionHandoff?.path)
    return { path: outputPath, focus, createdAt: new Date().toISOString(), sourceSkill: skillPath }
  } catch (error) {
    await rm(temporaryRoot, { recursive: true, force: true })
    throw error
  }
}

async function cleanupPreviousHandoff(previousPath: string | undefined) {
  if (!previousPath) return
  const temporaryRoot = path.resolve(os.tmpdir())
  const previousDirectory = path.dirname(path.resolve(previousPath))
  // 只删除本模块自己创建的单个前缀目录，绝不对任意路径做递归清理。
  if (path.dirname(previousDirectory) !== temporaryRoot || !path.basename(previousDirectory).startsWith('product-agent-handoff-')) return
  await rm(previousDirectory, { recursive: true, force: true })
}
