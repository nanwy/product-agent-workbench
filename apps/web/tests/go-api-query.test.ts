import { QueryClient } from '@tanstack/react-query'
import { describe, expect, it } from 'vitest'

import type {
  RequirementResponse,
  TaskRunResponse,
  VerificationReportResponse,
} from '../src/api/contracts'
import {
  goQueryKeys,
  writeRequirementCache,
  writeTaskRunCache,
  writeVerificationCache,
} from '../src/api/queries'

describe('Go React Query cache projection', () => {
  it('writes each server resource into its stable identity key', () => {
    const queryClient = new QueryClient()
    const requirement: RequirementResponse = {
      id: 'req-1', projectId: 'project-1', title: '搜索', stage: 'implementation', revision: 1,
    }
    const run: TaskRunResponse = {
      id: 'run-1', requirementId: 'req-1', ticketVersionId: 'tickets-v1', ticketId: 'TICKET-01',
      attempt: 1, status: 'queued', createdAt: '2026-10-08T00:00:00Z',
    }
    const report: VerificationReportResponse = {
      id: 'report-1', requirementId: 'req-1', ticketVersionId: 'tickets-v1', status: 'passed',
      records: [], startedAt: '2026-10-08T00:00:00Z', completedAt: '2026-10-08T00:00:01Z',
    }

    writeRequirementCache(queryClient, requirement)
    writeTaskRunCache(queryClient, run)
    writeVerificationCache(queryClient, report)

    expect(queryClient.getQueryData(goQueryKeys.requirement('req-1'))).toBe(requirement)
    expect(queryClient.getQueryData(goQueryKeys.taskRun('run-1'))).toBe(run)
    expect(queryClient.getQueryData(goQueryKeys.verification('req-1', 'tickets-v1'))).toBe(report)
  })

  it('keeps resources from different requirements in different cache slots', () => {
    expect(goQueryKeys.requirement('req-1')).not.toEqual(goQueryKeys.requirement('req-2'))
    expect(goQueryKeys.verification('req-1', 'tickets-v1')).not.toEqual(
      goQueryKeys.verification('req-1', 'tickets-v2'),
    )
  })
})

