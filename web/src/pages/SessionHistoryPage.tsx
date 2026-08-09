import { useCallback, useEffect, useMemo, useState } from 'react'
import { useNavigate } from 'react-router-dom'
import {
  agentApi,
  chatApi,
  SESSION_LIST_PAGE_SIZE,
  type Agent,
  type ChatSession,
  type SessionSearchHit,
} from '../api/client'
import './SessionHistoryPage.css'

type SessionRow = {
  session_id: string
  agent_id: string
  agent_name: string
  title: string
  preview: string
  updated_at: string
}

function fromChatSession(s: ChatSession): SessionRow | null {
  if (!s.id || !s.agent_id) return null
  return {
    session_id: s.id,
    agent_id: s.agent_id,
    agent_name: s.agent_name?.trim() || '—',
    title: s.title?.trim() || '未命名',
    preview: (s.preview ?? '').trim() || '—',
    updated_at: s.updated_at,
  }
}

function fromSearchHit(h: SessionSearchHit): SessionRow | null {
  if (!h.session_id || !h.agent_id) return null
  return {
    session_id: h.session_id,
    agent_id: h.agent_id,
    agent_name: h.agent_name?.trim() || '—',
    title: h.title?.trim() || '未命名',
    preview: (h.preview ?? '').trim() || '—',
    updated_at: h.updated_at,
  }
}

function formatTime(iso: string) {
  if (!iso) return '—'
  try {
    return new Date(iso).toLocaleString()
  } catch {
    return iso
  }
}

export default function SessionHistoryPage() {
  const navigate = useNavigate()
  const [searchInput, setSearchInput] = useState('')
  const [debouncedQuery, setDebouncedQuery] = useState('')

  const [agents, setAgents] = useState<Agent[]>([])
  const [agentIdFilter, setAgentIdFilter] = useState('')

  const [rows, setRows] = useState<SessionRow[]>([])
  const [total, setTotal] = useState(0)
  const [page, setPage] = useState(1)
  const [loading, setLoading] = useState(true)
  const [loadingMore, setLoadingMore] = useState(false)
  const [error, setError] = useState('')

  useEffect(() => {
    const t = window.setTimeout(() => setDebouncedQuery(searchInput.trim()), 300)
    return () => window.clearTimeout(t)
  }, [searchInput])

  useEffect(() => {
    agentApi
      .list({ page: 1, page_size: 100 })
      .then((res) => setAgents(res.items))
      .catch(() => setAgents([]))
  }, [])

  const loadPage = useCallback(async (nextPage: number, append: boolean) => {
    if (append) setLoadingMore(true)
    else {
      setLoading(true)
      setRows([])
    }
    setError('')
    try {
      const res = await chatApi.listAllSessions({
        page: nextPage,
        pageSize: SESSION_LIST_PAGE_SIZE,
        includePreview: true,
      })
      const mapped = (res.items ?? [])
        .map(fromChatSession)
        .filter((x): x is SessionRow => x != null)
      setTotal(res.total ?? 0)
      setPage(nextPage)
      setRows((prev) => (append ? [...prev, ...mapped] : mapped))
    } catch (e) {
      setError((e as Error).message)
      if (!append) setRows([])
    } finally {
      setLoading(false)
      setLoadingMore(false)
    }
  }, [])

  const runSearch = useCallback(async () => {
    setLoading(true)
    setRows([])
    setError('')
    try {
      const res = await chatApi.searchSessions({
        query: debouncedQuery,
        agentId: agentIdFilter || undefined,
        limit: 20,
      })
      const mapped = (res.items ?? [])
        .map(fromSearchHit)
        .filter((x): x is SessionRow => x != null)
      setRows(mapped)
      setTotal(mapped.length)
      setPage(1)
    } catch (e) {
      setError((e as Error).message)
      setRows([])
    } finally {
      setLoading(false)
    }
  }, [debouncedQuery, agentIdFilter])

  useEffect(() => {
    if (debouncedQuery.length > 0) return
    void loadPage(1, false)
  }, [debouncedQuery, loadPage])

  useEffect(() => {
    if (debouncedQuery.length === 0) return
    void runSearch()
  }, [debouncedQuery, agentIdFilter, runSearch])

  const isSearchMode = debouncedQuery.length > 0

  const hasMore = useMemo(() => {
    if (isSearchMode) return false
    return rows.length < total
  }, [isSearchMode, rows.length, total])

  const onLoadMore = () => {
    if (loadingMore || !hasMore) return
    void loadPage(page + 1, true)
  }

  const openSession = (agentId: string, sessionId: string) => {
    navigate(`/?agent=${encodeURIComponent(agentId)}&session=${encodeURIComponent(sessionId)}`)
  }

  if (loading && rows.length === 0 && !error) {
    return (
      <div className="loading">
        <div className="loading-spinner" />
        <span style={{ marginLeft: '0.75rem' }}>加载中...</span>
      </div>
    )
  }

  return (
    <div className="session-history">
      <div className="page-header session-history__header">
        <h1>会话历史</h1>
      </div>

      <div className="section-card session-history__filters">
        <input
          type="search"
          className="session-history__search"
          placeholder="搜索会话内容或标题…"
          value={searchInput}
          onChange={(e) => setSearchInput(e.target.value)}
          data-testid="sessions-history-search"
          aria-label="搜索会话"
        />
        <select
          className="session-history__agent-filter"
          value={agentIdFilter}
          onChange={(e) => setAgentIdFilter(e.target.value)}
          aria-label="按 Agent 筛选"
          disabled={!isSearchMode}
          title={!isSearchMode ? '输入搜索关键词后可按 Agent 筛选' : undefined}
        >
          <option value="">全部 Agent</option>
          {agents.map((a) => (
            <option key={a.id} value={a.id}>
              {a.name}
            </option>
          ))}
        </select>
      </div>

      {error && <div className="error session-history__error">{error}</div>}

      {rows.length === 0 && !loading ? (
        <div className="section-card empty-state session-history__empty">
          <p>{isSearchMode ? '未找到匹配的会话。' : '暂无会话记录。'}</p>
        </div>
      ) : (
        <div className="table-card session-history__table-wrap">
          <table>
            <thead>
              <tr>
                <th>标题</th>
                <th>Agent</th>
                <th>预览</th>
                <th>更新时间</th>
                <th>操作</th>
              </tr>
            </thead>
            <tbody>
              {rows.map((r) => (
                <tr key={r.session_id}>
                  <td className="session-history__cell-title">{r.title}</td>
                  <td style={{ color: 'var(--muted)' }}>{r.agent_name}</td>
                  <td className="session-history__cell-preview">{r.preview}</td>
                  <td style={{ color: 'var(--muted)', whiteSpace: 'nowrap' }}>{formatTime(r.updated_at)}</td>
                  <td>
                    <button
                      type="button"
                      className="btn btn-sm"
                      data-testid={`sessions-open-${r.session_id}`}
                      onClick={() => openSession(r.agent_id, r.session_id)}
                    >
                      打开
                    </button>
                  </td>
                </tr>
              ))}
            </tbody>
          </table>
        </div>
      )}

      {!isSearchMode && hasMore && (
        <div className="session-history__more">
          <button type="button" className="btn btn-secondary" disabled={loadingMore} onClick={onLoadMore}>
            {loadingMore ? '加载中…' : '加载更多'}
          </button>
        </div>
      )}
    </div>
  )
}
