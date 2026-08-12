import type { CreateToolRequest, Tool, ToolConfig } from '../api/client'

/** Portable tool document for import/export (no server ids / timestamps). */
export type ToolExportItem = {
  name: string
  description: string
  type: CreateToolRequest['type']
  config: ToolConfig
}

export type ToolExportFile = {
  version: 1
  kind: 'sixath.tools'
  exported_at: string
  tools: ToolExportItem[]
}

const TOOL_TYPES = new Set(['builtin', 'mcp', 'datasource', 'rca'])

/** Local normalize so this module stays free of runtime client imports (node:test). */
export function normalizeExportConfig(raw?: unknown): ToolConfig {
  if (!raw || typeof raw !== 'object') return {}
  const cfg = raw as Record<string, unknown>
  const rcaRaw = cfg.rca
  let rca: ToolConfig['rca'] | undefined
  if (rcaRaw && typeof rcaRaw === 'object') {
    const r = rcaRaw as Record<string, unknown>
    rca = {
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
  return {
    func_path: (cfg.func_path as string | undefined) ?? (cfg.funcPath as string | undefined),
    parameters: (cfg.parameters as Record<string, unknown> | undefined) ?? {},
    async: cfg.async as boolean | undefined,
    mcp_server_id: (cfg.mcp_server_id as string | undefined) ?? (cfg.mcpServerId as string | undefined),
    mcp_endpoint: (cfg.mcp_endpoint as string | undefined) ?? (cfg.mcpEndpoint as string | undefined),
    mcp_backend: (cfg.mcp_backend as string | undefined) ?? (cfg.mcpBackend as string | undefined),
    timeout_sec: (cfg.timeout_sec as number | undefined) ?? (cfg.timeoutSec as number | undefined),
    mcp: cfg.mcp as ToolConfig['mcp'],
    datasource: cfg.datasource as ToolConfig['datasource'],
    rca,
  }
}

export function toExportItem(tool: Pick<Tool, 'name' | 'description' | 'type' | 'config'>): ToolExportItem {
  return {
    name: tool.name,
    description: tool.description ?? '',
    type: tool.type as CreateToolRequest['type'],
    config: normalizeExportConfig(tool.config),
  }
}

export function buildExportFile(tools: ToolExportItem[]): ToolExportFile {
  return {
    version: 1,
    kind: 'sixath.tools',
    exported_at: new Date().toISOString(),
    tools,
  }
}

export function downloadToolsJson(tools: ToolExportItem[], filename?: string) {
  const doc = buildExportFile(tools)
  const blob = new Blob([JSON.stringify(doc, null, 2)], { type: 'application/json;charset=utf-8' })
  const url = URL.createObjectURL(blob)
  const a = document.createElement('a')
  a.href = url
  a.download = filename ?? `sixath-tools-${new Date().toISOString().slice(0, 10)}.json`
  document.body.appendChild(a)
  a.click()
  a.remove()
  URL.revokeObjectURL(url)
}

function isRecord(v: unknown): v is Record<string, unknown> {
  return !!v && typeof v === 'object' && !Array.isArray(v)
}

function parseOneTool(raw: unknown, index: number): ToolExportItem {
  if (!isRecord(raw)) {
    throw new Error(`tools[${index}] must be an object`)
  }
  const name = String(raw.name ?? '').trim()
  if (!name) throw new Error(`tools[${index}].name is required`)
  const type = String(raw.type ?? '').trim()
  if (!TOOL_TYPES.has(type)) {
    throw new Error(`tools[${index}].type must be one of builtin|mcp|datasource|rca`)
  }
  return {
    name,
    description: String(raw.description ?? ''),
    type: type as CreateToolRequest['type'],
    config: normalizeExportConfig(raw.config),
  }
}

/** Accepts `{ tools: [...] }`, a bare array, or a single tool object. */
export function parseToolsImportJson(text: string): ToolExportItem[] {
  let parsed: unknown
  try {
    parsed = JSON.parse(text)
  } catch {
    throw new Error('Invalid JSON file')
  }

  let list: unknown[]
  if (Array.isArray(parsed)) {
    list = parsed
  } else if (isRecord(parsed) && Array.isArray(parsed.tools)) {
    list = parsed.tools
  } else if (isRecord(parsed) && typeof parsed.name === 'string' && typeof parsed.type === 'string') {
    list = [parsed]
  } else {
    throw new Error('Expected { "tools": [...] }, a tool array, or a single tool object')
  }

  if (list.length === 0) throw new Error('No tools found in file')
  return list.map((item, i) => parseOneTool(item, i))
}
