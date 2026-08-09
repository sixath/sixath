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

    await page.goto('/')
    await page.locator('select.chat-home-agent-select').selectOption('agent-1')

    await expect(page.getByTestId('session-item-sess-a')).toBeVisible()
    await expect(page.getByTestId('session-item-sess-b')).toBeVisible()
    await expect(page.getByText('阿尔法')).toBeVisible()
  })

  test('搜索会话：带 q 时返回精简列表', async ({ page }) => {
    await chatDeps(page)
    await mockChatSessions(page, 'agent-1', [sessionA, sessionB], {
      searchItems: [sessionB],
    })

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
