/** Detect assistant markdown tables that claim more rows than were actually generated. */

const CLAIM_RE = /（(\d+)\s*条）/g
const SEP_RE = /^\|[\s:|-]+\|$/

export function countMarkdownTableBodyRows(content: string): number {
  const lines = content.split(/\r?\n/)
  let seenSep = false
  let body = 0
  for (const line of lines) {
    const t = line.trim()
    const isRow = t.startsWith('|') && t.endsWith('|') && t.length > 1
    if (!isRow) {
      seenSep = false
      continue
    }
    if (SEP_RE.test(t)) {
      seenSep = true
      continue
    }
    if (seenSep) body++
  }
  return body
}

export function claimedTableSize(content: string): number | null {
  let max = 0
  for (const m of content.matchAll(CLAIM_RE)) {
    const n = Number(m[1])
    if (Number.isFinite(n) && n > max) max = n
  }
  return max >= 20 ? max : null
}

export function truncatedTableHint(content: string): string | null {
  const claimed = claimedTableSize(content)
  if (claimed == null) return null
  const body = countMarkdownTableBodyRows(content)
  if (body >= claimed) return null
  if (body <= 0) return null
  return `标题写 ${claimed} 条，表格只生成了 ${body} 行（回复在输出上限处被截断）。完整结果请让 Agent 写入 workspace 文件，不要把几百行贴进对话。`
}
