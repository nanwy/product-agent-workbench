import { execFile } from 'node:child_process'
import { readFile, rm, writeFile } from 'node:fs/promises'
import os from 'node:os'
import path from 'node:path'
import { promisify } from 'node:util'

import { expect, test } from '@playwright/test'

test.describe.configure({ mode: 'serial' })
const execFileAsync = promisify(execFile)

// 主 E2E 按真实用户顺序贯穿需求、Grilling、探索、执行、验证和产品验收。
test('creates, restores, explores, previews, and confirms a product requirement', async ({
  page,
  request,
}) => {
  await page.goto('/')
  await expect(page.getByText('Fixture Product / front')).toBeVisible()

  await page.getByLabel('需求标题').fill('优化首页空状态文案')
  await page.getByRole('textbox', { name: '目标页面' }).fill('/competition')
  await page.getByLabel('期望结果').fill('把空状态文案改得更明确，同时保留现有页面结构')
  await page.getByRole('button', { name: '建立产品需求' }).click()
  await expect(page.getByRole('heading', { name: '优化首页空状态文案' })).toBeVisible()

  await expect(page.getByText('Grilling · 共同理解门禁')).toBeVisible()
  await page.getByRole('button', { name: '共同理解已达成' }).click()
  await expect(page.getByRole('button', { name: '开始只读探索' })).toBeVisible()

  await page.reload()
  await expect(page.getByRole('heading', { name: '优化首页空状态文案' })).toBeVisible()

  await page.getByRole('button', { name: '启动项目' }).click()
  await expect(page.getByTestId('process-status')).toHaveText('ready')
  await expect(page.getByTestId('process-ownership')).toHaveText('workbench')
  await expect(page.frameLocator('iframe').getByRole('heading', { name: 'Fixture target' })).toBeVisible()

  const gatewayResponse = await request.get('http://127.0.0.1:4174/competition')
  expect(gatewayResponse.headers()['x-frame-options']).toBeUndefined()
  expect(gatewayResponse.headers()['content-security-policy']).toBe("default-src 'self'; script-src 'self'")
  expect(gatewayResponse.headers()['x-fixture-header']).toBe('preserved')

  await page.getByRole('button', { name: '开始只读探索' }).click()
  await expect(page.getByText('候选 Spec')).toBeVisible()
  await expect(page.getByText('AGENTS.md', { exact: true })).toBeVisible()
  await expect(page.getByText('apps/front/AGENTS.md', { exact: true })).toBeVisible()
  await expect(page.getByText('product-copy-change', { exact: true })).toBeVisible()
  await expect(page.getByRole('heading', { name: '工作流步骤' })).toBeVisible()
  await expect(page.getByRole('heading', { name: '目标项目 Skills' })).toBeVisible()

  await page.getByRole('button', { name: '确认 Spec 与执行票' }).click()
  await expect(page.getByText('规格边界已确认，可以创建安全需求分支并执行。')).toBeVisible()

  const targetRoot = path.join(os.tmpdir(), 'product-agent-workbench-e2e-target')
  const unrelatedFile = path.join(targetRoot, 'unrelated.txt')
  await writeFile(unrelatedFile, 'existing user work\n')
  await page.getByRole('button', { name: '创建分支并执行' }).click()
  await expect(page.getByRole('alert').first()).toContainText('工作区存在 1 条未提交变更')
  await expect(page.getByText('未能创建需求分支：工作区存在 1 条未提交变更')).toBeVisible()
  await expect(page.getByText('工作区存在 1 条未提交变更').first()).toBeVisible()
  await rm(unrelatedFile)

  await writeFile(path.join(os.tmpdir(), 'product-agent-workbench-slow-turn'), 'slow\n')
  await page.getByRole('button', { name: '重新检查并执行' }).click()
  await expect(page.getByTestId('codex-status')).toContainText('running')
  await expect(page.getByTestId('codex-status')).toContainText('00000000-0000-4000-8000-000000000001')
  await page.getByRole('button', { name: '停止当前 Turn' }).click()
  await expect(page.getByTestId('codex-status')).toContainText('cancelled')
  await page.getByRole('button', { name: '继续当前 Session' }).click()
  await expect(page.getByTestId('codex-status')).toContainText('completed')
  await expect(page.locator('.ticket-row[data-status="completed"]')).toHaveCount(2)
  await expect(page.getByTestId('codex-status')).toContainText('00000000-0000-4000-8000-000000000001')
  await expect(page.getByText(/product\/spec-/)).toBeVisible()
  await expect(page.getByTestId('changed-files')).toContainText('apps/front/app/competition/page.tsx')
  await expect(page.getByText('Verification Matrix')).toBeVisible()
  await expect(page.locator('.validation-list article[data-result="failed"]')).toHaveCount(0)
  await expect(page.getByRole('button', { name: '通过产品验收' })).toBeVisible()
  await page.getByRole('button', { name: '通过产品验收' }).click()
  await expect(page.getByTestId('delivery-card')).toContainText('Frontend Delivery Handoff')
  await expect(page.getByTestId('delivery-card')).toContainText('apps/front/app/competition/page.tsx')
  const { stdout: commitSubject } = await execFileAsync('git', ['log', '-1', '--pretty=%s'], { cwd: targetRoot })
  expect(commitSubject).toContain('SPEC-')

  // Simulate a turn that started before the verification transition existed:
  // its persisted state says implementation is complete, but has no matrix yet.
  const requirementPath = path.join(os.tmpdir(), 'product-agent-workbench-e2e-data', 'current-requirement.json')
  const legacyRequirement = JSON.parse(await readFile(requirementPath, 'utf8'))
  legacyRequirement.stage = 'implementation'
  legacyRequirement.validation = []
  legacyRequirement.delivery = null
  await writeFile(requirementPath, `${JSON.stringify(legacyRequirement, null, 2)}\n`)
  await page.reload()
  await expect(page.getByRole('button', { name: '开始验证' })).toBeVisible()
  await page.getByRole('button', { name: '开始验证' }).click()
  await expect(page.getByText('Verification Matrix')).toBeVisible()

  await page.getByRole('button', { name: '停止' }).click()
  await expect(page.getByTestId('process-status')).toHaveText('stopped')

  // 第五票的负路径直接重放持久化矩阵快照：它们验证 UI/门禁，不重跑 Codex 或新建分支。
  const verifiedSnapshot = JSON.parse(await readFile(requirementPath, 'utf8'))
  const verifiedRecord = verifiedSnapshot.validation.find((item: { required: boolean; result: string }) => item.required && item.result === 'verified')

  await test.step('代码检查失败时停留在 verification', async () => {
    const failed = { ...verifiedSnapshot, stage: 'verification', validation: [{ ...verifiedRecord, id: 'code-failure', result: 'failed', detail: 'typecheck failed', exitCode: 2 }] }
    await writeFile(requirementPath, `${JSON.stringify(failed, null, 2)}\n`)
    await page.reload()
    await expect(page.locator('.validation-list article[data-result="failed"]')).toHaveCount(1)
    await expect(page.getByTestId('candidate-status')).toContainText('验证受阻')
    await expect(page.getByRole('button', { name: '通过产品验收' })).toHaveCount(0)
  })

  await test.step('环境原因 not-run 时阻止验收并展示原因', async () => {
    const notRun = { ...verifiedRecord, id: 'browser-not-run', source: 'browser', result: 'not-run', notRunReason: 'environment: agent-browser 未安装' }
    await writeFile(requirementPath, `${JSON.stringify({ ...verifiedSnapshot, stage: 'verification', validation: [notRun] }, null, 2)}\n`)
    await page.reload()
    await expect(page.locator('.validation-list article[data-result="not-run"]')).toContainText('agent-browser 未安装')
    await expect(page.getByTestId('candidate-status')).toContainText('验证受阻')
  })

  await test.step('新增 console error 作为失败证据', async () => {
    const consoleFailure = {
      ...verifiedRecord,
      id: 'console-error', source: 'browser', result: 'failed', detail: '发现 1 条新增 browser console/page error',
      browserEvidence: { targetUrl: 'http://127.0.0.1:4174/competition', viewport: { width: 1440, height: 900 }, accessibilityTree: '@e1 [heading] "Fixture target"', semanticRefs: ['@e1'], consoleErrors: ['[error] boom'] },
    }
    await writeFile(requirementPath, `${JSON.stringify({ ...verifiedSnapshot, stage: 'verification', validation: [consoleFailure] }, null, 2)}\n`)
    await page.reload()
    await expect(page.getByText('发现 1 条新增 browser console/page error')).toBeVisible()
    await page.getByText('agent-browser 证据').click()
    await expect(page.getByText('[error] boom')).toBeVisible()
  })

  await test.step('即使快照阶段错误，必要 not-run 仍禁用验收按钮', async () => {
    const notRun = { ...verifiedRecord, id: 'required-not-run', result: 'not-run', notRunReason: 'environment: preview unavailable' }
    await writeFile(requirementPath, `${JSON.stringify({ ...verifiedSnapshot, stage: 'awaiting-product-acceptance', validation: [notRun] }, null, 2)}\n`)
    await page.reload()
    await expect(page.getByRole('button', { name: '通过产品验收' })).toBeDisabled()
  })
})
