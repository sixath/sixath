import { useMemo, useState } from 'react'
import type { Repository } from '../api/repoRegistryTypes'
import { REPO_STATUS_LABELS } from '../utils/repoRegistry'

export function RepoPicker({
  repos,
  selected,
  onChange,
}: {
  repos: Repository[]
  selected: string[]
  onChange: (ids: string[]) => void
}) {
  const [query, setQuery] = useState('')
  const chosen = useMemo(() => new Set(selected), [selected])
  const visible = useMemo(() => {
    const q = query.trim().toLowerCase()
    if (!q) return repos
    return repos.filter((r) => `${r.rel_path} ${r.name}`.toLowerCase().includes(q))
  }, [repos, query])

  const toggle = (id: string) => onChange(chosen.has(id) ? selected.filter((x) => x !== id) : [...selected, id])

  return (
    <div className="repo-picker">
      <div className="repo-picker__bar">
        <input value={query} onChange={(e) => setQuery(e.target.value)} placeholder="搜索仓库路径或名称" />
        <span className="muted">已选 {selected.length} 个</span>
      </div>
      <ul className="repo-picker__list">
        {visible.map((r) => (
          <li key={r.id}>
            <label className="repo-picker__item">
              <input type="checkbox" checked={chosen.has(r.id)} onChange={() => toggle(r.id)} />
              <code>{r.rel_path}</code>
              {r.name && r.name !== r.rel_path ? <span className="muted">{r.name}</span> : null}
              {r.status !== 'active' ? (
                <span className={`badge badge-repo-${r.status}`}>{REPO_STATUS_LABELS[r.status]}</span>
              ) : null}
            </label>
          </li>
        ))}
        {visible.length === 0 ? <li className="muted">没有匹配的仓库</li> : null}
      </ul>
    </div>
  )
}
