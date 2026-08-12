import { toolApi, type CreateToolRequest } from '../api/client'
import {
  toExportItem,
  type ToolExportItem,
} from './toolExportFormat'

export type { ToolExportItem, ToolExportFile } from './toolExportFormat'
export {
  buildExportFile,
  downloadToolsJson,
  parseToolsImportJson,
  toExportItem,
  normalizeExportConfig,
} from './toolExportFormat'

export async function fetchAllToolsForExport(): Promise<ToolExportItem[]> {
  const pageSize = 100
  let page = 1
  const out: ToolExportItem[] = []
  for (;;) {
    const res = await toolApi.list({ page, page_size: pageSize })
    for (const t of res.items || []) {
      out.push(toExportItem(t))
    }
    if (!res.items?.length || out.length >= res.total) break
    page += 1
    if (page > 1000) break
  }
  return out
}

export type ImportDuplicateMode = 'skip' | 'overwrite'

export type ToolImportResult = {
  created: number
  updated: number
  skipped: number
  failed: { name: string; error: string }[]
}

export async function importTools(
  items: ToolExportItem[],
  mode: ImportDuplicateMode,
): Promise<ToolImportResult> {
  const result: ToolImportResult = { created: 0, updated: 0, skipped: 0, failed: [] }

  const byName = new Map<string, string>()
  {
    const pageSize = 100
    let page = 1
    for (;;) {
      const res = await toolApi.list({ page, page_size: pageSize })
      for (const t of res.items || []) {
        byName.set(t.name, t.id)
      }
      if (!res.items?.length || byName.size >= res.total) break
      page += 1
      if (page > 1000) break
    }
  }

  for (const item of items) {
    const body: CreateToolRequest = {
      name: item.name,
      description: item.description,
      type: item.type,
      config: item.config,
    }
    const existingId = byName.get(item.name)
    try {
      if (existingId) {
        if (mode === 'skip') {
          result.skipped += 1
          continue
        }
        await toolApi.update(existingId, body)
        result.updated += 1
      } else {
        const created = await toolApi.create(body)
        result.created += 1
        if (created.id) byName.set(item.name, created.id)
      }
    } catch (e) {
      result.failed.push({ name: item.name, error: (e as Error).message || String(e) })
    }
  }

  return result
}
