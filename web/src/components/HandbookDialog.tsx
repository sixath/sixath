import { useCallback, useEffect, useState } from 'react'
import { repoApi } from '../api/repoRegistry'
import type { HandbookView, Repository } from '../api/repoRegistryTypes'
import {
  HANDBOOK_STATE_LABELS,
  LLM_FALLBACK_REASON_LABELS,
  LLM_STATE_LABELS,
  enrichErrorMessage,
  handbookState,
  handbookViewLLMState,
  llmProgress,
  shortCommit,
  sortHandbookPages,
} from '../utils/repoRegistry'
import './ConfirmDialog.css'

const LLM_POLL_MS = 3000

interface HandbookDialogProps {
  repo: Repository | null
  onClose: () => void
  onEnrichStarted?: () => void
}

function formatTime(iso?: string): string {
  if (!iso) return ''
  const d = new Date(iso)
  return Number.isNaN(d.getTime()) ? iso : d.toLocaleString()
}

function HandbookLLMSection({
  view,
  enriching,
  notice,
  enrichError,
  onRegenerate,
}: {
  view: HandbookView
  enriching: boolean
  notice: string
  enrichError: string
  onRegenerate: () => void
}) {
  const state = handbookViewLLMState(view)
  const llm = view.llm
  const progress = llmProgress(llm)
  const failed = state === 'failed'
  const tokens = llm && (llm.tokens_in || llm.tokens_out)
  return (
    <section className="handbook-llm" data-testid="handbook-llm">
      <div className="handbook-llm__head">
        <strong>LLM 增强</strong>
        <span className={`badge badge-llm-${state}`} data-testid="handbook-llm-state">
          {LLM_STATE_LABELS[state]}
          {state !== 'off' && progress ? ` ${progress}` : ''}
        </span>
        {view.llm_model ? (
          <span className="muted">
            模型 <code>{view.llm_model}</code>
          </span>
        ) : (
          <span className="muted">未配置模型或该仓库已设为 off</span>
        )}
        <button
          type="button"
          className="btn btn-secondary btn-sm handbook-llm__action"
          disabled={!view.llm_model || view.llm_running || enriching}
          title={!view.llm_model ? '未启用' : view.llm_running ? '增强进行中' : '重建阶段骨架并重试失败的文件'}
          onClick={onRegenerate}
        >
          {enriching ? '提交中…' : '重新生成 LLM 内容'}
        </button>
      </div>
      {llm && state !== 'off' ? (
        <ul className="handbook-llm__facts">
          {llm.cards_total !== undefined ? (
            <li>
              文件卡片 {llm.cards_done ?? 0}/{llm.cards_total}
              {llm.cards_new ? `，本轮新增 ${llm.cards_new}` : ''}
              {llm.card_errors ? `，${llm.card_errors} 个文件生成失败` : ''}
              {llm.card_transport_errors ? `，${llm.card_transport_errors} 个文件调用出错（下轮重试）` : ''}
            </li>
          ) : null}
          {llm.stages ? (
            <li>
              阶段 {llm.stages} 个
              {llm.fallback
                ? `（按目录分区${llm.fallback_reason ? `：${LLM_FALLBACK_REASON_LABELS[llm.fallback_reason] ?? llm.fallback_reason}` : ''}）`
                : ''}
            </li>
          ) : null}
          {tokens || llm.run_at ? (
            <li>
              最近一轮
              {llm.run_at ? ` ${formatTime(llm.run_at)}` : ''}
              {llm.duration_ms ? `，耗时 ${Math.round(llm.duration_ms / 1000)} 秒` : ''}
              {tokens ? `，token 输入 ${llm.tokens_in ?? 0} / 输出 ${llm.tokens_out ?? 0}` : ''}
            </li>
          ) : null}
        </ul>
      ) : null}
      {llm?.last_error && state !== 'off' ? (
        <div className={failed ? 'error' : 'handbook-llm__warn'} data-testid="handbook-llm-error">
          {failed ? '失败' : '最近一轮有错误'}：{llm.last_error}
          {failed && llm.failed_commit ? `（commit ${shortCommit(llm.failed_commit)}，HEAD 变化或重新生成后重试）` : ''}
        </div>
      ) : null}
      {notice ? (
        <div className="success" data-testid="handbook-llm-notice">
          {notice}
        </div>
      ) : null}
      {enrichError ? (
        <div className="error" role="alert" data-testid="handbook-llm-enrich-error">
          {enrichError}
        </div>
      ) : null}
    </section>
  )
}

export function HandbookDialog({ repo, onClose, onEnrichStarted }: HandbookDialogProps) {
  const [view, setView] = useState<HandbookView | null>(null)
  const [page, setPage] = useState('')
  const [content, setContent] = useState('')
  const [error, setError] = useState('')
  const [viewLoading, setViewLoading] = useState(false)
  const [pageLoading, setPageLoading] = useState(false)
  const [enriching, setEnriching] = useState(false)
  const [enrichNotice, setEnrichNotice] = useState('')
  const [enrichError, setEnrichError] = useState('')
  const repoId = repo?.id ?? ''

  useEffect(() => {
    if (!repoId) return
    let cancelled = false
    setView(null)
    setPage('')
    setContent('')
    setError('')
    setEnrichNotice('')
    setEnrichError('')
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

  const refreshView = useCallback(() => {
    if (!repoId) return
    repoApi
      .handbook(repoId)
      .then((v) => setView((prev) => (prev && prev.repo_id === v.repo_id ? v : prev)))
      .catch(() => {})
  }, [repoId])

  const llmRunning = !!view?.llm_running
  useEffect(() => {
    if (!llmRunning) return
    const timer = window.setInterval(refreshView, LLM_POLL_MS)
    return () => window.clearInterval(timer)
  }, [llmRunning, refreshView])

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

  const regenerate = async () => {
    if (!repoId) return
    setEnriching(true)
    setEnrichNotice('')
    setEnrichError('')
    try {
      await repoApi.enrichHandbook(repoId, true)
      setEnrichNotice('已开始，完成后自动刷新')
      refreshView()
      onEnrichStarted?.()
    } catch (e) {
      setEnrichError(enrichErrorMessage((e as Error).message))
    } finally {
      setEnriching(false)
    }
  }

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
        {view ? (
          <HandbookLLMSection
            view={view}
            enriching={enriching}
            notice={enrichNotice}
            enrichError={enrichError}
            onRegenerate={() => void regenerate()}
          />
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
