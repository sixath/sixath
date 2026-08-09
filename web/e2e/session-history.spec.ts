import { expect, test } from '@playwright/test'
import { mockAgentListForChat, mockListAllSessions, mockSearchSessions } from './helpers/mock-api'

const histRow = {
  id: 'hist-1',
  agent_id: 'agent-1',
  agent_name: 'e2e-chat-agent-a',
  title: '列表里的会话',
  preview: '预览一行',
  created_at: '2026-05-25T00:00:00Z',
  updated_at: '2026-05-25T00:02:00Z',
}

const searchHit = {
  session_id: 'hist-fts',
  root_session_id: 'hist-fts',
  agent_id: 'agent-1',
  agent_name: 'e2e-chat-agent-a',
  title: 'FTS 命中标题',
  preview: '高亮摘要',
  matched_snippets: ['snippet'],
  updated_at: '2026-05-25T00:03:00Z',
}

test.describe('SessionHistoryPage', () => {
  test.beforeEach(async ({ page }) => {
    await mockAgentListForChat(page)
  })

  test('无搜索时加载全量会话分页列表', async ({ page }) => {
    await mockListAllSessions(page, [histRow])
    await page.goto('/sessions')

    await expect(page.getByRole('heading', { name: '会话历史' })).toBeVisible()
    await expect(page.getByText('列表里的会话')).toBeVisible()
    await expect(page.getByRole('cell', { name: 'e2e-chat-agent-a' })).toBeVisible()
  })

  test('搜索展示 FTS 结果', async ({ page }) => {
    await mockListAllSessions(page, [histRow])
    await mockSearchSessions(page, [searchHit])
    await page.goto('/sessions')
    await expect(page.getByText('列表里的会话')).toBeVisible()

    await page.getByTestId('sessions-history-search').fill('FTS')
    await expect(page.getByText('FTS 命中标题')).toBeVisible({ timeout: 5_000 })
    await expect(page.getByText('列表里的会话')).toHaveCount(0)
  })

  test('打开按钮跳转到首页并带上 agent 与 session 参数', async ({ page }) => {
    await mockListAllSessions(page, [histRow])
    await page.goto('/sessions')
    await expect(page.getByTestId('sessions-open-hist-1')).toBeVisible()

    await Promise.all([
      page.waitForURL(
        (u) =>
          u.pathname === '/' &&
          u.searchParams.get('agent') === 'agent-1' &&
          u.searchParams.get('session') === 'hist-1',
      ),
      page.getByTestId('sessions-open-hist-1').click(),
    ])
  })
})
