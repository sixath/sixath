import { useEffect, useId, useMemo, useRef, useState } from 'react'
import type { Tool } from '../api/client'
import { fuzzyFilterTools } from '../utils/fuzzyToolSearch'
import './SearchableToolSelect.css'

export interface SearchableToolSelectProps {
  tools: Tool[]
  value: string
  loading?: boolean
  total?: number
  pageSize?: number
  placeholder?: string
  searchValue: string
  onSearchChange: (q: string) => void
  onChange: (toolId: string) => void
}

export function SearchableToolSelect({
  tools,
  value,
  loading = false,
  total = 0,
  pageSize = 20,
  placeholder = '选择工具...',
  searchValue,
  onSearchChange,
  onChange,
}: SearchableToolSelectProps) {
  const [open, setOpen] = useState(false)
  const rootRef = useRef<HTMLDivElement>(null)
  const searchRef = useRef<HTMLInputElement>(null)
  const listId = useId()

  const filtered = useMemo(
    () => fuzzyFilterTools(tools, searchValue).slice(0, pageSize),
    [tools, searchValue, pageSize],
  )

  const selected =
    tools.find((t) => t.id === value) ??
    filtered.find((t) => t.id === value)
  const label = selected ? `${selected.name} (${selected.type})` : placeholder

  useEffect(() => {
    if (!open) return
    const onDoc = (e: MouseEvent) => {
      if (!rootRef.current?.contains(e.target as Node)) setOpen(false)
    }
    const onKey = (e: KeyboardEvent) => {
      if (e.key === 'Escape') setOpen(false)
    }
    document.addEventListener('mousedown', onDoc)
    document.addEventListener('keydown', onKey)
    return () => {
      document.removeEventListener('mousedown', onDoc)
      document.removeEventListener('keydown', onKey)
    }
  }, [open])

  useEffect(() => {
    if (open) {
      queueMicrotask(() => searchRef.current?.focus())
    }
  }, [open])

  const pick = (id: string) => {
    onChange(id)
    setOpen(false)
  }

  const matchedTotal = searchValue.trim()
    ? fuzzyFilterTools(tools, searchValue).length
    : tools.length

  return (
    <div className="tool-select" ref={rootRef}>
      <button
        type="button"
        className={`tool-select-trigger${open ? ' is-open' : ''}${value ? ' has-value' : ''}`}
        aria-haspopup="listbox"
        aria-expanded={open}
        aria-controls={listId}
        onClick={() => setOpen((v) => !v)}
      >
        <span className="tool-select-label">{label}</span>
        <span className="tool-select-caret" aria-hidden>▾</span>
      </button>

      {open && (
        <div className="tool-select-panel" role="presentation">
          <div className="tool-select-search">
            <input
              ref={searchRef}
              type="search"
              value={searchValue}
              onChange={(e) => onSearchChange(e.target.value)}
              placeholder="模糊搜索名称 / 描述 / 类型..."
              aria-label="模糊搜索工具"
              autoComplete="off"
            />
          </div>
          <ul id={listId} className="tool-select-list" role="listbox">
            {loading && tools.length === 0 ? (
              <li className="tool-select-empty">加载中...</li>
            ) : filtered.length === 0 ? (
              <li className="tool-select-empty">
                {searchValue.trim() ? '无匹配工具' : '暂无可绑定工具'}
              </li>
            ) : (
              filtered.map((t) => (
                <li key={t.id} role="option" aria-selected={t.id === value}>
                  <button
                    type="button"
                    className={`tool-select-option${t.id === value ? ' is-selected' : ''}`}
                    onClick={() => pick(t.id)}
                  >
                    <span className="tool-select-option-name">{t.name}</span>
                    <span className="tool-select-option-type">{t.type}</span>
                  </button>
                </li>
              ))
            )}
          </ul>
          <div className="tool-select-footer">
            {searchValue.trim()
              ? `匹配 ${matchedTotal} 个`
              : `共 ${total || tools.length} 个`}
            {matchedTotal > pageSize ? ` · 显示前 ${pageSize} 个，继续输入缩小范围` : ''}
          </div>
        </div>
      )}
    </div>
  )
}
