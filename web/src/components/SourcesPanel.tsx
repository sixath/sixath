import { useMemo, useState } from 'react'
import type { WebSourceItem } from '../api/chatStream'
import './SourcesPanel.css'

interface SourcesPanelProps {
  sources: WebSourceItem[]
}

export function SourcesPanel({ sources }: SourcesPanelProps) {
  const [open, setOpen] = useState(false)
  const unique = useMemo(() => {
    const seen = new Set<string>()
    const out: WebSourceItem[] = []
    for (const s of sources) {
      if (!s.url || seen.has(s.url)) continue
      seen.add(s.url)
      out.push(s)
    }
    return out
  }, [sources])

  if (unique.length === 0) return null

  return (
    <div className="sources-panel">
      <button type="button" className="sources-panel-toggle" onClick={() => setOpen((v) => !v)}>
        <span>已浏览 {unique.length} 个网页</span>
        <span aria-hidden>{open ? '▾' : '▸'}</span>
      </button>
      {open && (
        <ul className="sources-panel-list">
          {unique.map((s) => (
            <li key={s.url}>
              <a href={s.url} target="_blank" rel="noopener noreferrer" className="sources-panel-link">
                <span className="sources-panel-ext" aria-hidden>↗</span>
                {s.title}
              </a>
              {s.site_name ? <span className="sources-panel-site">{s.site_name}</span> : null}
            </li>
          ))}
        </ul>
      )}
    </div>
  )
}
