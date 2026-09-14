import { randomUUID } from 'node:crypto'
import { mkdir, readFile, rename, writeFile } from 'node:fs/promises'
import path from 'node:path'

type MutationRecord = { key: string; completedAt: string }
const dataDirectory = path.resolve(process.env.WORKBENCH_DATA_DIR ?? '.data')
const filePath = path.join(dataDirectory, 'completed-mutations.json')
const globalIdempotency = globalThis as typeof globalThis & { __workbenchMutationQueue?: Promise<unknown> }

export function runIdempotentMutation<T>(operation: string, key: string, onDuplicate: () => Promise<T>, action: () => Promise<T>): Promise<T> {
  if (!key.trim()) return Promise.reject(new Error('状态变更缺少 idempotencyKey'))
  const namespacedKey = `${operation}:${key}`
  const run = async () => {
    const records = await loadRecords()
    if (records.some((record) => record.key === namespacedKey)) return onDuplicate()
    const result = await action()
    await saveRecords([...records, { key: namespacedKey, completedAt: new Date().toISOString() }].slice(-200))
    return result
  }
  const queued = (globalIdempotency.__workbenchMutationQueue ?? Promise.resolve()).then(run, run)
  globalIdempotency.__workbenchMutationQueue = queued.then(() => undefined, () => undefined)
  return queued
}

async function loadRecords(): Promise<MutationRecord[]> {
  try {
    return JSON.parse(await readFile(filePath, 'utf8')) as MutationRecord[]
  } catch (error) {
    if (error instanceof Error && 'code' in error && error.code === 'ENOENT') return []
    throw error
  }
}

async function saveRecords(records: MutationRecord[]) {
  await mkdir(dataDirectory, { recursive: true })
  const temporaryPath = `${filePath}.${process.pid}.${randomUUID()}.tmp`
  await writeFile(temporaryPath, `${JSON.stringify(records, null, 2)}\n`, { mode: 0o600 })
  await rename(temporaryPath, filePath)
}
