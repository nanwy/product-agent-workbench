import { randomUUID } from 'node:crypto'
import { mkdir, readFile, rename, writeFile } from 'node:fs/promises'
import path from 'node:path'

import type { Requirement } from '../domain/workbench'

export class RequirementStore {
  readonly #filePath: string
  #queue: Promise<unknown> = Promise.resolve()

  constructor(dataDirectory = process.env.WORKBENCH_DATA_DIR ?? '.data') {
    this.#filePath = path.resolve(dataDirectory, 'current-requirement.json')
  }

  async load(): Promise<Requirement | null> {
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
      return migrateRequirement(JSON.parse(await readFile(this.#filePath, 'utf8')) as Requirement)
    } catch (error) {
      if (isMissingFile(error)) return null
      throw error
    }
  }

  async #writeDirect(requirement: Requirement) {
    await mkdir(path.dirname(this.#filePath), { recursive: true })
    const temporaryPath = `${this.#filePath}.${process.pid}.${randomUUID()}.tmp`
    await writeFile(temporaryPath, `${JSON.stringify(requirement, null, 2)}\n`, { mode: 0o600 })
    await rename(temporaryPath, this.#filePath)
  }
}

function isMissingFile(error: unknown): error is NodeJS.ErrnoException {
  return error instanceof Error && 'code' in error && error.code === 'ENOENT'
}

function migrateRequirement(requirement: Requirement): Requirement {
  return {
    ...requirement,
    grilling: requirement.grilling ?? { status: 'confirmed', questions: [], confirmedAt: requirement.createdAt },
    branch: requirement.branch ?? null,
    codex: requirement.codex ?? { status: 'idle', sessionId: null, activeTicketId: null, changedFiles: [], events: [], error: null },
  }
}

export const requirementStore = new RequirementStore()
