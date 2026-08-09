import { chromium } from '@playwright/test'
import { mkdirSync } from 'node:fs'
import { dirname, join } from 'node:path'
import { fileURLToPath } from 'node:url'

const url =
  process.argv[2] ??
  'http://localhost:5173/?agent=a3af7bc6-6888-4dde-b782-ef2bfcb04df1&session=2feb2c57-e972-40dc-9792-aad0c72e87b7'

const outDir = join(dirname(fileURLToPath(import.meta.url)), '..', 'output', 'playwright')
mkdirSync(outDir, { recursive: true })
const screenshotPath = join(outDir, 'debug-chat-layout.png')

const browser = await chromium.launch({ headless: true })
const page = await browser.newPage({ viewport: { width: 1280, height: 800 } })

try {
  const resp = await page.goto(url, { waitUntil: 'networkidle', timeout: 30_000 })
  await page.waitForSelector('.session-sidebar-list, .chat-home-agent-select', { timeout: 15_000 })
  await page.waitForTimeout(1500)

  const metrics = await page.evaluate(() => {
    const sidebar = document.querySelector('.session-sidebar')
    const sidebarList = document.querySelector('.session-sidebar-list')
    const chatHomeMain = document.querySelector('.chat-home-main')
    const chatInput =
      document.querySelector('.chat-input-wrap textarea') ??
      document.querySelector('.chat-input textarea') ??
      document.querySelector('textarea')
    const content = document.querySelector('.content')
    const chatPage = document.querySelector('.chat-page')

    const rect = (el) => (el ? el.getBoundingClientRect() : null)

    return {
      title: document.title,
      url: location.href,
      viewport: { w: innerWidth, h: innerHeight },
      pageScrollHeight: document.documentElement.scrollHeight,
      pageCanScroll: document.documentElement.scrollHeight > innerHeight + 8,
      bodyOverflowY: getComputedStyle(document.body).overflowY,
      content: content
        ? {
            overflow: getComputedStyle(content).overflow,
            overflowY: getComputedStyle(content).overflowY,
            height: content.clientHeight,
            scrollHeight: content.scrollHeight,
          }
        : null,
      chatHomeMain: chatHomeMain
        ? {
            height: chatHomeMain.clientHeight,
            overflow: getComputedStyle(chatHomeMain).overflow,
          }
        : null,
      sidebar: sidebar
        ? {
            height: sidebar.clientHeight,
            maxHeight: getComputedStyle(sidebar).maxHeight,
            overflow: getComputedStyle(sidebar).overflow,
          }
        : null,
      sidebarList: sidebarList
        ? {
            itemCount: sidebarList.querySelectorAll('.session-sidebar-item').length,
            clientHeight: sidebarList.clientHeight,
            scrollHeight: sidebarList.scrollHeight,
            canScroll: sidebarList.scrollHeight > sidebarList.clientHeight + 8,
            overflowY: getComputedStyle(sidebarList).overflowY,
          }
        : null,
      chatPage: chatPage ? { height: chatPage.clientHeight } : null,
      chatInputVisible: chatInput
        ? {
            bottom: rect(chatInput).bottom,
            inViewport: rect(chatInput).bottom <= innerHeight && rect(chatInput).top >= 0,
          }
        : null,
    }
  })

  await page.screenshot({ path: screenshotPath, fullPage: false })

  const pass =
    !metrics.pageCanScroll &&
    metrics.sidebarList?.canScroll === true &&
    metrics.chatInputVisible?.inViewport === true

  console.log('=== Playwright layout check ===')
  console.log(JSON.stringify(metrics, null, 2))
  console.log('screenshot:', screenshotPath)
  console.log('PASS:', pass)
  process.exitCode = pass ? 0 : 1
} catch (err) {
  console.error('FAIL:', err.message)
  await page.screenshot({ path: join(outDir, 'debug-chat-layout-error.png'), fullPage: true }).catch(() => {})
  process.exitCode = 2
} finally {
  await browser.close()
}
