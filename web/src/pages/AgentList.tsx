import { useEffect, useState } from 'react'
import { Link } from 'react-router-dom'
import { agentApi, type Agent } from '../api/client'
import { ConfirmDialog } from '../components/ConfirmDialog'

function initialOf(name: string): string {
  const trimmed = name.trim()
  return (trimmed[0] || 'A').toUpperCase()
}

function avatarTone(name: string): 'indigo' | 'amber' | 'teal' {
  let hash = 0
  for (let i = 0; i < name.length; i += 1) hash += name.charCodeAt(i)
  const tones = ['indigo', 'amber', 'teal'] as const
  return tones[hash % tones.length]
}

function formatRelative(iso: string): string {
  if (!iso) return '刚刚'
  const t = new Date(iso).getTime()
  if (Number.isNaN(t)) return iso
  const diffMin = Math.round((Date.now() - t) / 60000)
  if (Math.abs(diffMin) < 1) return '刚刚'
  if (Math.abs(diffMin) < 60) return `${Math.abs(diffMin)} 分钟前`
  const diffHour = Math.round(diffMin / 60)
  if (Math.abs(diffHour) < 24) return `${Math.abs(diffHour)} 小时前`
  const diffDay = Math.round(diffHour / 24)
  if (Math.abs(diffDay) < 30) return `${Math.abs(diffDay)} 天前`
  return new Date(iso).toLocaleDateString()
}

export default function AgentList() {
  const [agents, setAgents] = useState<Agent[]>([])
  const [total, setTotal] = useState(0)
  const [loading, setLoading] = useState(true)
  const [error, setError] = useState('')
  const [pendingDelete, setPendingDelete] = useState<{ id: string; name: string } | null>(null)
  const [confirmLoading, setConfirmLoading] = useState(false)

  useEffect(() => {
    agentApi.list({ page: 1, page_size: 50 })
      .then((res) => {
        setAgents(res.items)
        setTotal(res.total)
      })
      .catch((e) => setError(e.message))
      .finally(() => setLoading(false))
  }, [])

  const confirmDelete = async () => {
    if (!pendingDelete) return
    setConfirmLoading(true)
    try {
      await agentApi.delete(pendingDelete.id)
      setAgents((prev) => prev.filter((agent) => agent.id !== pendingDelete.id))
      setTotal((prev) => Math.max(0, prev - 1))
      setPendingDelete(null)
    } catch (e) {
      alert((e as Error).message)
    } finally {
      setConfirmLoading(false)
    }
  }

  if (loading) return (
    <div className="loading">
      <div className="loading-spinner" />
      <span style={{ marginLeft: '0.75rem' }}>加载中…</span>
    </div>
  )
  if (error) return <div className="error">加载失败：{error}</div>

  return (
    <div>
      <div className="page-header">
        <div>
          <div className="page-title-row">
            <h1>Agents</h1>
            <span className="page-count">{total}</span>
          </div>
          <p className="page-sub">管理和配置你的 AI 智能体团队。</p>
        </div>
        <Link to="/agents/new" className="btn">+ 创建 Agent</Link>
      </div>
      {total === 0 ? (
        <div className="section-card empty-state">
          <p>还没有 Agent。</p>
          <Link to="/agents/new" className="btn">+ 创建 Agent</Link>
        </div>
      ) : (
        <div className="entity-grid">
          {agents.map((agent) => {
            const tone = avatarTone(agent.name)
            const model = [agent.model_config?.provider, agent.model_config?.model].filter(Boolean).join('/')
            return (
              <article key={agent.id} className="entity-card">
                <div className="entity-card__top">
                  <div className={`entity-card__avatar entity-card__avatar--${tone}`}>{initialOf(agent.name)}</div>
                  <span className="badge badge-builtin">
                    <span className="cell-dot cell-dot--purple" aria-hidden />
                    已配置
                  </span>
                </div>
                <h3 className="entity-card__title">{agent.name}</h3>
                <p className="entity-card__desc">{agent.description || '暂无描述'}</p>
                <div className="entity-card__tags">
                  {model ? <span className="entity-card__tag">{model}</span> : null}
                  {agent.workspace ? <span className="entity-card__tag">{agent.workspace}</span> : null}
                </div>
                <div className="entity-card__foot">
                  <span>更新于 {formatRelative(agent.updated_at)}</span>
                  <div className="actions">
                    <Link to={`/agents/${agent.id}/chat`} className="entity-card__link">对话</Link>
                    <Link to={`/agents/${agent.id}`} className="entity-card__link">配置 →</Link>
                    <button
                      type="button"
                      className="btn btn-ghost btn-sm btn-danger"
                      onClick={() => setPendingDelete({ id: agent.id, name: agent.name })}
                    >
                      删除
                    </button>
                  </div>
                </div>
              </article>
            )
          })}
        </div>
      )}
      <ConfirmDialog
        open={!!pendingDelete}
        title="删除 Agent"
        description={pendingDelete ? `删除「${pendingDelete.name}」？此操作不可恢复。` : ''}
        confirmLabel="删除"
        variant="danger"
        loading={confirmLoading}
        onCancel={() => setPendingDelete(null)}
        onConfirm={confirmDelete}
      />
    </div>
  )
}
