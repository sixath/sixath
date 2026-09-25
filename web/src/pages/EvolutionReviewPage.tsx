import { useState, useEffect, useCallback } from 'react'
import { evolutionApi, type EvolutionProposal } from '../api/evolution'
import './EvolutionReviewPage.css'

const SIGNAL_LABELS: Record<string, string> = {
  style_correction: '风格纠正',
  workflow_correction: '工作流纠正',
  debugging_trick: '调试技巧',
  stale_skill: '过时技能',
  trial_error: '反复试错',
}

const SIGNAL_ICONS: Record<string, string> = {
  style_correction: '✏️',
  workflow_correction: '🔄',
  debugging_trick: '🐛',
  stale_skill: '⚠️',
  trial_error: '🔁',
}

const ACTION_LABELS: Record<string, string> = {
  create: '创建',
  patch: '修改',
  deprecate: '废弃',
}

const STATUS_LABELS: Record<string, string> = {
  pending: '待评审',
  approved: '已采纳',
  rejected: '已拒绝',
  expired: '已过期',
}

export default function EvolutionReviewPage() {
  const [proposals, setProposals] = useState<EvolutionProposal[]>([])
  const [total, setTotal] = useState(0)
  const [page, setPage] = useState(1)
  const [statusFilter, setStatusFilter] = useState('pending')
  const [expandedId, setExpandedId] = useState<string | null>(null)
  const [editingId, setEditingId] = useState<string | null>(null)
  const [editContent, setEditContent] = useState('')
  const [rejectComment, setRejectComment] = useState('')
  const [rejectingId, setRejectingId] = useState<string | null>(null)
  const [loading, setLoading] = useState(false)
  const [error, setError] = useState('')

  const fetchProposals = useCallback(async () => {
    setLoading(true)
    setError('')
    try {
      const data = await evolutionApi.list({ page, page_size: 20, status: statusFilter })
      setProposals(data.items || [])
      setTotal(data.total)
    } catch (e) {
      setError((e as Error).message || '加载失败')
    } finally {
      setLoading(false)
    }
  }, [page, statusFilter])

  useEffect(() => {
    fetchProposals()
  }, [fetchProposals])

  const handleApprove = async (id: string) => {
    try {
      await evolutionApi.approve(id)
      fetchProposals()
    } catch (e) {
      setError((e as Error).message || '采纳失败')
    }
  }

  const handleReject = async (id: string) => {
    try {
      await evolutionApi.reject(id, rejectComment)
      setRejectingId(null)
      setRejectComment('')
      fetchProposals()
    } catch (e) {
      setError((e as Error).message || '拒绝失败')
    }
  }

  const handleSaveEdit = async (id: string) => {
    try {
      await evolutionApi.patch(id, editContent)
      setEditingId(null)
      setEditContent('')
      fetchProposals()
    } catch (e) {
      setError((e as Error).message || '保存失败')
    }
  }

  const totalPages = Math.max(1, Math.ceil(total / 20))

  return (
    <div className="evolution-review-page">
      <div className="page-header evolution-review__header">
        <div>
          <div className="page-title-row">
            <h1>进化提案评审</h1>
            <span className="page-count">{total}</span>
          </div>
          <p className="page-sub">评审 Agent 自动生成的技能进化提案，采纳或拒绝后生效。</p>
        </div>
        <div className="filter-bar">
          <select
            value={statusFilter}
            onChange={(e) => { setStatusFilter(e.target.value); setPage(1) }}
            aria-label="按状态筛选"
          >
            <option value="pending">待评审</option>
            <option value="approved">已采纳</option>
            <option value="rejected">已拒绝</option>
            <option value="">全部</option>
          </select>
        </div>
      </div>

      {error && <div className="error evolution-review__error">{error}</div>}

      {loading ? (
        <div className="loading">
          <div className="loading-spinner" />
          <span style={{ marginLeft: '0.75rem' }}>加载中...</span>
        </div>
      ) : proposals.length === 0 ? (
        <div className="section-card empty-state evolution-review__empty">
          <p>{statusFilter === 'pending' ? '暂无待评审的进化提案。' : '暂无提案。'}</p>
        </div>
      ) : (
        <div className="proposal-list">
          {proposals.map((p) => (
            <div key={p.id} className={`proposal-card ${p.status}`}>
              <div className="proposal-header">
                <span className="signal-icon">{SIGNAL_ICONS[p.signal_type] || '📋'}</span>
                <span className="signal-type">{SIGNAL_LABELS[p.signal_type] || p.signal_type}</span>
                <span className={`confidence ${p.confidence >= 0.7 ? 'confidence-high' : 'confidence-low'}`}>
                  {(p.confidence * 100).toFixed(0)}%
                </span>
                <span className="target">{ACTION_LABELS[p.target_action]}: {p.target_path}</span>
                <span className={`status-tag status-${p.status}`}>
                  {STATUS_LABELS[p.status] || p.status}
                </span>
                {p.conflict && <span className="badge badge-conflict">冲突</span>}
                {p.conflict_check_failed && <span className="badge badge-warn">检测失败</span>}
                {p.dedup_skipped && <span className="badge badge-skip">已去重</span>}
              </div>

              <div className="proposal-summary">{p.problem_summary}</div>

              <div className="proposal-meta">
                <span title={p.agent_id}>Agent: {p.agent_id.slice(0, 8)}...</span>
                <span>轮次: #{p.turn_index}</span>
                <span>{new Date(p.created_at).toLocaleString()}</span>
              </div>

              {expandedId === p.id && (
                <div className="proposal-diff">
                  <h4>提案内容</h4>
                  {editingId === p.id ? (
                    <div className="edit-area">
                      <textarea
                        value={editContent}
                        onChange={(e) => setEditContent(e.target.value)}
                        rows={10}
                      />
                      <div className="edit-actions">
                        <button type="button" className="btn btn-primary" onClick={() => handleSaveEdit(p.id)}>保存</button>
                        <button type="button" className="btn btn-secondary" onClick={() => setEditingId(null)}>取消</button>
                      </div>
                    </div>
                  ) : (
                    <pre className="content-preview">{p.proposed_content}</pre>
                  )}
                  {p.conflict_detail && (
                    <div className="conflict-warning">
                      <strong>冲突警告:</strong> {p.conflict_detail}
                    </div>
                  )}
                </div>
              )}

              <div className="proposal-actions">
                <button
                  type="button"
                  className="btn btn-ghost btn-sm"
                  onClick={() => setExpandedId(expandedId === p.id ? null : p.id)}
                >
                  {expandedId === p.id ? '收起' : '展开详情'}
                </button>
                {p.status === 'pending' && (
                  <>
                    <button
                      type="button"
                      className="btn btn-primary btn-sm"
                      onClick={() => handleApprove(p.id)}
                    >
                      采纳
                    </button>
                    <button
                      type="button"
                      className="btn btn-ghost btn-sm"
                      onClick={() => {
                        setEditingId(p.id)
                        setEditContent(p.proposed_content)
                        setExpandedId(p.id)
                      }}
                    >
                      编辑
                    </button>
                    <button
                      type="button"
                      className="btn btn-danger btn-sm"
                      onClick={() => setRejectingId(p.id)}
                    >
                      拒绝
                    </button>
                  </>
                )}
                {p.review_comment && (
                  <span className="review-note">评审意见: {p.review_comment}</span>
                )}
              </div>

              {rejectingId === p.id && (
                <div className="reject-dialog">
                  <textarea
                    placeholder="拒绝理由（可选）"
                    value={rejectComment}
                    onChange={(e) => setRejectComment(e.target.value)}
                    rows={3}
                  />
                  <div className="reject-actions">
                    <button type="button" className="btn btn-danger btn-sm" onClick={() => handleReject(p.id)}>确认拒绝</button>
                    <button type="button" className="btn btn-secondary btn-sm" onClick={() => { setRejectingId(null); setRejectComment('') }}>取消</button>
                  </div>
                </div>
              )}
            </div>
          ))}

          {totalPages > 1 && (
            <div className="pagination">
              <button
                type="button"
                className="btn btn-ghost btn-sm"
                disabled={page <= 1}
                onClick={() => setPage(page - 1)}
              >
                上一页
              </button>
              <span className="pagination__info">{page} / {totalPages}</span>
              <button
                type="button"
                className="btn btn-ghost btn-sm"
                disabled={page >= totalPages}
                onClick={() => setPage(page + 1)}
              >
                下一页
              </button>
            </div>
          )}
        </div>
      )}
    </div>
  )
}