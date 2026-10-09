import type { WorkbenchSnapshot } from '../domain/workbench'
import type { TargetRuntimeResponse } from './contracts'

/**
 * 把 Go 的运行时资源投影到旧页面已经使用的 WorkbenchSnapshot。
 *
 * 前端视角：这类似 Redux selector。Go 响应只替换它真正负责的 target/process slice；
 * Requirement、Agent 对话、历史和验证仍保持原对象，不在接口迁移时重写页面读取模型。
 */
export function mergeGoRuntimeSnapshot(
  snapshot: WorkbenchSnapshot,
  runtime: TargetRuntimeResponse | undefined,
): WorkbenchSnapshot {
  if (!runtime) return snapshot

  return {
    ...snapshot,
    target: snapshot.target
      ? {
          ...snapshot.target,
          name: runtime.target.name,
          modificationScope: runtime.target.modificationScope,
        }
      : null,
    process: {
      ...runtime.process,
      error: runtime.process.error ?? null,
    },
  }
}
