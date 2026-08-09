import { useEffect, useState } from 'react'
import { useSearchParams, Link } from 'react-router-dom'
import { agentApi, chatApi, type Agent } from '../api/client'
import SessionSidebar from '../components/SessionSidebar'
import ChatPage from './ChatPage'
import './ChatHome.css'

/**
 * 首页：Agent 选择器与对话区分离
 * 顶部：Agent 下拉选择
 * 中间：对话内容
 * 底部：输入框（独立于 Agent 选择）
 */
export default function ChatHome() {
  const [searchParams, setSearchParams] = useSearchParams()
  const [agents, setAgents] = useState<Agent[]>([])
  const [loading, setLoading] = useState(true)

  const agentId = searchParams.get('agent') || ''
  const sessionId = searchParams.get('session') || ''

  useEffect(() => {
    agentApi.list({ page: 1, page_size: 50 })
      .then((res) => {
        const items = res.items
        setAgents(items)
        if (items.length === 1 && !searchParams.get('agent')) {
          setSearchParams({ agent: items[0].id }, { replace: true })
        }
      })
      .catch(() => setAgents([]))
      .finally(() => setLoading(false))
  }, [])

  const updateUrl = (aId: string, sId?: string) => {
    const next = new URLSearchParams(searchParams)
    next.set('agent', aId)
    if (sId) next.set('session', sId)
    else next.delete('session')
    setSearchParams(next, { replace: true })
  }

  const handleNewSession = async () => {
    if (!agentId) return
    try {
      const s = await chatApi.createSession(agentId)
      updateUrl(agentId, s.id)
    } catch (e) {
      console.error(e)
    }
  }

  if (loading) {
    return (
      <div className="chat-home chat-home-loading">
        <div className="loading-spinner" />
        <span>加载中...</span>
      </div>
    )
  }

  if (agents.length === 0) {
    return (
      <div className="chat-home chat-home-empty">
        <h2>暂无 Agent</h2>
        <p>请先创建 Agent 以开始对话</p>
        <Link to="/agents/new" className="btn">创建 Agent</Link>
      </div>
    )
  }

  return (
    <div className="chat-home chat-home-layout">
      <div className="chat-home-agent-bar">
        <div className="chat-home-agent-bar-inner">
          <label className="chat-home-agent-label">选择 Agent</label>
          <select
            className="chat-home-agent-select"
            value={agentId}
            onChange={(e) => updateUrl(e.target.value)}
          >
            <option value="">请选择 Agent...</option>
            {agents.map((a) => (
              <option key={a.id} value={a.id}>
                {a.name} ({a.model_config?.provider}/{a.model_config?.model})
              </option>
            ))}
          </select>
        </div>
      </div>
      <div className="chat-home-main">
        <div className="chat-home-session-col">
          <SessionSidebar
            agentId={agentId}
            sessionId={sessionId}
            onSelect={(sid) => updateUrl(agentId, sid)}
            onNewSession={() => void handleNewSession()}
          />
        </div>
        <div className="chat-home-content">
          <ChatPage
            agentId={agentId || undefined}
            sessionId={sessionId || undefined}
            isHome
            onNavigate={updateUrl}
          />
        </div>
      </div>
    </div>
  )
}
