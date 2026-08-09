import { expect, test } from '@playwright/test'
import {
  chatHomeAgentA,
  mockAgentById,
  mockAgentListForChat,
  mockChatSessions,
  mockGetChatSession,
  mockListMessages,
  sampleSession,
} from './helpers/mock-api'

const session = { ...sampleSession, id: 'sess-compact', title: '压缩会话' }

test.describe('Compact boundary banner', () => {
  test('展示 boundary 并可折叠上方历史', async ({ page }) => {
    await mockAgentListForChat(page, [chatHomeAgentA])
    await mockAgentById(page, chatHomeAgentA)
    await mockChatSessions(page, 'agent-1', [session])
    await mockGetChatSession(page, session.id, session)

    const messages = [
      { id: 'm1', session_id: session.id, role: 'user', content: '旧消息 1', created_at: '2026-05-26T00:00:00Z' },
      { id: 'm2', session_id: session.id, role: 'assistant', content: '旧回复', created_at: '2026-05-26T00:00:01Z' },
      {
        id: 'boundary-1',
        session_id: session.id,
        role: 'system',
        content: '[会话已压缩]',
        created_at: '2026-05-26T00:05:00Z',
        metadata: { sixathOrigin: 'compact_boundary' },
      },
      { id: 'm3', session_id: session.id, role: 'user', content: '新消息', created_at: '2026-05-26T00:06:00Z' },
    ]
    await mockListMessages(page, session.id, messages)

    await page.goto('/')
    await page.locator('select.chat-home-agent-select').selectOption('agent-1')
    await page.getByTestId(`session-item-${session.id}`).click()

    await expect(page.getByText('上下文已压缩')).toBeVisible()
    await expect(page.getByText('旧消息 1')).toBeVisible()
    await expect(page.getByText('新消息')).toBeVisible()

    await page.getByRole('button', { name: /收起上方 2 条历史/ }).click()
    await expect(page.getByText('旧消息 1')).not.toBeVisible()
    await expect(page.getByText('旧回复')).not.toBeVisible()
    await expect(page.getByText('新消息')).toBeVisible()

    await page.getByRole('button', { name: /展开上方 2 条历史/ }).click()
    await expect(page.getByText('旧消息 1')).toBeVisible()
  })
})
