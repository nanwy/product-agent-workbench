import { describe, expect, it } from 'vitest'

import { assertContinueRefinementInput, assertCreateAgentHandoffInput, assertCreateRequirementInput } from '../src/domain/workbench'
import { assertCanCreateRequirement, assertConfirmedScope, assertLegacyPlanConfirmationAllowed, assertProductChangeBoundary, confirmGrillingAnswers } from '../src/server/workbench-service'

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

  it('treats refinement text as bounded product data', () => {
    expect(assertContinueRefinementInput({ requestedOutcome: ' 调整按钮文案 ' })).toEqual({ requestedOutcome: '调整按钮文案' })
    expect(() => assertContinueRefinementInput({ requestedOutcome: ' ' })).toThrow('具体的返修结果')
    expect(() => assertContinueRefinementInput({ requestedOutcome: 'x'.repeat(2001) })).toThrow('2000')
  })

  it('normalizes the Agent handoff focus without accepting unbounded content', () => {
    expect(assertCreateAgentHandoffInput({ focus: ' ' })).toEqual({ focus: '继续当前产品需求' })
    expect(assertCreateAgentHandoffInput({ focus: ' 下一轮修复验证 ' })).toEqual({ focus: '下一轮修复验证' })
    expect(() => assertCreateAgentHandoffInput({ focus: 'x'.repeat(1001) })).toThrow('1000')
  })

  it('allows only the configured scope or a child directory', () => {
    expect(() => assertConfirmedScope('apps/front', target)).not.toThrow()
    expect(() => assertConfirmedScope('apps/front/app', target)).not.toThrow()
    expect(() => assertConfirmedScope('../outside', target)).toThrow('修改边界必须是')
    expect(() => assertConfirmedScope('/tmp/workbench-target', target)).toThrow('修改边界必须是')
  })

  it('accepts a route as the confirmed product change boundary', () => {
    expect(() => assertProductChangeBoundary('/sign-in')).not.toThrow()
    expect(() => assertProductChangeBoundary('只修改登录页文案，不修改注册页')).not.toThrow()
    expect(() => assertProductChangeBoundary(' ')).toThrow('请确认产品修改边界')
  })

  it('does not let an API request replace an unfinished active requirement', () => {
    expect(() => assertCanCreateRequirement(null)).not.toThrow()
    expect(() => assertCanCreateRequirement({ stage: 'accepted' })).not.toThrow()
    expect(() => assertCanCreateRequirement({ stage: 'implementation' })).toThrow('不能建立第二条活动需求')
    expect(() => assertCanCreateRequirement({ stage: 'failed' })).toThrow('不能建立第二条活动需求')
  })

  it('rejects legacy endpoints that could bypass the versioned Spec gate', () => {
    expect(() => assertLegacyPlanConfirmationAllowed({ specVersions: [{ id: 'SPEC-v1' }] as never })).toThrow('必须通过当前版本门禁确认')
  })
})
