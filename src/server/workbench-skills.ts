import { access, realpath } from 'node:fs/promises'
import path from 'node:path'

/** 随工作台仓库发布的产品流程 Skill；目标仓库无需安装这些文件。 */
export const workbenchSkillNames = [
  'grilling',
  'to-spec',
  'to-tickets',
  'implement',
  'code-review',
  'tdd',
  'setup-matt-pocock-skills',
  'codebase-design',
] as const

export type WorkbenchSkillName = (typeof workbenchSkillNames)[number]

/**
 * 返回产品流程 Skill 的绝对入口路径。
 *
 * Codex turn 的 cwd 是目标仓库，所以不能把 `.agents/...` 当作目标仓库相对路径。
 * 默认以启动工作台时的 cwd 为产品根目录；部署器也可通过 WORKBENCH_ROOT 显式指定。
 */
export function resolveWorkbenchSkillPath(name: WorkbenchSkillName) {
  const workbenchRoot = path.resolve(process.env.WORKBENCH_ROOT ?? process.cwd())
  return path.join(workbenchRoot, '.agents', 'skills', name, 'SKILL.md')
}

/** 在创建目标分支前验证整套随仓库分发的 Skill，避免执行到一半才发现安装不完整。 */
export async function assertWorkbenchSkillBundle() {
  await Promise.all(workbenchSkillNames.map(async (name) => {
    const source = resolveWorkbenchSkillPath(name)
    try {
      await access(source)
      await realpath(source)
    } catch {
      throw new Error(`工作台 Skill 缺失：${source}。请确认克隆内容包含 .agents/skills。`)
    }
  }))
}
