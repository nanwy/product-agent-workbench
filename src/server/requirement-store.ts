import { randomUUID } from 'node:crypto'
import { mkdir, readFile, rename, writeFile } from 'node:fs/promises'
import path from 'node:path'

import type { DiscoveredSkill, Requirement, WorkflowStep } from '../domain/workbench'

export class RequirementStore {
  readonly #filePath: string
  #queue: Promise<unknown> = Promise.resolve()

  constructor(dataDirectory = process.env.WORKBENCH_DATA_DIR ?? '.data') {
    this.#filePath = path.resolve(dataDirectory, 'current-requirement.json')
  }

  async load(): Promise<Requirement | null> {
    // 等待先前的写入队列，防止读到 rename 前的旧快照。
    await this.#queue.catch(() => undefined)
    return this.#loadDirect()
  }

  save(requirement: Requirement): Promise<Requirement> {
    return this.#enqueue(async () => {
      await this.#writeDirect(requirement)
      return requirement
    })
  }

  update(mutator: (current: Requirement | null) => Requirement | Promise<Requirement>): Promise<Requirement> {
    // 把“读当前值 → 计算新值 → 原子写入”放进同一队列，避免 SSE 事件和按钮请求互相覆盖。
    return this.#enqueue(async () => {
      const next = await mutator(await this.#loadDirect())
      await this.#writeDirect(next)
      return next
    })
  }

  #enqueue<T>(operation: () => Promise<T>): Promise<T> {
    const result = this.#queue.then(operation, operation)
    this.#queue = result.then(() => undefined, () => undefined)
    return result
  }

  async #loadDirect(): Promise<Requirement | null> {
    try {
      return await migrateRequirement(JSON.parse(await readFile(this.#filePath, 'utf8')) as Requirement)
    } catch (error) {
      if (isMissingFile(error)) return null
      throw error
    }
  }

  async #writeDirect(requirement: Requirement) {
    await mkdir(path.dirname(this.#filePath), { recursive: true })
    const temporaryPath = `${this.#filePath}.${process.pid}.${randomUUID()}.tmp`
    // 0600 限制为当前用户可读写；同目录 rename 确保页面刷新时只会看到旧或新完整 JSON。
    await writeFile(temporaryPath, `${JSON.stringify(requirement, null, 2)}\n`, { mode: 0o600 })
    await rename(temporaryPath, this.#filePath)
  }
}

function isMissingFile(error: unknown): error is NodeJS.ErrnoException {
  return error instanceof Error && 'code' in error && error.code === 'ENOENT'
}

async function migrateRequirement(requirement: Requirement): Promise<Requirement> {
  // JSON 是持久合约：新版字段必须在读取时补默认值，而不要求用户删除当前需求。
  // 旧版把工作流步骤与目标项目 Skill 混在 skills 中；读取时拆开，写回后即使用新模型。
  const legacySkills = (requirement.skills ?? []) as Array<DiscoveredSkill & {
    kind?: 'workflow' | 'target-project'
    content?: string | null
  }>
  const workflow = requirement.workflow ?? legacySkills
    .filter((skill) => skill.kind === 'workflow')
    .map(({ name, description, active, activationReason }): WorkflowStep => ({
      name,
      description,
      active,
      activationReason,
    }))
  const skills = legacySkills
    .filter((skill) => skill.kind !== 'workflow')
    // 旧快照可能缓存 Skill 全文；迁移时丢弃缓存，只保留可审计的目标仓库相对路径。
    .map(({ kind: _legacyKind, content: _legacyContent, ...skill }) => skill)
  return {
    ...requirement,
    specId: requirement.specId ?? requirement.id.replace(/^REQ-/, 'SPEC-'),
    metrics: requirement.metrics ?? { firstPreviewAt: null, acceptedAt: requirement.delivery?.acceptedAt ?? null },
    refinements: requirement.refinements ?? [],
    grilling: requirement.grilling ?? { status: 'confirmed', questions: [], confirmedAt: requirement.createdAt },
    workflow,
    skills,
    processFacts: {
      ...requirement.processFacts,
      ownership: requirement.processFacts?.ownership ?? 'none',
    },
    branch: requirement.branch ?? null,
    codex: requirement.codex ?? { status: 'idle', sessionId: null, activeTicketId: null, changedFiles: [], events: [], error: null },
    validation: (requirement.validation ?? []).map((record) => ({
      // 兼容第五票补全前已经落盘的矩阵，避免要求用户删除当前需求。
      ...record,
      criterionId: record.criterionId ?? null,
      required: record.required ?? record.id !== 'product-acceptance',
      browserEvidence: record.browserEvidence ?? null,
    })),
    delivery: requirement.delivery ? { ...requirement.delivery, artifactPaths: requirement.delivery.artifactPaths ?? [] } : null,
    agentSessionHandoff: requirement.agentSessionHandoff ?? null,
  }
}

export const requirementStore = new RequirementStore()
