import { useEffect, useMemo, useRef, useState } from 'react'
import { Link, useNavigate } from 'react-router-dom'
import { toolApi, type Tool } from '../api/client'
import { copyTool } from '../utils/toolCopy'
import { ConfirmDialog } from '../components/ConfirmDialog'
import {
  downloadToolsJson,
  fetchAllToolsForExport,
  importTools,
  parseToolsImportJson,
  toExportItem,
  type ImportDuplicateMode,
  type ToolImportResult,
} from '../utils/toolImportExport'

function typeDotClass(type: Tool['type']): string {
  if (type === 'datasource') return 'cell-dot--amber'
  if (type === 'mcp') return 'cell-dot--purple'
  return 'cell-dot--cyan'
}

export default function ToolList() {
  const navigate = useNavigate()
  const [tools, setTools] = useState<Tool[]>([])
  const [total, setTotal] = useState(0)
  const [loading, setLoading] = useState(true)
  const [error, setError] = useState('')
  const [pendingDelete, setPendingDelete] = useState<{ id: string; name: string } | null>(null)
  const [confirmLoading, setConfirmLoading] = useState(false)
  const [exporting, setExporting] = useState(false)
  const [importing, setImporting] = useState(false)
  const [importSummary, setImportSummary] = useState<string>('')
  const [selectedIds, setSelectedIds] = useState<Set<string>>(() => new Set())
  const fileInputRef = useRef<HTMLInputElement>(null)
  const selectAllRef = useRef<HTMLInputElement>(null)
  const pendingImportRef = useRef<{ items: Awaited<ReturnType<typeof parseToolsImportJson>>; mode: ImportDuplicateMode } | null>(null)
  const [dupDialogOpen, setDupDialogOpen] = useState(false)
  const [dupCount, setDupCount] = useState(0)
  const [copyingId, setCopyingId] = useState<string | null>(null)

  const selectedCount = selectedIds.size
  const pageSelectedCount = useMemo(
    () => tools.reduce((n, t) => n + (selectedIds.has(t.id) ? 1 : 0), 0),
    [tools, selectedIds],
  )
  const allPageSelected = tools.length > 0 && pageSelectedCount === tools.length
  const somePageSelected = pageSelectedCount > 0 && !allPageSelected

  const reload = async () => {
    const res = await toolApi.list({ page: 1, page_size: 100 })
    setTools(res.items)
    setTotal(res.total)
    setSelectedIds((prev) => {
      const next = new Set<string>()
      for (const id of prev) {
        if (res.items.some((t) => t.id === id)) next.add(id)
      }
      return next
    })
  }

  useEffect(() => {
    toolApi.list({ page: 1, page_size: 100 })
      .then((res) => {
        setTools(res.items)
        setTotal(res.total)
      })
      .catch((e) => setError(e.message))
      .finally(() => setLoading(false))
  }, [])

  useEffect(() => {
    if (selectAllRef.current) {
      selectAllRef.current.indeterminate = somePageSelected
    }
  }, [somePageSelected])

  const toggleOne = (id: string) => {
    setSelectedIds((prev) => {
      const next = new Set(prev)
      if (next.has(id)) next.delete(id)
      else next.add(id)
      return next
    })
  }

  const toggleAllPage = () => {
    setSelectedIds((prev) => {
      const next = new Set(prev)
      if (allPageSelected) {
        for (const t of tools) next.delete(t.id)
      } else {
        for (const t of tools) next.add(t.id)
      }
      return next
    })
  }

  const handleCopy = async (tool: Tool) => {
    setCopyingId(tool.id)
    setImportSummary('')
    try {
      const full = await toolApi.get(tool.id)
      const created = await copyTool(full, tools.map((t) => t.name))
      setImportSummary(`已复制为 ${created.name}`)
      await reload()
      navigate(`/tools/${created.id}/edit`)
    } catch (e) {
      alert((e as Error).message)
    } finally {
      setCopyingId(null)
    }
  }

  const confirmDelete = async () => {
    if (!pendingDelete) return
    setConfirmLoading(true)
    try {
      await toolApi.delete(pendingDelete.id)
      setTools((prev) => prev.filter((t) => t.id !== pendingDelete.id))
      setTotal((prev) => Math.max(0, prev - 1))
      setSelectedIds((prev) => {
        const next = new Set(prev)
        next.delete(pendingDelete.id)
        return next
      })
      setPendingDelete(null)
    } catch (e) {
      alert((e as Error).message)
    } finally {
      setConfirmLoading(false)
    }
  }

  const handleExportSelected = () => {
    const selected = tools.filter((t) => selectedIds.has(t.id))
    if (selected.length === 0) {
      alert('请先勾选要导出的工具')
      return
    }
    setExporting(true)
    setImportSummary('')
    try {
      downloadToolsJson(selected.map(toExportItem), `sixath-tools-selected-${selected.length}.json`)
      setImportSummary(`已导出 ${selected.length} 个工具`)
    } catch (e) {
      alert((e as Error).message)
    } finally {
      setExporting(false)
    }
  }

  const handleExportAll = async () => {
    setExporting(true)
    setImportSummary('')
    try {
      const items = await fetchAllToolsForExport()
      if (items.length === 0) {
        alert('没有可导出的工具')
        return
      }
      downloadToolsJson(items, `sixath-tools-all-${items.length}.json`)
      setImportSummary(`已导出全部 ${items.length} 个工具`)
    } catch (e) {
      alert((e as Error).message)
    } finally {
      setExporting(false)
    }
  }

  const formatImportResult = (r: ToolImportResult) => {
    const parts = [
      `created ${r.created}`,
      `updated ${r.updated}`,
      `skipped ${r.skipped}`,
    ]
    if (r.failed.length) {
      parts.push(`failed ${r.failed.length}`)
    }
    let msg = `Import done: ${parts.join(', ')}.`
    if (r.failed.length) {
      msg += ' Failures: ' + r.failed.map((f) => `${f.name} (${f.error})`).join('; ')
    }
    return msg
  }

  const runImport = async (mode: ImportDuplicateMode, items: Awaited<ReturnType<typeof parseToolsImportJson>>) => {
    setImporting(true)
    setImportSummary('')
    try {
      const result = await importTools(items, mode)
      setImportSummary(formatImportResult(result))
      await reload()
    } catch (e) {
      alert((e as Error).message)
    } finally {
      setImporting(false)
      pendingImportRef.current = null
      setDupDialogOpen(false)
      if (fileInputRef.current) fileInputRef.current.value = ''
    }
  }

  const onPickFile = async (file: File | null) => {
    if (!file) return
    setImportSummary('')
    try {
      const text = await file.text()
      const items = parseToolsImportJson(text)

      const existing = new Set<string>()
      {
        const pageSize = 100
        let page = 1
        for (;;) {
          const res = await toolApi.list({ page, page_size: pageSize })
          for (const t of res.items || []) existing.add(t.name)
          if (!res.items?.length || existing.size >= res.total) break
          page += 1
          if (page > 1000) break
        }
      }
      const overlap = items.filter((t) => existing.has(t.name)).length
      if (overlap > 0) {
        pendingImportRef.current = { items, mode: 'skip' }
        setDupCount(overlap)
        setDupDialogOpen(true)
        return
      }
      await runImport('skip', items)
    } catch (e) {
      alert((e as Error).message)
      if (fileInputRef.current) fileInputRef.current.value = ''
    }
  }

  if (loading) return (
    <div className="loading">
      <div className="loading-spinner" />
      <span style={{ marginLeft: '0.75rem' }}>Loading...</span>
    </div>
  )
  if (error) return <div className="error">Load failed: {error}</div>

  return (
    <div>
      <div className="page-header">
        <div>
          <div className="page-title-row">
            <h1>Tools</h1>
            <span className="page-count">{total}</span>
          </div>
          <p className="page-sub">管理你的 AI 智能体工具和数据源。</p>
        </div>
        <div className="actions">
          <button
            type="button"
            className="btn btn-secondary"
            disabled={exporting || importing}
            onClick={() => fileInputRef.current?.click()}
          >
            {importing ? '导入中…' : '导入 JSON'}
          </button>
          <button
            type="button"
            className="btn btn-secondary"
            disabled={exporting || importing || total === 0}
            onClick={() => void handleExportAll()}
          >
            {exporting ? '导出中…' : '导出全部'}
          </button>
          <input
            ref={fileInputRef}
            type="file"
            accept="application/json,.json"
            style={{ display: 'none' }}
            onChange={(e) => void onPickFile(e.target.files?.[0] ?? null)}
          />
          <Link to="/tools/new" className="btn">新建 Tool</Link>
        </div>
      </div>
      {importSummary ? (
        <div className="section-card" style={{ marginBottom: '1rem', color: 'var(--muted)' }}>
          {importSummary}
        </div>
      ) : null}
      {total === 0 ? (
        <div className="section-card empty-state">
          <p>还没有工具。</p>
          <div className="actions" style={{ justifyContent: 'center' }}>
            <button
              type="button"
              className="btn btn-secondary"
              disabled={importing}
              onClick={() => fileInputRef.current?.click()}
            >
              导入 JSON
            </button>
            <Link to="/tools/new" className="btn">新建 Tool</Link>
          </div>
        </div>
      ) : (
        <div className="table-card table-card--tools">
          <div className="table-toolbar">
            <div className="actions">
              <button
                type="button"
                className="btn btn-secondary btn-sm"
                disabled={exporting || importing || selectedCount === 0}
                onClick={handleExportSelected}
                title={selectedCount === 0 ? '先勾选工具' : `导出已选 ${selectedCount} 个`}
              >
                {exporting ? '导出中…' : '批量导出'}
              </button>
              <span className="table-toolbar__meta">
                {selectedCount > 0 ? `已选择 ${selectedCount} 项` : '已选择 0 项'}
              </span>
              {selectedCount > 0 ? (
                <button
                  type="button"
                  className="btn btn-ghost btn-sm"
                  onClick={() => setSelectedIds(new Set())}
                >
                  清空选择
                </button>
              ) : null}
            </div>
          </div>
          <table>
            <thead>
              <tr>
                <th style={{ width: 44 }}>
                  <label className="checkbox-field" title="全选本页">
                    <input
                      ref={selectAllRef}
                      type="checkbox"
                      checked={allPageSelected}
                      onChange={toggleAllPage}
                      aria-label="全选本页"
                    />
                  </label>
                </th>
                <th>Name</th>
                <th>Type</th>
                <th>Description</th>
                <th className="col-actions">Actions</th>
              </tr>
            </thead>
            <tbody>
              {tools.map((tool) => (
                <tr key={tool.id}>
                  <td>
                    <label className="checkbox-field">
                      <input
                        type="checkbox"
                        checked={selectedIds.has(tool.id)}
                        onChange={() => toggleOne(tool.id)}
                        aria-label={`选择 ${tool.name}`}
                      />
                    </label>
                  </td>
                  <td>
                    <span className="cell-name">
                      <span className={`cell-dot ${typeDotClass(tool.type)}`} aria-hidden />
                      {tool.name}
                    </span>
                  </td>
                  <td><span className={`badge badge-${tool.type}`}>{tool.type}</span></td>
                  <td className="cell-desc">{tool.description}</td>
                  <td className="col-actions">
                    <div className="row-actions">
                      <Link to={`/tools/${tool.id}/edit`} className="btn btn-ghost btn-sm">编辑</Link>
                      <button
                        type="button"
                        className="btn btn-ghost btn-sm"
                        disabled={copyingId === tool.id}
                        onClick={() => void handleCopy(tool)}
                      >
                        {copyingId === tool.id ? '复制中…' : '复制'}
                      </button>
                      <button
                        type="button"
                        className="btn btn-ghost btn-sm btn-danger"
                        onClick={() => setPendingDelete({ id: tool.id, name: tool.name })}
                      >
                        删除
                      </button>
                    </div>
                  </td>
                </tr>
              ))}
            </tbody>
          </table>
        </div>
      )}
      <ConfirmDialog
        open={!!pendingDelete}
        title="Delete tool"
        description={pendingDelete ? `Delete "${pendingDelete.name}"? This action cannot be undone.` : ''}
        confirmLabel="Delete"
        variant="danger"
        loading={confirmLoading}
        onCancel={() => setPendingDelete(null)}
        onConfirm={confirmDelete}
      />
      <ConfirmDialog
        open={dupDialogOpen}
        title="Duplicate tool names"
        description={
          `Found ${dupCount} tool name(s) that already exist. ` +
          'Choose Skip to keep existing tools, or Overwrite to update them from the file.'
        }
        confirmLabel="Overwrite"
        cancelLabel="Skip existing"
        variant="danger"
        loading={importing}
        onCancel={() => {
          const pending = pendingImportRef.current
          if (!pending) {
            setDupDialogOpen(false)
            return
          }
          void runImport('skip', pending.items)
        }}
        onConfirm={() => {
          const pending = pendingImportRef.current
          if (!pending) {
            setDupDialogOpen(false)
            return
          }
          void runImport('overwrite', pending.items)
        }}
      />
    </div>
  )
}
