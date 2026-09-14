import { rm } from 'node:fs/promises'
import path from 'node:path'

import { expect, test } from '@playwright/test'

test.describe.configure({ mode: 'serial' })

test.beforeAll(async () => {
  await rm(path.resolve('test-results/workbench-data'), {
    force: true,
    recursive: true,
  })
})

test('creates, restores, explores, previews, and confirms a product requirement', async ({
  page,
  request,
}) => {
  await page.goto('/')
  await expect(page.getByText('Fixture Product / front')).toBeVisible()

  await page.getByLabel('需求标题').fill('优化首页空状态文案')
  await page.getByRole('textbox', { name: '目标页面' }).fill('/competition')
  await page.getByLabel('期望结果').fill('把空状态文案改得更明确')
  await page.getByRole('button', { name: '建立产品需求' }).click()
  await expect(page.getByRole('heading', { name: '优化首页空状态文案' })).toBeVisible()

  await page.reload()
  await expect(page.getByRole('heading', { name: '优化首页空状态文案' })).toBeVisible()

  await page.getByRole('button', { name: '启动项目' }).click()
  await expect(page.getByTestId('process-status')).toHaveText('ready')
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

  await page.getByRole('button', { name: '确认 Spec 与执行票' }).click()
  await expect(page.getByText('规格边界已确认，等待执行 Agent。')).toBeVisible()

  await page.getByRole('button', { name: '停止' }).click()
  await expect(page.getByTestId('process-status')).toHaveText('stopped')
})
