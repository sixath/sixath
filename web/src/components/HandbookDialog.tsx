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
  const [viewLoading, setViewLoading] = useState(false)
  const [pageLoading, setPageLoading] = useState(false)
  const repoId = repo?.id ?? ''

  useEffect(() => {
    if (!repoId) return
    let cancelled = false
    setView(null)
    setPage('')
    setContent('')
    setError('')
    setViewLoading(true)
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
      .finally(() => {
        if (!cancelled) setViewLoading(false)
      })
    return () => {
      cancelled = true
    }
  }, [repoId])

  useEffect(() => {
    if (!repoId || !page) return
    let cancelled = false
    setContent('')
    setError('')
    setPageLoading(true)
    repoApi
      .handbookPage(repoId, page)
      .then((p) => {
        if (!cancelled) setContent(p.content)
      })
      .catch((e) => {
        if (!cancelled) setError((e as Error).message)
      })
      .finally(() => {
        if (!cancelled) setPageLoading(false)
      })
    return () => {
      cancelled = true
    }
  }, [repoId, page])

  useEffect(() => {
    if (!repoId) return
    const onKey = (e: KeyboardEvent) => {
      if (e.key === 'Escape') onClose()
    }
    window.addEventListener('keydown', onKey)
    return () => window.removeEventListener('keydown', onKey)
  }, [repoId, onClose])

  if (!repo) return null
  const pages = view ? sortHandbookPages(view.pages) : []
  const loading = viewLoading || pageLoading
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
          <p className="muted" data-testid="handbook-dialog-meta">
            {
              HANDBOOK_STATE_LABELS[
                handbookState({
                  handbook_status: view.status,
                  handbook_commit: view.commit,
                  head_commit: view.head_commit,
                })
              ]
            }{' '}
            · v{view.version} · commit <code title={view.commit}>{shortCommit(view.commit)}</code>
            {view.head_commit && view.head_commit !== view.commit ? (
              <>
                {' '}
                · HEAD <code title={view.head_commit}>{shortCommit(view.head_commit)}</code>
              </>
            ) : null}
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
          <pre className="handbook-dialog__content" data-testid="handbook-content" aria-busy={loading}>
            {loading ? '加载中…' : content}
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
