import { useState, useEffect, useCallback } from 'react'
import { evolutionApi, type EvolutionProposal, type EvolutionConfig } from '../api/evolution'
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

const SIGNAL_KEYS = ['style_correction', 'workflow_correction', 'debugging_trick', 'stale_skill'] as const
type SignalKey = typeof SIGNAL_KEYS[number]

const DEFAULT_CONFIG: EvolutionConfig = {
  enabled: false,
  rules: { style_correction: [], workflow_correction: [], debugging_trick: [], stale_skill: [] },
  trial_and_error: { min_tool_failures: 2, min_occurrences: 3, observation_window: 50, similarity_threshold: 0.8 },
  classifier: { provider: '', model: '', api_key: '', base_url: '', max_tokens: 2048 },
  embedding: { provider: '', model: '', api_key: '', base_url: '' },
  dedup_threshold: 0.85,
}

function cloneConfig(c: EvolutionConfig): EvolutionConfig {
  return JSON.parse(JSON.stringify(c))
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

  // Config form state
  const [configExpanded, setConfigExpanded] = useState(false)
  const [configDirty, setConfigDirty] = useState(false)
  const [configSaving, setConfigSaving] = useState(false)
  const [configLoaded, setConfigLoaded] = useState(false)
  const [formConfig, setFormConfig] = useState<EvolutionConfig>(DEFAULT_CONFIG)
  const [newKeywords, setNewKeywords] = useState<Record<SignalKey, string>>({
    style_correction: '',
    workflow_correction: '',
    debugging_trick: '',
    stale_skill: '',
  })

  const fetchConfig = useCallback(async () => {
    try {
      const data = await evolutionApi.getConfig()
      if (data.config) {
        const c = cloneConfig(DEFAULT_CONFIG)
        const src = data.config
        c.enabled = data.enabled
        if (src.rules) {
          for (const k of SIGNAL_KEYS) {
            if (Array.isArray((src.rules as Record<string, unknown>)[k])) {
              ;(c.rules as Record<string, string[]>)[k] = [...((src.rules as Record<string, unknown>)[k] as string[])]
            }
          }
        }
        if (src.trial_and_error) Object.assign(c.trial_and_error!, src.trial_and_error)
        if (src.classifier) Object.assign(c.classifier!, src.classifier)
        if (src.embedding) Object.assign(c.embedding!, src.embedding)
        if (typeof src.dedup_threshold === 'number') c.dedup_threshold = src.dedup_threshold
        setFormConfig(c)
      } else {
        setFormConfig(cloneConfig(DEFAULT_CONFIG))
        setFormConfig(prev => ({ ...prev, enabled: data.enabled }))
      }
    } catch {
      // Config endpoint may not be available yet
    } finally {
      setConfigLoaded(true)
    }
  }, [])

  useEffect(() => {
    fetchConfig()
  }, [fetchConfig])

  const updateConfig = (patch: Partial<EvolutionConfig>) => {
    setFormConfig(prev => {
      const next = cloneConfig(prev)
      Object.assign(next, patch)
      return next
    })
    setConfigDirty(true)
  }

  const updateTrial = (field: string, value: number) => {
    setFormConfig(prev => {
      const next = cloneConfig(prev)
      ;(next.trial_and_error as Record<string, number>)[field] = value
      return next
    })
    setConfigDirty(true)
  }

  const updateClassifier = (field: string, value: string | number) => {
    setFormConfig(prev => {
      const next = cloneConfig(prev)
      ;(next.classifier as Record<string, unknown>)[field] = value
      return next
    })
    setConfigDirty(true)
  }

  const updateEmbedding = (field: string, value: string) => {
    setFormConfig(prev => {
      const next = cloneConfig(prev)
      ;(next.embedding as Record<string, string>)[field] = value
      return next
    })
    setConfigDirty(true)
  }

  const addKeyword = (signal: SignalKey) => {
    const kw = newKeywords[signal].trim()
    if (!kw) return
    setFormConfig(prev => {
      const next = cloneConfig(prev)
      const arr = (next.rules as Record<string, string[]>)[signal]
      if (!arr.includes(kw)) arr.push(kw)
      return next
    })
    setNewKeywords(prev => ({ ...prev, [signal]: '' }))
    setConfigDirty(true)
  }

  const removeKeyword = (signal: SignalKey, kw: string) => {
    setFormConfig(prev => {
      const next = cloneConfig(prev)
      const arr = (next.rules as Record<string, string[]>)[signal]
      ;(next.rules as Record<string, string[]>)[signal] = arr.filter(k => k !== kw)
      return next
    })
    setConfigDirty(true)
  }

  const handleSaveConfig = async () => {
    setConfigSaving(true)
    setError('')
    try {
      await evolutionApi.putConfig(formConfig)
      setConfigDirty(false)
    } catch (e) {
      setError((e as Error).message || '配置保存失败')
    } finally {
      setConfigSaving(false)
    }
  }

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

  const rules = (formConfig.rules || {}) as Record<string, string[]>

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
          <button
            type="button"
            className={`btn btn-ghost btn-sm ${configExpanded ? 'btn-active' : ''}`}
            onClick={() => { setConfigExpanded(!configExpanded); if (!configLoaded) fetchConfig() }}
          >
            配置 {configDirty ? '●' : ''}
          </button>
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

      {configExpanded && configLoaded && (
        <div className="section-card evolution-config-form">
          {/* Enable toggle */}
          <div className="config-section">
            <h3 className="config-section__title">进化检测开关</h3>
            <p className="muted config-section__desc">开启后，每次对话结束自动检测进化信号并生成提案。</p>
            <button
              type="button"
              className={`toggle-switch ${formConfig.enabled ? 'toggle-on' : 'toggle-off'}`}
              onClick={() => updateConfig({ enabled: !formConfig.enabled })}
              aria-label={formConfig.enabled ? '关闭进化检测' : '开启进化检测'}
            >
              <span className="toggle-knob" />
              <span className="toggle-label">{formConfig.enabled ? '已开启' : '已关闭'}</span>
            </button>
          </div>

          {/* Keyword rules */}
          <div className="config-section">
            <h3 className="config-section__title">关键词规则</h3>
            <p className="muted config-section__desc">每种信号类型的关键词触发列表。</p>
            {SIGNAL_KEYS.map(signal => (
              <div key={signal} className="config-field">
                <label className="config-field__label">{SIGNAL_LABELS[signal]}</label>
                <div className="tag-input">
                  {(rules[signal] || []).map(kw => (
                    <span key={kw} className="tag">
                      {kw}
                      <button type="button" className="tag__remove" onClick={() => removeKeyword(signal, kw)} aria-label={`移除 ${kw}`}>&times;</button>
                    </span>
                  ))}
                  <input
                    type="text"
                    className="tag-input__field"
                    value={newKeywords[signal]}
                    onChange={(e) => setNewKeywords(prev => ({ ...prev, [signal]: e.target.value }))}
                    onKeyDown={(e) => { if (e.key === 'Enter') { e.preventDefault(); addKeyword(signal) } }}
                    placeholder="输入关键词，回车添加"
                  />
                  <button type="button" className="btn btn-ghost btn-sm tag-input__add" onClick={() => addKeyword(signal)} disabled={!newKeywords[signal].trim()}>添加</button>
                </div>
              </div>
            ))}
          </div>

          {/* Trial-and-Error thresholds */}
          <div className="config-section">
            <h3 className="config-section__title">试错检测</h3>
            <p className="muted config-section__desc">反复试错信号检测参数。</p>
            <div className="config-fields-grid">
              <div className="config-field">
                <label className="config-field__label">最小工具失败次数</label>
                <input type="number" className="config-field__input" min={1} value={formConfig.trial_and_error?.min_tool_failures ?? 2} onChange={(e) => updateTrial('min_tool_failures', Number(e.target.value))} />
              </div>
              <div className="config-field">
                <label className="config-field__label">最小出现次数</label>
                <input type="number" className="config-field__input" min={1} value={formConfig.trial_and_error?.min_occurrences ?? 3} onChange={(e) => updateTrial('min_occurrences', Number(e.target.value))} />
              </div>
              <div className="config-field">
                <label className="config-field__label">观察窗口 (轮次)</label>
                <input type="number" className="config-field__input" min={1} value={formConfig.trial_and_error?.observation_window ?? 50} onChange={(e) => updateTrial('observation_window', Number(e.target.value))} />
              </div>
              <div className="config-field">
                <label className="config-field__label">相似度阈值</label>
                <input type="number" className="config-field__input" min={0} max={1} step={0.05} value={formConfig.trial_and_error?.similarity_threshold ?? 0.8} onChange={(e) => updateTrial('similarity_threshold', Number(e.target.value))} />
              </div>
            </div>
          </div>

          {/* Classifier */}
          <div className="config-section">
            <h3 className="config-section__title">分类器模型</h3>
            <p className="muted config-section__desc">对进化信号分类和生成提案文本的 LLM。</p>
            <div className="config-fields-grid">
              <div className="config-field">
                <label className="config-field__label">Provider</label>
                <input type="text" className="config-field__input" value={formConfig.classifier?.provider || ''} onChange={(e) => updateClassifier('provider', e.target.value)} placeholder="如 openai" />
              </div>
              <div className="config-field">
                <label className="config-field__label">Model</label>
                <input type="text" className="config-field__input" value={formConfig.classifier?.model || ''} onChange={(e) => updateClassifier('model', e.target.value)} placeholder="如 gpt-4o-mini" />
              </div>
              <div className="config-field">
                <label className="config-field__label">API Key</label>
                <input type="password" className="config-field__input" value={formConfig.classifier?.api_key || ''} onChange={(e) => updateClassifier('api_key', e.target.value)} placeholder="sk-..." />
              </div>
              <div className="config-field">
                <label className="config-field__label">Base URL</label>
                <input type="text" className="config-field__input" value={formConfig.classifier?.base_url || ''} onChange={(e) => updateClassifier('base_url', e.target.value)} placeholder="https://api.openai.com/v1" />
              </div>
              <div className="config-field">
                <label className="config-field__label">Max Tokens</label>
                <input type="number" className="config-field__input" min={1} value={formConfig.classifier?.max_tokens ?? 2048} onChange={(e) => updateClassifier('max_tokens', Number(e.target.value))} />
              </div>
            </div>
          </div>

          {/* Embedding */}
          <div className="config-section">
            <h3 className="config-section__title">嵌入模型</h3>
            <p className="muted config-section__desc">用于提案去重向量化的 Embedding 模型。</p>
            <div className="config-fields-grid">
              <div className="config-field">
                <label className="config-field__label">Provider</label>
                <input type="text" className="config-field__input" value={formConfig.embedding?.provider || ''} onChange={(e) => updateEmbedding('provider', e.target.value)} placeholder="如 openai" />
              </div>
              <div className="config-field">
                <label className="config-field__label">Model</label>
                <input type="text" className="config-field__input" value={formConfig.embedding?.model || ''} onChange={(e) => updateEmbedding('model', e.target.value)} placeholder="如 bge-m3" />
              </div>
              <div className="config-field">
                <label className="config-field__label">API Key</label>
                <input type="password" className="config-field__input" value={formConfig.embedding?.api_key || ''} onChange={(e) => updateEmbedding('api_key', e.target.value)} placeholder="sk-..." />
              </div>
              <div className="config-field">
                <label className="config-field__label">Base URL</label>
                <input type="text" className="config-field__input" value={formConfig.embedding?.base_url || ''} onChange={(e) => updateEmbedding('base_url', e.target.value)} placeholder="https://api.openai.com/v1" />
              </div>
            </div>
          </div>

          {/* Dedup threshold */}
          <div className="config-section">
            <h3 className="config-section__title">去重</h3>
            <p className="muted config-section__desc">向量相似度高于此阈值的提案视为重复，自动跳过。</p>
            <div className="config-field" style={{ maxWidth: 240 }}>
              <label className="config-field__label">去重阈值</label>
              <input type="number" className="config-field__input" min={0} max={1} step={0.05} value={formConfig.dedup_threshold ?? 0.85} onChange={(e) => { setFormConfig(prev => ({ ...prev, dedup_threshold: Number(e.target.value) })); setConfigDirty(true) }} />
            </div>
          </div>

          {/* Save bar */}
          <div className="config-form__actions">
            <button type="button" className="btn btn-primary" disabled={!configDirty || configSaving} onClick={handleSaveConfig}>
              {configSaving ? '保存中...' : '保存配置'}
            </button>
            <button type="button" className="btn btn-secondary" disabled={!configDirty || configSaving} onClick={() => { fetchConfig(); setConfigDirty(false) }}>
              取消
            </button>
            {configDirty && <span className="muted" style={{ fontSize: '0.85em' }}>有未保存的更改</span>}
          </div>
        </div>
      )}

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