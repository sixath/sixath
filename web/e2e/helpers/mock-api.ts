import type { Page, Route } from '@playwright/test'

const ok = { code: 0, message: 'ok' }

/** 可被 mockChatSessions 按引用读取，便于删除后对列表 GET 返回变化 */
export type ChatSessionListRef = { items: Record<string, unknown>[] }

export const sampleSession = {
  id: 'sess-1',
  agent_id: 'agent-1',
  title: '测试对话',
  preview: 'hello',
  created_at: '2026-05-25T00:00:00Z',
  updated_at: '2026-05-25T00:01:00Z',
}

/** ChatHome：至少两条 Agent，避免单 Agent 时自动 replace URL */
export const chatHomeAgentA = {
  id: 'agent-1',
  name: 'e2e-chat-agent-a',
  description: 'e2e',
  system_prompt: '',
  model_config: { provider: 'openai', model: 'gpt-4o-mini' },
  workspace: '/tmp/e2e',
  debug_run: false,
  runtime_tools: {} as Record<string, boolean>,
  tool_ids: [] as string[],
  created_at: '2026-05-25T00:00:00Z',
  updated_at: '2026-05-25T00:00:00Z',
}

export const chatHomeAgentB = {
  ...chatHomeAgentA,
  id: 'agent-2',
  name: 'e2e-chat-agent-b',
}

export const sampleAgent = {
  id: 'agent-e2e-1',
  name: 'e2e-runtime-agent',
  description: 'playwright e2e',
  system_prompt: '',
  model_config: { provider: 'openai', model: 'gpt-4o-mini' },
  workspace: '/tmp/e2e-agent',
  debug_run: false,
  runtime_tools: {
    memory_write_enabled: true,
    todo_enabled: true,
    workspace_files_enabled: true,
  },
  tool_ids: [] as string[],
  created_at: '2026-05-25T00:00:00Z',
  updated_at: '2026-05-25T00:00:00Z',
}

function isAgentCollectionUrl(url: string): boolean {
  try {
    const pathname = new URL(url).pathname
    return pathname === '/api/v1/agents' || pathname.endsWith('/api/v1/agents')
  } catch {
    return false
  }
}

export async function mockToolsList(page: Page) {
  await page.route('**/api/v1/tools**', async (route: Route) => {
    await route.fulfill({
      status: 200,
      contentType: 'application/json',
      body: JSON.stringify({ ret: ok, items: [], total: 0 }),
    })
  })
}

export async function mockAgentList(page: Page, items = [sampleAgent]) {
  await page.route(/\/api\/v1\/agents(\?.*)?$/, async (route: Route) => {
    if (route.request().method() !== 'GET' || !isAgentCollectionUrl(route.request().url())) {
      await route.continue()
      return
    }
    await route.fulfill({
      status: 200,
      contentType: 'application/json',
      body: JSON.stringify({ ret: ok, items, total: items.length }),
    })
  })
}

export async function mockAgentById(
  page: Page,
  agent = sampleAgent,
  handlers?: {
    onUpdate?: (body: Record<string, unknown>) => void
  },
) {
  await page.route(`**/api/v1/agents/${agent.id}`, async (route: Route) => {
    const method = route.request().method()
    if (method === 'GET') {
      await route.fulfill({
        status: 200,
        contentType: 'application/json',
        body: JSON.stringify({ ret: ok, ...agent }),
      })
      return
    }
    if (method === 'PUT') {
      const body = route.request().postDataJSON() as Record<string, unknown>
      handlers?.onUpdate?.(body)
      await route.fulfill({
        status: 200,
        contentType: 'application/json',
        body: JSON.stringify({
          ret: ok,
          ...agent,
          ...body,
          id: agent.id,
        }),
      })
      return
    }
    await route.continue()
  })
}

/** @deprecated use mockAgentById */
export async function mockAgentGet(page: Page, agent = sampleAgent) {
  await mockAgentById(page, agent)
}

export async function mockAgentCreate(page: Page, onCreate?: (body: Record<string, unknown>) => void) {
  await page.route(/\/api\/v1\/agents(\?.*)?$/, async (route: Route) => {
    if (route.request().method() !== 'POST' || !isAgentCollectionUrl(route.request().url())) {
      await route.continue()
      return
    }
    const body = route.request().postDataJSON() as Record<string, unknown>
    onCreate?.(body)
    await route.fulfill({
      status: 200,
      contentType: 'application/json',
      body: JSON.stringify({
        ret: ok,
        ...sampleAgent,
        name: body.name ?? sampleAgent.name,
        workspace: body.workspace ?? sampleAgent.workspace,
        runtime_tools: body.runtime_tools ?? sampleAgent.runtime_tools,
      }),
    })
  })
}

/** @deprecated use mockAgentById with onUpdate */
export async function mockAgentUpdate(page: Page, agentId: string, onUpdate?: (body: Record<string, unknown>) => void) {
  await mockAgentById(page, { ...sampleAgent, id: agentId }, { onUpdate })
}

export async function mockAgentDetailDeps(page: Page, agent = sampleAgent) {
  await mockToolsList(page)
  await mockAgentById(page, agent)
}

function agentSessionsPathname(requestUrl: string, agentId: string): boolean {
  try {
    const u = new URL(requestUrl)
    return u.pathname === `/api/v1/agents/${agentId}/sessions`
  } catch {
    return false
  }
}

/**
 * GET /api/v1/agents/:agentId/sessions
 * 当 URL 带非空 q 且提供 opts.searchItems 时返回 searchItems，否则返回 items（或 listRef.items）。opts.q 预留与真实 API 对齐，当前不参与分支。
 */
export async function mockChatSessions(
  page: Page,
  agentId: string,
  items: Record<string, unknown>[],
  opts?: { q?: string; searchItems?: Record<string, unknown>[]; listRef?: ChatSessionListRef },
) {
  const listRef = opts?.listRef
  await page.route(
    (u: URL) => u.pathname === `/api/v1/agents/${agentId}/sessions`,
    async (route: Route) => {
      if (route.request().method() !== 'GET' || !agentSessionsPathname(route.request().url(), agentId)) {
        await route.continue()
        return
      }
      const u = new URL(route.request().url())
      const q = u.searchParams.get('q')?.trim() ?? ''
      const base = listRef ? listRef.items : items
      const bodyItems = q && opts?.searchItems !== undefined ? opts.searchItems : base
      await route.fulfill({
        status: 200,
        contentType: 'application/json',
        body: JSON.stringify({ ret: ok, items: bodyItems, total: bodyItems.length }),
      })
    },
  )
}

/** POST /api/v1/agents/:agentId/sessions — 创建会话 */
export async function mockCreateChatSession(
  page: Page,
  agentId: string,
  responseSession: Record<string, unknown>,
) {
  await page.route(
    (u: URL) => u.pathname === `/api/v1/agents/${agentId}/sessions`,
    async (route: Route) => {
      const method = route.request().method()
      if (!agentSessionsPathname(route.request().url(), agentId)) {
        await route.continue()
        return
      }
      if (method === 'POST') {
        await route.fulfill({
          status: 200,
          contentType: 'application/json',
          body: JSON.stringify({ ret: ok, ...responseSession }),
        })
        return
      }
      await route.continue()
    },
  )
}

/** GET /api/v1/sessions/:sessionId/messages */
export async function mockListMessages(
  page: Page,
  sessionId: string,
  items: Record<string, unknown>[],
) {
  await page.route(`**/api/v1/sessions/${sessionId}/messages**`, async (route: Route) => {
    if (route.request().method() !== 'GET') {
      await route.continue()
      return
    }
    await route.fulfill({
      status: 200,
      contentType: 'application/json',
      body: JSON.stringify({ ret: ok, items, total: items.length }),
    })
  })
}

/** GET /api/v1/sessions?page=…（仅根路径分页列表） */
export async function mockListAllSessions(page: Page, items: Record<string, unknown>[], total?: number) {
  await page.route(
    (u: URL) => u.pathname === '/api/v1/sessions',
    async (route: Route) => {
      if (route.request().method() !== 'GET') {
        await route.continue()
        return
      }
      await route.fulfill({
        status: 200,
        contentType: 'application/json',
        body: JSON.stringify({ ret: ok, items, total: total ?? items.length }),
      })
    },
  )
}

/** GET /api/v1/sessions/:id（Chat页校验会话归属） */
export async function mockGetChatSession(page: Page, sessionId: string, session: Record<string, unknown>) {
  await page.route(
    (u: URL) => u.pathname === `/api/v1/sessions/${sessionId}`,
    async (route: Route) => {
      if (route.request().method() !== 'GET') {
        await route.continue()
        return
      }
      await route.fulfill({
        status: 200,
        contentType: 'application/json',
        body: JSON.stringify({ ret: ok, ...session }),
      })
    },
  )
}

/** DELETE /api/v1/sessions/:id。与 mockGetChatSession 同 pathname 时需后注册 route，否则会拦不到 DELETE。 */
export async function mockDeleteChatSession(
  page: Page,
  handlers?: { onDeleted?: (sessionId: string) => void },
) {
  await page.route(
    (u: URL) =>
      /^\/api\/v1\/sessions\/[^/]+$/.test(u.pathname) && u.pathname !== '/api/v1/sessions/search',
    async (route: Route) => {
      if (route.request().method() !== 'DELETE') {
        await route.continue()
        return
      }
      const pathname = new URL(route.request().url()).pathname
      const id = pathname.replace(/^\/api\/v1\/sessions\//, '')
      handlers?.onDeleted?.(id)
      await route.fulfill({
        status: 200,
        contentType: 'application/json',
        body: JSON.stringify({ ret: ok }),
      })
    },
  )
}

/** GET /api/v1/sessions/search?query=… */
export async function mockSearchSessions(page: Page, items: Record<string, unknown>[]) {
  await page.route('**/api/v1/sessions/search**', async (route: Route) => {
    if (route.request().method() !== 'GET') {
      await route.continue()
      return
    }
    await route.fulfill({
      status: 200,
      contentType: 'application/json',
      body: JSON.stringify({ ret: ok, items }),
    })
  })
}

export async function mockAgentListForChat(page: Page, items = [chatHomeAgentA, chatHomeAgentB]) {
  await mockAgentList(page, items)
}
