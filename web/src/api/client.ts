import { parseConfirmRequiredPayload, parseConfirmResultPayload, parseInputRequiredPayload, parseSourcesBrowsedPayload, parseToolCallPayload, parseModelCallPayload, shouldTreatStreamErrorAsWarning, type ChatConfirmationRequest, type ConfirmResultPayload, type ChatInputSubmitBody, type ChatInputRequest, type SourcesBrowsedPayload, type WebSourceItem, type ToolCallPayload, type ModelCallPayload } from './chatStream'
import { authHeaders, hasApiToken, handleUnauthorized } from './auth'
import type { TimelineNode } from '../pages/timelineReducer'
import { normalizeTimeline } from '../pages/timelineReducer'

const API_BASE = '/api/v1'

/** 统一响应基类（proto3 JSON 常省略默认 code=0） */
export interface BaseResponse {
  code?: number
  message?: string
  reason?: string
}

function parseApiErrorBody(body: string): { message?: string; reason?: string; code?: number } {
  const raw = body.trim()
  if (!raw) return {}
  try {
    const j = JSON.parse(raw) as { ret?: BaseResponse; message?: string; reason?: string; code?: number }
    if (j?.ret) {
      return {
        message: j.ret.message,
        reason: j.ret.reason,
        code: j.ret.code,
      }
    }
    return { message: j.message, reason: j.reason, code: j.code }
  } catch {
    return { message: raw }
  }
}

function maybeUnauthorized(status: number, body: string): boolean {
  const parsed = parseApiErrorBody(body)
  const code = parsed.code ?? status
  return status === 401 || code === 401 || parsed.reason === 'UNAUTHORIZED'
}

function httpErrorMessage(status: number, body: string): string {
  const parsed = parseApiErrorBody(body)
  const code = parsed.code ?? status
  if (status === 401 || code === 401 || parsed.reason === 'UNAUTHORIZED') {
    return '凭证无效，请重新登录'
  }
  if (status === 403 || code === 403 || parsed.reason === 'FORBIDDEN_PERM') {
    return parsed.message || '权限不足（FORBIDDEN_PERM）'
  }
  return parsed.message || body || `HTTP ${status}`
}

async function request<T>(
  path: string,
  options: RequestInit = {}
): Promise<T> {
  const url = `${API_BASE}${path}`
  const res = await fetch(url, {
    ...options,
    headers: {
      'Content-Type': 'application/json',
      ...authHeaders(),
      ...options.headers,
    },
  })
  if (!res.ok) {
    const err = await res.text()
    if (maybeUnauthorized(res.status, err)) {
      handleUnauthorized()
    }
    throw new Error(httpErrorMessage(res.status, err))
  }
  return res.json() as Promise<T>
}

export { hasApiToken }

/** 检查 ret.code，非 0 时抛出。proto3 JSON 省略 code 时视为 0（成功）。 */
function checkRet<T extends { ret?: BaseResponse }>(data: T): T {
  if (data?.ret) {
    const code = data.ret.code ?? 0
    if (code !== 0) {
      const body = JSON.stringify({ ret: data.ret })
      if (maybeUnauthorized(code, body)) {
        handleUnauthorized()
      }
      throw new Error(httpErrorMessage(code, body))
    }
  }
  return data
}

// Tool API
/** MCP 配置，与 framework/tool.McpConfig 对齐 */
export interface McpConfig {
  endpoint?: string
  id?: string
  backend?: string
}

/** 数据源配置，与 framework/datasource.Config 对齐 */
export interface DatasourceConfig {
  id?: string
  type?: string
  dsn?: string
  host?: string
  port?: number
  user?: string
  password?: string
  dbname?: string
  read_only?: boolean
}

export interface ToolConfig {
  func_path?: string
  parameters?: Record<string, unknown>
  async?: boolean
  mcp_server_id?: string
  mcp_endpoint?: string
  mcp_backend?: string
  timeout_sec?: number
  mcp?: McpConfig
  datasource?: DatasourceConfig
  rca?: {
    func_path?: 'rca_code' | 'rca_symbol' | 'jaeger_trace' | 'es_log_query'
    roots?: string[]
    query_url?: string
    datasource_id?: string
    default_index?: string
    trace_id_field?: string
    gopls_path?: string
    ready_timeout_sec?: number
    request_timeout_sec?: number
  }
}

export interface Tool {
  id: string
  name: string
  description: string
  type: string
  config: ToolConfig
  created_at: string
  updated_at: string
}

export interface CreateToolRequest {
  name: string
  description: string
  type: 'builtin' | 'mcp' | 'datasource' | 'rca'
  config: ToolConfig
}

/** 工具列表响应 */
export interface ToolListResponse {
  ret?: BaseResponse
  items: Tool[]
  total: number
}

/** 工具详情响应 */
export interface ToolResponse {
  ret?: BaseResponse
  id: string
  name: string
  description: string
  type: string
  config: ToolConfig
  created_at: string
  updated_at: string
}

function normalizeToolConfig(raw?: ToolConfig & Record<string, unknown>): ToolConfig {
  if (!raw) return {}
  const cfg = raw as Record<string, unknown>
  return {
    func_path: (cfg.func_path as string | undefined) ?? (cfg.funcPath as string | undefined),
    parameters: (cfg.parameters as Record<string, unknown> | undefined) ?? {},
    async: cfg.async as boolean | undefined,
    mcp_server_id: (cfg.mcp_server_id as string | undefined) ?? (cfg.mcpServerId as string | undefined),
    mcp_endpoint: (cfg.mcp_endpoint as string | undefined) ?? (cfg.mcpEndpoint as string | undefined),
    mcp_backend: (cfg.mcp_backend as string | undefined) ?? (cfg.mcpBackend as string | undefined),
    timeout_sec: (cfg.timeout_sec as number | undefined) ?? (cfg.timeoutSec as number | undefined),
    mcp: cfg.mcp as McpConfig | undefined,
    datasource: cfg.datasource as DatasourceConfig | undefined,
    rca: normalizeRCAConfig(cfg.rca),
  }
}

// normalizeRCAConfig 把后端 protojson 返回的 rca 配置(camelCase 键)统一成前端使用的 snake_case,
// 避免编辑回填后表单再写入 snake_case 键、导致同一对象同时含 func_path 与 funcPath(protojson 解码报 duplicate field)。
function normalizeRCAConfig(raw: unknown): ToolConfig['rca'] | undefined {
  if (!raw || typeof raw !== 'object') return undefined
  const r = raw as Record<string, unknown>
  return {
    func_path: (r.func_path ?? r.funcPath) as ToolConfig['rca'] extends { func_path?: infer F } ? F : never,
    roots: r.roots as string[] | undefined,
    query_url: (r.query_url as string | undefined) ?? (r.queryUrl as string | undefined),
    datasource_id: (r.datasource_id as string | undefined) ?? (r.datasourceId as string | undefined),
    default_index: (r.default_index as string | undefined) ?? (r.defaultIndex as string | undefined),
    trace_id_field: (r.trace_id_field as string | undefined) ?? (r.traceIdField as string | undefined),
    gopls_path: (r.gopls_path as string | undefined) ?? (r.goplsPath as string | undefined),
    ready_timeout_sec: (r.ready_timeout_sec as number | undefined) ?? (r.readyTimeoutSec as number | undefined),
    request_timeout_sec: (r.request_timeout_sec as number | undefined) ?? (r.requestTimeoutSec as number | undefined),
  }
}

function normalizeTool(raw: Tool & Record<string, unknown>): Tool {
  const item = raw as Record<string, unknown>
  return {
    ...(raw as Tool),
    config: normalizeToolConfig(item.config as ToolConfig & Record<string, unknown> | undefined),
    created_at: (item.created_at as string | undefined) ?? (item.createdAt as string | undefined) ?? '',
    updated_at: (item.updated_at as string | undefined) ?? (item.updatedAt as string | undefined) ?? '',
  }
}

export const toolApi = {
  list: async (params?: { page?: number; page_size?: number; name?: string; type?: string }) => {
    const q = new URLSearchParams()
    if (params?.page) q.set('page', String(params.page))
    if (params?.page_size) q.set('page_size', String(params.page_size))
    if (params?.name) q.set('name', params.name)
    if (params?.type) q.set('type', params.type)
    const query = q.toString()
    const data = await request<ToolListResponse>(`/tools${query ? '?' + query : ''}`)
    checkRet(data)
    return {
      ...data,
      items: (data.items || []).map((item) => normalizeTool(item as Tool & Record<string, unknown>)),
    }
  },
  get: async (id: string) => {
    const data = await request<ToolResponse & Record<string, unknown>>(`/tools/${id}`)
    checkRet(data)
    return normalizeTool(data as Tool & Record<string, unknown>) as ToolResponse
  },
  create: async (data: CreateToolRequest) => {
    const res = await request<ToolResponse>('/tools', { method: 'POST', body: JSON.stringify(data) })
    checkRet(res)
    return res
  },
  update: async (id: string, data: Partial<CreateToolRequest>) => {
    const res = await request<ToolResponse>(`/tools/${id}`, { method: 'PUT', body: JSON.stringify(data) })
    checkRet(res)
    return res
  },
  delete: async (id: string) => {
    const res = await request<{ ret?: BaseResponse }>(`/tools/${id}`, { method: 'DELETE' })
    checkRet(res)
  },
}

// MCP Server API
export type McpServer = {
  id: string
  name: string
  description: string
  transport: 'http' | 'stdio'
  endpoint?: string
  backend?: string
  command?: string
  args?: string[]
  env?: Record<string, string>
  timeout_sec?: number
  created_at?: string
  updated_at?: string
}

export type CreateMcpServerRequest = {
  id: string
  name: string
  description?: string
  transport: 'http' | 'stdio'
  endpoint?: string
  backend?: string
  command?: string
  args?: string[]
  env?: Record<string, string>
  timeout_sec?: number
}

export interface McpServerListResponse {
  ret?: BaseResponse
  items: McpServer[]
  total: number
}

export interface McpServerResponse {
  ret?: BaseResponse
  server: McpServer
}

export interface McpServerTestResponse {
  ret?: BaseResponse
  tool_names: string[]
}

function normalizeMcpServer(raw: McpServer & Record<string, unknown>): McpServer {
  const item = raw as Record<string, unknown>
  const transportRaw = String(item.transport ?? 'http')
  const transport: 'http' | 'stdio' = transportRaw === 'stdio' ? 'stdio' : 'http'
  return {
    id: (item.id as string | undefined) ?? '',
    name: (item.name as string | undefined) ?? '',
    description: (item.description as string | undefined) ?? '',
    transport,
    endpoint: (item.endpoint as string | undefined) ?? '',
    backend: (item.backend as string | undefined) ?? '',
    command: (item.command as string | undefined) ?? '',
    args: Array.isArray(item.args) ? (item.args as string[]) : [],
    env: (item.env as Record<string, string> | undefined) ?? {},
    timeout_sec:
      (item.timeout_sec as number | undefined) ?? (item.timeoutSec as number | undefined),
    created_at: (item.created_at as string | undefined) ?? (item.createdAt as string | undefined) ?? '',
    updated_at: (item.updated_at as string | undefined) ?? (item.updatedAt as string | undefined) ?? '',
  }
}

export const mcpServerApi = {
  list: async (params?: { page?: number; page_size?: number; name?: string }) => {
    const q = new URLSearchParams()
    if (params?.page) q.set('page', String(params.page))
    if (params?.page_size) q.set('page_size', String(params.page_size))
    if (params?.name) q.set('name', params.name)
    const query = q.toString()
    const data = await request<McpServerListResponse>(`/mcp-servers${query ? '?' + query : ''}`)
    checkRet(data)
    return {
      ...data,
      items: (data.items || []).map((item) =>
        normalizeMcpServer(item as McpServer & Record<string, unknown>),
      ),
    }
  },
  get: async (id: string) => {
    const data = await request<McpServerResponse & Record<string, unknown>>(`/mcp-servers/${id}`)
    checkRet(data)
    const server = (data.server ?? data) as McpServer & Record<string, unknown>
    return normalizeMcpServer(server)
  },
  create: async (body: CreateMcpServerRequest) => {
    const res = await request<McpServerResponse>('/mcp-servers', {
      method: 'POST',
      body: JSON.stringify(body),
    })
    checkRet(res)
    return normalizeMcpServer((res.server ?? res) as McpServer & Record<string, unknown>)
  },
  update: async (id: string, body: Partial<CreateMcpServerRequest>) => {
    const res = await request<McpServerResponse>(`/mcp-servers/${id}`, {
      method: 'PUT',
      body: JSON.stringify({ ...body, id }),
    })
    checkRet(res)
    return normalizeMcpServer((res.server ?? res) as McpServer & Record<string, unknown>)
  },
  remove: async (id: string) => {
    const res = await request<{ ret?: BaseResponse }>(`/mcp-servers/${id}`, { method: 'DELETE' })
    checkRet(res)
  },
  test: async (id: string) => {
    const res = await request<McpServerTestResponse & Record<string, unknown>>(
      `/mcp-servers/${id}/test`,
      { method: 'POST', body: JSON.stringify({}) },
    )
    checkRet(res)
    const names =
      (res.tool_names as string[] | undefined) ??
      (res.toolNames as string[] | undefined) ??
      []
    return { tool_names: names }
  },
}

// Agent API
export interface ModelConfig {
  provider: string
  model: string
  api_key?: string
  base_url?: string
  /** 单次回复 max_tokens；0 或未设则用服务端默认 8192 */
  max_output_tokens?: number
}

export interface RuntimeToolsConfig {
  memory_write_enabled?: boolean
  skill_runtime_manage_enabled?: boolean
  todo_enabled?: boolean
  workspace_files_enabled?: boolean
  web_tools_enabled?: boolean
  terminal_local_enabled?: boolean
  cronjob_tool_enabled?: boolean
  browser_enabled?: boolean
  /** Memory Hub overrides; omit = process defaults. Not in RUNTIME_TOOL_FIELDS. */
  hub_governance?: string
  hub_knowledge?: string
  hub_fallback_to_default_on_read_error?: boolean
}

export interface MemoryHubCatalog {
  defaults: { governance: string; knowledge: string }
  governance: string[]
  knowledge: string[]
}

type RuntimeToolFlagKey =
  | 'memory_write_enabled'
  | 'skill_runtime_manage_enabled'
  | 'todo_enabled'
  | 'workspace_files_enabled'
  | 'web_tools_enabled'
  | 'terminal_local_enabled'
  | 'cronjob_tool_enabled'
  | 'browser_enabled'

export const RUNTIME_TOOL_FIELDS: { key: RuntimeToolFlagKey; label: string; hint?: string }[] = [
  { key: 'memory_write_enabled', label: '记忆写入 (memory)' },
  { key: 'skill_runtime_manage_enabled', label: '技能管理 (skills_list/view/manage)' },
  { key: 'todo_enabled', label: '任务列表 (todo)' },
  { key: 'workspace_files_enabled', label: '工作区文件 (read/write/patch/search)' },
  { key: 'web_tools_enabled', label: 'Web 搜索 (web_search/extract)', hint: '需服务端配置 BOCHA_API_KEY 或 TAVILY_API_KEY' },
  { key: 'terminal_local_enabled', label: '本地终端 (terminal)', hint: '有安全风险，仅在可信环境启用' },
  { key: 'cronjob_tool_enabled', label: '定时任务 (cronjob)' },
  { key: 'browser_enabled', label: '浏览器 (navigate/snapshot/click/…)', hint: '需本机 Chrome 或设置 BROWSER_CDP_URL；下载默认 deny' },
]

export const CODING_ASSISTANT_RUNTIME_TOOLS: RuntimeToolsConfig = {
  workspace_files_enabled: true,
  memory_write_enabled: true,
  skill_runtime_manage_enabled: true,
  terminal_local_enabled: true,
  todo_enabled: true,
}

export interface Agent {
  id: string
  name: string
  description: string
  system_prompt: string
  model_config: ModelConfig
  workspace: string
  debug_run?: boolean
  runtime_tools?: RuntimeToolsConfig
  tool_ids?: string[]
  toolIds?: string[]
  mcp_server_ids?: string[]
  mcpServerIds?: string[]
  wecom_channel_id?: string
  created_at: string
  updated_at: string
}

/** Agent 列表响应 */
export interface AgentListResponse {
  ret?: BaseResponse
  items: Agent[]
  total: number
}

/** Agent 详情响应 */
export interface AgentResponse {
  ret?: BaseResponse
  id: string
  name: string
  description: string
  system_prompt: string
  model_config: ModelConfig
  workspace: string
  debug_run?: boolean
  runtime_tools?: RuntimeToolsConfig
  tool_ids?: string[]
  toolIds?: string[]
  mcp_server_ids?: string[]
  mcpServerIds?: string[]
  wecom_channel_id?: string
  created_at: string
  updated_at: string
}

/** 技能元数据 */
export interface SkillMeta {
  name: string
  description: string
  path?: string
}

/** 技能列表响应 */
export interface ListSkillsResponse {
  ret?: BaseResponse
  items: SkillMeta[]
}

/** 技能包上传响应 */
export interface UploadSkillPackageResponse {
  ret?: BaseResponse
  success: boolean
  message?: string
}

/** Chat 响应 */
export interface ChatResponse {
  ret?: BaseResponse
  text: string
}

export interface CreateAgentRequest {
  name: string
  description?: string
  system_prompt?: string
  model_config: ModelConfig
  workspace: string
  debug_run?: boolean
  runtime_tools?: RuntimeToolsConfig
  tool_ids?: string[]
  wecom_channel_id?: string
}

function normalizeModelConfig(raw?: ModelConfig & Record<string, unknown>): ModelConfig {
  if (!raw) return { provider: '', model: '' }
  const cfg = raw as Record<string, unknown>
  const maxOut =
    (cfg.max_output_tokens as number | undefined) ??
    (cfg.maxOutputTokens as number | undefined)
  return {
    provider: (cfg.provider as string | undefined) ?? '',
    model: (cfg.model as string | undefined) ?? '',
    api_key: (cfg.api_key as string | undefined) ?? (cfg.apiKey as string | undefined),
    base_url: (cfg.base_url as string | undefined) ?? (cfg.baseUrl as string | undefined),
    max_output_tokens: maxOut && maxOut > 0 ? maxOut : undefined,
  }
}

function normalizeRuntimeTools(raw?: RuntimeToolsConfig | Record<string, unknown>): RuntimeToolsConfig {
  if (!raw) return {}
  const cfg = raw as Record<string, unknown>
  const hubGov = (cfg.hub_governance as string | undefined) ?? (cfg.hubGovernance as string | undefined)
  const hubKnow = (cfg.hub_knowledge as string | undefined) ?? (cfg.hubKnowledge as string | undefined)
  const hubFb =
    (cfg.hub_fallback_to_default_on_read_error as boolean | undefined) ??
    (cfg.hubFallbackToDefaultOnReadError as boolean | undefined)
  const out: RuntimeToolsConfig = {
    memory_write_enabled: (cfg.memory_write_enabled as boolean | undefined) ?? (cfg.memoryWriteEnabled as boolean | undefined),
    skill_runtime_manage_enabled: (cfg.skill_runtime_manage_enabled as boolean | undefined) ?? (cfg.skillRuntimeManageEnabled as boolean | undefined),
    todo_enabled: (cfg.todo_enabled as boolean | undefined) ?? (cfg.todoEnabled as boolean | undefined),
    workspace_files_enabled: (cfg.workspace_files_enabled as boolean | undefined) ?? (cfg.workspaceFilesEnabled as boolean | undefined),
    web_tools_enabled: (cfg.web_tools_enabled as boolean | undefined) ?? (cfg.webToolsEnabled as boolean | undefined),
    terminal_local_enabled: (cfg.terminal_local_enabled as boolean | undefined) ?? (cfg.terminalLocalEnabled as boolean | undefined),
    cronjob_tool_enabled: (cfg.cronjob_tool_enabled as boolean | undefined) ?? (cfg.cronjobToolEnabled as boolean | undefined),
    browser_enabled: (cfg.browser_enabled as boolean | undefined) ?? (cfg.browserEnabled as boolean | undefined),
  }
  if (typeof hubGov === 'string' && hubGov.trim()) out.hub_governance = hubGov.trim()
  if (typeof hubKnow === 'string' && hubKnow.trim()) out.hub_knowledge = hubKnow.trim()
  if (typeof hubFb === 'boolean') out.hub_fallback_to_default_on_read_error = hubFb
  return out
}

/** Explicit true/false for every known flag so PUT never drops browser_enabled via sparse objects. */
export function serializeRuntimeTools(cfg?: RuntimeToolsConfig): RuntimeToolsConfig {
  const n = normalizeRuntimeTools(cfg)
  const out: RuntimeToolsConfig = {}
  for (const { key } of RUNTIME_TOOL_FIELDS) {
    out[key] = !!n[key]
  }
  if (n.hub_governance) out.hub_governance = n.hub_governance
  if (n.hub_knowledge) out.hub_knowledge = n.hub_knowledge
  if (typeof n.hub_fallback_to_default_on_read_error === 'boolean') {
    out.hub_fallback_to_default_on_read_error = n.hub_fallback_to_default_on_read_error
  }
  return out
}


function normalizeAgent(raw: Agent & Record<string, unknown>): Agent {
  const item = raw as Record<string, unknown>
  const modelRaw = (item.model_config ?? item.modelConfig) as ModelConfig & Record<string, unknown> | undefined
  const runtimeRaw = (item.runtime_tools ?? item.runtimeTools) as RuntimeToolsConfig & Record<string, unknown> | undefined
  return {
    id: (item.id as string | undefined) ?? '',
    name: (item.name as string | undefined) ?? '',
    description: (item.description as string | undefined) ?? '',
    system_prompt: (item.system_prompt as string | undefined) ?? (item.systemPrompt as string | undefined) ?? '',
    model_config: normalizeModelConfig(modelRaw),
    workspace: (item.workspace as string | undefined) ?? '',
    debug_run: (item.debug_run as boolean | undefined) ?? (item.debugRun as boolean | undefined),
    runtime_tools: normalizeRuntimeTools(runtimeRaw),
    tool_ids: (item.tool_ids as string[] | undefined) ?? (item.toolIds as string[] | undefined) ?? [],
    mcp_server_ids:
      (item.mcp_server_ids as string[] | undefined) ??
      (item.mcpServerIds as string[] | undefined) ??
      [],
    wecom_channel_id:
      (item.wecom_channel_id as string | undefined) ?? (item.wecomChannelId as string | undefined) ?? '',
    created_at: (item.created_at as string | undefined) ?? (item.createdAt as string | undefined) ?? '',
    updated_at: (item.updated_at as string | undefined) ?? (item.updatedAt as string | undefined) ?? '',
  }
}

export const memoryHubApi = {
  catalog: async (): Promise<MemoryHubCatalog> => {
    const data = await request<MemoryHubCatalog>('/memory-hub/catalog')
    return {
      defaults: {
        governance: data?.defaults?.governance || 'local',
        knowledge: data?.defaults?.knowledge || 'local',
      },
      governance: data?.governance || ['local'],
      knowledge: data?.knowledge || ['local'],
    }
  },
  loadout: async (agentId: string): Promise<HubLoadoutView> => {
    return request<HubLoadoutView>(`/agents/${agentId}/hub/loadout`)
  },
  bindings: async (agentId: string): Promise<HubBindingsView> => {
    return request<HubBindingsView>(`/agents/${agentId}/hub/bindings`)
  },
  bind: async (agentId: string, assets: HubAsset[]) => {
    return request<{ ok: boolean }>(`/agents/${agentId}/hub/bindings`, {
      method: 'POST',
      body: JSON.stringify({ assets }),
    })
  },
  unbind: async (agentId: string, assets: HubAsset[]) => {
    return request<{ ok: boolean }>(`/agents/${agentId}/hub/bindings`, {
      method: 'DELETE',
      body: JSON.stringify({ assets }),
    })
  },
  clearBindings: async (agentId: string) => {
    return request<{ ok: boolean; cleared: number }>(`/agents/${agentId}/hub/bindings/clear`, {
      method: 'POST',
      body: JSON.stringify({}),
    })
  },
  setStatus: async (agentId: string, asset: HubAsset, status: string) => {
    return request<{ ok: boolean }>(`/agents/${agentId}/hub/assets/status`, {
      method: 'POST',
      body: JSON.stringify({ asset, status }),
    })
  },
  listKnowledgeDrafts: (agentId: string, source?: string) =>
    request<{ drafts: KnowledgeDraftItem[] }>(
      `/agents/${agentId}/hub/knowledge/drafts${source ? `?source=${encodeURIComponent(source)}` : ''}`,
    ),
  approveKnowledgeDraft: (agentId: string, body: { source: string; id: string; overwrite?: boolean }) =>
    request<{ ok: boolean }>(`/agents/${agentId}/hub/knowledge/approve`, {
      method: 'POST',
      body: JSON.stringify(body),
    }),
}

export interface KnowledgeDraftItem {
  source: string
  id: string
  title?: string
  preview?: string
  updated_at?: string
}

export interface HubAsset {
  kind: string
  id: string
  hub?: string
  name?: string
  status?: string
}

export interface HubLoadoutView {
  provider: string
  items: HubAsset[]
  total: number
}

export interface HubBindingsView {
  provider: string
  items: HubAsset[]
  total: number
}

export const agentApi = {
  list: async (params?: { page?: number; page_size?: number }) => {
    const q = new URLSearchParams()
    if (params?.page) q.set('page', String(params.page))
    if (params?.page_size) q.set('page_size', String(params.page_size))
    const query = q.toString()
    const data = await request<AgentListResponse>(`/agents${query ? '?' + query : ''}`)
    checkRet(data)
    return {
      ...data,
      items: (data.items || []).map((item) => normalizeAgent(item as Agent & Record<string, unknown>)),
    }
  },
  get: async (id: string) => {
    const data = await request<AgentResponse & Record<string, unknown>>(`/agents/${id}`)
    checkRet(data)
    return normalizeAgent(data as Agent & Record<string, unknown>) as AgentResponse
  },
  create: async (data: CreateAgentRequest) => {
    const res = await request<AgentResponse & Record<string, unknown>>('/agents', { method: 'POST', body: JSON.stringify(data) })
    checkRet(res)
    return normalizeAgent(res as Agent & Record<string, unknown>) as AgentResponse
  },
  update: async (id: string, data: Partial<CreateAgentRequest>) => {
    const res = await request<AgentResponse & Record<string, unknown>>(`/agents/${id}`, { method: 'PUT', body: JSON.stringify(data) })
    checkRet(res)
    return normalizeAgent(res as Agent & Record<string, unknown>) as AgentResponse
  },
  delete: async (id: string) => {
    const res = await request<{ ret?: BaseResponse }>(`/agents/${id}`, { method: 'DELETE' })
    checkRet(res)
  },
  bindTools: async (id: string, toolIds: string[]) => {
    const res = await request<{ ret?: BaseResponse }>(`/agents/${id}/tools`, {
      method: 'POST',
      body: JSON.stringify({ id, tool_ids: toolIds }),
    })
    checkRet(res)
  },
  unbindTools: async (id: string, toolIds: string[]) => {
    const q = toolIds.map((t) => `tool_ids=${encodeURIComponent(t)}`).join('&')
    const res = await request<{ ret?: BaseResponse }>(`/agents/${id}/tools?${q}`, { method: 'DELETE' })
    checkRet(res)
  },
  bindMcpServers: async (id: string, serverIds: string[]) => {
    const res = await request<{ ret?: BaseResponse }>(`/agents/${id}/mcp-servers`, {
      method: 'POST',
      body: JSON.stringify({ server_ids: serverIds }),
    })
    checkRet(res)
  },
  unbindMcpServers: async (id: string, serverIds: string[]) => {
    const q = `server_ids=${encodeURIComponent(serverIds.join(','))}`
    const res = await request<{ ret?: BaseResponse }>(`/agents/${id}/mcp-servers?${q}`, {
      method: 'DELETE',
    })
    checkRet(res)
  },
  listSkills: async (id: string) => {
    const data = await request<ListSkillsResponse>(`/agents/${id}/skills`)
    checkRet(data)
    return data
  },
  deleteSkill: async (id: string, skillName: string) => {
    const res = await request<{ ret?: BaseResponse }>(`/agents/${id}/skills/${encodeURIComponent(skillName)}`, {
      method: 'DELETE',
    })
    checkRet(res)
  },
  uploadSkillPackage: async (id: string, file: File) => {
    const buf = await file.arrayBuffer()
    const bytes = new Uint8Array(buf)
    let binary = ''
    for (let i = 0; i < bytes.length; i++) binary += String.fromCharCode(bytes[i])
    const base64 = btoa(binary)
    const res = await fetch(`${API_BASE}/agents/${id}/skills/upload`, {
      method: 'POST',
      headers: {
        'Content-Type': 'application/json',
        ...authHeaders(),
      },
      body: JSON.stringify({ file: base64 }),
    })
    if (!res.ok) {
      const err = await res.text()
      if (maybeUnauthorized(res.status, err)) {
        handleUnauthorized()
      }
      throw new Error(httpErrorMessage(res.status, err))
    }
    const data = (await res.json()) as UploadSkillPackageResponse
    checkRet(data)
    return data
  },
  chat: async (id: string, content: string) => {
    const data = await request<ChatResponse>(`/agents/${id}/chat`, {
      method: 'POST',
      body: JSON.stringify({ id, content }),
    })
    checkRet(data)
    return data
  },
}

// Chat 会话与消息 API
export const DEFAULT_SESSION_TITLE = '新对话'
export const SESSION_LIST_PAGE_SIZE = 50

export interface ChatSession {
  id: string
  agent_id: string
  title: string
  created_at: string
  updated_at: string
  parent_session_id?: string
  preview?: string
  agent_name?: string
}

/** 对齐 chat.v1.SearchHitReply（snake_case 字段） */
export interface SessionSearchHit {
  session_id: string
  root_session_id: string
  agent_id: string
  agent_name: string
  title: string
  preview: string
  matched_snippets: string[]
  updated_at: string
}

export interface ChatMessage {
  id: string
  session_id: string
  role: 'user' | 'assistant' | 'system'
  content: string
  created_at: string
  metadata?: {
    sources?: WebSourceItem[]
    sixath_origin?: string
    compact_summary_hash?: string
    /** Finalize 后的执行时间线；刷新后从 listMessages 回放 */
    timeline?: TimelineNode[]
  }
}

export function normalizeSession(raw: Record<string, unknown>): ChatSession {
  return {
    id: (raw.id as string | undefined) ?? '',
    agent_id: (raw.agent_id as string | undefined) ?? (raw.agentId as string | undefined) ?? '',
    title: (raw.title as string | undefined) ?? '',
    created_at: (raw.created_at as string | undefined) ?? (raw.createdAt as string | undefined) ?? '',
    updated_at: (raw.updated_at as string | undefined) ?? (raw.updatedAt as string | undefined) ?? '',
    parent_session_id:
      (raw.parent_session_id as string | undefined) ?? (raw.parentSessionId as string | undefined),
    preview: raw.preview as string | undefined,
    agent_name:
      (raw.agent_name as string | undefined) ?? (raw.agentName as string | undefined),
  }
}

function normalizeSessionSearchHit(raw: Record<string, unknown>): SessionSearchHit {
  return {
    session_id: (raw.session_id as string | undefined) ?? (raw.sessionId as string | undefined) ?? '',
    root_session_id:
      (raw.root_session_id as string | undefined) ?? (raw.rootSessionId as string | undefined) ?? '',
    agent_id: (raw.agent_id as string | undefined) ?? (raw.agentId as string | undefined) ?? '',
    agent_name: (raw.agent_name as string | undefined) ?? (raw.agentName as string | undefined) ?? '',
    title: (raw.title as string | undefined) ?? '',
    preview: (raw.preview as string | undefined) ?? '',
    matched_snippets:
      (raw.matched_snippets as string[] | undefined) ??
      (raw.matchedSnippets as string[] | undefined) ??
      [],
    updated_at: (raw.updated_at as string | undefined) ?? (raw.updatedAt as string | undefined) ?? '',
  }
}

function normalizeMessageMetadata(raw: unknown): ChatMessage['metadata'] | undefined {
  if (!raw || typeof raw !== 'object') return undefined
  const md = raw as Record<string, unknown>
  const sixathOrigin =
    typeof md.sixath_origin === 'string'
      ? md.sixath_origin
      : typeof md['sixath.origin'] === 'string'
        ? (md['sixath.origin'] as string)
        : typeof md.sixathOrigin === 'string'
          ? md.sixathOrigin
          : undefined
  const compactSummaryHash =
    typeof md.compact_summary_hash === 'string'
      ? md.compact_summary_hash
      : typeof md.compactSummaryHash === 'string'
        ? md.compactSummaryHash
        : undefined

  let sources: WebSourceItem[] | undefined
  const sourcesRaw = md.sources
  if (Array.isArray(sourcesRaw) && sourcesRaw.length > 0) {
    const parsed: WebSourceItem[] = []
    for (const item of sourcesRaw) {
      if (!item || typeof item !== 'object') continue
      const row = item as Record<string, unknown>
      const url = typeof row.url === 'string' ? row.url : ''
      if (!url) continue
      const title = typeof row.title === 'string' ? row.title : url
      parsed.push({
        title,
        url,
        site_name: typeof row.site_name === 'string' ? row.site_name : undefined,
      })
    }
    if (parsed.length > 0) sources = parsed
  }

  const timeline = normalizeTimeline(md.timeline)

  if (!sources && !sixathOrigin && !compactSummaryHash && !timeline) return undefined
  return {
    sources,
    sixath_origin: sixathOrigin,
    compact_summary_hash: compactSummaryHash,
    timeline,
  }
}

function normalizeChatMessage(raw: Record<string, unknown>): ChatMessage {
  const role = raw.role as string | undefined
  const normalizedRole =
    role === 'user' || role === 'assistant' || role === 'system' ? role : ('user' as const)
  return {
    id: (raw.id as string | undefined) ?? '',
    session_id:
      (raw.session_id as string | undefined) ?? (raw.sessionId as string | undefined) ?? '',
    role: normalizedRole,
    content: (raw.content as string | undefined) ?? '',
    created_at: (raw.created_at as string | undefined) ?? (raw.createdAt as string | undefined) ?? '',
    metadata: normalizeMessageMetadata(raw.metadata),
  }
}

export const chatApi = {
  createSession: async (
    agentId: string,
    titleOrOpts?: string | { title?: string; parentSessionId?: string }
  ): Promise<ChatSession> => {
    let title: string | undefined
    let parentSessionId: string | undefined
    if (typeof titleOrOpts === 'string') {
      title = titleOrOpts
    } else if (titleOrOpts && typeof titleOrOpts === 'object') {
      title = titleOrOpts.title
      parentSessionId = titleOrOpts.parentSessionId
    }
    const body: Record<string, unknown> = {
      agent_id: agentId,
      title: title || DEFAULT_SESSION_TITLE,
    }
    if (parentSessionId) body.parent_session_id = parentSessionId
    const data = await request<Record<string, unknown> & { ret?: BaseResponse }>(
      `/agents/${agentId}/sessions`,
      { method: 'POST', body: JSON.stringify(body) }
    )
    checkRet(data)
    return normalizeSession(data)
  },
  listSessions: async (
    agentId: string,
    opts?: { page?: number; pageSize?: number; q?: string; includePreview?: boolean }
  ) => {
    const q = new URLSearchParams()
    q.set('page', String(opts?.page ?? 1))
    q.set('page_size', String(opts?.pageSize ?? SESSION_LIST_PAGE_SIZE))
    if (opts?.q) q.set('q', opts.q)
    if (opts?.includePreview !== undefined) q.set('include_preview', String(opts.includePreview))
    const query = q.toString()
    const data = await request<{ ret?: BaseResponse; items?: Record<string, unknown>[]; total: number }>(
      `/agents/${agentId}/sessions${query ? '?' + query : ''}`
    )
    checkRet(data)
    return {
      ...data,
      items: (data.items ?? []).map((item) => normalizeSession(item)),
    }
  },
  listAllSessions: async (opts?: {
    page?: number
    pageSize?: number
    includePreview?: boolean
  }) => {
    const q = new URLSearchParams()
    q.set('page', String(opts?.page ?? 1))
    q.set('page_size', String(opts?.pageSize ?? SESSION_LIST_PAGE_SIZE))
    if (opts?.includePreview !== undefined) q.set('include_preview', String(opts.includePreview))
    const query = q.toString()
    const data = await request<{ ret?: BaseResponse; items?: Record<string, unknown>[]; total: number }>(
      `/sessions${query ? '?' + query : ''}`
    )
    checkRet(data)
    return {
      ...data,
      items: (data.items ?? []).map((item) => normalizeSession(item)),
    }
  },
  searchSessions: async (opts: { query: string; agentId?: string; limit?: number }) => {
    const q = new URLSearchParams()
    q.set('query', opts.query)
    if (opts.agentId) q.set('agent_id', opts.agentId)
    if (opts.limit != null) q.set('limit', String(opts.limit))
    const query = q.toString()
    const data = await request<{ ret?: BaseResponse; items?: Record<string, unknown>[] }>(
      `/sessions/search${query ? '?' + query : ''}`
    )
    checkRet(data)
    return {
      items: (data.items ?? []).map((item) => normalizeSessionSearchHit(item)),
    }
  },
  listMessages: async (sessionId: string, limit = 100) => {
    const q = new URLSearchParams()
    q.set('limit', String(limit))
    const data = await request<{ ret?: BaseResponse; items?: Record<string, unknown>[] }>(
      `/sessions/${sessionId}/messages?${q.toString()}`
    )
    checkRet(data)
    return {
      ...data,
      items: (data.items ?? []).map((item) => normalizeChatMessage(item)),
    }
  },
  getSession: async (id: string) => {
    const data = await request<Record<string, unknown> & { ret?: BaseResponse }>(`/sessions/${id}`)
    checkRet(data)
    return normalizeSession(data)
  },
  updateSession: async (id: string, title: string) => {
    const data = await request<ChatSession & { ret?: BaseResponse }>(`/sessions/${id}`, {
      method: 'PUT',
      body: JSON.stringify({ id, title }),
    })
    checkRet(data)
    return data
  },
  deleteSession: async (id: string) => {
    const data = await request<{ ret?: BaseResponse }>(`/sessions/${id}`, { method: 'DELETE' })
    checkRet(data)
  },
  /** 流式发送消息，onChunk 收到增量，onDone 结束，onError 错误；返回 AbortController 用于停止 */
  sendMessageStream: (
    sessionId: string,
    content: string,
    callbacks: {
      onChunk: (text: string) => void
      onDone: () => void
      onError: (err: string) => void
      onConfirmRequired?: (confirmation: ChatConfirmationRequest) => void
      onConfirmResult?: (result: ConfirmResultPayload) => void
      onInputRequired?: (input: ChatInputRequest) => void
      onSourcesBrowsed?: (payload: SourcesBrowsedPayload) => void
      onToolCall?: (payload: ToolCallPayload) => void
      onModelCall?: (payload: ModelCallPayload) => void
      onDebug?: (text: string) => void
    },
    options?: {
      input_response?: ChatInputSubmitBody['input_response']
      confirm_response?: { kind: string; token: string }
    }
  ): AbortController => {
    const ac = new AbortController()
    const body: Record<string, unknown> = { content }
    if (options?.input_response) {
      body.input_response = options.input_response
    }
    if (options?.confirm_response) {
      body.confirm_response = options.confirm_response
    }
    fetch(`${API_BASE}/sessions/${sessionId}/messages/stream`, {
      method: 'POST',
      headers: {
        'Content-Type': 'application/json',
        'Accept': 'text/event-stream',
        ...authHeaders(),
      },
      body: JSON.stringify(body),
      signal: ac.signal,
    })
    .then(async (res) => {
    if (!res.ok) {
      const err = await res.text()
      if (maybeUnauthorized(res.status, err)) {
        handleUnauthorized()
      }
      callbacks.onError(httpErrorMessage(res.status, err))
      return
    }
    const reader = res.body?.getReader()
    if (!reader) {
      callbacks.onError('No response body')
      return
    }
    const dec = new TextDecoder()
    let buf = ''
    let curEvent = ''
    let hasAssistantContent = false
    let settled = false
    const finish = () => {
      if (settled) return
      settled = true
      callbacks.onDone()
    }
    const fail = (err: string) => {
      if (settled) return
      settled = true
      callbacks.onError(err)
    }
    try {
      while (true) {
        const { done, value } = await reader.read()
        if (done) break
        buf += dec.decode(value, { stream: true })
        const lines = buf.split('\n')
        buf = lines.pop() || ''
        for (const line of lines) {
          if (line.startsWith('event: ')) curEvent = line.slice(7).trim()
          else if (line.startsWith('data: ')) {
            try {
              const d = JSON.parse(line.slice(6)) as Record<string, unknown>
              if (curEvent === 'chunk' && typeof d.content === 'string') {
                if (d.content) hasAssistantContent = true
                callbacks.onChunk(d.content)
              }
              else if (curEvent === 'done') {
                finish()
                await reader.cancel().catch(() => {})
                return
              }
              else if (curEvent === 'error') {
                const err = (d.error as string) || 'Unknown error'
                if (shouldTreatStreamErrorAsWarning(err, hasAssistantContent)) {
                  finish()
                } else {
                  fail(err)
                }
                await reader.cancel().catch(() => {})
                return
              }
              else if (curEvent === 'debug' && typeof d.content === 'string') callbacks.onDebug?.(d.content)
              else if (curEvent === 'confirm_required') {
                const confirmation = parseConfirmRequiredPayload(d)
                if (confirmation) callbacks.onConfirmRequired?.(confirmation)
              }
              else if (curEvent === 'confirm_result') {
                const result = parseConfirmResultPayload(d)
                if (result) callbacks.onConfirmResult?.(result)
              }
              else if (curEvent === 'input_required') {
                const input = parseInputRequiredPayload(d)
                if (input) callbacks.onInputRequired?.(input)
              }
              else if (curEvent === 'sources_browsed') {
                const payload = parseSourcesBrowsedPayload(d)
                if (payload) callbacks.onSourcesBrowsed?.(payload)
              }
              else if (curEvent === 'tool_call') {
                const tc = parseToolCallPayload(d)
                if (tc) callbacks.onToolCall?.(tc)
              }
              else if (curEvent === 'model_call') {
                const mc = parseModelCallPayload(d)
                if (mc) callbacks.onModelCall?.(mc)
              }
            } catch (_) {}
          }
        }
      }
      finish()
    } catch (e) {
      if ((e as Error).name === 'AbortError') return
      const err = (e as Error).message
      if (shouldTreatStreamErrorAsWarning(err, hasAssistantContent)) finish()
      else fail(err)
    }
    })
    .catch((e) => {
      if ((e as Error).name === 'AbortError') return
      const err = (e as Error).message
      if (shouldTreatStreamErrorAsWarning(err, false)) callbacks.onDone()
      else callbacks.onError(err)
    })
    return ac
  },
  /** Phase 2: soft-hide this message and all later turns; reload listMessages after. */
  rewindSession: async (sessionId: string, messageId: string) => {
    const data = await request<{
      ret?: BaseResponse
      session_id?: string
      rewind_count?: number
      deactivated_messages?: string[]
      deactivated_traces?: string[]
    }>(`/sessions/${sessionId}/rewind`, {
      method: 'POST',
      body: JSON.stringify({ message_id: messageId }),
    })
    checkRet(data)
    return {
      session_id: data.session_id ?? sessionId,
      rewind_count: data.rewind_count ?? 0,
      deactivated_messages: data.deactivated_messages ?? [],
      deactivated_traces: data.deactivated_traces ?? [],
    }
  },
  /** Phase 2: aggregate turn_traces insights for an agent. */
  getInsights: async (agentId: string, opts?: { from?: string; to?: string }) => {
    const q = new URLSearchParams()
    if (opts?.from) q.set('from', opts.from)
    if (opts?.to) q.set('to', opts.to)
    const query = q.toString()
    const data = await request<AgentInsights & { ret?: BaseResponse }>(
      `/agents/${agentId}/insights${query ? '?' + query : ''}`
    )
    checkRet(data)
    return data
  },
}

export interface AgentInsights {
  agent_id: string
  from: string
  to: string
  turns: number
  tool_calls: number
  error_calls: number
  error_rate: number
  blocked_calls: number
  top_tools: { name: string; calls: number; errors: number }[]
  top_sessions: { session_id: string; turns: number; errors: number }[]
  truncated?: boolean
}

// Channel API
export interface Channel {
  id: string
  channel_id: string
  type: string
  default_agent: string
  allowed_agents?: string[]
  enabled: boolean
  webhook_path?: string
  webhook_url_masked?: string
  ip_whitelist?: string[]
  default_uids?: string[]
  created_at: string
  updated_at: string
}

export interface ChannelListResponse {
  ret?: BaseResponse
  items: Channel[]
  total: number
}

export interface CreateChannelRequest {
  channel_id: string
  type: 'web' | 'api' | 'webhook' | 'wxpusher' | 'wecom'
  default_agent?: string
  allowed_agents?: string[]
  enabled?: boolean
  webhook_path?: string
  webhook_secret?: string
  webhook_url?: string
  ip_whitelist?: string[]
  app_token?: string
  default_uids?: string[]
}

function normalizeChannel(raw: Channel & Record<string, unknown>): Channel {
  return {
    id: (raw.id as string) ?? '',
    channel_id: (raw.channel_id as string) ?? (raw.channelId as string) ?? '',
    type: (raw.type as string) ?? '',
    default_agent: (raw.default_agent as string) ?? (raw.defaultAgent as string) ?? '',
    allowed_agents:
      (raw.allowed_agents as string[] | undefined) ?? (raw.allowedAgents as string[] | undefined),
    enabled: (raw.enabled as boolean) ?? false,
    webhook_path: (raw.webhook_path as string) ?? (raw.webhookPath as string),
    webhook_url_masked: (raw.webhook_url_masked as string) ?? (raw.webhookUrlMasked as string),
    ip_whitelist: (raw.ip_whitelist as string[]) ?? (raw.ipWhitelist as string[]),
    default_uids: (raw.default_uids as string[]) ?? (raw.defaultUids as string[]),
    created_at: (raw.created_at as string) ?? (raw.createdAt as string) ?? '',
    updated_at: (raw.updated_at as string) ?? (raw.updatedAt as string) ?? '',
  }
}

export const channelApi = {
  list: async (params?: { page?: number; page_size?: number; type?: string; enabled?: boolean }) => {
    const q = new URLSearchParams()
    if (params?.page) q.set('page', String(params.page))
    if (params?.page_size) q.set('page_size', String(params.page_size))
    if (params?.type) q.set('type', params.type)
    if (params?.enabled !== undefined) q.set('enabled', String(params.enabled))
    const query = q.toString()
    const data = await request<ChannelListResponse>(`/channels${query ? '?' + query : ''}`)
    checkRet(data)
    return {
      ...data,
      items: (data.items || []).map((item) => normalizeChannel(item as Channel & Record<string, unknown>)),
    }
  },
  get: async (id: string) => {
    const data = await request<Channel & { ret?: BaseResponse }>(`/channels/${id}`)
    checkRet(data)
    return normalizeChannel(data as Channel & Record<string, unknown>)
  },
  create: async (data: CreateChannelRequest) => {
    const res = await request<Channel & { ret?: BaseResponse }>('/channels', { method: 'POST', body: JSON.stringify(data) })
    checkRet(res)
    return normalizeChannel(res as Channel & Record<string, unknown>)
  },
  update: async (id: string, data: Partial<CreateChannelRequest> & Record<string, unknown>) => {
    const res = await request<Channel & { ret?: BaseResponse }>(`/channels/${id}`, {
      method: 'PUT',
      body: JSON.stringify({ updates: data }),
    })
    checkRet(res)
    return normalizeChannel(res as Channel & Record<string, unknown>)
  },
  delete: async (id: string) => {
    const res = await request<{ ret?: BaseResponse }>(`/channels/${id}`, { method: 'DELETE' })
    checkRet(res)
  },
  send: async (id: string, data: { content: string; summary?: string; uids?: string[] }) => {
    const res = await request<{ ret?: BaseResponse }>(`/channels/${id}/send`, {
      method: 'POST',
      body: JSON.stringify(data),
    })
    checkRet(res)
  },
}

// Cron Task API
export interface CronTask {
  id: string
  name: string
  agent_id: string
  schedule_kind: string
  schedule_expr: string
  timezone: string
  stagger_sec: number
  payload_kind: string
  payload_content: string
  timeout_sec: number
  retry_count: number
  retry_interval_sec: number
  delivery_mode: string
  delivery_webhook_url?: string
  delivery_session_id?: string
  delivery_channel_id?: string
  enabled: boolean
  next_run_at?: string
  created_at: string
  updated_at: string
}

export interface CronRun {
  id: string
  task_id: string
  triggered_at: string
  started_at: string
  finished_at?: string
  status: string
  output_summary?: string
  error?: string
  delivery_ok?: boolean
}

export interface CronTaskListResponse {
  ret?: BaseResponse
  items: CronTask[]
  total: number
}

export interface CronRunListResponse {
  ret?: BaseResponse
  items: CronRun[]
  total: number
}

export interface CreateCronTaskRequest {
  name: string
  agent_id: string
  schedule_kind: 'cron' | 'every' | 'at'
  schedule_expr: string
  timezone?: string
  stagger_sec?: number
  payload_kind: 'agent_turn' | 'skill_execute'
  payload_content: string
  timeout_sec?: number
  retry_count?: number
  retry_interval_sec?: number
  delivery_mode?: 'none' | 'webhook' | 'session' | 'channel'
  delivery_webhook_url?: string
  delivery_secret?: string
  delivery_best_effort?: boolean
  delivery_session_id?: string
  delivery_channel_id?: string
  enabled?: boolean
}

function normalizeCronTask(raw: CronTask & Record<string, unknown>): CronTask {
  return {
    id: (raw.id as string | undefined) ?? '',
    name: (raw.name as string | undefined) ?? '',
    agent_id: (raw.agent_id as string | undefined) ?? (raw.agentId as string | undefined) ?? '',
    schedule_kind: (raw.schedule_kind as string | undefined) ?? (raw.scheduleKind as string | undefined) ?? '',
    schedule_expr: (raw.schedule_expr as string | undefined) ?? (raw.scheduleExpr as string | undefined) ?? '',
    timezone: (raw.timezone as string | undefined) ?? '',
    stagger_sec: (raw.stagger_sec as number | undefined) ?? (raw.staggerSec as number | undefined) ?? 0,
    payload_kind: (raw.payload_kind as string | undefined) ?? (raw.payloadKind as string | undefined) ?? '',
    payload_content: (raw.payload_content as string | undefined) ?? (raw.payloadContent as string | undefined) ?? '',
    timeout_sec: (raw.timeout_sec as number | undefined) ?? (raw.timeoutSec as number | undefined) ?? 0,
    retry_count: (raw.retry_count as number | undefined) ?? (raw.retryCount as number | undefined) ?? 0,
    retry_interval_sec:
      (raw.retry_interval_sec as number | undefined) ?? (raw.retryIntervalSec as number | undefined) ?? 0,
    delivery_mode: (raw.delivery_mode as string | undefined) ?? (raw.deliveryMode as string | undefined) ?? '',
    delivery_webhook_url:
      (raw.delivery_webhook_url as string | undefined) ?? (raw.deliveryWebhookUrl as string | undefined),
    delivery_session_id:
      (raw.delivery_session_id as string | undefined) ?? (raw.deliverySessionId as string | undefined),
    delivery_channel_id:
      (raw.delivery_channel_id as string | undefined) ?? (raw.deliveryChannelId as string | undefined),
    enabled: (raw.enabled as boolean | undefined) ?? false,
    next_run_at: (raw.next_run_at as string | undefined) ?? (raw.nextRunAt as string | undefined),
    created_at: (raw.created_at as string | undefined) ?? (raw.createdAt as string | undefined) ?? '',
    updated_at: (raw.updated_at as string | undefined) ?? (raw.updatedAt as string | undefined) ?? '',
  }
}

function normalizeCronRun(raw: CronRun & Record<string, unknown>): CronRun {
  return {
    id: (raw.id as string | undefined) ?? '',
    task_id: (raw.task_id as string | undefined) ?? (raw.taskId as string | undefined) ?? '',
    triggered_at: (raw.triggered_at as string | undefined) ?? (raw.triggeredAt as string | undefined) ?? '',
    started_at: (raw.started_at as string | undefined) ?? (raw.startedAt as string | undefined) ?? '',
    finished_at: (raw.finished_at as string | undefined) ?? (raw.finishedAt as string | undefined),
    status: (raw.status as string | undefined) ?? '',
    output_summary: (raw.output_summary as string | undefined) ?? (raw.outputSummary as string | undefined),
    error: raw.error as string | undefined,
    delivery_ok: (raw.delivery_ok as boolean | undefined) ?? (raw.deliveryOk as boolean | undefined),
  }
}

export const cronApi = {
  list: async (params?: { page?: number; page_size?: number; agent_id?: string; enabled?: boolean }) => {
    const q = new URLSearchParams()
    if (params?.page) q.set('page', String(params.page))
    if (params?.page_size) q.set('page_size', String(params.page_size))
    if (params?.agent_id) q.set('agent_id', params.agent_id)
    if (params?.enabled !== undefined) q.set('enabled', String(params.enabled))
    const query = q.toString()
    const data = await request<CronTaskListResponse & { items?: Array<CronTask & Record<string, unknown>> }>(
      `/cron/tasks${query ? '?' + query : ''}`
    )
    checkRet(data)
    return {
      ...data,
      items: (data.items || []).map((item) => normalizeCronTask(item as CronTask & Record<string, unknown>)),
    }
  },
  get: async (id: string) => {
    const data = await request<CronTask & { ret?: BaseResponse } & Record<string, unknown>>(`/cron/tasks/${id}`)
    checkRet(data)
    return normalizeCronTask(data)
  },
  create: async (data: CreateCronTaskRequest) => {
    const res = await request<CronTask & { ret?: BaseResponse } & Record<string, unknown>>('/cron/tasks', {
      method: 'POST',
      body: JSON.stringify(data),
    })
    checkRet(res)
    return normalizeCronTask(res)
  },
  update: async (id: string, data: Partial<CreateCronTaskRequest> & Record<string, unknown>) => {
    const res = await request<CronTask & { ret?: BaseResponse } & Record<string, unknown>>(`/cron/tasks/${id}`, {
      method: 'PUT',
      body: JSON.stringify({ updates: data }),
    })
    checkRet(res)
    return normalizeCronTask(res)
  },
  delete: async (id: string) => {
    const res = await request<{ ret?: BaseResponse }>(`/cron/tasks/${id}`, { method: 'DELETE' })
    checkRet(res)
  },
  run: async (id: string) => {
    const res = await request<{ ret?: BaseResponse }>(`/cron/tasks/${id}/run`, { method: 'POST' })
    checkRet(res)
  },
  listRuns: async (taskId: string, params?: { page?: number; page_size?: number }) => {
    const q = new URLSearchParams()
    if (params?.page) q.set('page', String(params.page))
    if (params?.page_size) q.set('page_size', String(params.page_size))
    const query = q.toString()
    const data = await request<CronRunListResponse & { items?: Array<CronRun & Record<string, unknown>> }>(
      `/cron/tasks/${taskId}/runs${query ? '?' + query : ''}`
    )
    checkRet(data)
    return {
      ...data,
      items: (data.items || []).map((item) => normalizeCronRun(item as CronRun & Record<string, unknown>)),
    }
  },
}
