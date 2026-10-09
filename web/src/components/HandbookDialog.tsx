import { useEffect, useState } from 'react'
import { repoApi } from '../api/repoRegistry'
import type { HandbookView, Repository } from '../api/repoRegistryTypes'
import { HANDBOOK_STATE_LABELS, handbookState, shortCommit, sortHandbookPages } from '../utils/repoRegistry'
import './ConfirmDialog.css'

interface HandbookDialogProps {
  repo: Repository | null
  onClose: () => void
}

export function HandbookDialog({ repo, onClose }: HandbookDialogProps) {
  const [view, setView] = useState<HandbookView | null>(null)
  const [page, setPage] = useState('')
  const [content, setContent] = useState('')
  const [error, setError] = useState('')
  const repoId = repo?.id ?? ''

  useEffect(() => {
    if (!repoId) return
    let cancelled = false
    setView(null)
    setPage('')
    setContent('')
    setError('')
    repoApi
      .handbook(repoId)
      .then((v) => {
        if (cancelled) return
        setView(v)
        setPage(sortHandbookPages(v.pages)[0] ?? '')
      })
      .catch((e) => {
        if (!cancelled) setError((e as Error).message)
      })
    return () => {
      cancelled = true
    }
  }, [repoId])

  useEffect(() => {
    if (!repoId || !page) return
    let cancelled = false
    setContent('')
    repoApi
      .handbookPage(repoId, page)
      .then((p) => {
        if (!cancelled) setContent(p.content)
      })
      .catch((e) => {
        if (!cancelled) setError((e as Error).message)
      })
    return () => {
      cancelled = true
    }
  }, [repoId, page])

  if (!repo) return null
  const pages = view ? sortHandbookPages(view.pages) : []
  return (
    <div className="confirm-dialog-backdrop" role="presentation">
      <div
        className="confirm-dialog form-dialog handbook-dialog"
        role="dialog"
        aria-modal="true"
        aria-labelledby="handbook-dialog-title"
      >
        <h2 id="handbook-dialog-title">Handbook · {repo.rel_path}</h2>
        {view ? (
          <p className="muted">
            {HANDBOOK_STATE_LABELS[handbookState(repo)]} · v{view.version} · commit{' '}
            <code>{shortCommit(view.commit)}</code>
          </p>
        ) : null}
        {error ? <div className="error">{error}</div> : null}
        <div className="handbook-dialog__body">
          <ul className="handbook-dialog__pages" aria-label="handbook 页面">
            {pages.map((p) => (
              <li key={p}>
                <button
                  type="button"
                  className={`btn btn-ghost btn-sm ${p === page ? 'active' : ''}`}
                  aria-pressed={p === page}
                  onClick={() => setPage(p)}
                >
                  {p}
                </button>
              </li>
            ))}
          </ul>
          <pre className="handbook-dialog__content" data-testid="handbook-content">
            {content}
          </pre>
        </div>
        <div className="confirm-dialog-actions">
          <button type="button" className="btn btn-secondary" onClick={onClose}>
            关闭
          </button>
        </div>
      </div>
    </div>
  )
}
