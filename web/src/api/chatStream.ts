export interface ChatConfirmationRequest {
  id?: string
  kind: string
  title: string
  description: string
  token: string
  dsl: string
  expires_in?: number
  /** Stable resource key for supersede (skill_manage: action:name) */
  resource_key?: string
  /** RFC3339 absolute expiry; preferred over expires_in for countdown */
  expires_at?: string
  severity: 'default' | 'danger'
}

export interface ConfirmResultPayload {
  ok: boolean
  kind: string
  token: string
  error?: string
  error_code?: string
}

export interface ChatInputRequest {
  id?: string
  tool_call_id?: string
  request_id: string
  token: string
  kind: 'text' | 'password' | 'select' | 'confirm'
  field: string
  title: string
  prompt: string
  options?: string[]
  required?: boolean
  expires_in?: number
  severity?: 'default' | 'warning'
}

export interface ChatInputSubmitBody {
  content: string
  input_response: {
    token: string
    request_id: string
    field: string
    value?: string
    cancelled?: boolean
    tool_call_id?: string
  }
}

export function parseConfirmRequiredPayload(payload: unknown): ChatConfirmationRequest | null {
  if (!isRecord(payload)) return null
  const raw = payload.confirmation
  if (!isRecord(raw)) return null

  const kind = stringValue(raw.kind)
  const title = stringValue(raw.title)
  const description = stringValue(raw.description)
  const token = stringValue(raw.token)
  const dsl = stringValue(raw.dsl)

  if (!kind || !title || !description || !token || !dsl) return null

  const id = stringValue(raw.id)
  const severity = raw.severity === 'danger' ? 'danger' : 'default'
  const expires = typeof raw.expires_in === 'number' ? raw.expires_in : undefined
  const resource_key = stringValue(raw.resource_key)
  const expires_at = stringValue(raw.expires_at)

  return {
    ...(id ? { id } : {}),
    kind,
    title,
    description,
    token,
    dsl,
    ...(expires !== undefined ? { expires_in: expires } : {}),
    ...(resource_key ? { resource_key } : {}),
    ...(expires_at ? { expires_at } : {}),
    severity,
  }
}

export function parseConfirmResultPayload(payload: unknown): ConfirmResultPayload | null {
  if (!isRecord(payload)) return null
  const raw = payload.confirm_result
  if (!isRecord(raw)) return null

  const kind = stringValue(raw.kind)
  const token = stringValue(raw.token)
  if (!kind || !token || typeof raw.ok !== 'boolean') return null

  const error = stringValue(raw.error)
  const error_code = stringValue(raw.error_code)

  return {
    ok: raw.ok,
    kind,
    token,
    ...(error ? { error } : {}),
    ...(error_code ? { error_code } : {}),
  }
}

export function buildConfirmMessage(request: ChatConfirmationRequest): string {
  return `Please execute the pending ${request.kind} operation with confirm_token: ${request.token}`
}

export function buildConfirmSubmitBody(request: ChatConfirmationRequest): {
  content: string
  confirm_response: { kind: string; token: string }
} {
  return {
    content: '',
    confirm_response: {
      kind: request.kind,
      token: request.token,
    },
  }
}

export type RestoredConfirmationStatus = 'pending' | 'expired' | 'superseded' | 'failed' | 'confirmed'

/** Confirmation reconstructed from persisted message timeline (page refresh / session reopen). */
export interface RestoredConfirmation extends ChatConfirmationRequest {
  messageKey: string
  status: RestoredConfirmationStatus
  receivedAt: number
  error?: string
}

type TimelineToolLike = {
  kind?: string
  id?: string
  toolName?: string
  tool_name?: string
  result?: unknown
}

type MessageLike = {
  id?: string
  role?: string
  content?: string
  created_at?: string
  createdAt?: string
  metadata?: { timeline?: TimelineToolLike[] } | null
}

const EXPIRED_CONFIRM_HINT = '确认已过期或刷新后未确认，请让 Agent 重新发起该操作'
const CONFIRM_PLACEHOLDER_RE = /^\[confirmed:\s*([^\]]+)\]\s*$/i

/** Rebuild confirm cards from assistant message timelines after reload. */
export function restoreConfirmationsFromMessages(
  messages: MessageLike[],
  nowMs: number = Date.now(),
): RestoredConfirmation[] {
  const collected: RestoredConfirmation[] = []
  const messageIndexByKey = new Map<string, number>()

  for (let idx = 0; idx < messages.length; idx++) {
    const m = messages[idx]
    if (!m || m.role !== 'assistant') continue
    const messageKey = m.id || `${m.created_at || m.createdAt || 'assistant'}-${idx}`
    messageIndexByKey.set(messageKey, idx)
    const createdRaw = m.created_at || m.createdAt || ''
    const createdMs = Date.parse(createdRaw)
    const receivedAt = Number.isNaN(createdMs) ? nowMs : createdMs
    const timeline = m.metadata?.timeline
    if (!Array.isArray(timeline)) continue

    for (const node of timeline) {
      if (!node || node.kind !== 'tool') continue
      const toolName = stringValue(node.toolName) || stringValue(node.tool_name)
      const toolCallId = stringValue(node.id) || `${messageKey}-${toolName}`
      const req = confirmationRequestFromTimelineTool(toolName, node.result, toolCallId)
      if (!req) continue

      let status: RestoredConfirmationStatus = 'pending'
      let error: string | undefined
      const deadline = confirmationDeadlineMs(req, receivedAt)
      if (deadline != null && nowMs >= deadline) {
        status = 'expired'
        error = EXPIRED_CONFIRM_HINT
      }

      collected.push({
        ...req,
        messageKey,
        status,
        receivedAt,
        ...(error ? { error } : {}),
      })
    }
  }

  // skill_manage: same resource_key → older pending become superseded (only among still-pending)
  const lastPendingByResource = new Map<string, string>()
  for (let i = collected.length - 1; i >= 0; i--) {
    const item = collected[i]
    if (!item || item.kind !== 'skill_manage' || item.status !== 'pending') continue
    const key = item.resource_key || item.token
    if (!lastPendingByResource.has(key)) {
      lastPendingByResource.set(key, item.token)
    }
  }
  for (const item of collected) {
    if (item.kind !== 'skill_manage' || item.status !== 'pending') continue
    const key = item.resource_key || item.token
    const keep = lastPendingByResource.get(key)
    if (keep && keep !== item.token) {
      item.status = 'superseded'
      item.error = '已有更新提案'
    }
  }

  applyConfirmOutcomesFromHistory(messages, collected, messageIndexByKey)
  return collected
}

/** Apply later [confirmed: …] outcomes so reload does not revive already-settled cards. */
function applyConfirmOutcomesFromHistory(
  messages: MessageLike[],
  collected: RestoredConfirmation[],
  messageIndexByKey: Map<string, number>,
): void {
  if (collected.length === 0) return

  const proposalIndex = (c: RestoredConfirmation): number => {
    const idx = messageIndexByKey.get(c.messageKey)
    return typeof idx === 'number' ? idx : Number.POSITIVE_INFINITY
  }

  for (let i = 0; i < messages.length; i++) {
    const m = messages[i]
    if (!m || m.role !== 'user') continue
    const kindMatch = CONFIRM_PLACEHOLDER_RE.exec((m.content || '').trim())
    if (!kindMatch) continue
    const kind = kindMatch[1].trim()
    const next = messages[i + 1]
    if (!next || next.role !== 'assistant') continue
    const text = next.content || ''

    // Only settle cards proposed before this confirm click — later re-proposes stay actionable.
    const candidates = collected.filter(
      (c) =>
        c.kind === kind &&
        proposalIndex(c) < i &&
        (c.status === 'pending' || c.status === 'expired' || c.status === 'superseded'),
    )
    if (candidates.length === 0) continue

    const pendingFirst = [...candidates].reverse().find((c) => c.status === 'pending')
    const target = pendingFirst || candidates[candidates.length - 1]
    if (!target) continue

    if (text.includes('技能操作已确认并执行') || text.includes('操作已确认并执行')) {
      target.status = 'confirmed'
      target.error = undefined
      if (target.resource_key) {
        for (const c of collected) {
          if (
            c !== target &&
            c.resource_key === target.resource_key &&
            proposalIndex(c) < i &&
            (c.status === 'pending' || c.status === 'expired' || c.status === 'superseded')
          ) {
            c.status = 'confirmed'
            c.error = undefined
          }
        }
      }
      continue
    }

    if (text.includes('技能确认失败') || text.includes('确认失败')) {
      const err =
        text.replace(/^技能确认失败[：:]\s*/u, '').replace(/^确认失败[：:]\s*/u, '').trim() ||
        '确认失败'
      target.status = 'failed'
      target.error = err
      if (err.includes('已使用') && target.resource_key) {
        for (const c of collected) {
          if (
            c.resource_key === target.resource_key &&
            proposalIndex(c) < i &&
            (c.status === 'pending' || c.status === 'expired')
          ) {
            c.status = 'failed'
            c.error = err
          }
        }
      }
    }
  }
}

function confirmationRequestFromTimelineTool(
  toolName: string,
  result: unknown,
  toolCallId: string,
): ChatConfirmationRequest | null {
  if (!isRecord(result)) return null
  if (stringValue(result.status) !== 'pending') return null
  const token = stringValue(result.token)
  if (!token) return null

  const expires_in = typeof result.expires_in === 'number' ? result.expires_in : undefined
  const expires_at = stringValue(result.expires_at) || undefined

  if (toolName === 'skill_manage') {
    const action = stringValue(result.action)
    const name = stringValue(result.name)
    if (!action || !name) return null
    const preview = stringValue(result.preview) || `${action} skill: ${name}`
    return {
      id: `${toolCallId}:${token}`,
      kind: 'skill_manage',
      title: `Confirm skill ${action}`,
      description: `Review the skill "${name}" before it is applied.`,
      token,
      dsl: preview,
      ...(expires_in !== undefined ? { expires_in } : {}),
      ...(expires_at ? { expires_at } : {}),
      resource_key: `${action}:${name}`,
      severity: 'danger',
    }
  }

  if (toolName === 'execute_write') {
    const dsl = stringValue(result.dsl)
    if (!dsl) return null
    return {
      id: `${toolCallId}:${token}`,
      kind: 'execute_write',
      title: 'Confirm write operation',
      description: 'Review the operation before it is executed.',
      token,
      dsl,
      ...(expires_in !== undefined ? { expires_in } : {}),
      ...(expires_at ? { expires_at } : {}),
      severity: 'danger',
    }
  }

  return null
}

function confirmationDeadlineMs(
  item: Pick<ChatConfirmationRequest, 'expires_at' | 'expires_in'>,
  receivedAt: number,
): number | null {
  if (item.expires_at) {
    const t = Date.parse(item.expires_at)
    if (!Number.isNaN(t)) return t
  }
  if (typeof item.expires_in === 'number' && Number.isFinite(item.expires_in)) {
    return receivedAt + item.expires_in * 1000
  }
  return null
}

export function parseInputRequiredPayload(payload: unknown): ChatInputRequest | null {
  if (!isRecord(payload)) return null
  const raw = payload.input
  if (!isRecord(raw)) return null

  const request_id = stringValue(raw.request_id)
  const token = stringValue(raw.token)
  const kind = stringValue(raw.kind)
  const field = stringValue(raw.field)
  const title = stringValue(raw.title)
  const prompt = stringValue(raw.prompt)
  if (!request_id || !token || !kind || !field || !title || !prompt) return null
  if (kind !== 'text' && kind !== 'password' && kind !== 'select' && kind !== 'confirm') return null

  const id = stringValue(raw.id)
  const tool_call_id = stringValue(raw.tool_call_id)
  const severity = raw.severity === 'warning' ? 'warning' : 'default'
  const required = typeof raw.required === 'boolean' ? raw.required : undefined
  const expires = typeof raw.expires_in === 'number' ? raw.expires_in : undefined
  const options = Array.isArray(raw.options)
    ? raw.options.filter((item): item is string => typeof item === 'string' && item.length > 0)
    : undefined

  return {
    ...(id ? { id } : {}),
    ...(tool_call_id ? { tool_call_id } : {}),
    request_id,
    token,
    kind,
    field,
    title,
    prompt,
    ...(options && options.length > 0 ? { options } : {}),
    ...(required !== undefined ? { required } : {}),
    ...(expires !== undefined ? { expires_in: expires } : {}),
    severity,
  }
}

export function buildInputSubmitBody(input: ChatInputRequest, value: string, cancelled = false): ChatInputSubmitBody {
  return {
    content: '',
    input_response: {
      token: input.token,
      request_id: input.request_id,
      field: input.field,
      ...(input.tool_call_id ? { tool_call_id: input.tool_call_id } : {}),
      ...(cancelled ? { cancelled: true } : { value }),
    },
  }
}

export function inputProvidedLabel(field: string): string {
  return `[input provided: ${field || 'input'}]`
}

export type RestoredInputStatus = 'pending' | 'submitted' | 'cancelled' | 'expired'

/** ask_user card reconstructed from persisted message timeline (reload / reopen). */
export interface RestoredInput extends ChatInputRequest {
  messageKey: string
  status: RestoredInputStatus
  draft: string
  error?: string
}

const EXPIRED_INPUT_HINT = '输入请求已过期，请让 Agent 重新发起'
const INPUT_PROVIDED_RE = /^\[input provided:\s*([^\]]+)\]\s*$/i
const INPUT_CANCELLED_RE = /^\[input cancelled\]\s*$/i

/** Rebuild ask_user input cards from assistant message timelines after reload. */
export function restoreInputsFromMessages(
  messages: MessageLike[],
  nowMs: number = Date.now(),
): RestoredInput[] {
  const collected: RestoredInput[] = []
  const messageIndexByKey = new Map<string, number>()

  for (let idx = 0; idx < messages.length; idx++) {
    const m = messages[idx]
    if (!m || m.role !== 'assistant') continue
    const messageKey = m.id || `${m.created_at || m.createdAt || 'assistant'}-${idx}`
    messageIndexByKey.set(messageKey, idx)
    const createdRaw = m.created_at || m.createdAt || ''
    const createdMs = Date.parse(createdRaw)
    const receivedAt = Number.isNaN(createdMs) ? nowMs : createdMs
    const timeline = m.metadata?.timeline
    if (!Array.isArray(timeline)) continue

    for (const node of timeline) {
      if (!node || node.kind !== 'tool') continue
      const toolName = stringValue(node.toolName) || stringValue(node.tool_name)
      if (toolName !== 'ask_user') continue
      const toolCallId = stringValue(node.id) || `${messageKey}-ask_user`
      const req = inputRequestFromTimelineResult(node.result, toolCallId)
      if (!req) continue

      let status: RestoredInputStatus = 'pending'
      let error: string | undefined
      const deadline = confirmationDeadlineMs(
        { expires_in: req.expires_in },
        receivedAt,
      )
      if (deadline != null && nowMs >= deadline) {
        status = 'expired'
        error = EXPIRED_INPUT_HINT
      }

      collected.push({
        ...req,
        messageKey,
        status,
        draft: '',
        ...(error ? { error } : {}),
      })
    }
  }

  applyInputOutcomesFromHistory(messages, collected, messageIndexByKey)
  return collected
}

function inputRequestFromTimelineResult(
  result: unknown,
  toolCallId: string,
): ChatInputRequest | null {
  if (!isRecord(result)) return null
  if (stringValue(result.status) !== 'pending') return null
  const token = stringValue(result.token)
  const request_id = stringValue(result.request_id)
  const kind = stringValue(result.kind) || 'text'
  const field = stringValue(result.field) || 'input'
  const prompt = stringValue(result.prompt)
  const title = stringValue(result.title) || prompt
  if (!token || !request_id || !prompt) return null
  if (kind !== 'text' && kind !== 'password' && kind !== 'select' && kind !== 'confirm') return null

  const expires_in = typeof result.expires_in === 'number' ? result.expires_in : undefined
  const required = typeof result.required === 'boolean' ? result.required : undefined
  const options = Array.isArray(result.options)
    ? result.options.filter((item): item is string => typeof item === 'string' && item.length > 0)
    : undefined

  return {
    id: `${toolCallId}:${token}`,
    tool_call_id: toolCallId,
    request_id,
    token,
    kind,
    field,
    title,
    prompt,
    ...(options && options.length > 0 ? { options } : {}),
    ...(required !== undefined ? { required } : {}),
    ...(expires_in !== undefined ? { expires_in } : {}),
    severity: kind === 'password' ? 'warning' : 'default',
  }
}

function applyInputOutcomesFromHistory(
  messages: MessageLike[],
  collected: RestoredInput[],
  messageIndexByKey: Map<string, number>,
): void {
  if (collected.length === 0) return

  const proposalIndex = (c: RestoredInput): number => {
    const idx = messageIndexByKey.get(c.messageKey)
    return typeof idx === 'number' ? idx : Number.POSITIVE_INFINITY
  }

  for (let i = 0; i < messages.length; i++) {
    const m = messages[i]
    if (!m || m.role !== 'user') continue
    const text = (m.content || '').trim()
    const provided = INPUT_PROVIDED_RE.exec(text)
    const cancelled = INPUT_CANCELLED_RE.test(text)
    if (!provided && !cancelled) continue

    const candidates = collected.filter(
      (c) =>
        proposalIndex(c) < i &&
        (c.status === 'pending' || c.status === 'expired') &&
        (!provided || c.field === provided[1].trim()),
    )
    if (candidates.length === 0) continue
    const target = candidates[candidates.length - 1]
    if (!target) continue

    if (cancelled) {
      target.status = 'cancelled'
      target.error = undefined
      continue
    }
    target.status = 'submitted'
    target.error = undefined
  }
}

export function shouldTreatStreamErrorAsWarning(error: string, hasAssistantContent: boolean): boolean {
  if (!hasAssistantContent) return false
  return error.includes('context deadline exceeded') || error.includes('context canceled')
}

export interface WebSourceItem {
  title: string
  url: string
  site_name?: string
}

export interface SourcesBrowsedPayload {
  tool: 'web_search' | 'web_extract'
  query?: string
  sources: WebSourceItem[]
}

export function parseSourcesBrowsedPayload(payload: unknown): SourcesBrowsedPayload | null {
  if (!isRecord(payload)) return null
  const raw = payload.sources_browsed
  if (!isRecord(raw)) return null
  const tool = raw.tool as string
  if (tool !== 'web_search' && tool !== 'web_extract') return null
  const sourcesRaw = raw.sources
  if (!Array.isArray(sourcesRaw)) return null
  const sources: WebSourceItem[] = []
  for (const item of sourcesRaw) {
    if (!isRecord(item)) continue
    const title = typeof item.title === 'string' ? item.title : ''
    const url = typeof item.url === 'string' ? item.url : ''
    if (!url) continue
    sources.push({
      title: title || url,
      url,
      site_name: typeof item.site_name === 'string' ? item.site_name : undefined,
    })
  }
  if (sources.length === 0) return null
  return {
    tool,
    query: typeof raw.query === 'string' ? raw.query : undefined,
    sources,
  }
}

export interface ToolCallPayload {
  id: string
  step: number
  phase: 'started' | 'completed' | 'failed'
  tool_name: string
  arguments?: unknown
  result?: unknown
  error?: string
  allowed: boolean
  decision?: string
  duration_ms?: number
  truncated?: boolean
}

export interface ModelCallPayload {
  step: number
  phase: 'invoked' | 'responded'
  mode?: string
  model?: string
  input_tokens?: number
  output_tokens?: number
  message_count?: number
}

export function parseToolCallPayload(d: Record<string, unknown>): ToolCallPayload | null {
  const tc = d.tool_call as Record<string, unknown> | undefined
  if (!tc || typeof tc.id !== 'string' || typeof tc.tool_name !== 'string' || typeof tc.phase !== 'string') return null
  return tc as unknown as ToolCallPayload
}

export function parseModelCallPayload(d: Record<string, unknown>): ModelCallPayload | null {
  const mc = d.model_call as Record<string, unknown> | undefined
  if (!mc || typeof mc.phase !== 'string' || typeof mc.step !== 'number') return null
  return mc as unknown as ModelCallPayload
}

function isRecord(value: unknown): value is Record<string, unknown> {
  return typeof value === 'object' && value !== null
}

function stringValue(value: unknown): string {
  return typeof value === 'string' ? value : ''
}
