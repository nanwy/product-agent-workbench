import { mkdir, readFile, rename, writeFile } from 'node:fs/promises'
import path from 'node:path'

import type { Requirement } from '../domain/workbench'

export class RequirementStore {
  readonly #filePath: string

  constructor(dataDirectory = process.env.WORKBENCH_DATA_DIR ?? '.data') {
    this.#filePath = path.resolve(dataDirectory, 'current-requirement.json')
  }

  async load(): Promise<Requirement | null> {
    try {
      return JSON.parse(await readFile(this.#filePath, 'utf8')) as Requirement
    } catch (error) {
      if (isMissingFile(error)) {
        return null
      }
      throw error
    }
  }

  async save(requirement: Requirement): Promise<Requirement> {
    await mkdir(path.dirname(this.#filePath), { recursive: true })
    const temporaryPath = `${this.#filePath}.${process.pid}.tmp`
    await writeFile(temporaryPath, `${JSON.stringify(requirement, null, 2)}\n`, {
      mode: 0o600,
    })
    await rename(temporaryPath, this.#filePath)
    return requirement
  }
}

function isMissingFile(error: unknown): error is NodeJS.ErrnoException {
  return error instanceof Error && 'code' in error && error.code === 'ENOENT'
}

export const requirementStore = new RequirementStore()
