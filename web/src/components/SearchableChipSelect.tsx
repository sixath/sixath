import { useEffect, useMemo, useRef, useState, type ReactNode } from 'react'
import './SearchableChipSelect.css'

export type SearchableChipOption = {
  value: string
  label: string
  group?: string
}

export function SearchableChipSelect({
  value,
  options,
  onChange,
  disabled = false,
  placeholder = '请选择…',
  searchPlaceholder = '搜索…',
  emptyText = '没有匹配项',
  placement = 'down',
  className = '',
  leading,
}: {
  value: string
  options: SearchableChipOption[]
  onChange: (value: string) => void
  disabled?: boolean
  placeholder?: string
  searchPlaceholder?: string
  emptyText?: string
  placement?: 'up' | 'down'
  className?: string
  leading?: ReactNode
}) {
  const [open, setOpen] = useState(false)
  const [query, setQuery] = useState('')
  const rootRef = useRef<HTMLDivElement>(null)
  const searchRef = useRef<HTMLInputElement>(null)

  const selected = options.find((o) => o.value === value)
  const label = selected?.label || placeholder

  const filtered = useMemo(() => {
    const q = query.trim().toLowerCase()
    if (!q) return options
    return options.filter((o) => {
      const hay = `${o.label} ${o.group ?? ''} ${o.value}`.toLowerCase()
      return hay.includes(q)
    })
  }, [options, query])

  const groups = useMemo(() => {
    const order: string[] = []
    const map = new Map<string, SearchableChipOption[]>()
    for (const item of filtered) {
      const key = item.group || ''
      if (!map.has(key)) {
        map.set(key, [])
        order.push(key)
      }
      map.get(key)!.push(item)
    }
    return order.map((key) => ({ key, items: map.get(key)! }))
  }, [filtered])

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
    if (!open) {
      setQuery('')
      return
    }
    queueMicrotask(() => searchRef.current?.focus())
  }, [open])

  const pick = (next: string) => {
    onChange(next)
    setOpen(false)
  }

  return (
    <div
      className={`chip-select ${placement === 'up' ? 'chip-select--up' : ''} ${open ? 'is-open' : ''}`}
      ref={rootRef}
    >
      <button
        type="button"
        className={`chat-chip ${className}`.trim()}
        disabled={disabled}
        aria-haspopup="listbox"
        aria-expanded={open}
        onClick={() => {
          if (disabled) return
          setOpen((v) => !v)
        }}
      >
        {leading}
        <span className="chat-chip__text">{label}</span>
        <svg className="chat-chip__chevron" viewBox="0 0 24 24" fill="none" stroke="currentColor" aria-hidden>
          <path strokeLinecap="round" strokeLinejoin="round" strokeWidth="2" d="M19 9l-7 7-7-7" />
        </svg>
      </button>
      {open ? (
        <div className="chip-select__panel" role="listbox">
          <div className="chip-select__search">
            <input
              ref={searchRef}
              type="search"
              value={query}
              placeholder={searchPlaceholder}
              onChange={(e) => setQuery(e.target.value)}
              onKeyDown={(e) => e.stopPropagation()}
            />
          </div>
          <div className="chip-select__list">
            {groups.length === 0 ? (
              <div className="chip-select__empty">{emptyText}</div>
            ) : (
              groups.map((group) => (
                <div key={group.key || '__default'} className="chip-select__group">
                  {group.key ? <div className="chip-select__group-label">{group.key}</div> : null}
                  {group.items.map((item) => (
                    <button
                      key={item.value}
                      type="button"
                      role="option"
                      aria-selected={item.value === value}
                      className={`chip-select__option${item.value === value ? ' is-active' : ''}`}
                      onClick={() => pick(item.value)}
                    >
                      {item.label}
                    </button>
                  ))}
                </div>
              ))
            )}
          </div>
        </div>
      ) : null}
    </div>
  )
}
