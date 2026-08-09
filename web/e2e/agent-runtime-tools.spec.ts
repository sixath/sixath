import { expect, test } from '@playwright/test'
import {
  mockAgentCreate,
  mockAgentDetailDeps,
  mockAgentById,
  mockAgentList,
  sampleAgent,
} from './helpers/mock-api'

test.describe('Agent runtime tools (P0) UI', () => {
  test.beforeEach(async ({ page }) => {
    await mockAgentList(page, [])
  })

  test('新建 Agent 页展示运行时工具 checkbox', async ({ page }) => {
    await page.goto('/agents/new')
    await expect(page.getByText('运行时工具（Hermes P0）')).toBeVisible()
    await expect(page.getByTestId('runtime-tool-memory_write_enabled')).toBeVisible()
    await expect(page.getByTestId('runtime-tool-web_tools_enabled')).toBeVisible()
    await expect(page.getByTestId('runtime-tool-terminal_local_enabled')).toBeVisible()
  })

  test('编码助手预设勾选常用工具', async ({ page }) => {
    await page.goto('/agents/new')
    await page.getByTestId('coding-assistant-preset').click()

    await expect(page.getByTestId('runtime-tool-workspace_files_enabled')).toBeChecked()
    await expect(page.getByTestId('runtime-tool-memory_write_enabled')).toBeChecked()
    await expect(page.getByTestId('runtime-tool-skill_runtime_manage_enabled')).toBeChecked()
    await expect(page.getByTestId('runtime-tool-terminal_local_enabled')).toBeChecked()
    await expect(page.getByTestId('runtime-tool-todo_enabled')).toBeChecked()
    await expect(page.getByTestId('runtime-tool-web_tools_enabled')).not.toBeChecked()
  })

  test('保存 Agent 时 POST body 包含 runtime_tools', async ({ page }) => {
    let posted: Record<string, unknown> | null = null
    await mockAgentCreate(page, (body) => {
      posted = body
    })

    await page.goto('/agents/new')
    await page.getByPlaceholder('如 my-agent').fill('e2e-agent')
    await page.getByPlaceholder('/data/agents/my-agent').fill('/data/e2e/workspace')
    await page.getByTestId('coding-assistant-preset').click()
    await page.getByRole('button', { name: '保存' }).click()

    await expect(page).toHaveURL(/\/agents$/)
    expect(posted).not.toBeNull()
    expect(posted!.runtime_tools).toMatchObject({
      workspace_files_enabled: true,
      memory_write_enabled: true,
      skill_runtime_manage_enabled: true,
      terminal_local_enabled: true,
      todo_enabled: true,
    })
  })

  test('Agent 详情页展示已启用运行时工具 badge', async ({ page }) => {
    await mockAgentDetailDeps(page, sampleAgent)
    await page.goto(`/agents/${sampleAgent.id}`)

    await expect(page.getByTestId('runtime-tools-section')).toBeVisible()
    await expect(page.getByTestId('runtime-tools-badges')).toContainText('记忆写入 (memory)')
    await expect(page.getByTestId('runtime-tools-badges')).toContainText('任务列表 (todo)')
    await expect(page.getByTestId('runtime-tools-badges')).toContainText('工作区文件')
  })

  test('编辑 Agent 时加载已保存的 runtime_tools', async ({ page }) => {
    await mockAgentById(page, sampleAgent)
    await page.goto(`/agents/${sampleAgent.id}/edit`)

    await expect(page.getByTestId('runtime-tool-memory_write_enabled')).toBeChecked()
    await expect(page.getByTestId('runtime-tool-todo_enabled')).toBeChecked()
    await expect(page.getByTestId('runtime-tool-workspace_files_enabled')).toBeChecked()
    await expect(page.getByTestId('runtime-tool-web_tools_enabled')).not.toBeChecked()
  })

  test('更新 Agent 时 PUT body 包含修改后的 runtime_tools', async ({ page }) => {
    let updated: Record<string, unknown> | null = null
    await mockAgentList(page, [sampleAgent])
    await mockAgentById(page, sampleAgent, {
      onUpdate: (body) => {
        updated = body
      },
    })

    await page.goto(`/agents/${sampleAgent.id}/edit`)
    await page.getByTestId('runtime-tool-web_tools_enabled').check()
    await page.getByRole('button', { name: '保存' }).click()

    await expect(page).toHaveURL(/\/agents$/)
    expect(updated).not.toBeNull()
    expect(updated!.runtime_tools).toMatchObject({
      web_tools_enabled: true,
      memory_write_enabled: true,
    })
  })
})
