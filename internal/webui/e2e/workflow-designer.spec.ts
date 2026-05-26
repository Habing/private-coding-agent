import { test, expect } from '@playwright/test'

import {
  E2E_WORKFLOW_SLUG,
  apiLogin,
  ensureMockChainWorkflow,
  loginViaUi,
  watchDesignCompile,
  watchDesignDecompile,
} from './helpers'

test.describe('Workflow SWD designer smoke', () => {
  test.beforeAll(async ({ request, baseURL }) => {
    if (!baseURL) {
      test.skip(true, 'missing baseURL')
      return
    }
    try {
      const health = await request.get(`${baseURL}/healthz`, { timeout: 8_000 })
      if (!health.ok()) {
        test.skip(true, `healthz not ok: ${health.status()}`)
        return
      }
      const token = await apiLogin(request, baseURL)
      await ensureMockChainWorkflow(request, baseURL, token)
    } catch (e) {
      test.skip(true, `compose server not ready: ${e instanceof Error ? e.message : String(e)}`)
    }
  })

  test('login, open mock chain, select step, compile', async ({ page }) => {
    await loginViaUi(page)

    await page.goto('/workflows')
    await page.getByRole('button', { name: '我的工作流' }).click()

    const row = page.locator('li').filter({ hasText: E2E_WORKFLOW_SLUG })
    await expect(row).toBeVisible({ timeout: 15_000 })

    const decompileDone = watchDesignDecompile(page)
    await row.getByRole('button', { name: '编辑' }).click()
    await page.getByRole('button', { name: '设计器' }).click()
    await decompileDone.catch(() => {})

    const canvas = page.getByTestId('workflow-swd-canvas').or(page.locator('.swd-embed'))
    await expect(canvas).toBeVisible({ timeout: 25_000 })
    await expect(canvas.locator('.sqd-designer')).toBeVisible({ timeout: 25_000 })

    await expect(page.getByText('status · fetch_status', { exact: true })).toBeVisible({
      timeout: 15_000,
    })

    const statusStep = page.getByText('status · fetch_status', { exact: true })
    await expect(statusStep).toBeVisible({ timeout: 10_000 })
    await statusStep.click()

    const detail = page.getByTestId('workflow-step-detail')
    if ((await detail.count()) > 0) {
      await expect(detail).toHaveAttribute('data-selected-step-id', 'status')
    }
    await expect(page.getByText('在左侧画布中选中一步')).toBeHidden({ timeout: 10_000 })
    const stepSidebar = page.getByRole('complementary')
    await expect(stepSidebar.getByText('status', { exact: true })).toBeVisible()
    await expect(stepSidebar.getByText('工具', { exact: true })).toBeVisible()

    const nameInput = page.locator('#wf-name-sidebar')
    const compileDone = watchDesignCompile(page)
    await nameInput.fill('Mock 状态巡检 (e2e)')
    await compileDone.catch(() => {})

    await expect(nameInput).toHaveValue('Mock 状态巡检 (e2e)')
  })
})
