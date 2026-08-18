import type { CreateToolRequest, Tool, ToolConfig } from '../api/client'

const COPY_SUFFIX_RE = /_copy(\d+)?$/

/** Next unused name: `foo` → `foo_copy` → `foo_copy2`. */
export function nextCopyName(name: string, taken: Iterable<string>): string {
  const takenSet = new Set(taken)
  const trimmed = name.trim() || 'tool'
  const stem = trimmed.replace(COPY_SUFFIX_RE, '') || trimmed
  for (let n = 1; n <= 1000; n++) {
    const candidate = n === 1 ? `${stem}_copy` : `${stem}_copy${n}`
    if (!takenSet.has(candidate)) return candidate
  }
  return `${stem}_copy${Date.now()}`
}

export function buildCopiedTool(tool: Pick<Tool, 'name' | 'description' | 'type' | 'config'>, taken: Iterable<string>): CreateToolRequest {
  const name = nextCopyName(tool.name, taken)
  const type = (tool.type || 'builtin') as CreateToolRequest['type']
  return {
    name,
    description: tool.description ?? '',
    type,
    config: patchCopiedConfig(cloneConfig(tool.config), name),
  }
}

function cloneConfig(config: ToolConfig | undefined): ToolConfig {
  if (!config) return {}
  return JSON.parse(JSON.stringify(config)) as ToolConfig
}

function patchCopiedConfig(config: ToolConfig, newName: string): ToolConfig {
  const ds = config.datasource
  if (!ds) return config
  return {
    ...config,
    datasource: { ...ds, id: newName },
  }
}

export async function copyTool(tool: Pick<Tool, 'name' | 'description' | 'type' | 'config'>, takenNames?: Iterable<string>): Promise<{ id: string; name: string }> {
  const { toolApi } = await import('../api/client')
  const taken = new Set(takenNames ?? [])
  if (taken.size === 0) {
    const pageSize = 100
    let page = 1
    for (;;) {
      const res = await toolApi.list({ page, page_size: pageSize })
      for (const t of res.items || []) taken.add(t.name)
      if (!res.items?.length || taken.size >= res.total) break
      page += 1
      if (page > 1000) break
    }
  }
  const body = buildCopiedTool(tool, taken)
  const created = await toolApi.create(body)
  if (!created.id) {
    throw new Error('复制失败：未返回新工具 ID')
  }
  return { id: created.id, name: created.name || body.name }
}
