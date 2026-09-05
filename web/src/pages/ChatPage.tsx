import { useCallback, useEffect, useRef, useState } from 'react'
import { useNavigate, useParams } from 'react-router-dom'
import { agentApi, chatApi, DEFAULT_SESSION_TITLE, type Agent, type ChatMessage } from '../api/client'
import {
  buildConfirmSubmitBody,
  buildInputSubmitBody,
  inputProvidedLabel,
  restoreConfirmationsFromMessages,
  restoreInputsFromMessages,
  type ChatConfirmationRequest,
  type ChatInputRequest,
  type ConfirmResultPayload,
  type WebSourceItem,
} from '../api/chatStream'
import { MarkdownContent } from '../components/MarkdownContent'
import { CompactBoundaryBanner } from '../components/CompactBoundaryBanner'
import { SourcesPanel } from '../components/SourcesPanel'
import { SpillResultTable, useSpillTable } from '../components/SpillResultTable'
import { isCompactBoundaryMessage, isMessageVisibleAtIndex } from '../utils/compactBoundary'
import { applyToolCall, applyModelCall, finalizeTimeline, type TimelineNode } from './timelineReducer'
import { toolVerb } from './toolVerbMap'
import './ChatPage.css'

function formatMessageTime(iso: string): string {
  if (!iso) return ''
  const d = new Date(iso)
  if (Number.isNaN(d.getTime())) return ''
  const now = new Date()
  const sameDay =
    d.getFullYear() === now.getFullYear() &&
    d.getMonth() === now.getMonth() &&
    d.getDate() === now.getDate()
  if (sameDay) {
    return d.toLocaleTimeString([], { hour: '2-digit', minute: '2-digit', second: '2-digit' })
  }
  return d.toLocaleString([], {
    month: '2-digit',
    day: '2-digit',
    hour: '2-digit',
    minute: '2-digit',
  })
}

export interface ChatPageProps {
  agentId?: string
  sessionId?: string
  isHome?: boolean
  onNavigate?: (agentId: string, sessionId?: string) => void
}

interface ChatConfirmationItem extends ChatConfirmationRequest {
  messageKey: string
  status: 'pending' | 'confirming' | 'confirmed' | 'cancelled' | 'superseded' | 'expired' | 'failed'
  error?: string
  receivedAt: number
}

interface ChatInputItem extends ChatInputRequest {
  messageKey: string
  status: 'pending' | 'submitting' | 'submitted' | 'cancelled' | 'expired'
  draft: string
  error?: string
}

const SUPERSEDED_HINT = '已有更新提案'

function confirmationDeadlineMs(item: Pick<ChatConfirmationItem, 'expires_at' | 'expires_in' | 'receivedAt'>): number | null {
  if (item.expires_at) {
    const t = Date.parse(item.expires_at)
    if (!Number.isNaN(t)) return t
  }
  if (typeof item.expires_in === 'number' && Number.isFinite(item.expires_in)) {
    return item.receivedAt + item.expires_in * 1000
  }
  return null
}

function remainingConfirmSeconds(item: ChatConfirmationItem, nowMs: number): number | null {
  const deadline = confirmationDeadlineMs(item)
  if (deadline == null) return null
  return Math.max(0, Math.ceil((deadline - nowMs) / 1000))
}

function statusFromConfirmResult(result: ConfirmResultPayload): ChatConfirmationItem['status'] {
  if (result.ok) return 'confirmed'
  if (result.error_code === 'superseded') return 'superseded'
  if (result.error_code === 'expired') return 'expired'
  return 'failed'
}

function applyIncomingConfirmation(
  prev: ChatConfirmationItem[],
  confirmation: ChatConfirmationRequest,
  messageKey: string,
): ChatConfirmationItem[] {
  const receivedAt = Date.now()
  const next = prev.map((c) => {
    if (c.status !== 'pending') return c

    if (confirmation.kind === 'skill_manage') {
      if (
        c.kind === 'skill_manage' &&
        confirmation.resource_key &&
        c.resource_key === confirmation.resource_key
      ) {
        return { ...c, status: 'superseded' as const, error: SUPERSEDED_HINT }
      }
      return c
    }

    if (confirmation.resource_key) {
      if (c.kind === confirmation.kind && c.resource_key === confirmation.resource_key) {
        return { ...c, status: 'superseded' as const, error: SUPERSEDED_HINT }
      }
      return c
    }

    if (c.kind === confirmation.kind) {
      return { ...c, status: 'superseded' as const, error: SUPERSEDED_HINT }
    }
    return c
  })

  return [
    ...next,
    {
      ...confirmation,
      messageKey,
      status: 'pending',
      receivedAt,
    },
  ]
}

function confirmButtonLabel(status: ChatConfirmationItem['status']): string {
  switch (status) {
    case 'confirming':
      return 'Confirming...'
    case 'confirmed':
      return 'Confirmed'
    case 'superseded':
      return 'Superseded'
    case 'expired':
      return 'Expired'
    case 'failed':
      return 'Failed'
    case 'cancelled':
      return 'Cancelled'
    default:
      return 'Confirm'
  }
}

function AssistantReplyBody({
  sessionId,
  content,
  nodes,
  showCursor,
  interrupted,
}: {
  sessionId?: string
  content: string
  nodes: TimelineNode[]
  showCursor: boolean
  interrupted: boolean
}) {
  const spill = useSpillTable(sessionId, content, nodes)
  const banner = spill.table
    ? `标题写的行数多于对话里贴出的表格；下面已加载工具落盘的完整 ${spill.table.rows.length} 行。`
    : spill.hint ?? (interrupted ? '这条回复在生成时被中断，内容可能不完整。' : null)
  return (
    <>
      {banner && <p className="chat-truncated-banner">{banner}{spill.loading ? ' 正在加载…' : ''}{spill.error ? ` ${spill.error}` : ''}</p>}
      {spill.table && <SpillResultTable columns={spill.table.columns} rows={spill.table.rows} />}
      <MarkdownContent showCursor={showCursor}>{spill.displayContent}</MarkdownContent>
    </>
  )
}

function TimelineView({ nodes }: { nodes: TimelineNode[] }) {
  const [open, setOpen] = useState<Record<string, boolean>>({})
  const [tab, setTab] = useState<Record<string, 'args' | 'result' | 'meta'>>({})
  if (!nodes.length) return null
  return (
    <div className="tl">
      {nodes.map((n) => {
        const key = n.kind === 'tool' ? `t:${n.id}` : `m:${n.step}`
        const isOpen = open[key]
        const t = tab[key] ?? 'args'
        const isFail = n.kind === 'tool' && (n.phase === 'failed' || !n.allowed)
        const dotClass = n.kind === 'model' ? 'tl-dot tl-dot-model' : isFail ? 'tl-dot tl-dot-fail' : 'tl-dot'
        const running = (n.kind === 'tool' && n.phase === 'started') || (n.kind === 'model' && n.phase === 'invoked')
        const interrupted = n.phase === 'interrupted'
        return (
          <div className="tl-item" key={key}>
            <span className={`${dotClass}${running ? ' tl-dot-run' : ''}`} />
            <div className="tl-row" onClick={() => setOpen((o) => ({ ...o, [key]: !o[key] }))}>
              {n.kind === 'model' ? (
                <span className="tl-verb">🧠 模型推理</span>
              ) : (
                <span className="tl-verb">{toolVerb(n.toolName)}</span>
              )}
              <span className="tl-meta">
                {n.kind === 'model'
                  ? (interrupted ? '已中断' : `${n.model ?? ''}${n.outputTokens != null ? ` · ${(n.inputTokens ?? 0) + n.outputTokens} tokens` : ''}`)
                  : running ? '执行中…' : interrupted ? '已中断' : isFail ? '失败' : `✓ ${n.durationMs ?? 0}ms`}
              </span>
            </div>
            {isOpen && (
              <div className="tl-panel">
                {n.kind === 'tool' ? (
                  <>
                    <div className="tl-tabs">
                      <span className={t === 'args' ? 'tl-tab on' : 'tl-tab'} onClick={() => setTab((tb) => ({ ...tb, [key]: 'args' }))}>入参</span>
                      <span className={t === 'result' ? 'tl-tab on' : 'tl-tab'} onClick={() => setTab((tb) => ({ ...tb, [key]: 'result' }))}>结果</span>
                      <span className={t === 'meta' ? 'tl-tab on' : 'tl-tab'} onClick={() => setTab((tb) => ({ ...tb, [key]: 'meta' }))}>元数据</span>
                    </div>
                    {t === 'args' && <pre className="tl-pre">{JSON.stringify(n.arguments, null, 2)}</pre>}
                    {t === 'result' && (
                      <>
                        <pre className="tl-pre">{n.error ? n.error : JSON.stringify(n.result, null, 2)}</pre>
                        {n.truncated && <div className="tl-trunc">结果已截断</div>}
                      </>
                    )}
                    {t === 'meta' && (
                      <pre className="tl-pre">{JSON.stringify({ duration_ms: n.durationMs, allowed: n.allowed, decision: n.decision, step: n.step }, null, 2)}</pre>
                    )}
                  </>
                ) : (
                  <pre className="tl-pre">{JSON.stringify({ mode: n.mode, model: n.model, input_tokens: n.inputTokens, output_tokens: n.outputTokens, message_count: n.messageCount }, null, 2)}</pre>
                )}
              </div>
            )}
          </div>
        )
      })}
    </div>
  )
}

export default function ChatPage(props?: ChatPageProps) {
  const params = useParams()
  const navigate = useNavigate()
  const agentId = props?.agentId ?? params.id
  const sessionId = props?.sessionId ?? params.sessionId
  const isHome = props?.isHome ?? false
  const onNavigate = props?.onNavigate
  const [agent, setAgent] = useState<Agent | null>(null)
  const [messages, setMessages] = useState<ChatMessage[]>([])
  const [confirmations, setConfirmations] = useState<ChatConfirmationItem[]>([])
  const [inputs, setInputs] = useState<ChatInputItem[]>([])
  const [messageSources, setMessageSources] = useState<Record<string, WebSourceItem[]>>({})
  const [messageTimelines, setMessageTimelines] = useState<Record<string, TimelineNode[]>>({})
  /** compact boundary 消息 id → 是否折叠其上方历史（默认展开，不在 Set 中） */
  const [collapsedBoundaries, setCollapsedBoundaries] = useState<Set<string>>(() => new Set())
  /** 仅用于展示；实际缓冲在 ref 中合并刷新，避免每条 SSE 触发整页重绘 */
  const [debugText, setDebugText] = useState('')
  const [debugEventCount, setDebugEventCount] = useState(0)
  const [showDebug, setShowDebug] = useState(false)
  const [input, setInput] = useState('')
  const [streaming, setStreaming] = useState(false)
  const [loading, setLoading] = useState(true)
  const [loadingHistory, setLoadingHistory] = useState(false)
  const [error, setError] = useState('')
  const [rewinding, setRewinding] = useState(false)
  const [nowMs, setNowMs] = useState(() => Date.now())
  const messagesEndRef = useRef<HTMLDivElement>(null)
  const abortRef = useRef<AbortController | null>(null)
  /** 当前流式请求所属会话；用于在切换路由会话时中止旧会话的流，且避免「无 session → 首条消息」误杀同一会话的流 */
  const streamSessionRef = useRef<string | null>(null)
  /** 防止确认卡双击在 React 重渲染前发出两次 confirm_response（第二次会 already_used） */
  const confirmInFlightRef = useRef<Set<string>>(new Set())
  const showDebugRef = useRef(false)
  const debugTextRef = useRef('')
  const debugEventCountRef = useRef(0)
  const debugDebounceTimerRef = useRef<ReturnType<typeof setTimeout> | null>(null)
  const pendingChunkRef = useRef('')
  const chunkFlushRafRef = useRef<number | null>(null)
  const scrollDebounceRef = useRef<ReturnType<typeof setTimeout> | null>(null)

  const MAX_DEBUG_CHARS = 160_000
  const DEBUG_UI_DEBOUNCE_MS = 400

  const sessionIdRef = useRef(sessionId)
  sessionIdRef.current = sessionId

  const scrollToBottom = (smooth: boolean) =>
    messagesEndRef.current?.scrollIntoView({ behavior: smooth ? 'smooth' : 'auto' })

  const toggleCompactBoundary = useCallback((boundaryId: string) => {
    setCollapsedBoundaries((prev) => {
      const next = new Set(prev)
      if (next.has(boundaryId)) next.delete(boundaryId)
      else next.add(boundaryId)
      return next
    })
  }, [])

  const loadAgent = useCallback(async () => {
    if (!agentId) return
    try {
      const a = await agentApi.get(agentId)
      setAgent(a)
    } catch (e) {
      setError((e as Error).message)
    }
  }, [agentId])

  useEffect(() => {
    if (!agentId) {
      setLoading(false)
      setAgent(null)
      return
    }
    loadAgent().finally(() => setLoading(false))
  }, [agentId, loadAgent])

  useEffect(() => {
    showDebugRef.current = showDebug
    if (showDebug) {
      setDebugText(debugTextRef.current)
      setDebugEventCount(debugEventCountRef.current)
    }
  }, [showDebug])

  const goTo = useCallback((aId: string, sId?: string) => {
    if (isHome && onNavigate) {
      onNavigate(aId, sId)
    } else if (sId) {
      navigate(`/agents/${aId}/chat/${sId}`)
    } else {
      navigate(`/agents/${aId}/chat`)
    }
  }, [isHome, onNavigate, navigate])

  useEffect(() => {
    if (sessionId && agentId) {
      chatApi.getSession(sessionId)
        .then((s) => {
          const sAgentId = s.agent_id || (s as { agentId?: string }).agentId
          if (sAgentId !== agentId) {
            goTo(agentId, undefined)
            setMessages([])
            setConfirmations([])
            setInputs([])
            setCollapsedBoundaries(new Set())
            setError('')
          } else {
            setError('')
          }
        })
        .catch((e) => {
          const message = (e as Error).message
          if (message.includes('invalid connection')) {
            console.warn('Session refresh failed after stream:', message)
            return
          }
          setError(message)
        })
    } else {
      setMessages([])
      setConfirmations([])
      setInputs([])
      setCollapsedBoundaries(new Set())
      debugTextRef.current = ''
      debugEventCountRef.current = 0
      setDebugText('')
      setDebugEventCount(0)
      if (debugDebounceTimerRef.current) {
        clearTimeout(debugDebounceTimerRef.current)
        debugDebounceTimerRef.current = null
      }
    }
  }, [sessionId, agentId, goTo])

  useEffect(() => {
    const streamSid = streamSessionRef.current
    if (streamSid != null && streamSid !== sessionId) {
      abortRef.current?.abort()
      abortRef.current = null
      streamSessionRef.current = null
      setStreaming(false)
    }

    if (!sessionId) {
      setMessages([])
      setLoadingHistory(false)
      return
    }

    if (streaming && streamSessionRef.current === sessionId) {
      setLoadingHistory(false)
      return
    }

    const targetSid = sessionId
    let cancelled = false
    setLoadingHistory(true)

    chatApi
      .listMessages(targetSid)
      .then((res) => {
        if (cancelled) return
        if (sessionIdRef.current !== targetSid) return
        setMessages(res.items)
        setMessageTimelines({})
        setMessageSources({})
        setCollapsedBoundaries(new Set())
        setConfirmations(restoreConfirmationsFromMessages(res.items))
        setInputs(restoreInputsFromMessages(res.items))
        setError('')
      })
      .catch((e) => {
        if (cancelled) return
        if (sessionIdRef.current !== targetSid) return
        setError((e as Error).message)
      })
      .finally(() => {
        if (cancelled) return
        if (sessionIdRef.current !== targetSid) return
        setLoadingHistory(false)
      })

    return () => {
      cancelled = true
    }
  }, [sessionId])

  const reloadMessages = useCallback(async (sid: string) => {
    const res = await chatApi.listMessages(sid)
    setMessages(res.items)
    setMessageTimelines({})
    setMessageSources({})
    setCollapsedBoundaries(new Set())
    setConfirmations(restoreConfirmationsFromMessages(res.items))
    setInputs(restoreInputsFromMessages(res.items))
  }, [])

  const handleRewind = useCallback(async (messageId: string) => {
    if (!sessionId || !messageId || streaming || rewinding) return
    if (!window.confirm('Rewind to before this message? Later messages will be hidden from the chat and search.')) {
      return
    }
    setRewinding(true)
    setError('')
    try {
      abortRef.current?.abort()
      await chatApi.rewindSession(sessionId, messageId)
      await reloadMessages(sessionId)
    } catch (e) {
      setError((e as Error).message)
    } finally {
      setRewinding(false)
    }
  }, [sessionId, streaming, rewinding, reloadMessages])

  useEffect(() => () => {
    if (debugDebounceTimerRef.current) {
      clearTimeout(debugDebounceTimerRef.current)
      debugDebounceTimerRef.current = null
    }
  }, [])

  useEffect(() => {
    if (scrollDebounceRef.current) clearTimeout(scrollDebounceRef.current)
    const smooth = !streaming
    const delay = streaming ? 200 : 0
    scrollDebounceRef.current = setTimeout(() => {
      scrollDebounceRef.current = null
      scrollToBottom(smooth)
    }, delay)
    return () => {
      if (scrollDebounceRef.current) clearTimeout(scrollDebounceRef.current)
    }
  }, [messages, confirmations, streaming])

  const hasPendingConfirm = confirmations.some((c) => c.status === 'pending')

  useEffect(() => {
    if (!hasPendingConfirm) return
    setNowMs(Date.now())
    const timer = setInterval(() => {
      const now = Date.now()
      setNowMs(now)
      setConfirmations((prev) => {
        let changed = false
        const next = prev.map((c) => {
          if (c.status !== 'pending') return c
          const deadline = confirmationDeadlineMs(c)
          if (deadline == null || now < deadline) return c
          changed = true
          return { ...c, status: 'expired' as const, error: c.error || '确认已过期' }
        })
        return changed ? next : prev
      })
    }, 1000)
    return () => clearInterval(timer)
  }, [hasPendingConfirm])

  const handleSend = async (
    overrideContent?: string,
    submit?: {
      input_response?: ReturnType<typeof buildInputSubmitBody>['input_response']
      confirm_response?: ReturnType<typeof buildConfirmSubmitBody>['confirm_response']
    }
  ) => {
    const content = (overrideContent ?? input).trim()
    if ((!content && !submit) || !agentId || streaming) return

    const userMsgCountBefore = messages.filter((m) => m.role === 'user').length
    const shouldAutoTitleAfterStream =
      !submit && content.length > 0 && userMsgCountBefore === 0

    let sid = sessionId
    if (!sid) {
      try {
        const res = await chatApi.createSession(agentId)
        sid = res.id
        goTo(agentId, sid)
      } catch (e) {
        alert((e as Error).message)
        return
      }
    }

    if (!overrideContent && !submit) setInput('')
    const userDisplay = submit?.input_response
      ? inputProvidedLabel(submit.input_response.field)
      : submit?.confirm_response
        ? `[confirmed: ${submit.confirm_response.kind}]`
        : content
    setMessages((prev) => [...prev, { id: '', session_id: sid, role: 'user', content: userDisplay, created_at: new Date().toISOString() }])
    setStreaming(true)

    const assistantKey = `${sid}-assistant-${Date.now()}`
    const assistantPlaceholder: ChatMessage = {
      id: assistantKey,
      session_id: sid,
      role: 'assistant',
      content: '',
      created_at: new Date().toISOString(),
    }
    setMessages((prev) => [...prev, assistantPlaceholder])

    const ac = startMessageStream(sid, submit ? '' : content, assistantKey, {
      shouldAutoTitleAfterStream,
      autoTitleContent: content,
      streamOptions: submit
        ? {
            ...(submit.input_response ? { input_response: submit.input_response } : {}),
            ...(submit.confirm_response ? { confirm_response: submit.confirm_response } : {}),
          }
        : undefined,
    })
    abortRef.current = ac
    streamSessionRef.current = sid
  }

  const startMessageStream = (
    sid: string,
    requestContent: string,
    assistantKey: string,
    opts?: {
      shouldAutoTitleAfterStream?: boolean
      autoTitleContent?: string
      streamOptions?: {
        input_response?: ReturnType<typeof buildInputSubmitBody>['input_response']
        confirm_response?: ReturnType<typeof buildConfirmSubmitBody>['confirm_response']
      }
      onConfirmResult?: (result: ConfirmResultPayload) => void
      onStreamSettled?: (outcome: 'done' | 'error', err?: string) => void
    },
  ): AbortController => {
    const flushPendingChunks = () => {
      chunkFlushRafRef.current = null
      const chunk = pendingChunkRef.current
      if (!chunk) return
      pendingChunkRef.current = ''
      setMessages((prev) => {
        const next = [...prev]
        const last = next[next.length - 1]
        if (last?.role === 'assistant') {
          next[next.length - 1] = { ...last, content: last.content + chunk }
        }
        return next
      })
    }

    const scheduleChunkFlush = () => {
      if (chunkFlushRafRef.current != null) return
      chunkFlushRafRef.current = requestAnimationFrame(flushPendingChunks)
    }

    const flushDebugPanelSync = () => {
      if (debugDebounceTimerRef.current) {
        clearTimeout(debugDebounceTimerRef.current)
        debugDebounceTimerRef.current = null
      }
      if (!showDebugRef.current) return
      setDebugText(debugTextRef.current)
      setDebugEventCount(debugEventCountRef.current)
    }

    const scheduleDebugFlush = () => {
      if (!showDebugRef.current) return
      if (debugDebounceTimerRef.current) clearTimeout(debugDebounceTimerRef.current)
      debugDebounceTimerRef.current = setTimeout(() => {
        debugDebounceTimerRef.current = null
        setDebugText(debugTextRef.current)
        setDebugEventCount(debugEventCountRef.current)
      }, DEBUG_UI_DEBOUNCE_MS)
    }

    const finishStreamUi = () => {
      if (chunkFlushRafRef.current != null) {
        cancelAnimationFrame(chunkFlushRafRef.current)
        chunkFlushRafRef.current = null
      }
      flushPendingChunks()
      flushDebugPanelSync()
      setStreaming(false)
      abortRef.current = null
      streamSessionRef.current = null
      setMessageTimelines((prev) => {
        const cur = prev[assistantKey]
        if (!cur) return prev
        return { ...prev, [assistantKey]: finalizeTimeline(cur) }
      })
    }

    return chatApi.sendMessageStream(
      sid,
      requestContent,
      {
        onChunk: (text) => {
          pendingChunkRef.current += text
          scheduleChunkFlush()
        },
        onDone: () => {
          finishStreamUi()
          // Replace ephemeral stream ids (and empty user id) with persisted message ids
          // so Rewind can call the API with real UUIDs.
          if (sid) {
            void reloadMessages(sid).catch(() => {
              /* keep streamed content if reload fails */
            })
          }
          if (opts?.shouldAutoTitleAfterStream && sid) {
            const sidForTitle = sid
            const rawForTitle = opts.autoTitleContent ?? ''
            void (async () => {
              try {
                const s = await chatApi.getSession(sidForTitle)
                if (s.title !== DEFAULT_SESSION_TITLE) return
                const title = rawForTitle.replace(/[\r\n]+/g, ' ').trim().slice(0, 30)
                if (!title) return
                await chatApi.updateSession(sidForTitle, title)
              } catch {
                /* 自动标题失败不打扰正文 */
              }
            })()
          }
          opts?.onStreamSettled?.('done')
        },
        onError: (err) => {
          finishStreamUi()
          setMessages((prev) => {
            const next = [...prev]
            const last = next[next.length - 1]
            if (last?.role === 'assistant') {
              next[next.length - 1] = { ...last, content: last.content || `Error: ${err}` }
            }
            return next
          })
          opts?.onStreamSettled?.('error', err)
        },
        onConfirmRequired: (confirmation) => {
          setConfirmations((prev) => applyIncomingConfirmation(prev, confirmation, assistantKey))
        },
        onConfirmResult: (result) => {
          opts?.onConfirmResult?.(result)
        },
        onInputRequired: (inputRequest) => {
          setInputs((prev) => {
            if (prev.some((c) => c.token === inputRequest.token)) return prev
            return [
              ...prev,
              { ...inputRequest, messageKey: assistantKey, status: 'pending', draft: '' },
            ]
          })
        },
        onSourcesBrowsed: (payload) => {
          setMessageSources((prev) => {
            const existing = prev[assistantKey] ?? []
            const seen = new Set(existing.map((s) => s.url))
            const merged = [...existing]
            for (const s of payload.sources) {
              if (!seen.has(s.url)) {
                seen.add(s.url)
                merged.push(s)
              }
            }
            return { ...prev, [assistantKey]: merged }
          })
        },
        onToolCall: (p) => {
          setMessageTimelines((prev) => ({
            ...prev,
            [assistantKey]: applyToolCall(prev[assistantKey] ?? [], p),
          }))
        },
        onModelCall: (p) => {
          setMessageTimelines((prev) => ({
            ...prev,
            [assistantKey]: applyModelCall(prev[assistantKey] ?? [], p),
          }))
        },
        onDebug: (text) => {
          debugTextRef.current += text
          if (debugTextRef.current.length > MAX_DEBUG_CHARS) {
            debugTextRef.current = debugTextRef.current.slice(-MAX_DEBUG_CHARS)
          }
          debugEventCountRef.current += 1
          if (showDebugRef.current) scheduleDebugFlush()
        },
      },
      opts?.streamOptions,
    )
  }

  const updateConfirmation = (
    item: ChatConfirmationItem,
    patch: Partial<Pick<ChatConfirmationItem, 'status' | 'error'>>
  ) => {
    setConfirmations((prev) => prev.map((c) => (
      c.messageKey === item.messageKey && c.token === item.token ? { ...c, ...patch } : c
    )))
  }

  const submitConfirmation = async (item: ChatConfirmationItem) => {
    if (item.status !== 'pending' || streaming || !agentId) return
    const inflightKey = `${item.kind}:${item.token}`
    if (confirmInFlightRef.current.has(inflightKey)) return
    confirmInFlightRef.current.add(inflightKey)

    updateConfirmation(item, { status: 'confirming', error: undefined })

    let sid = sessionId
    if (!sid) {
      try {
        const res = await chatApi.createSession(agentId)
        sid = res.id
        goTo(agentId, sid)
      } catch (e) {
        confirmInFlightRef.current.delete(inflightKey)
        updateConfirmation(item, { status: 'pending', error: (e as Error).message })
        return
      }
    }

    const body = buildConfirmSubmitBody(item)
    setMessages((prev) => [
      ...prev,
      {
        id: '',
        session_id: sid!,
        role: 'user',
        content: `[confirmed: ${body.confirm_response.kind}]`,
        created_at: new Date().toISOString(),
      },
    ])
    setStreaming(true)

    const assistantKey = `${sid}-assistant-${Date.now()}`
    setMessages((prev) => [
      ...prev,
      {
        id: assistantKey,
        session_id: sid!,
        role: 'assistant',
        content: '',
        created_at: new Date().toISOString(),
      },
    ])

    let confirmResult: ConfirmResultPayload | null = null

    try {
      await new Promise<void>((resolve) => {
        const ac = startMessageStream(sid!, '', assistantKey, {
          streamOptions: { confirm_response: body.confirm_response },
          onConfirmResult: (result) => {
            if (result.token !== item.token) return
            confirmResult = result
            if (item.kind === 'skill_manage') {
              updateConfirmation(item, {
                status: statusFromConfirmResult(result),
                error: result.ok ? undefined : (result.error || '确认失败'),
              })
            }
          },
          onStreamSettled: (outcome, err) => {
            if (item.kind === 'skill_manage') {
              if (!confirmResult) {
                updateConfirmation(item, {
                  status: 'failed',
                  error: err || '未收到确认结果',
                })
              }
            } else if (outcome === 'error') {
              updateConfirmation(item, { status: 'failed', error: err || '确认失败' })
            } else {
              updateConfirmation(item, { status: 'confirmed' })
            }
            resolve()
          },
        })
        abortRef.current = ac
        streamSessionRef.current = sid
      })
    } finally {
      confirmInFlightRef.current.delete(inflightKey)
    }
  }

  const handleConfirmAction = (item: ChatConfirmationItem) => {
    void submitConfirmation(item)
  }

  const handleCancelAction = (item: ChatConfirmationItem) => {
    if (item.status !== 'pending') return
    updateConfirmation(item, { status: 'cancelled' })
  }

  const updateInput = (
    item: ChatInputItem,
    patch: Partial<Pick<ChatInputItem, 'status' | 'error' | 'draft'>>
  ) => {
    setInputs((prev) => prev.map((c) => (
      c.messageKey === item.messageKey && c.token === item.token ? { ...c, ...patch } : c
    )))
  }

  const handleInputSubmit = async (item: ChatInputItem) => {
    if (item.status !== 'pending' || streaming) return
    const value = item.kind === 'confirm' ? 'yes' : item.draft.trim()
    if (item.kind !== 'confirm' && !value) return
    updateInput(item, { status: 'submitting', error: undefined })
    try {
      await handleSend('', buildInputSubmitBody(item, value))
      updateInput(item, { status: 'submitted' })
    } catch (e) {
      updateInput(item, { status: 'pending', error: (e as Error).message })
    }
  }

  const handleInputCancel = async (item: ChatInputItem) => {
    if (item.status !== 'pending' || streaming) return
    updateInput(item, { status: 'submitting', error: undefined })
    try {
      await handleSend('', buildInputSubmitBody(item, '', true))
      updateInput(item, { status: 'cancelled' })
    } catch (e) {
      updateInput(item, { status: 'pending', error: (e as Error).message })
    }
  }

  const handleKeyDown = (e: React.KeyboardEvent) => {
    if (e.key === 'Enter' && !e.shiftKey) {
      e.preventDefault()
      handleSend()
    }
  }

  if (loading) return (
    <div className="chat-loading">
      <div className="loading-spinner" />
      <span>Loading...</span>
    </div>
  )

  const hasAgent = !!agentId && !!agent

  return (
    <div className="chat-page">
      <main className="chat-main">
        <div className="chat-main-header">
          <span className="chat-header-title">Chat</span>
          <button
            type="button"
            className={`chat-debug-toggle ${showDebug ? 'chat-debug-toggle-on' : ''}`}
            onClick={() => setShowDebug((prev) => !prev)}
            disabled={!hasAgent}
          >
            {showDebug ? 'Hide Debug' : 'Show Debug'}
          </button>
        </div>
        {error && (
          <div className="chat-error-banner">
            <span>{error}</span>
            <button type="button" className="chat-error-dismiss" onClick={() => setError('')}>x</button>
          </div>
        )}
        <div className="chat-messages">
          <div className="chat-messages-inner">
            {!hasAgent ? (
              <div className="chat-welcome">
                <p>Select an Agent to start chatting.</p>
              </div>
            ) : !sessionId ? (
              <div className="chat-welcome">
                <p>Start a new conversation.</p>
              </div>
            ) : loadingHistory ? (
              <div className="chat-welcome">
                <div className="loading-spinner" />
                <p>Loading history…</p>
              </div>
            ) : (
              <>
                {messages.map((m, idx) => {
                  if (!isMessageVisibleAtIndex(idx, messages, collapsedBoundaries)) return null
                  const messageKey = m.id || m.created_at + m.role + idx
                  if (isCompactBoundaryMessage(m)) {
                    const boundaryId = m.id || messageKey
                    return (
                      <CompactBoundaryBanner
                        key={messageKey}
                        message={m}
                        hiddenCount={idx}
                        collapsed={collapsedBoundaries.has(boundaryId)}
                        onToggle={() => toggleCompactBoundary(boundaryId)}
                      />
                    )
                  }
                  const sources = messageSources[messageKey] ?? m.metadata?.sources ?? []
                  const isLastAssistant =
                    m.role === 'assistant' &&
                    !messages.slice(idx + 1).some((later) => later.role === 'assistant')
                  const messageConfirmations = confirmations.filter((c) => {
                    if (c.messageKey === messageKey) return true
                    // 流式结束后 assistantKey 可能与落库 message id 不一致：仅把仍可操作的卡挂到最新 assistant
                    return (
                      isLastAssistant &&
                      (c.status === 'pending' || c.status === 'confirming')
                    )
                  })
                  return (
                    <div key={messageKey} className={`chat-msg chat-msg-${m.role}`}>
                      <div className="chat-msg-avatar">{m.role === 'user' ? 'U' : 'A'}</div>
                      <div className="chat-msg-content">
                        {m.role === 'assistant' ? (
                          <>
                            {((messageTimelines[messageKey]?.length ?? 0) > 0 || (m.metadata?.timeline?.length ?? 0) > 0) && (
                              <TimelineView nodes={messageTimelines[messageKey] ?? m.metadata?.timeline ?? []} />
                            )}
                            <SourcesPanel sources={sources} />
                            <AssistantReplyBody
                              sessionId={sessionId}
                              content={m.content}
                              nodes={messageTimelines[messageKey] ?? m.metadata?.timeline ?? []}
                              showCursor={streaming && idx === messages.length - 1}
                              interrupted={(messageTimelines[messageKey] ?? m.metadata?.timeline ?? []).some((n) => n.phase === 'interrupted')}
                            />
                            {(() => {
                              const messageInputs = inputs.filter((c) => {
                                if (c.messageKey === messageKey) return true
                                return (
                                  isLastAssistant &&
                                  (c.status === 'pending' || c.status === 'submitting')
                                )
                              })
                              return messageInputs.map((c) => (
                              <div key={`${c.messageKey}-${c.token}`} className={`chat-input-card chat-input-card-${c.severity || 'default'}`}>
                                <div className="chat-input-title">{c.title}</div>
                                <div className="chat-input-description">{c.prompt}</div>
                                {c.kind === 'select' ? (
                                  <select
                                    className="chat-input-field"
                                    value={c.draft}
                                    disabled={c.status !== 'pending' || streaming}
                                    onChange={(e) => updateInput(c, { draft: e.target.value })}
                                  >
                                    <option value="">Select...</option>
                                    {(c.options || []).map((opt) => (
                                      <option key={opt} value={opt}>{opt}</option>
                                    ))}
                                  </select>
                                ) : c.kind === 'confirm' ? null : (
                                  <input
                                    className="chat-input-field"
                                    type={c.kind === 'password' ? 'password' : 'text'}
                                    autoComplete={c.kind === 'password' ? 'off' : undefined}
                                    value={c.draft}
                                    disabled={c.status !== 'pending' || streaming}
                                    onChange={(e) => updateInput(c, { draft: e.target.value })}
                                  />
                                )}
                                {c.expires_in ? <div className="chat-input-meta">Expires in {c.expires_in}s</div> : null}
                                {c.error ? <div className="chat-input-error">{c.error}</div> : null}
                                <div className="chat-input-actions">
                                  <button
                                    type="button"
                                    className="btn btn-sm"
                                    disabled={c.status !== 'pending' || streaming}
                                    onClick={() => handleInputSubmit(c)}
                                  >
                                    {c.status === 'submitting' ? 'Submitting...' : c.status === 'submitted' ? 'Submitted' : c.status === 'expired' ? 'Expired' : c.kind === 'confirm' ? 'Confirm' : 'Submit'}
                                  </button>
                                  <button
                                    type="button"
                                    className="btn btn-secondary btn-sm"
                                    disabled={c.status !== 'pending' || streaming}
                                    onClick={() => handleInputCancel(c)}
                                  >
                                    {c.status === 'cancelled' ? 'Cancelled' : 'Cancel'}
                                  </button>
                                </div>
                              </div>
                              ))
                            })()}
                            {messageConfirmations.map((c) => {
                              const remaining = remainingConfirmSeconds(c, nowMs)
                              const inactive = c.status !== 'pending'
                              return (
                              <div
                                key={`${c.messageKey}-${c.token}`}
                                className={`chat-confirm-card chat-confirm-card-${c.severity}${inactive ? ' chat-confirm-card-inactive' : ''}`}
                              >
                                <div className="chat-confirm-title">{c.title}</div>
                                <div className="chat-confirm-description">{c.description}</div>
                                <pre className="chat-confirm-dsl">{c.dsl}</pre>
                                {c.status === 'pending' && remaining != null ? (
                                  <div className="chat-confirm-meta">剩余 {remaining}s</div>
                                ) : null}
                                {c.status === 'superseded' ? (
                                  <div className="chat-confirm-meta">{c.error || SUPERSEDED_HINT}</div>
                                ) : null}
                                {c.status === 'expired' ? (
                                  <div className="chat-confirm-meta">{c.error || '确认已过期'}</div>
                                ) : null}
                                {c.status === 'failed' && c.error ? (
                                  <div className="chat-confirm-error">{c.error}</div>
                                ) : null}
                                {c.status === 'pending' && c.error ? (
                                  <div className="chat-confirm-error">{c.error}</div>
                                ) : null}
                                <div className="chat-confirm-actions">
                                  <button
                                    type="button"
                                    className="btn btn-danger btn-sm"
                                    disabled={c.status !== 'pending' || streaming}
                                    onClick={() => handleConfirmAction(c)}
                                  >
                                    {confirmButtonLabel(c.status)}
                                  </button>
                                  <button
                                    type="button"
                                    className="btn btn-secondary btn-sm"
                                    disabled={c.status !== 'pending' || streaming}
                                    onClick={() => handleCancelAction(c)}
                                  >
                                    {c.status === 'cancelled' ? 'Cancelled' : 'Cancel'}
                                  </button>
                                </div>
                              </div>
                              )
                            })}
                          </>
                        ) : (
                          <pre style={{ margin: 0, whiteSpace: 'pre-wrap', fontFamily: 'inherit' }}>{m.content}</pre>
                        )}
                        {!(streaming && idx === messages.length - 1) && formatMessageTime(m.created_at) ? (
                          <div className="chat-msg-time" title={m.created_at}>
                            {formatMessageTime(m.created_at)}
                          </div>
                        ) : null}
                        {m.id && sessionId && !streaming && (m.role === 'user' || m.role === 'assistant') ? (
                          <div className="chat-msg-actions">
                            <button
                              type="button"
                              className="btn btn-secondary btn-sm chat-rewind-btn"
                              title="Hide this message and everything after; continue from earlier context"
                              disabled={rewinding}
                              onClick={() => handleRewind(m.id)}
                            >
                              {rewinding ? 'Rewinding…' : 'Rewind here'}
                            </button>
                          </div>
                        ) : null}
                      </div>
                    </div>
                  )
                })}
                {showDebug && hasAgent && sessionId && (
                  <div className="chat-debug-panel">
                    <div className="chat-debug-panel-header">
                      <span>
                        Debug Stream Events ({debugEventCount}
                        {debugText.length >= MAX_DEBUG_CHARS ? ', truncated' : ''})
                      </span>
                      <button
                        type="button"
                        className="chat-debug-clear"
                        disabled={debugEventCount === 0 && debugText.length === 0}
                        onClick={() => {
                          if (debugDebounceTimerRef.current) {
                            clearTimeout(debugDebounceTimerRef.current)
                            debugDebounceTimerRef.current = null
                          }
                          debugTextRef.current = ''
                          debugEventCountRef.current = 0
                          setDebugText('')
                          setDebugEventCount(0)
                        }}
                      >
                        Clear
                      </button>
                    </div>
                    <pre className="chat-debug-content">
                      {debugText.length > 0 ? debugText : 'No debug events yet.'}
                    </pre>
                  </div>
                )}
                <div ref={messagesEndRef} />
              </>
            )}
          </div>
        </div>
        <div className="chat-input-wrap">
          <div className="chat-input-wrap-inner">
            <textarea
              className="chat-input"
              placeholder={hasAgent ? 'Type a message. Enter to send, Shift+Enter for newline.' : 'Select an Agent first'}
              value={input}
              onChange={(e) => setInput(e.target.value)}
              onKeyDown={handleKeyDown}
              disabled={streaming || !hasAgent}
              rows={2}
            />
            <button
              className="btn chat-send"
              onClick={() => handleSend()}
              disabled={!input.trim() || streaming || !hasAgent}
            >
              {streaming ? 'Sending...' : 'Send'}
            </button>
            {streaming && (
              <button
                className="btn btn-secondary chat-stop"
                onClick={() => {
                  abortRef.current?.abort()
                  streamSessionRef.current = null
                  setStreaming(false)
                }}
              >
                Stop
              </button>
            )}
          </div>
        </div>
      </main>
    </div>
  )
}
