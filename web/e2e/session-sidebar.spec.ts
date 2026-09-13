import { expect, test } from '@playwright/test'
import {
  chatHomeAgentA,
  chatHomeAgentB,
  mockAgentById,
  mockAgentList,
  mockAgentListForChat,
  mockChatSessions,
  mockCreateChatSession,
  mockDeleteChatSession,
  mockGetChatSession,
  mockListMessages,
  sampleSession,
  type ChatSessionListRef,
} from './helpers/mock-api'

const sessionA = { ...sampleSession, id: 'sess-a', title: '阿尔法', preview: 'p1' }
const sessionB = { ...sampleSession, id: 'sess-b', title: '香蕉话题', preview: 'p2' }

async function chatDeps(page: import('@playwright/test').Page) {
  await mockAgentListForChat(page, [chatHomeAgentA, chatHomeAgentB])
  await mockAgentById(page, chatHomeAgentA)
  await mockAgentById(page, chatHomeAgentB)
}

test.describe('SessionSidebar', () => {
  test.beforeEach(async ({ page }) => {
    await mockAgentList(page, [])
  })

  test('选择 Agent 后展示会话列表', async ({ page }) => {
    await chatDeps(page)
    await mockChatSessions(page, 'agent-1', [sessionA, sessionB])
    await mockGetChatSession(page, 'sess-a', sessionA)
    await mockListMessages(page, 'sess-a', [])

    await page.goto('/')
    await page.locator('select.chat-home-agent-select').selectOption('agent-1')

    await expect(page.getByTestId('session-item-sess-a')).toBeVisible()
    await expect(page.getByTestId('session-item-sess-b')).toBeVisible()
    await expect(page.getByText('阿尔法')).toBeVisible()
  })

  test('选择 Agent 且 URL 无 session 时打开最近一条', async ({ page }) => {
    await chatDeps(page)
    const latest = { ...sessionA, id: 'sess-latest', title: '最近一条' }
    const older = { ...sessionB, id: 'sess-older', title: '更早的' }
    await mockChatSessions(page, 'agent-1', [latest, older])
    await mockGetChatSession(page, 'sess-latest', latest)
    await mockListMessages(page, 'sess-latest', [
      {
        id: 'm1',
        session_id: 'sess-latest',
        role: 'user',
        content: '续写这条历史',
        created_at: '2026-09-13T16:00:00Z',
      },
    ])

    await page.goto('/')
    await page.locator('select.chat-home-agent-select').selectOption('agent-1')

    await expect(page).toHaveURL(/[?&]session=sess-latest(?:&|$)/)
    await expect(page.getByTestId('session-item-sess-latest')).toHaveClass(
      /session-sidebar-item--active/,
    )
    await expect(page.getByText('续写这条历史')).toBeVisible()
  })

  test('Agent 没有任何会话时不自动写入 session', async ({ page }) => {
    await chatDeps(page)
    await mockChatSessions(page, 'agent-1', [])

    await page.goto('/')
    await page.locator('select.chat-home-agent-select').selectOption('agent-1')

    await expect(page.getByTestId('session-sidebar-new')).toBeVisible()
    await expect(page).toHaveURL(/agent=agent-1/)
    await expect(page).not.toHaveURL(/[?&]session=/)
  })

  test('切换 Agent 打开该 Agent 最近一条', async ({ page }) => {
    await chatDeps(page)
    await mockChatSessions(page, 'agent-1', [sessionA])
    await mockGetChatSession(page, 'sess-a', sessionA)
    await mockListMessages(page, 'sess-a', [])
    const bLatest = { ...sessionB, id: 'sess-b-latest', agent_id: 'agent-2', title: 'AgentB最近' }
    await mockChatSessions(page, 'agent-2', [bLatest])
    await mockGetChatSession(page, 'sess-b-latest', bLatest)
    await mockListMessages(page, 'sess-b-latest', [])

    await page.goto('/')
    await page.locator('select.chat-home-agent-select').selectOption('agent-1')
    await expect(page).toHaveURL(/[?&]session=sess-a(?:&|$)/)

    await page.locator('select.chat-home-agent-select').selectOption('agent-2')
    await expect(page).toHaveURL(/agent=agent-2/)
    await expect(page).toHaveURL(/[?&]session=sess-b-latest(?:&|$)/)
  })

  test('搜索会话：带 q 时返回精简列表', async ({ page }) => {
    await chatDeps(page)
    await mockChatSessions(page, 'agent-1', [sessionA, sessionB], {
      searchItems: [sessionB],
    })

    await mockGetChatSession(page, 'sess-a', sessionA)
    await mockListMessages(page, 'sess-a', [])

    await page.goto('/')
    await page.locator('select.chat-home-agent-select').selectOption('agent-1')
    await expect(page.getByTestId('session-item-sess-a')).toBeVisible()

    await page.getByTestId('session-sidebar-search').fill('香蕉')
    await expect(page.getByTestId('session-item-sess-b')).toBeVisible({ timeout: 5_000 })
    await expect(page.getByTestId('session-item-sess-a')).toHaveCount(0)
  })

  test('新建对话会把 session 写入 URL', async ({ page }) => {
    await chatDeps(page)
    await mockChatSessions(page, 'agent-1', [sessionA])
    const newSession = {
      ...sampleSession,
      id: 'sess-new',
      title: '新对话',
      agent_id: 'agent-1',
    }
    await mockCreateChatSession(page, 'agent-1', newSession)
    await mockGetChatSession(page, 'sess-a', sessionA)
    await mockListMessages(page, 'sess-a', [])
    await mockGetChatSession(page, 'sess-new', newSession)
    await mockListMessages(page, 'sess-new', [])

    await page.goto('/')
    await page.locator('select.chat-home-agent-select').selectOption('agent-1')
    await page.getByTestId('session-sidebar-new').click()

    await expect(page).toHaveURL(/[?&]session=sess-new(?:&|$)/)
    await expect(page).toHaveURL(/agent=agent-1/)
  })

  test('删除会话需确认对话框，删除后切换到其余会话', async ({ page }) => {
    await chatDeps(page)
    const listRef: ChatSessionListRef = { items: [{ ...sessionA }, { ...sessionB }] }
    await mockChatSessions(page, 'agent-1', [], { listRef })
    await mockGetChatSession(page, 'sess-a', { ...sessionA })
    await mockListMessages(page, 'sess-a', [])
    await mockGetChatSession(page, 'sess-b', { ...sessionB })
    await mockListMessages(page, 'sess-b', [])
    // 需最后注册：与 GET /sessions/:id 同源路径时先于 mockGetChatSession 会拦不到 DELETE
    await mockDeleteChatSession(page, {
      onDeleted: (id) => {
        listRef.items = listRef.items.filter((x) => (x.id as string) !== id)
      },
    })

    await page.goto('/?agent=agent-1&session=sess-a')
    await expect(page.getByTestId('session-item-sess-a')).toBeVisible()

    await Promise.all([
      page.waitForEvent('dialog').then(async (d) => {
        expect(d.message()).toContain('删除')
        await d.accept()
      }),
      page
        .getByTestId('session-item-sess-a')
        .getByRole('button', { name: '删除' })
        .click({ force: true }),
    ])

    await expect(page).toHaveURL(/[?&]session=sess-b(?:&|$)/)
    await expect(page.getByTestId('session-item-sess-a')).toHaveCount(0)
    await expect(page.getByTestId('session-item-sess-b')).toBeVisible()
  })
})
