const globalIdempotency = globalThis as typeof globalThis & {
  __workbenchMutations?: Map<string, Promise<unknown>>
  __workbenchMutationQueue?: Promise<unknown>
}

const mutations = (globalIdempotency.__workbenchMutations ??= new Map())

export function runIdempotentMutation<T>(key: string, operation: () => Promise<T>): Promise<T> {
  if (!key.trim()) return Promise.reject(new Error('状态变更缺少 idempotencyKey'))
  const existing = mutations.get(key) as Promise<T> | undefined
  if (existing) return existing
  const queued = (globalIdempotency.__workbenchMutationQueue ?? Promise.resolve()).then(operation, operation)
  mutations.set(key, queued)
  globalIdempotency.__workbenchMutationQueue = queued.then(() => undefined, () => undefined)
  void queued.finally(() => {
    setTimeout(() => mutations.delete(key), 60_000).unref?.()
  }).catch(() => undefined)
  return queued
}
