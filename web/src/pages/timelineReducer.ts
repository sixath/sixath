import type { ToolCallPayload, ModelCallPayload } from '../api/chatStream.ts'

export type ToolNode = {
  kind: 'tool'
  id: string
  step: number
  seq: number
  phase: 'started' | 'completed' | 'failed' | 'interrupted'
  toolName: string
  arguments?: unknown
  result?: unknown
  error?: string
  allowed: boolean
  decision?: string
  durationMs?: number
  truncated?: boolean
}

export type ModelNode = {
  kind: 'model'
  step: number
  seq: number
  phase: 'invoked' | 'responded' | 'interrupted'
  mode?: string
  model?: string
  inputTokens?: number
  outputTokens?: number
  messageCount?: number
}

export type TimelineNode = ToolNode | ModelNode

const toolPhases = new Set(['started', 'completed', 'failed', 'interrupted'])
const modelPhases = new Set(['invoked', 'responded', 'interrupted'])

/** Parse persisted metadata.timeline into TimelineNode[] (refresh replay). */
export function normalizeTimeline(raw: unknown): TimelineNode[] | undefined {
  if (!Array.isArray(raw) || raw.length === 0) return undefined
  const out: TimelineNode[] = []
  for (const item of raw) {
    if (!item || typeof item !== 'object') continue
    const row = item as Record<string, unknown>
    const kind = row.kind
    const step = typeof row.step === 'number' ? row.step : Number(row.step)
    const seq = typeof row.seq === 'number' ? row.seq : Number(row.seq) || 0
    if (!Number.isFinite(step)) continue
    if (kind === 'tool') {
      const id = typeof row.id === 'string' ? row.id : ''
      const toolName = typeof row.toolName === 'string' ? row.toolName : typeof row.tool_name === 'string' ? row.tool_name : ''
      const phase = typeof row.phase === 'string' && toolPhases.has(row.phase) ? row.phase as ToolNode['phase'] : 'completed'
      if (!id || !toolName) continue
      out.push({
        kind: 'tool',
        id,
        step,
        seq,
        phase,
        toolName,
        arguments: row.arguments,
        result: row.result,
        error: typeof row.error === 'string' ? row.error : undefined,
        allowed: row.allowed !== false,
        decision: typeof row.decision === 'string' ? row.decision : undefined,
        durationMs: typeof row.durationMs === 'number' ? row.durationMs : typeof row.duration_ms === 'number' ? row.duration_ms : undefined,
        truncated: row.truncated === true,
      })
      continue
    }
    if (kind === 'model') {
      const phase = typeof row.phase === 'string' && modelPhases.has(row.phase) ? row.phase as ModelNode['phase'] : 'responded'
      out.push({
        kind: 'model',
        step,
        seq,
        phase,
        mode: typeof row.mode === 'string' ? row.mode : undefined,
        model: typeof row.model === 'string' ? row.model : undefined,
        inputTokens: typeof row.inputTokens === 'number' ? row.inputTokens : typeof row.input_tokens === 'number' ? row.input_tokens : undefined,
        outputTokens: typeof row.outputTokens === 'number' ? row.outputTokens : typeof row.output_tokens === 'number' ? row.output_tokens : undefined,
        messageCount: typeof row.messageCount === 'number' ? row.messageCount : typeof row.message_count === 'number' ? row.message_count : undefined,
      })
    }
  }
  return out.length > 0 ? out : undefined
}

let seqCounter = 0
const nextSeq = () => ++seqCounter

function sortNodes(nodes: TimelineNode[]): TimelineNode[] {
  return [...nodes].sort((a, b) => (a.step - b.step) || (a.seq - b.seq))
}

export function applyToolCall(nodes: TimelineNode[], p: ToolCallPayload): TimelineNode[] {
  const idx = nodes.findIndex((n) => n.kind === 'tool' && n.id === p.id)
  if (idx >= 0) {
    const prev = nodes[idx] as ToolNode
    const merged: ToolNode = {
      ...prev,
      phase: p.phase,
      toolName: p.tool_name,
      arguments: p.arguments ?? prev.arguments,
      result: p.result ?? prev.result,
      error: p.error ?? prev.error,
      allowed: p.allowed,
      decision: p.decision ?? prev.decision,
      durationMs: p.duration_ms ?? prev.durationMs,
      truncated: p.truncated ?? prev.truncated,
    }
    const copy = [...nodes]
    copy[idx] = merged
    return sortNodes(copy)
  }
  const node: ToolNode = {
    kind: 'tool', id: p.id, step: p.step, seq: nextSeq(),
    phase: p.phase, toolName: p.tool_name, arguments: p.arguments,
    result: p.result, error: p.error, allowed: p.allowed,
    decision: p.decision, durationMs: p.duration_ms, truncated: p.truncated,
  }
  return sortNodes([...nodes, node])
}

export function applyModelCall(nodes: TimelineNode[], p: ModelCallPayload): TimelineNode[] {
  const idx = nodes.findIndex((n) => n.kind === 'model' && n.step === p.step)
  if (idx >= 0) {
    const prev = nodes[idx] as ModelNode
    const merged: ModelNode = {
      ...prev,
      phase: p.phase,
      mode: p.mode ?? prev.mode,
      model: p.model ?? prev.model,
      inputTokens: p.input_tokens ?? prev.inputTokens,
      outputTokens: p.output_tokens ?? prev.outputTokens,
      messageCount: p.message_count ?? prev.messageCount,
    }
    const copy = [...nodes]
    copy[idx] = merged
    return sortNodes(copy)
  }
  const node: ModelNode = {
    kind: 'model', step: p.step, seq: nextSeq(), phase: p.phase,
    mode: p.mode, model: p.model, inputTokens: p.input_tokens,
    outputTokens: p.output_tokens, messageCount: p.message_count,
  }
  return sortNodes([...nodes, node])
}

export function finalizeTimeline(nodes: TimelineNode[]): TimelineNode[] {
  return nodes.map((n) => {
    if (n.kind === 'tool' && n.phase === 'started') return { ...n, phase: 'interrupted' as const }
    if (n.kind === 'model' && n.phase === 'invoked') return { ...n, phase: 'interrupted' as const }
    return n
  })
}
