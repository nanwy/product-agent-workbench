import { randomUUID } from 'node:crypto'
import { mkdir, readFile, rename, writeFile } from 'node:fs/promises'
import path from 'node:path'

type MutationRecord = { key: string; completedAt: string }
const dataDirectory = path.resolve(process.env.WORKBENCH_DATA_DIR ?? '.data')
const filePath = path.join(dataDirectory, 'completed-mutations.json')
const globalIdempotency = globalThis as typeof globalThis & {
  __workbenchMutationQueue?: Promise<unknown>
  __workbenchMutationsInFlight?: Map<string, Promise<unknown>>
}
const inFlight = (globalIdempotency.__workbenchMutationsInFlight ??= new Map())

/**
 * 将每个 UI 变更按“操作名 + 客户随机 key”去重。
 * 同一请求因网络重试到达两次时，第二次只返回最新快照，不重复建分支、启动进程或创建 commit。
 */
export function runIdempotentMutation<T>(operation: string, key: string, onDuplicate: () => Promise<T>, action: () => Promise<T>): Promise<T> {
  if (!key.trim()) return Promise.reject(new Error('状态变更缺少 idempotencyKey'))
  const namespacedKey = `${operation}:${key}`
  const active = inFlight.get(namespacedKey)
  if (active) return active.then(onDuplicate) as Promise<T>
  const run = async () => {
    const records = await loadRecords()
    if (records.some((record) => record.key === namespacedKey)) return onDuplicate()
    const result = await action()
    await enqueueRecord(namespacedKey)
    return result
  }
  // 同一 key 由 inFlight 折叠；不同业务动作可并发，使取消不会排在长 Turn 后面。
  // 只串行化很短的完成记录合并，避免 completed-mutations.json 互相覆盖。
  const executing = run()
  inFlight.set(namespacedKey, executing)
  return executing.finally(() => {
    if (inFlight.get(namespacedKey) === executing) inFlight.delete(namespacedKey)
  })
}

/** 仅串行合并已完成 key；业务 action 本身保持并发，停止操作才能打断长 Turn。 */
function enqueueRecord(namespacedKey: string) {
  const append = async () => {
    const records = await loadRecords()
    if (records.some((record) => record.key === namespacedKey)) return
    await saveRecords([...records, { key: namespacedKey, completedAt: new Date().toISOString() }].slice(-200))
  }
  const queued = (globalIdempotency.__workbenchMutationQueue ?? Promise.resolve()).then(append, append)
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
  // 先写同目录临时文件再 rename，防止进程中断留下半截 JSON。
  await writeFile(temporaryPath, `${JSON.stringify(records, null, 2)}\n`, { mode: 0o600 })
  await rename(temporaryPath, filePath)
}
