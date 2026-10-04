import { defineConfig, devices } from '@playwright/test'
import os from 'node:os'
import path from 'node:path'

export default defineConfig({
  testDir: './tests/e2e',
  globalSetup: './tests/global-setup.ts',
  globalTeardown: './tests/global-teardown.ts',
  fullyParallel: false,
  use: {
    baseURL: 'http://127.0.0.1:3000',
    trace: 'retain-on-failure',
  },
  projects: [
    {
      name: 'chromium',
      use: { ...devices['Desktop Chrome'] },
    },
  ],
  webServer: {
    // E2E 使用独立数据目录、临时 Git 仓库和假 Codex，绝不操作开发者的真实目标仓库。
    command: 'pnpm dev --host 127.0.0.1',
    url: 'http://127.0.0.1:3000',
    reuseExistingServer: false,
    env: {
      WORKBENCH_ROOT: path.resolve('../..'),
      WORKBENCH_DATA_DIR: path.join(os.tmpdir(), 'product-agent-workbench-e2e-data'),
      WORKBENCH_TARGET_CONFIG: 'tests/fixtures/target-config.json',
      WORKBENCH_CODEX_COMMAND: path.resolve('tests/fixtures/fake-codex.mjs'),
      WORKBENCH_AGENT_BROWSER_COMMAND: path.resolve('tests/fixtures/fake-agent-browser.mjs'),
      WORKBENCH_TARGET_ROOT: path.join(os.tmpdir(), 'product-agent-workbench-e2e-target'),
    },
  },
})
