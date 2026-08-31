import type { TimelineNode } from '../pages/timelineReducer'

export type SpillCandidate = {
  path: string
  count: number
  toolName: string
}

function toolResultObject(result: unknown): Record<string, unknown> | null {
  if (result && typeof result === 'object' && !Array.isArray(result)) {
    return result as Record<string, unknown>
  }
  if (typeof result === 'string') {
    try {
      const parsed = JSON.parse(result) as unknown
      if (parsed && typeof parsed === 'object' && !Array.isArray(parsed)) {
        return parsed as Record<string, unknown>
      }
    } catch {
      return null
    }
  }
  return null
}

export function collectSpillCandidates(nodes: TimelineNode[]): SpillCandidate[] {
  const out: SpillCandidate[] = []
  for (const n of nodes) {
    if (n.kind !== 'tool') continue
    const obj = toolResultObject(n.result)
    if (!obj) continue
    const path = typeof obj.path === 'string' ? obj.path.replace(/\\/g, '/') : ''
    if (!path.includes('tmp/results/') || !path.toLowerCase().endsWith('.jsonl')) continue
    const count = typeof obj.count === 'number' ? obj.count : 0
    out.push({ path, count, toolName: n.toolName })
  }
  return out
}

export function pickSpillPath(sessionId: string, claimed: number, nodes: TimelineNode[]): string | null {
  const sid = sessionId.trim()
  if (!sid) return null
  const prefix = `tmp/results/${sid}/`
  const cands = collectSpillCandidates(nodes).filter(
    (c) => c.path.startsWith(prefix) && !c.path.includes('..'),
  )
  if (!cands.length) return null
  const scripts = cands.filter((c) => c.toolName === 'run_result_script')
  const pool = scripts.length ? scripts : cands
  const notPage = pool.filter((c) => c.count !== 500)
  const ranked = (notPage.length ? notPage : pool).slice()
  ranked.sort((a, b) => {
    if (claimed > 0) return Math.abs(a.count - claimed) - Math.abs(b.count - claimed)
    return b.count - a.count
  })
  return ranked[0]?.path ?? null
}

const MAPPING_RE = /^(\S+)\s+.*?vmid=(\d+)\s*,\s*gid=(\d+)\s*$/i

export type SpillTable = {
  columns: string[]
  rows: string[][]
}

export function parseSpillItems(items: Record<string, unknown>[]): SpillTable {
  const mapping: string[][] = []
  for (const item of items) {
    const line = typeof item.line === 'string' ? item.line.trim() : ''
    const m = line.match(MAPPING_RE)
    if (m) mapping.push([m[1], m[2], m[3]])
  }
  if (mapping.length >= 1 && mapping.length * 2 >= items.length) {
    return { columns: ['flowId', 'vmid', 'gid'], rows: mapping }
  }
  if (!items.length) return { columns: [], rows: [] }
  const columns = Object.keys(items[0])
  return {
    columns,
    rows: items.map((item) => columns.map((c) => stringifyCell(item[c]))),
  }
}

function stringifyCell(v: unknown): string {
  if (v == null) return ''
  if (typeof v === 'string' || typeof v === 'number' || typeof v === 'boolean') return String(v)
  try {
    return JSON.stringify(v)
  } catch {
    return String(v)
  }
}

export function dropIncompleteMarkdownTable(content: string): string {
  const lines = content.split('\n')
  const out: string[] = []
  for (let i = 0; i < lines.length; i++) {
    const t = lines[i].trim()
    const next = (lines[i + 1] ?? '').trim()
    const isHeader = t.startsWith('|') && t.endsWith('|') && /^\|[\s:|-]+\|$/.test(next)
    if (isHeader) {
      i++
      while (i + 1 < lines.length) {
        const row = lines[i + 1].trim()
        if (row.startsWith('|') && row.endsWith('|')) {
          i++
          continue
        }
        break
      }
      continue
    }
    out.push(lines[i])
  }
  return out.join('\n').replace(/\n{3,}/g, '\n\n').trim()
}
