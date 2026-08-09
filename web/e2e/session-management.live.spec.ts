import { expect, test } from '@playwright/test'

/**
 * 全栈 E2E：会话侧栏 + 历史页 + 消息持久化。
 * 需要 Portal (localhost:8000) + MySQL 已启动。
 * 运行：E2E_LIVE=1 npm run test:e2e -- e2e/session-management.live.spec.ts
 */
const liveEnabled = process.env.E2E_LIVE === '1' || process.env.E2E_LIVE === 'true'

test.describe('Session management live stack', () => {
  test.skip(!liveEnabled, 'Set E2E_LIVE=1 and start portal + mysql')

  test('侧栏新建会话、刷新后历史页可见', async ({ page, request }) => {
    const health = await request.get('/api/v1/agents?page=1&page_size=1').catch(() => null)
    test.skip(!health || !health.ok(), 'Portal API not reachable at /api (start portal on :8000)')

    const agentsRes = await request.get('/api/v1/agents?page=1&page_size=50')
    expect(agentsRes.ok()).toBeTruthy()
    const agentsBody = (await agentsRes.json()) as { items?: { id: string; name: string }[] }
    const agents = agentsBody.items ?? []
    test.skip(agents.length === 0, 'No agents in portal — create one first')

    const agent = agents[0]

    await page.goto('/')
    await page.locator('.chat-home-agent-select').waitFor({ state: 'visible' })
    await page.locator('.chat-home-agent-select').selectOption(agent.id)

    await page.getByTestId('session-sidebar-new').click()
    await expect(page).toHaveURL(new RegExp(`agent=${agent.id}.*session=`))

    const url = new URL(page.url())
    const sessionId = url.searchParams.get('session')
    expect(sessionId).toBeTruthy()

    const msgRes = await request.get(`/api/v1/sessions/${sessionId}/messages`)
    expect(msgRes.ok()).toBeTruthy()

    await page.reload()
    await expect(page.getByTestId(`session-item-${sessionId}`)).toBeVisible({ timeout: 15_000 })

    const listAll = await request.get('/api/v1/sessions?page=1&page_size=50')
    if (!listAll.ok()) {
      test.info().annotations.push({
        type: 'note',
        description: 'GET /api/v1/sessions not available — deploy new portal for global history UI',
      })
      return
    }

    await page.goto('/sessions')
    await expect(page.getByTestId('sessions-history-search')).toBeVisible()
    await expect(page.getByTestId(`sessions-open-${sessionId}`)).toBeVisible({ timeout: 15_000 })

    await page.getByTestId(`sessions-open-${sessionId}`).click()
    await expect(page).toHaveURL(new RegExp(`agent=${agent.id}.*session=${sessionId}`))
  })
})
