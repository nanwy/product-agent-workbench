import { describe, expect, it, vi } from 'vitest'

import { createWorkbenchApiClient, WorkbenchApiError } from '../src/api/client'

describe('Go API client', () => {
  it('serializes a typed create Requirement request', async () => {
    const fetchRequest = vi.fn(async () => jsonResponse({
      id: 'req-1', projectId: 'project-1', title: '增加搜索', stage: 'grilling', revision: 1,
    }, 201))
    const client = createWorkbenchApiClient({ baseUrl: 'http://127.0.0.1:8080/api/v1/', fetch: fetchRequest })

    await expect(client.createRequirement({ projectId: 'project-1', title: '增加搜索' })).resolves.toMatchObject({
      id: 'req-1', revision: 1,
    })
    expect(fetchRequest).toHaveBeenCalledWith('http://127.0.0.1:8080/api/v1/requirements', expect.objectContaining({
      method: 'POST',
      body: JSON.stringify({ projectId: 'project-1', title: '增加搜索' }),
      headers: { Accept: 'application/json', 'Content-Type': 'application/json' },
    }))
  })

  it('encodes path and query values instead of concatenating raw user input', async () => {
    let requestedUrl: RequestInfo | URL | undefined
    const fetchRequest = vi.fn(async (input: RequestInfo | URL) => {
      requestedUrl = input
      return jsonResponse({
        id: 'report-1', requirementId: 'req/1', ticketVersionId: 'tickets & 1',
        status: 'passed', records: [], startedAt: '2026-10-08T00:00:00Z', completedAt: '2026-10-08T00:00:01Z',
      })
    })
    const client = createWorkbenchApiClient({ fetch: fetchRequest })

    await client.getLatestVerification('req/1', 'tickets & 1')

    expect(requestedUrl).toBe(
      '/api/v1/requirements/req%2F1/verification?ticketVersionId=tickets+%26+1',
    )
  })

  it('turns Go error JSON into a stable WorkbenchApiError', async () => {
    const fetchRequest = vi.fn(async () => jsonResponse({
      code: 'conflict', message: '资源冲突', requestId: 'request-1',
    }, 409))
    const client = createWorkbenchApiClient({ fetch: fetchRequest })

    const error = await client.cancelTaskRun('run-1').catch((reason: unknown) => reason)

    expect(error).toBeInstanceOf(WorkbenchApiError)
    expect(error).toMatchObject({ status: 409, code: 'conflict', requestId: 'request-1' })
  })

  it('reports malformed successful JSON as a protocol error', async () => {
    const client = createWorkbenchApiClient({ fetch: async () => new Response('not-json') })

    await expect(client.getHealth()).rejects.toMatchObject({ code: 'invalid_response', status: 200 })
  })
})

function jsonResponse(body: unknown, status = 200) {
  return new Response(JSON.stringify(body), {
    status,
    headers: { 'Content-Type': 'application/json' },
  })
}
