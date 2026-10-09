import { describe, expect, it } from 'vitest'

import type { WorkbenchSnapshot } from '../src/domain/workbench'
import { mergeGoRuntimeSnapshot } from '../src/api/workbench-runtime-adapter'

describe('Go runtime Snapshot 适配', () => {
  it('只替换运行时字段，保留 Requirement 和历史对象', () => {
    const requirement = { id: 'req-node' } as WorkbenchSnapshot['requirement']
    const history = [{ id: 'history-node' }] as WorkbenchSnapshot['history']
    const snapshot = {
      requirement,
      history,
      target: {
        name: 'old',
        modificationScope: 'old-scope',
      },
      process: { status: 'stopped' },
    } as WorkbenchSnapshot

    const result = mergeGoRuntimeSnapshot(snapshot, {
      target: { name: 'Go target', modificationScope: 'apps/front' },
      process: {
        status: 'ready',
        ownership: 'workbench',
        pid: 42,
        command: 'pnpm dev',
        healthUrl: 'http://127.0.0.1:3000/health',
        previewUrl: 'http://127.0.0.1:3000',
        gatewayUrl: 'http://127.0.0.1:8080/preview',
        startedAt: '2026-10-09T00:00:00Z',
        recentOutput: [],
      },
    })

    expect(result.requirement).toBe(requirement)
    expect(result.history).toBe(history)
    expect(result.target).toMatchObject({ name: 'Go target', modificationScope: 'apps/front' })
    expect(result.process).toMatchObject({ status: 'ready', pid: 42, error: null })
  })
})
