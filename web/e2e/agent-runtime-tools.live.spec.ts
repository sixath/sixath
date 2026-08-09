import { expect, test } from '@playwright/test'

/**
 * 全栈 E2E：需要 Portal (localhost:8000) + MySQL 已启动。
 * 运行：E2E_LIVE=1 npm run test:e2e -- e2e/agent-runtime-tools.live.spec.ts
 */
const liveEnabled = process.env.E2E_LIVE === '1' || process.env.E2E_LIVE === 'true'

test.describe('Agent runtime tools live stack', () => {
  test.skip(!liveEnabled, 'Set E2E_LIVE=1 and start portal + mysql')

  test('创建带 runtime_tools 的 Agent 并在详情页可见', async ({ page, request }) => {
    const health = await request.get('/api/v1/agents?page=1&page_size=1').catch(() => null)
    test.skip(!health || !health.ok(), 'Portal API not reachable at /api (start portal on :8000)')

    const suffix = Date.now()
    const agentName = `e2e-live-${suffix}`
    const workspace = `/tmp/e2e-live-${suffix}`

    await page.goto('/agents/new')
    await page.getByPlaceholder('如 my-agent').fill(agentName)
    await page.getByPlaceholder('/data/agents/my-agent').fill(workspace)
    await page.getByTestId('runtime-tool-todo_enabled').check()
    await page.getByTestId('runtime-tool-workspace_files_enabled').check()
    await page.getByRole('button', { name: '保存' }).click()
    await expect(page).toHaveURL(/\/agents$/)

    await page.getByRole('link', { name: agentName }).click()
    await expect(page.getByTestId('runtime-tools-badges')).toContainText('任务列表 (todo)')
    await expect(page.getByTestId('runtime-tools-badges')).toContainText('工作区文件')
  })
})
