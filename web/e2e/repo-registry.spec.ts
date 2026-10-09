import { expect, test, type Page, type Route } from '@playwright/test'
import { mockAgentById, mockAgentDetailDeps, mockAgentList, sampleAgent } from './helpers/mock-api'

const now = '2026-10-01T00:00:00Z'
const repoA = {
  id: 'r-a',
  code_root: '/codes',
  rel_path: 'cloudgame/svc-a',
  name: 'svc-a',
  description: '',
  tags: [],
  git_remote: '',
  git_branch: 'main',
  head_commit: 'abcdef1234567890',
  sync_mode: 'registry_only',
  status: 'active',
  handbook_status: 'none',
  owner_id: '',
  last_scanned_at: now,
  created_at: now,
  updated_at: now,
}
const repoB = { ...repoA, id: 'r-b', rel_path: 'cloudgame/svc-b', name: 'svc-b' }
const group = {
  id: 'g-1',
  name: 'cloudgame',
  kind: 'dir',
  rule: { code_root: '/codes', rel_prefix: 'cloudgame' },
  auto_apply_new: true,
  owner_id: '',
  created_at: now,
  updated_at: now,
  repo_ids: ['r-a', 'r-b'],
}

type GroupBody = { name: string; repo_ids: string[] }

async function mockRepoRegistry(
  page: Page,
  opts: {
    onPutBindings?: (body: { bindings: unknown[] }) => void
    onMigrate?: (apply: boolean) => void
    onCreateGroup?: (body: GroupBody) => void
    bindings?: unknown[]
    repos?: unknown[] | (() => unknown[])
  } = {},
) {
  const groups: Record<string, unknown>[] = [group]
  await page.route(/\/api\/v1\/repos(\/[^?]*)?(\?.*)?$/, async (route: Route) => {
    const url = new URL(route.request().url())
    const method = route.request().method()
    if (url.pathname === '/api/v1/repos/scan' && method === 'POST') {
      await route.fulfill({ json: { roots: 1, found: 2, added: 0, restored: 0, missing: 0 } })
      return
    }
    if (url.pathname === '/api/v1/repos/migrate-legacy-links' && method === 'POST') {
      const apply = url.searchParams.get('apply') === 'true'
      opts.onMigrate?.(apply)
      await route.fulfill({
        json: {
          apply,
          items: [
            {
              agent_id: sampleAgent.id,
              target: '/codes/cloudgame',
              action: 'bind_group',
              bindings: [{ target_kind: 'repo_group', target_id: 'g-1', mode: 'include' }],
              after_roots: ['cloudgame/svc-a', 'cloudgame/svc-b'],
              applied: apply,
            },
            { agent_id: 'agent-2', target: '/codes', action: 'manual_multi', reason: '目标下有多个仓库', applied: false },
          ],
        },
      })
      return
    }
    if (url.pathname === '/api/v1/repos' && method === 'GET') {
      const items = (typeof opts.repos === 'function' ? opts.repos() : opts.repos) ?? [repoA, repoB]
      await route.fulfill({ json: { items, total: items.length } })
      return
    }
    await route.continue()
  })
  await page.route(/\/api\/v1\/repo-groups(\?.*)?$/, async (route: Route) => {
    if (route.request().method() === 'POST') {
      const body = route.request().postDataJSON() as GroupBody
      opts.onCreateGroup?.(body)
      const created = {
        id: 'g-manual',
        name: body.name,
        kind: 'manual',
        auto_apply_new: false,
        owner_id: '',
        created_at: now,
        updated_at: now,
        repo_ids: body.repo_ids,
      }
      groups.push(created)
      await route.fulfill({ json: created })
      return
    }
    await route.fulfill({ json: { items: groups } })
  })
  await page.route(`**/api/v1/agents/${sampleAgent.id}/repo-bindings`, async (route: Route) => {
    if (route.request().method() === 'PUT') {
      const body = route.request().postDataJSON() as { bindings: unknown[] }
      opts.onPutBindings?.(body)
      await route.fulfill({ json: { bindings: body.bindings, effective: [] } })
      return
    }
    await route.fulfill({ json: { bindings: opts.bindings ?? [], effective: [] } })
  })
}

async function mockHandbookConfig(page: Page, model = 'qwen/qwen-max') {
  await page.route('**/api/v1/handbook/config', async (route: Route) => {
    await route.fulfill({ json: { model, enabled: model !== '' } })
  })
}

test.describe('Repo registry UI', () => {
  test('仓库列表展示仓库并可手动扫描', async ({ page }) => {
    await mockRepoRegistry(page)
    await page.goto('/repos')
    await expect(page.getByText('cloudgame/svc-a')).toBeVisible()
    await page.getByRole('button', { name: '立即扫描' }).click()
    await expect(page.getByTestId('repo-scan-report')).toContainText('发现 2')
  })

  test('Agent 详情页绑定目录组并排除一个仓库', async ({ page }) => {
    let put: { bindings: unknown[] } | null = null
    await mockAgentList(page, [sampleAgent])
    await mockAgentDetailDeps(page, sampleAgent)
    await mockRepoRegistry(page, { onPutBindings: (b) => (put = b) })

    await page.goto(`/agents/${sampleAgent.id}`)
    const section = page.getByTestId('repo-bindings-section')
    await expect(section).toBeVisible()
    await section.getByTestId('repo-group-g-1').check()
    await expect(section.getByTestId('repo-bindings-summary')).toContainText('可见 2 个仓库')
    await section.getByRole('button', { name: '展开' }).click()
    await section.getByTestId('repo-member-g-1-r-b').uncheck()
    await expect(section.getByTestId('repo-bindings-summary')).toContainText('可见 1 个仓库')
    await section.getByTestId('repo-bindings-save').click()
    await expect(section.getByText('已保存')).toBeVisible()
    expect(put).not.toBeNull()
    expect(put!.bindings).toEqual([
      { target_kind: 'repo_group', target_id: 'g-1', mode: 'include' },
      { target_kind: 'repo', target_id: 'r-b', mode: 'exclude' },
    ])
  })

  test('迁移页先出报告再应用自动迁移', async ({ page }) => {
    const calls: boolean[] = []
    await mockRepoRegistry(page, { onMigrate: (apply) => calls.push(apply) })
    await page.goto('/repos/migration')
    await page.getByRole('button', { name: '生成报告' }).click()
    await expect(page.getByRole('cell', { name: '需人工：多个仓库', exact: true })).toBeVisible()
    await expect(page.getByTestId('migration-apply')).toContainText('（1）')
    await page.getByTestId('migration-apply').click()
    await page.getByRole('button', { name: '应用', exact: true }).click()
    await expect(page.getByText('已写入')).toBeVisible()
    expect(calls).toEqual([false, true])
  })

  test('分组页新建手工组', async ({ page }) => {
    let created: GroupBody | null = null
    await mockRepoRegistry(page, { onCreateGroup: (b) => (created = b) })
    await page.goto('/repo-groups')
    await expect(page.getByRole('cell', { name: 'cloudgame', exact: true })).toBeVisible()
    await page.getByRole('button', { name: '新建手工组' }).click()
    const dialog = page.getByRole('dialog')
    await dialog.locator('.form-group input').fill('core')
    await dialog.locator('label', { hasText: 'cloudgame/svc-a' }).getByRole('checkbox').check()
    await dialog.getByRole('button', { name: '保存' }).click()
    await expect(dialog).toHaveCount(0)
    await expect(page.getByRole('cell', { name: 'core', exact: true })).toBeVisible()
    expect(created).toEqual({ name: 'core', repo_ids: ['r-a'] })
  })

  test('编辑页在已有绑定时提示链接被覆盖', async ({ page }) => {
    await mockAgentById(page, sampleAgent)
    await mockRepoRegistry(page, {
      bindings: [{ agent_id: sampleAgent.id, target_kind: 'repo_group', target_id: 'g-1', mode: 'include', priority: 0 }],
    })
    await page.route(/\/api\/v1\/code-roots(\?.*)?$/, async (route: Route) => {
      await route.fulfill({ json: { roots: ['/codes'] } })
    })
    await page.route('**/api/v1/code-roots/browse**', async (route: Route) => {
      await route.fulfill({ json: { root: '/codes', path: '', entries: [] } })
    })
    await page.route(`**/api/v1/agents/${sampleAgent.id}/workspace-link`, async (route: Route) => {
      await route.fulfill({ json: { exists: false } })
    })
    await page.route(/\/api\/v1\/(channels|proxies)(\?.*)?$/, async (route: Route) => {
      await route.fulfill({ json: { ret: { code: 0, message: 'ok' }, items: [], total: 0 } })
    })

    await page.goto(`/agents/${sampleAgent.id}/edit`)
    await expect(page.getByTestId('workspace-link-override-hint')).toContainText('1 条代码仓库绑定')
  })

  test('仓库页展示 handbook 状态，可浏览与重建', async ({ page }) => {
    const rebuilt: string[] = []
    const withHandbook = {
      ...repoA,
      handbook_status: 'ready',
      handbook_commit: repoA.head_commit,
      handbook_version: 2,
      handbook_stats: { files: 12 },
    }
    await mockRepoRegistry(page, { repos: [withHandbook, repoB] })
    await page.route(/\/api\/v1\/repos\/[^/]+\/handbook(\/[^?]*)?(\?.*)?$/, async (route: Route) => {
      const url = new URL(route.request().url())
      const id = url.pathname.split('/')[4]
      if (url.pathname.endsWith('/handbook/rebuild')) {
        rebuilt.push(id)
        await route.fulfill({ json: { accepted: true } })
        return
      }
      if (url.pathname.endsWith('/handbook/page')) {
        const p = url.searchParams.get('path')
        await route.fulfill({ json: { path: p, content: p === 'SKILL.md' ? 'Handbook body' : `page ${p}` } })
        return
      }
      await route.fulfill({
        json: {
          repo_id: id,
          status: 'ready',
          commit: repoA.head_commit,
          head_commit: repoA.head_commit,
          version: 2,
          pages: ['references/index.md', 'SKILL.md'],
        },
      })
    })

    await page.goto('/repos')
    await expect(page.getByTestId('handbook-state-r-a')).toHaveText('最新')
    await expect(page.getByTestId('handbook-state-r-b')).toHaveText('未生成')

    await page.getByRole('button', { name: '查看 cloudgame/svc-a 的 handbook' }).click()
    const dialog = page.getByRole('dialog')
    await expect(dialog.getByTestId('handbook-content')).toHaveText('Handbook body')
    await dialog.getByRole('button', { name: 'references/index.md' }).click()
    await expect(dialog.getByTestId('handbook-content')).toHaveText('page references/index.md')
    await dialog.getByRole('button', { name: '关闭' }).click()
    await expect(dialog).toHaveCount(0)

    await expect(page.getByRole('button', { name: '查看 cloudgame/svc-b 的 handbook' })).toBeDisabled()
    await page.getByRole('button', { name: '重建 cloudgame/svc-b 的 handbook' }).click()
    await expect(page.getByTestId('handbook-state-r-b')).toHaveText('生成中')
    expect(rebuilt).toEqual(['r-b'])
  })

  test('生成中的仓库仍可重建并提示 409，构建完成后自动刷新', async ({ page }) => {
    let finished = false
    const building = {
      ...repoA,
      handbook_status: 'building',
      handbook_lease_until: new Date(Date.now() + 30 * 60_000).toISOString(),
    }
    const ready = { ...repoA, handbook_status: 'ready', handbook_commit: repoA.head_commit, handbook_version: 1 }
    await mockRepoRegistry(page, { repos: () => [finished ? ready : building, repoB] })
    await page.route(/\/api\/v1\/repos\/[^/]+\/handbook\/rebuild$/, async (route: Route) => {
      await route.fulfill({
        status: 409,
        json: { code: 409, reason: 'HANDBOOK_BUILDING', message: 'handbook: build already running' },
      })
    })

    await page.goto('/repos')
    await expect(page.getByTestId('handbook-state-r-a')).toHaveText('生成中')
    const rebuild = page.getByRole('button', { name: '重建 cloudgame/svc-a 的 handbook' })
    await expect(rebuild).toBeEnabled()
    await rebuild.click()
    await expect(page.getByTestId('repo-rebuild-error')).toContainText('handbook: build already running')

    finished = true
    await expect(page.getByTestId('handbook-state-r-a')).toHaveText('最新', { timeout: 10_000 })
  })

  test('仓库列表展示 LLM 增强状态与进度', async ({ page }) => {
    const partial = {
      ...repoA,
      handbook_status: 'ready',
      handbook_commit: repoA.head_commit,
      handbook_version: 2,
      handbook_model: '',
      handbook_llm: { state: 'partial', commit: repoA.head_commit, cards_done: 120, cards_total: 600 },
    }
    const off = { ...repoB, handbook_model: 'off' }
    await mockRepoRegistry(page, { repos: [partial, off] })
    await mockHandbookConfig(page)

    await page.goto('/repos')
    await expect(page.getByTestId('llm-state-r-a')).toContainText('部分完成 120/600')
    await expect(page.getByTestId('llm-state-r-b')).toContainText('未启用')
  })

  test('编辑弹窗可设置 Handbook 模型', async ({ page }) => {
    let patched: Record<string, unknown> | null = null
    await mockRepoRegistry(page)
    await mockHandbookConfig(page)
    await page.route(/\/api\/v1\/model-providers(\?.*)?$/, async (route: Route) => {
      await route.fulfill({
        json: { providers: [{ id: 'p1', name: 'qwen', kind: 'openai_compat', base_url: '', has_api_key: true, enabled: true }] },
      })
    })
    await page.route(/\/api\/v1\/model-catalog(\?.*)?$/, async (route: Route) => {
      await route.fulfill({
        json: { items: [{ id: 'e1', provider_id: 'p1', model: 'qwen-max', display_name: 'Qwen Max', hidden: false, source: 'sync' }] },
      })
    })
    await page.route(/\/api\/v1\/repos\/r-a$/, async (route: Route) => {
      if (route.request().method() === 'PATCH') {
        patched = route.request().postDataJSON() as Record<string, unknown>
        await route.fulfill({ json: { ...repoA, handbook_model: patched.handbook_model } })
        return
      }
      await route.fulfill({ json: { repository: repoA, agent_ids: [] } })
    })

    await page.goto('/repos')
    await page.getByRole('row', { name: /cloudgame\/svc-a/ }).getByRole('button', { name: '编辑' }).click()
    const dialog = page.getByRole('dialog')
    const model = dialog.getByLabel('Handbook 模型')
    await expect(model).toHaveAttribute('placeholder', '继承全局（qwen/qwen-max）')
    await expect(dialog.locator('#handbook-model-options option[value="qwen/qwen-max"]')).toHaveCount(1)
    await model.fill(' off ')
    await dialog.getByRole('button', { name: '保存' }).click()
    await expect(dialog).toHaveCount(0)
    expect(patched).not.toBeNull()
    expect(patched!.handbook_model).toBe('off')
    await expect(page.getByTestId('llm-state-r-a')).toContainText('未启用')
  })

  test('handbook 弹窗展示 LLM 区块并可重新生成', async ({ page }) => {
    const enriched: string[] = []
    let enrichStatus = 409
    const ready = { ...repoA, handbook_status: 'ready', handbook_commit: repoA.head_commit, handbook_version: 2 }
    await mockRepoRegistry(page, { repos: [ready, repoB] })
    await mockHandbookConfig(page)
    await page.route(/\/api\/v1\/repos\/[^/]+\/handbook(\/[^?]*)?(\?.*)?$/, async (route: Route) => {
      const url = new URL(route.request().url())
      if (url.pathname.endsWith('/handbook/enrich')) {
        enriched.push(url.search)
        if (enrichStatus === 409) {
          await route.fulfill({
            status: 409,
            json: { code: 409, reason: 'HANDBOOK_LLM_BUSY', message: 'handbook: another LLM run is in progress' },
          })
          return
        }
        await route.fulfill({ json: { accepted: true } })
        return
      }
      if (url.pathname.endsWith('/handbook/page')) {
        await route.fulfill({ json: { path: 'SKILL.md', content: 'Handbook body' } })
        return
      }
      await route.fulfill({
        json: {
          repo_id: 'r-a',
          status: 'ready',
          commit: repoA.head_commit,
          head_commit: repoA.head_commit,
          version: 2,
          pages: ['SKILL.md'],
          llm_model: 'qwen/qwen-max',
          llm_running: false,
          llm: {
            state: 'complete',
            commit: repoA.head_commit,
            cards_done: 598,
            cards_total: 600,
            card_errors: 2,
            stages: 6,
            fallback: true,
            fallback_reason: 'bad_reply',
            tokens_in: 1200,
            tokens_out: 300,
            last_error: 'skeleton reply truncated',
          },
        },
      })
    })

    await page.goto('/repos')
    await page.getByRole('button', { name: '查看 cloudgame/svc-a 的 handbook' }).click()
    const section = page.getByRole('dialog').getByTestId('handbook-llm')
    await expect(section.getByTestId('handbook-llm-state')).toHaveText('已完成 598/600')
    await expect(section).toContainText('2 个文件生成失败')
    await expect(section).toContainText('按目录分区')
    await expect(section).toContainText('输入 1200 / 输出 300')
    await expect(section.getByTestId('handbook-llm-error')).toContainText('最近一轮有错误')

    const regenerate = section.getByRole('button', { name: '重新生成 LLM 内容' })
    await regenerate.click()
    await expect(section.getByTestId('handbook-llm-enrich-error')).toContainText('其他仓库的 LLM 增强正在运行')

    enrichStatus = 200
    await regenerate.click()
    await expect(section.getByTestId('handbook-llm-notice')).toContainText('已开始')
    expect(enriched).toEqual(['?full=1', '?full=1'])
  })

  test('handbook 弹窗显示加载中、以接口状态为准并可按 Esc 关闭', async ({ page }) => {
    const snapshot = {
      ...repoA,
      handbook_status: 'ready',
      handbook_commit: repoA.head_commit,
      handbook_version: 3,
    }
    let releasePage: () => void = () => {}
    const pageGate = new Promise<void>((resolve) => (releasePage = resolve))
    await mockRepoRegistry(page, { repos: [snapshot, repoB] })
    await page.route(/\/api\/v1\/repos\/[^/]+\/handbook(\/[^?]*)?(\?.*)?$/, async (route: Route) => {
      const url = new URL(route.request().url())
      if (url.pathname.endsWith('/handbook/page')) {
        await pageGate
        await route.fulfill({ json: { path: 'SKILL.md', content: 'Handbook body' } })
        return
      }
      await route.fulfill({
        json: {
          repo_id: 'r-a',
          status: 'ready',
          commit: 'ffff0000aaaa',
          head_commit: repoA.head_commit,
          version: 3,
          pages: ['SKILL.md'],
        },
      })
    })

    await page.goto('/repos')
    await page.getByRole('button', { name: '查看 cloudgame/svc-a 的 handbook' }).click()
    const dialog = page.getByRole('dialog')
    await expect(dialog.getByTestId('handbook-content')).toHaveText('加载中…')
    await expect(dialog.getByTestId('handbook-dialog-meta')).toContainText('待更新')
    await expect(dialog.getByTestId('handbook-dialog-meta')).toContainText('ffff0000')
    releasePage()
    await expect(dialog.getByTestId('handbook-content')).toHaveText('Handbook body')
    await page.keyboard.press('Escape')
    await expect(dialog).toHaveCount(0)
  })
})
