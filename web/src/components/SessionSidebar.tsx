import { useCallback, useEffect, useRef, useState, type MouseEvent } from 'react'
import { chatApi, SESSION_LIST_PAGE_SIZE, type ChatSession } from '../api/client'
import './SessionSidebar.css'

export interface SessionSidebarProps {
  agentId: string
  sessionId: string
  onSelect: (sessionId?: string) => void
  onNewSession: () => void
}

function formatRelativeUpdated(iso: string): string {
  if (!iso) return ''
  const d = new Date(iso)
  const t = d.getTime()
  if (Number.isNaN(t)) return ''
  const now = Date.now()
  let diffSec = Math.round((now - t) / 1000)
  const rtf = new Intl.RelativeTimeFormat('zh-CN', { numeric: 'auto' })
  if (Math.abs(diffSec) < 45) return rtf.format(-Math.max(diffSec, 1), 'second')
  const diffMin = Math.round(diffSec / 60)
  if (Math.abs(diffMin) < 60) return rtf.format(-diffMin, 'minute')
  const diffHour = Math.round(diffMin / 60)
  if (Math.abs(diffHour) < 24) return rtf.format(-diffHour, 'hour')
  const diffDay = Math.round(diffHour / 24)
  if (Math.abs(diffDay) < 30) return rtf.format(-diffDay, 'day')
  const diffMonth = Math.round(diffDay / 30)
  if (Math.abs(diffMonth) < 12) return rtf.format(-diffMonth, 'month')
  const diffYear = Math.round(diffDay / 365)
  return rtf.format(-diffYear, 'year')
}

export default function SessionSidebar({
  agentId,
  sessionId,
  onSelect,
  onNewSession,
}: SessionSidebarProps) {
  const [search, setSearch] = useState('')
  const [debouncedQ, setDebouncedQ] = useState('')
  const [sessions, setSessions] = useState<ChatSession[]>([])
  const [loading, setLoading] = useState(false)
  const [editingId, setEditingId] = useState<string | null>(null)
  const [editTitle, setEditTitle] = useState('')
  const inputRef = useRef<HTMLInputElement | null>(null)
  const skipBlurSubmitRef = useRef(false)

  useEffect(() => {
    const timer = window.setTimeout(() => {
      setDebouncedQ(search.trim())
    }, 300)
    return () => window.clearTimeout(timer)
  }, [search])

  const loadSessions = useCallback(async () => {
    if (!agentId) return
    setLoading(true)
    try {
      const res = await chatApi.listSessions(agentId, {
        pageSize: SESSION_LIST_PAGE_SIZE,
        includePreview: true,
        q: debouncedQ || undefined,
      })
      setSessions(res.items)
    } catch {
      setSessions([])
    } finally {
      setLoading(false)
    }
  }, [agentId, debouncedQ])

  useEffect(() => {
    void loadSessions()
  }, [loadSessions, sessionId])

  useEffect(() => {
    if (editingId && inputRef.current) {
      inputRef.current.focus()
      inputRef.current.select()
    }
  }, [editingId])

  const openRenamePrompt = (s: ChatSession, e: MouseEvent) => {
    e.stopPropagation()
    const next = window.prompt('重命名会话', s.title)
    if (next == null) return
    const title = next.trim()
    if (!title || title === s.title) return
    void (async () => {
      try {
        await chatApi.updateSession(s.id, title)
        await loadSessions()
      } catch (err) {
        console.error(err)
      }
    })()
  }

  const submitInlineRename = async (id: string) => {
    const title = editTitle.trim()
    setEditingId(null)
    if (!title) {
      await loadSessions()
      return
    }
    const prev = sessions.find((x) => x.id === id)?.title
    if (title === prev) return
    try {
      await chatApi.updateSession(id, title)
      await loadSessions()
    } catch (err) {
      console.error(err)
    }
  }

  const handleDelete = (id: string, e: MouseEvent) => {
    e.stopPropagation()
    if (!window.confirm('确定删除该会话？此操作不可撤销。')) return
    void (async () => {
      try {
        await chatApi.deleteSession(id)
        const res = await chatApi.listSessions(agentId, {
          pageSize: SESSION_LIST_PAGE_SIZE,
          includePreview: true,
          q: debouncedQ || undefined,
        })
        const nextItems = res.items
        setSessions(nextItems)
        if (id === sessionId) {
          const first = nextItems[0]?.id
          if (first) onSelect(first)
          else onSelect(undefined)
        }
      } catch (err) {
        console.error(err)
      }
    })()
  }

  if (!agentId) {
    return (
      <aside className="session-sidebar session-sidebar--placeholder" aria-label="会话列表">
        <p className="session-sidebar-placeholder">请先选择 Agent</p>
      </aside>
    )
  }

  return (
    <aside className="session-sidebar" aria-label="会话列表">
      <div className="session-sidebar-toolbar">
        <button
          type="button"
          className="session-sidebar-new"
          data-testid="session-sidebar-new"
          onClick={() => onNewSession()}
        >
          新建对话
        </button>
      </div>
      <div className="session-sidebar-search-wrap">
        <input
          type="search"
          className="session-sidebar-search"
          data-testid="session-sidebar-search"
          placeholder="搜索会话"
          value={search}
          onChange={(e) => setSearch(e.target.value)}
          autoComplete="off"
        />
      </div>
      <div className="session-sidebar-list" role="list">
        {loading && sessions.length === 0 ? (
          <div className="session-sidebar-muted session-sidebar-loading">加载中...</div>
        ) : sessions.length === 0 ? (
          <div className="session-sidebar-muted">暂无会话</div>
        ) : (
          sessions.map((s) => {
            const active = sessionId === s.id
            const preview =
              typeof s.preview === 'string'
                ? s.preview.replace(/\s+/g, ' ').trim()
                : ''
            const branch = Boolean(s.parent_session_id?.trim())
            const isEditing = editingId === s.id

            return (
              <div
                key={s.id}
                role="listitem"
                data-testid={`session-item-${s.id}`}
                className={
                  'session-sidebar-item' + (active ? ' session-sidebar-item--active' : '')
                }
                onClick={() => onSelect(s.id)}
              >
                {isEditing ? (
                  <input
                    ref={inputRef}
                    className="session-sidebar-item-input"
                    value={editTitle}
                    onChange={(e) => setEditTitle(e.target.value)}
                    onClick={(e) => e.stopPropagation()}
                    onDoubleClick={(e) => e.stopPropagation()}
                    onKeyDown={(e) => {
                      if (e.key === 'Enter') {
                        e.preventDefault()
                        void submitInlineRename(s.id)
                      }
                      if (e.key === 'Escape') {
                        e.preventDefault()
                        skipBlurSubmitRef.current = true
                        setEditingId(null)
                        void loadSessions()
                      }
                    }}
                    onBlur={() => {
                      if (skipBlurSubmitRef.current) {
                        skipBlurSubmitRef.current = false
                        return
                      }
                      void submitInlineRename(s.id)
                    }}
                  />
                ) : (
                  <>
                    <div
                      className="session-sidebar-item-header"
                      onDoubleClick={(e) => {
                        e.stopPropagation()
                        setEditingId(s.id)
                        setEditTitle(s.title)
                      }}
                    >
                      <span className="session-sidebar-item-title">{s.title || '未命名'}</span>
                      {branch && (
                        <span className="session-sidebar-badge" title="分支会话">
                          分支
                        </span>
                      )}
                    </div>
                    {preview ? (
                      <div className="session-sidebar-item-preview">{preview}</div>
                    ) : null}
                    <div className="session-sidebar-item-meta">
                      <span>{formatRelativeUpdated(s.updated_at)}</span>
                    </div>
                  </>
                )}
                {!isEditing && (
                  <div className="session-sidebar-item-actions">
                    <button
                      type="button"
                      className="session-sidebar-action"
                      title="重命名"
                      onClick={(e) => openRenamePrompt(s, e)}
                    >
                      重命名
                    </button>
                    <button
                      type="button"
                      className="session-sidebar-action session-sidebar-action--danger"
                      title="删除"
                      onClick={(e) => handleDelete(s.id, e)}
                    >
                      删除
                    </button>
                  </div>
                )}
              </div>
            )
          })
        )}
      </div>
    </aside>
  )
}
