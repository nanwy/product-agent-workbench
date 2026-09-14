import { describe, expect, it } from 'vitest'

import { assertCreateRequirementInput } from '../src/domain/workbench'
import { assertCanCreateRequirement, assertConfirmedScope, confirmGrillingAnswers } from '../src/server/workbench-service'

const target = {
  name: 'Fixture', repositoryRoot: '/tmp/workbench-target', modificationScope: 'apps/front',
  startCommand: { command: 'pnpm', args: [] }, healthUrl: 'http://127.0.0.1:1', previewUrl: 'http://127.0.0.1:1',
  previewGatewayPort: 4174, startupTimeoutMs: 1000,
}

describe('product requirement boundaries', () => {
  it('validates product input without accepting an external URL', () => {
    expect(assertCreateRequirementInput({ title: ' 标题 ', targetPage: ' /competition ', requestedOutcome: ' 结果 ' })).toEqual({
      title: '标题', targetPage: '/competition', requestedOutcome: '结果',
    })
    expect(() => assertCreateRequirementInput({ title: '标题', targetPage: 'https://example.com', requestedOutcome: '结果' })).toThrow('站内路径')
  })

  it('requires every Grilling answer and trims the confirmed value', () => {
    const questions = [{ id: 'scope', title: '修改边界', prompt: '', recommendation: '', answer: null }]
    expect(confirmGrillingAnswers(questions, { scope: ' apps/front ' })[0].answer).toBe('apps/front')
    expect(() => confirmGrillingAnswers(questions, {})).toThrow('请回答：修改边界')
  })

  it('allows only the configured scope or a child directory', () => {
    expect(() => assertConfirmedScope('apps/front', target)).not.toThrow()
    expect(() => assertConfirmedScope('apps/front/app', target)).not.toThrow()
    expect(() => assertConfirmedScope('../outside', target)).toThrow('修改边界必须是')
    expect(() => assertConfirmedScope('/tmp/workbench-target', target)).toThrow('修改边界必须是')
  })

  it('does not let an API request replace an unfinished active requirement', () => {
    expect(() => assertCanCreateRequirement(null)).not.toThrow()
    expect(() => assertCanCreateRequirement({ stage: 'accepted' })).not.toThrow()
    expect(() => assertCanCreateRequirement({ stage: 'implementation' })).toThrow('不能建立第二条活动需求')
    expect(() => assertCanCreateRequirement({ stage: 'failed' })).toThrow('不能建立第二条活动需求')
  })
})

