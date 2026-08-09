import type { Tool } from '../api/client'

/** 子串优先；否则按字符顺序跳字匹配（如 cga → cgarchive）。返回 0 表示不匹配。 */
export function fuzzyScore(query: string, text: string): number {
  const q = query.trim().toLowerCase()
  const t = (text || '').toLowerCase()
  if (!q) return 1
  if (!t) return 0

  const idx = t.indexOf(q)
  if (idx >= 0) {
    // 越靠前、越短命中越高
    return 200 - idx - Math.min(40, t.length - q.length)
  }

  let qi = 0
  let gaps = 0
  let last = -1
  for (let i = 0; i < t.length && qi < q.length; i++) {
    if (t[i] === q[qi]) {
      if (last >= 0) gaps += i - last - 1
      last = i
      qi++
    }
  }
  if (qi === q.length) {
    return Math.max(10, 80 - gaps)
  }
  return 0
}

/** 多词（空格分隔）需全部命中；匹配 name / description / type。 */
export function fuzzyScoreTool(query: string, tool: Tool): number {
  const parts = query.trim().toLowerCase().split(/\s+/).filter(Boolean)
  if (parts.length === 0) return 1

  const fields = [tool.name, tool.description ?? '', tool.type]
  let total = 0
  for (const part of parts) {
    let best = 0
    for (const field of fields) {
      best = Math.max(best, fuzzyScore(part, field))
    }
    if (best <= 0) return 0
    total += best
  }
  return total
}

export function fuzzyFilterTools(tools: Tool[], query: string): Tool[] {
  const q = query.trim()
  if (!q) return tools
  return tools
    .map((t) => ({ t, score: fuzzyScoreTool(q, t) }))
    .filter((x) => x.score > 0)
    .sort((a, b) => b.score - a.score || a.t.name.localeCompare(b.t.name))
    .map((x) => x.t)
}
