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
