import { useCallback, useEffect, useMemo, useRef, useState } from 'react'
import { createPortal } from 'react-dom'
import { useNavigate, useParams } from 'react-router-dom'
import { agentApi, chatApi, DEFAULT_SESSION_TITLE, modelCatalogApi, type Agent, type ChatAttachment, type ChatMessage, type ModelChoiceItem, type ModelChoiceSelected } from '../api/client'
import { findLatestSessionId, prepareSessionForSend } from '../api/resolveSession'
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
  type PlanStepPayload,
} from '../api/chatStream'
import { SearchableChipSelect } from '../components/SearchableChipSelect'
import { MarkdownContent } from '../components/MarkdownContent'
import { CompactBoundaryBanner } from '../components/CompactBoundaryBanner'
import { SourcesPanel } from '../components/SourcesPanel'
import { SpillResultTable, useSpillTable } from '../components/SpillResultTable'
import { isCompactBoundaryMessage, isMessageVisibleAtIndex } from '../utils/compactBoundary'
import { applyToolCall, applyModelCall, finalizeTimeline, type TimelineNode } from './timelineReducer'
import { toolVerb } from './toolVerbMap'
import { sendTerminalChat, type TerminalChatResponse } from '../api/terminal'
import './ChatPage.css'

const ATTACH_ACCEPT = '.png,.jpg,.jpeg,.webp,.gif,.txt,.log,.md,.json,.csv,image/*'

/** Detects "@打开 <vmid> 终端" pattern. Returns the numeric VMID if matched, null otherwise. */
const TERMINAL_OPEN_RE = /@打开\s*(\d+)\s*终端/

type TerminalOutput = {
  key: string
  role: 'user' | 'cmd' | 'stdout' | 'stderr' | 'ok'
  content: string
  /** Working directory when this stdout listing was produced (for click-to-cd). */
  cwd?: string
}

/** Unwrap VM /runCmd JSON if the backend still returns raw payloads; keep tables intact. */
function formatTerminalStdout(raw: string): string {
  const trimmed = (raw ?? '').trim()
  if (!trimmed) return ''
  let text = raw
  if (trimmed.startsWith('{') && trimmed.includes('codeDesc')) {
    try {
      const parsed = JSON.parse(trimmed) as { codeDesc?: string; retCode?: number }
      if (typeof parsed.codeDesc === 'string') {
        text = parsed.codeDesc.replace(/\r\n/g, '\n').replace(/\r/g, '\n').replace(/^\n+|\n+$/g, '')
        if (parsed.retCode && parsed.retCode !== 0) {
          text = text ? `${text}\n[exit ${parsed.retCode}]` : `[exit ${parsed.retCode}]`
        }
      }
    } catch {
      /* keep raw */
    }
  }
  return prettifyTerminalText(text)
}

/** Pretty-print JSON log lines; leave dir/tasklist tables alone. */
function prettifyTerminalText(text: string): string {
  if (!text) return text
  if (/的目录|Directory of/i.test(text)) return text
  const lines = text.replace(/\r\n/g, '\n').replace(/\r/g, '\n').split('\n')
  return lines.map((line) => prettifyTerminalLine(line)).join('\n')
}

function prettifyTerminalLine(line: string): string {
  const t = line.trim()
  if (!t) return line
  if (!(t.startsWith('{') || t.startsWith('['))) return line
  try {
    const parsed = JSON.parse(t) as unknown
    // Nested runCmd wrapper that slipped through
    if (parsed && typeof parsed === 'object' && !Array.isArray(parsed) && 'codeDesc' in parsed) {
      const cd = (parsed as { codeDesc?: unknown }).codeDesc
      if (typeof cd === 'string') {
        const inner = cd.trim()
        if (inner.startsWith('{') || inner.startsWith('[')) {
          try {
            return JSON.stringify(JSON.parse(inner), null, 2)
          } catch {
            return cd.replace(/\\n/g, '\n').replace(/\\t/g, '\t')
          }
        }
        return cd.replace(/\\n/g, '\n').replace(/\\t/g, '\t')
      }
    }
    return JSON.stringify(parsed, null, 2)
  } catch {
    return line
  }
}

function isDirListingText(text: string): boolean {
  return /的目录|Directory of/i.test(text)
}

/** Prefer path printed by `dir` header; fall back to session cwd. */
function extractListingCwd(stdout: string, fallback: string): string {
  const en = stdout.match(/Directory of\s+(.+)/i)
  if (en?.[1]) return en[1].trim().replace(/\//g, '\\')
  const zh = stdout.match(/^(.+?)\s*的目录\s*$/m)
  if (zh?.[1]) return zh[1].trim().replace(/\//g, '\\')
  return fallback
}

function joinWinPath(base: string, name: string): string {
  const n = name.trim()
  if (!n) return base
  if (/^[a-zA-Z]:[\\/]?/.test(n)) {
    return n.length === 2 ? `${n.toUpperCase()}\\` : n.replace(/\//g, '\\')
  }
  if (n === '..') {
    const trimmed = base.replace(/[\\/]+$/, '')
    const i = trimmed.lastIndexOf('\\')
    if (i <= 0) return /^[a-zA-Z]:$/i.test(trimmed) ? `${trimmed}\\` : 'C:\\'
    if (i === 2 && trimmed[1] === ':') return `${trimmed.slice(0, 2)}\\`
    return trimmed.slice(0, i)
  }
  if (n === '.') return base
  const root = base.replace(/[\\/]+$/, '')
  return `${root}\\${n}`
}

function terminalStdoutEntries(
  batchKey: string,
  stdout: string,
  stderr?: string,
  sessionCwd?: string
): TerminalOutput[] {
  const entries: TerminalOutput[] = []
  const formatted = formatTerminalStdout(stdout)
  if (formatted) {
    entries.push({
      key: `${batchKey}-out`,
      role: 'stdout',
      content: formatted,
      cwd: extractListingCwd(formatted, sessionCwd || 'C:\\'),
    })
  } else if (!stderr) {
    entries.push({ key: `${batchKey}-ok`, role: 'ok', content: '(成功，无输出)' })
  }
  if (stderr) {
    entries.push({ key: `${batchKey}-err`, role: 'stderr', content: stderr })
  }
  return entries
}

const DIR_LINE_RE = /<(DIR|JUNCTION|SYMLINKD)>\s+(.+?)(?:\s+\[[^\]]*\])?\s*$/i
const DIR_FILE_LINE_RE =
  /^\s*\d{4}[/-]\d{2}[/-]\d{2}\s+\d{1,2}:\d{2}(?:\s*[AP]M)?\s+([\d,]+)\s+(.+?)\s*$/i

type TerminalEntry = { name: string; kind: 'dir' | 'file' }

/** Parse `dir` listing into clickable/completable names; null if not a listing. */
function parseDirEntries(stdout: string): TerminalEntry[] | null {
  if (!/的目录|Directory of/i.test(stdout)) return null
  const entries: TerminalEntry[] = []
  const seen = new Set<string>()
  for (const line of stdout.split('\n')) {
    const dirM = line.match(DIR_LINE_RE)
    if (dirM) {
      const name = dirM[2].trim()
      if (!name || name === '.' || seen.has(name)) continue
      seen.add(name)
      entries.push({ name, kind: 'dir' })
      continue
    }
    const fileM = line.match(DIR_FILE_LINE_RE)
    if (fileM) {
      const name = fileM[2].trim()
      if (!name || seen.has(name)) continue
      seen.add(name)
      entries.push({ name, kind: 'file' })
    }
  }
  return entries
}

/** Last whitespace-separated token in the composer (for path/file completion). */
function getCompletionQuery(text: string): { query: string; replaceFrom: number } {
  const m = /([^\s]*)$/.exec(text)
  if (!m) return { query: '', replaceFrom: text.length }
  const replaceFrom = m.index ?? text.length
  let query = m[1]
  if (/^["'「]/.test(query)) query = query.slice(1)
  query = query.replace(/["'」]+$/, '')
  return { query, replaceFrom }
}

function quoteCmdPath(name: string): string {
  if (/[\s&()^]/.test(name)) return `"${name.replace(/"/g, '')}"`
  return name
}

function TerminalStdoutView({
  content,
  disabled,
  onEnterDir,
}: {
  content: string
  disabled: boolean
  onEnterDir: (name: string) => void
}) {
  const lines = content.split('\n')
  const tabular = isDirListingText(content)
  return (
    <pre className={`terminal-stdout${tabular ? ' terminal-stdout--table' : ' terminal-stdout--wrap'}`}>
      {lines.map((line, i) => {
        const m = line.match(DIR_LINE_RE)
        if (!m) {
          return (
            <span key={i}>
              {line}
              {i < lines.length - 1 ? '\n' : ''}
            </span>
          )
        }
        const name = m[2].trim()
        if (!name || name === '.') {
          return (
            <span key={i}>
              {line}
              {i < lines.length - 1 ? '\n' : ''}
            </span>
          )
        }
        const idx = line.lastIndexOf(name)
        const before = idx >= 0 ? line.slice(0, idx) : line
        const after = idx >= 0 ? line.slice(idx + name.length) : ''
        return (
          <span key={i}>
            {before}
            <button
              type="button"
              className="terminal-dir-link"
              title={`进入 ${name}`}
              disabled={disabled}
              onClick={() => onEnterDir(name)}
            >
              {name}
            </button>
            {after}
            {i < lines.length - 1 ? '\n' : ''}
          </span>
        )
      })}
    </pre>
  )
}

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

async function downloadAttachment(sessionId: string, att: ChatAttachment) {
  const blob = await chatApi.fetchAttachmentBlob(sessionId, att.id)
  const url = URL.createObjectURL(blob)
  const a = document.createElement('a')
  a.href = url
  a.download = att.name || 'download'
  a.rel = 'noopener'
  document.body.appendChild(a)
  a.click()
  a.remove()
  URL.revokeObjectURL(url)
}

function AttachmentImageThumb({
  sessionId,
  att,
  className = 'chat-att-thumb',
}: {
  sessionId: string
  att: ChatAttachment
  className?: string
}) {
  const [url, setUrl] = useState<string | null>(null)
  const [open, setOpen] = useState(false)

  useEffect(() => {
    let objectUrl = ''
    let cancelled = false
    void chatApi
      .fetchAttachmentBlob(sessionId, att.id)
      .then((blob) => {
        objectUrl = URL.createObjectURL(blob)
        if (!cancelled) setUrl(objectUrl)
      })
      .catch(() => {
        /* thumbnail optional */
      })
    return () => {
      cancelled = true
      if (objectUrl) URL.revokeObjectURL(objectUrl)
    }
  }, [sessionId, att.id])

  useEffect(() => {
    if (!open) return
    const onKey = (e: KeyboardEvent) => {
      if (e.key === 'Escape') setOpen(false)
    }
    const prevOverflow = document.body.style.overflow
    document.body.style.overflow = 'hidden'
    document.addEventListener('keydown', onKey)
    return () => {
      document.body.style.overflow = prevOverflow
      document.removeEventListener('keydown', onKey)
    }
  }, [open])

  if (!url) {
    return <span className={`${className} chat-att-thumb--loading`} title={att.name}>{att.name}</span>
  }

  const lightbox = open
    ? createPortal(
        <div
          className="chat-lightbox"
          role="dialog"
          aria-modal="true"
          aria-label={`预览 ${att.name}`}
          onClick={() => setOpen(false)}
        >
          <button
            type="button"
            className="chat-lightbox__close"
            aria-label="关闭预览"
            onClick={() => setOpen(false)}
          >
            ×
          </button>
          <img
            className="chat-lightbox__img"
            src={url}
            alt={att.name}
            onClick={(e) => e.stopPropagation()}
          />
          <div className="chat-lightbox__caption">{att.name}</div>
        </div>,
        document.body,
      )
    : null

  return (
    <>
      <button
        type="button"
        className="chat-att-thumb-btn"
        title={`点击放大 · ${att.name}`}
        aria-label={`放大预览 ${att.name}`}
        onClick={() => setOpen(true)}
      >
        <img className={className} src={url} alt={att.name} />
      </button>
      {lightbox}
    </>
  )
}

function MessageAttachmentList({
  sessionId,
  attachments,
}: {
  sessionId: string
  attachments: ChatAttachment[]
}) {
  if (!attachments.length) return null
  return (
    <div className="chat-msg-attachments">
      {attachments.map((att) =>
        att.kind === 'image' ? (
          <AttachmentImageThumb key={att.id} sessionId={sessionId} att={att} />
        ) : (
          <button
            key={att.id}
            type="button"
            className="chat-att-file"
            title={`下载 ${att.name}`}
            onClick={() => {
              void downloadAttachment(sessionId, att).catch(() => {
                /* ignore download errors in bubble */
              })
            }}
          >
            {att.name}
          </button>
        ),
      )}
    </div>
  )
}

export interface ChatPageProps {
  agentId?: string
  sessionId?: string
  isHome?: boolean
  onNavigate?: (agentId: string, sessionId?: string) => void
  agents?: Agent[]
  onAgentChange?: (agentId: string) => void
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

type PlanState = { steps: PlanStepPayload[]; currentStepId?: string }

function PlanPanel({ plan }: { plan: PlanState }) {
  const currentIndex = plan.currentStepId ? plan.steps.findIndex((s) => s.id === plan.currentStepId) : -1
  if (plan.steps.length === 0) return null
  return (
    <div className="plan-panel">
      <div className="plan-panel-title">Plan</div>
      <ol className="plan-steps">
        {plan.steps.map((s, i) => {
          const state = i < currentIndex ? 'done' : i === currentIndex ? 'active' : 'pending'
          return (
            <li key={s.id || `${i}`} className={`plan-step plan-step-${state}`}>
              <span className="plan-step-marker">{i < currentIndex ? '✓' : i === currentIndex ? '▶' : '·'}</span>
              <span className="plan-step-goal">{s.goal}</span>
              {s.suggested_tools && s.suggested_tools.length > 0 && (
                <span className="plan-step-tools">{s.suggested_tools.join(', ')}</span>
              )}
            </li>
          )
        })}
      </ol>
    </div>
  )
}

function AssistantReplyBody({
  sessionId,
  content,
  nodes,
  showCursor,
  interrupted,
  emptyReply,
}: {
  sessionId?: string
  content: string
  nodes: TimelineNode[]
  showCursor: boolean
  interrupted: boolean
  emptyReply?: boolean
}) {
  const spill = useSpillTable(sessionId, content, nodes)
  const blank = !showCursor && !(spill.displayContent ?? '').trim()
  const banner = spill.table
    ? `标题写的行数多于对话里贴出的表格；下面已加载工具落盘的完整 ${spill.table.rows.length} 行。`
    : spill.hint ?? (interrupted ? '这条回复在生成时被中断，内容可能不完整。' : null)
  return (
    <>
      {banner && <p className="chat-truncated-banner">{banner}{spill.loading ? ' 正在加载…' : ''}{spill.error ? ` ${spill.error}` : ''}</p>}
      {spill.table && <SpillResultTable columns={spill.table.columns} rows={spill.table.rows} />}
      {blank || emptyReply ? (
        <p className="chat-empty-reply-marker" role="status">
          {interrupted
            ? '本轮已取消，未生成回复。'
            : (spill.displayContent ?? '').trim() || '本轮未生成有效回复，请重试或换个说法继续。'}
        </p>
      ) : (
        <MarkdownContent showCursor={showCursor}>{spill.displayContent}</MarkdownContent>
      )}
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

function TimelineCard({ nodes }: { nodes: TimelineNode[] }) {
  const [expanded, setExpanded] = useState(false)
  if (!nodes.length) return null
  const running = nodes.some((n) => (n.kind === 'tool' && n.phase === 'started') || (n.kind === 'model' && n.phase === 'invoked'))
  const failed = nodes.some((n) => n.kind === 'tool' && (n.phase === 'failed' || n.allowed === false))
  const interrupted = nodes.some((n) => n.phase === 'interrupted')
  let badge = '执行成功'
  let badgeMod = 'ok'
  if (running) {
    badge = '执行中'
    badgeMod = 'run'
  } else if (interrupted) {
    badge = '已中断'
    badgeMod = 'warn'
  } else if (failed) {
    badge = '有失败'
    badgeMod = 'fail'
  }
  const visible = expanded || nodes.length === 1 ? nodes : nodes.slice(-1)
  return (
    <div className={`chat-exec-card${expanded ? ' is-open' : ''}`}>
      <button
        type="button"
        className="chat-exec-card__head"
        aria-expanded={expanded}
        onClick={() => setExpanded((v) => !v)}
      >
        <span className="chat-exec-card__title">
          <span className="chat-exec-card__chevron" aria-hidden />
          执行链路
          {nodes.length > 1 ? (
            <span className="chat-exec-card__count">{expanded ? `${nodes.length} 步` : `最近 1 / ${nodes.length}`}</span>
          ) : null}
        </span>
        <span className={`chat-exec-card__badge is-${badgeMod}`}>{badge}</span>
      </button>
      <div className="chat-exec-card__body">
        <TimelineView nodes={visible} />
      </div>
    </div>
  )
}

function choiceValue(selected: ModelChoiceSelected | null | undefined): string {
  if (!selected?.provider_id || !selected.model) return 'agent_default'
  return `${selected.provider_id}::${selected.model}`
}

export default function ChatPage(props?: ChatPageProps) {
  const params = useParams()
  const navigate = useNavigate()
  const agentId = props?.agentId ?? params.id
  const sessionId = props?.sessionId ?? params.sessionId
  const isHome = props?.isHome ?? false
  const onNavigate = props?.onNavigate
  const homeAgents = props?.agents
  const onAgentChange = props?.onAgentChange
  const [agent, setAgent] = useState<Agent | null>(null)
  const [messages, setMessages] = useState<ChatMessage[]>([])
  const [confirmations, setConfirmations] = useState<ChatConfirmationItem[]>([])
  const [inputs, setInputs] = useState<ChatInputItem[]>([])
  const [messageSources, setMessageSources] = useState<Record<string, WebSourceItem[]>>({})
  const [messageTimelines, setMessageTimelines] = useState<Record<string, TimelineNode[]>>({})
  /** plan 模式：messageKey → 规划 + 当前步骤（流式期间展示，刷新后丢弃） */
  const [messagePlans, setMessagePlans] = useState<Record<string, PlanState>>({})
  /** compact boundary 消息 id → 是否折叠其上方历史（默认展开，不在 Set 中） */
  const [collapsedBoundaries, setCollapsedBoundaries] = useState<Set<string>>(() => new Set())
  /** 向前翻页游标；空串表示已到会话开头（不显示「加载更早」）。 */
  const [historyCursor, setHistoryCursor] = useState('')
  const [loadingEarlier, setLoadingEarlier] = useState(false)
  /** 本轮被用户停止/取消（服务端已保存部分回复） */
  const [cancelledNotice, setCancelledNotice] = useState(false)
  /** 仅用于展示；实际缓冲在 ref 中合并刷新，避免每条 SSE 触发整页重绘 */
  const [debugText, setDebugText] = useState('')
  const [debugEventCount, setDebugEventCount] = useState(0)
  const [showDebug] = useState(false)
  const [input, setInput] = useState('')
  const [streaming, setStreaming] = useState(false)
  const [loading, setLoading] = useState(true)
  const [loadingHistory, setLoadingHistory] = useState(false)
  const [error, setError] = useState('')
  const [rewinding, setRewinding] = useState(false)
  const [forking, setForking] = useState(false)
  const [modelChoices, setModelChoices] = useState<ModelChoiceItem[]>([])
  const [modelChoice, setModelChoice] = useState('agent_default')
  const [nowMs, setNowMs] = useState(() => Date.now())
  const [pendingAttachments, setPendingAttachments] = useState<ChatAttachment[]>([])
  const [uploadingCount, setUploadingCount] = useState(0)
  const [composerDragOver, setComposerDragOver] = useState(false)
  // Terminal mode
  const [terminalMode, setTerminalMode] = useState(false)
  const [terminalVMID, setTerminalVMID] = useState('')
  const [terminalSessionID, setTerminalSessionID] = useState<string | null>(null)
  const [terminalWorkDir, setTerminalWorkDir] = useState('C:\\')
  const [terminalBusy, setTerminalBusy] = useState(false)
  const [terminalOutputs, setTerminalOutputs] = useState<TerminalOutput[]>([])
  const [terminalEntries, setTerminalEntries] = useState<TerminalEntry[]>([])
  const [suggestIndex, setSuggestIndex] = useState(0)
  const [suggestDismissed, setSuggestDismissed] = useState(false)
  const [suggestForceAll, setSuggestForceAll] = useState(false)
  const suggestOpenRef = useRef(false)
  /** Avoid ghost click on Send after picking a suggestion (mousedown → list closes → mouseup on Send). */
  const suppressSendUntilRef = useRef(0)

  const exitTerminalMode = useCallback(() => {
    setTerminalMode(false)
    setTerminalOutputs([])
    setTerminalEntries([])
    setSuggestIndex(0)
    setSuggestDismissed(false)
    setSuggestForceAll(false)
    setTerminalVMID('')
    setTerminalSessionID(null)
    setTerminalWorkDir('C:\\')
  }, [])

  const terminalSuggestions = useMemo(() => {
    if (!terminalMode || terminalEntries.length === 0) return []
    const { query } = getCompletionQuery(input)
    const q = query.toLowerCase()
    if (!q && !suggestForceAll) return []
    const list = !q
      ? terminalEntries.filter((e) => e.name !== '..')
      : terminalEntries.filter((e) => e.name !== '..' && e.name.toLowerCase().includes(q))
    return list
      .sort((a, b) => {
        if (!q) return a.name.localeCompare(b.name)
        const al = a.name.toLowerCase()
        const bl = b.name.toLowerCase()
        const as = al.startsWith(q) ? 0 : 1
        const bs = bl.startsWith(q) ? 0 : 1
        if (as !== bs) return as - bs
        return al.localeCompare(bl)
      })
      .slice(0, 10)
  }, [terminalMode, terminalEntries, input, suggestForceAll])

  const suggestOpen = terminalMode && !suggestDismissed && terminalSuggestions.length > 0
  suggestOpenRef.current = suggestOpen

  useEffect(() => {
    setSuggestIndex(0)
  }, [terminalSuggestions])

  const applyTerminalSuggestion = useCallback((name: string) => {
    setInput((prev) => {
      const { replaceFrom } = getCompletionQuery(prev)
      return prev.slice(0, replaceFrom) + quoteCmdPath(name)
    })
    setSuggestIndex(0)
    setSuggestDismissed(true)
    setSuggestForceAll(false)
    suppressSendUntilRef.current = Date.now() + 400
  }, [])

  useEffect(() => {
    if (!terminalMode) return
    const onKeyDown = (e: KeyboardEvent) => {
      if (e.key !== 'Escape') return
      const t = e.target as HTMLElement | null
      if (t?.closest?.('[role="dialog"], [aria-modal="true"]')) return
      if (suggestOpenRef.current) {
        e.preventDefault()
        setSuggestDismissed(true)
        setSuggestForceAll(false)
        return
      }
      e.preventDefault()
      exitTerminalMode()
    }
    window.addEventListener('keydown', onKeyDown)
    return () => window.removeEventListener('keydown', onKeyDown)
  }, [terminalMode, exitTerminalMode])
  const messagesEndRef = useRef<HTMLDivElement>(null)
  const terminalAreaRef = useRef<HTMLDivElement>(null)
  const terminalEndRef = useRef<HTMLDivElement>(null)
  const fileInputRef = useRef<HTMLInputElement>(null)
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
    if (!agentId || sessionId) return
    let cancelled = false
    void (async () => {
      try {
        const latest = await findLatestSessionId(agentId, chatApi)
        if (cancelled || !latest) return
        goTo(agentId, latest)
      } catch {
        /* keep empty composer; send path will create if needed */
      }
    })()
    return () => {
      cancelled = true
    }
  }, [agentId, sessionId, goTo])

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
    if (!agentId) {
      setModelChoices([])
      setModelChoice('agent_default')
      return
    }
    let cancelled = false
    modelCatalogApi
      .listModelChoices(agentId, sessionId)
      .then((res) => {
        if (cancelled) return
        setModelChoices(res.items)
        setModelChoice(choiceValue(res.selected))
      })
      .catch((e) => {
        if (!cancelled) setError(e.message)
      })
    return () => {
      cancelled = true
    }
  }, [agentId, sessionId])

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
        setHistoryCursor(res.next_cursor ?? '')
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
    setHistoryCursor(res.next_cursor ?? '')
    setMessageTimelines({})
    setMessageSources({})
    setCollapsedBoundaries(new Set())
    setConfirmations(restoreConfirmationsFromMessages(res.items))
    setInputs(restoreInputsFromMessages(res.items))
  }, [])

  /** 向上翻页：把更早的一页插到最前面。 */
  const loadEarlier = useCallback(async () => {
    const sid = sessionId
    if (!sid || !historyCursor || loadingEarlier) return
    setLoadingEarlier(true)
    try {
      const res = await chatApi.listMessages(sid, { before: historyCursor })
      if (sessionIdRef.current !== sid) return
      setMessages((prev) => [...res.items, ...prev])
      setHistoryCursor(res.next_cursor ?? '')
    } catch (e) {
      setError((e as Error).message)
    } finally {
      setLoadingEarlier(false)
    }
  }, [historyCursor, loadingEarlier, sessionId])

  const handleRewind = useCallback(async (messageId: string) => {
    if (!sessionId || !messageId || streaming || rewinding || forking) return
    if (!window.confirm('回溯到这条消息之前？之后的消息会从对话和搜索中隐藏。')) {
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
  }, [sessionId, streaming, rewinding, forking, reloadMessages])

  const handleFork = useCallback(async (messageId: string) => {
    if (!sessionId || !messageId || streaming || rewinding || forking) return
    setForking(true)
    setError('')
    try {
      abortRef.current?.abort()
      const out = await chatApi.forkSession(sessionId, messageId)
      if (agentId && out.session_id) goTo(agentId, out.session_id)
    } catch (e) {
      setError((e as Error).message)
    } finally {
      setForking(false)
    }
  }, [sessionId, agentId, streaming, rewinding, forking, goTo])

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

  useEffect(() => {
    if (!terminalMode) return
    const area = terminalAreaRef.current
    if (area) {
      area.scrollTop = area.scrollHeight
      return
    }
    terminalEndRef.current?.scrollIntoView({ behavior: 'auto', block: 'end' })
  }, [terminalMode, terminalOutputs, terminalBusy])

  const hasPendingConfirm = confirmations.some((c) => c.status === 'pending')

  const prevSessionIdRef = useRef(sessionId)
  useEffect(() => {
    const prev = prevSessionIdRef.current
    prevSessionIdRef.current = sessionId
    setComposerDragOver(false)
    // Clear pending only when switching between two concrete sessions (not undefined → id during first attach).
    if (prev && sessionId && prev !== sessionId) {
      setPendingAttachments([])
      setUploadingCount(0)
    }
  }, [sessionId])

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

  const runTerminalTurn = async (opts: {
    vmid: number
    userLabel: string
    content: string
    cmd?: string
    /** After a successful cd, refresh directory listing. */
    listAfter?: boolean
  }) => {
    setTerminalBusy(true)
    setTerminalOutputs((prev) => [...prev, { key: `tu-${Date.now()}`, role: 'user', content: opts.userLabel }])
    try {
      const res: TerminalChatResponse = await sendTerminalChat({
        agent_id: agentId!,
        vmid: opts.vmid,
        content: opts.content,
        cmd: opts.cmd,
        session_id: terminalSessionID ?? undefined,
      })
      if (res.session_id) setTerminalSessionID(res.session_id)
      const nextCwd = res.workdir || terminalWorkDir
      if (res.workdir) setTerminalWorkDir(res.workdir)
      const batchKey = `t-${Date.now()}`
      setTerminalOutputs((prev) => [
        ...prev,
        { key: `${batchKey}-cmd`, role: 'cmd', content: res.cmd },
        ...terminalStdoutEntries(batchKey, res.stdout, res.stderr, nextCwd),
      ])
      {
        const listing = parseDirEntries(formatTerminalStdout(res.stdout) || res.stdout || '')
        if (listing) setTerminalEntries(listing)
      }

      const failed = Boolean(res.stderr) || /\[exit\s+\d+\]/i.test(res.stdout || '') || /找不到指定的路径/.test(res.stdout || '')
      if (opts.listAfter && !failed) {
        const listRes: TerminalChatResponse = await sendTerminalChat({
          agent_id: agentId!,
          vmid: opts.vmid,
          content: '列出当前目录',
          cmd: 'dir',
          session_id: res.session_id || terminalSessionID || undefined,
        })
        if (listRes.session_id) setTerminalSessionID(listRes.session_id)
        const listCwd = listRes.workdir || nextCwd
        if (listRes.workdir) setTerminalWorkDir(listRes.workdir)
        const listKey = `t-${Date.now()}-ls`
        setTerminalOutputs((prev) => [
          ...prev,
          { key: `${listKey}-cmd`, role: 'cmd', content: listRes.cmd },
          ...terminalStdoutEntries(listKey, listRes.stdout, listRes.stderr, listCwd),
        ])
        {
          const listing = parseDirEntries(formatTerminalStdout(listRes.stdout) || listRes.stdout || '')
          if (listing) setTerminalEntries(listing)
        }
      }
    } catch (err) {
      setTerminalOutputs((prev) => [
        ...prev,
        { key: `terr-${Date.now()}`, role: 'stderr', content: err instanceof Error ? err.message : 'Unknown error' },
      ])
    } finally {
      setTerminalBusy(false)
    }
  }

  const handleEnterTerminalDir = (name: string, listingCwd?: string) => {
    if (terminalBusy || !agentId) return
    const vmid = parseInt(terminalVMID, 10)
    if (isNaN(vmid) || vmid <= 0) return
    const base = listingCwd || terminalWorkDir || 'C:\\'
    const target = joinWinPath(base, name)
    const cmd = `cd /d ${quoteCmdPath(target)}`
    void runTerminalTurn({
      vmid,
      userLabel: `进入 ${name}`,
      content: `进入 ${target}`,
      cmd,
      listAfter: true,
    })
  }

  const handleSend = async (
    overrideContent?: string,
    submit?: {
      input_response?: ReturnType<typeof buildInputSubmitBody>['input_response']
      confirm_response?: ReturnType<typeof buildConfirmSubmitBody>['confirm_response']
    }
  ) => {
    const content = (overrideContent ?? input).trim()
    if (Date.now() < suppressSendUntilRef.current) return

    // Terminal mode: detect "@打开 <vmid> 终端" to enter, then translate NL→cmd
    const termMatch = content.match(TERMINAL_OPEN_RE)
    if (termMatch && !terminalMode) {
      const vmid = parseInt(termMatch[1], 10)
      if (!isNaN(vmid) && vmid > 0) {
        setTerminalMode(true)
        setTerminalVMID(String(vmid))
        setTerminalWorkDir('C:\\')
        setTerminalSessionID(null)
        setTerminalEntries([])
        setSuggestDismissed(false)
        setSuggestForceAll(false)
        const rest = content.replace(TERMINAL_OPEN_RE, '').trim()
        setInput('')
        if (!rest) return
        await runTerminalTurn({ vmid, userLabel: rest, content: rest })
        return
      }
    }

    if (terminalMode && content && !submit) {
      const vmid = parseInt(terminalVMID, 10)
      if (isNaN(vmid) || vmid <= 0) return
      setInput('')
      await runTerminalTurn({ vmid, userLabel: content, content })
      return
    }

    const pendingSnapshot = pendingAttachments
    const attachmentIds = submit ? [] : pendingSnapshot.map((a) => a.id)
    if ((!content && !submit && attachmentIds.length === 0) || !agentId || streaming || uploadingCount > 0) return

    let sid = sessionId
    let userMsgCountBefore = messages.filter((m) => m.role === 'user').length
    if (!sid) {
      try {
        const prepared = await prepareSessionForSend(agentId, sessionId, chatApi)
        sid = prepared.id
        if (prepared.history !== null) {
          const history = prepared.history as ChatMessage[]
          setMessages(history)
          setConfirmations(restoreConfirmationsFromMessages(history))
          setInputs(restoreInputsFromMessages(history))
          userMsgCountBefore = history.filter((m) => m.role === 'user').length
        }
        streamSessionRef.current = sid
        setStreaming(true)
        goTo(agentId, sid)
      } catch (e) {
        alert((e as Error).message)
        return
      }
    }

    const shouldAutoTitleAfterStream =
      !submit && content.length > 0 && userMsgCountBefore === 0

    if (!overrideContent && !submit) setInput('')
    if (!submit && attachmentIds.length > 0) setPendingAttachments([])
    const userDisplay = submit?.input_response
      ? inputProvidedLabel(submit.input_response.field)
      : submit?.confirm_response
        ? `[confirmed: ${submit.confirm_response.kind}]`
        : content
    setMessages((prev) => [
      ...prev,
      {
        id: '',
        session_id: sid,
        role: 'user',
        content: userDisplay,
        created_at: new Date().toISOString(),
        metadata:
          attachmentIds.length > 0 ? { attachments: pendingSnapshot } : undefined,
      },
    ])
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
        : attachmentIds.length > 0
          ? { attachment_ids: attachmentIds }
          : undefined,
    })
    abortRef.current = ac
    streamSessionRef.current = sid
  }

  const ensureSessionForAttach = useCallback(async (): Promise<string | null> => {
    if (!agentId) return null
    if (sessionId) return sessionId
    try {
      const prepared = await prepareSessionForSend(agentId, sessionId, chatApi)
      const sid = prepared.id
      if (prepared.history !== null) {
        const history = prepared.history as ChatMessage[]
        setMessages(history)
        setConfirmations(restoreConfirmationsFromMessages(history))
        setInputs(restoreInputsFromMessages(history))
      }
      goTo(agentId, sid)
      return sid
    } catch (e) {
      setError((e as Error).message)
      return null
    }
  }, [agentId, sessionId, goTo])

  const uploadFiles = useCallback(
    async (files: FileList | File[]) => {
      const list = Array.from(files).filter(Boolean)
      if (!list.length || !agentId) return
      const sid = await ensureSessionForAttach()
      if (!sid) return
      for (const file of list) {
        setUploadingCount((c) => c + 1)
        try {
          const att = await chatApi.uploadAttachment(sid, file)
          setPendingAttachments((prev) => [...prev, att])
        } catch (e) {
          setError((e as Error).message)
        } finally {
          setUploadingCount((c) => Math.max(0, c - 1))
        }
      }
    },
    [agentId, ensureSessionForAttach],
  )

  const removePendingAttachment = useCallback(
    async (attId: string) => {
      setPendingAttachments((prev) => prev.filter((a) => a.id !== attId))
      const sid = sessionId
      if (!sid) return
      try {
        await chatApi.deleteAttachment(sid, attId)
      } catch (e) {
        if ((e as { status?: number }).status === 409) return
        setError((e as Error).message)
      }
    },
    [sessionId],
  )

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
        attachment_ids?: string[]
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
        onPlan: (plan) => {
          setMessagePlans((prev) => ({ ...prev, [assistantKey]: { steps: plan.steps } }))
        },
        onPlanStep: (step) => {
          setMessagePlans((prev) => {
            const cur = prev[assistantKey]
            if (!cur) return prev
            return { ...prev, [assistantKey]: { ...cur, currentStepId: step.id } }
          })
        },
        onCancelled: () => {
          setCancelledNotice(true)
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
        const prepared = await prepareSessionForSend(agentId, sessionId, chatApi)
        sid = prepared.id
        if (prepared.history !== null) {
          const history = prepared.history as ChatMessage[]
          setMessages(history)
          setConfirmations(restoreConfirmationsFromMessages(history))
          setInputs(restoreInputsFromMessages(history))
        }
        streamSessionRef.current = sid
        setStreaming(true)
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
    if (terminalMode && suggestOpen) {
      if (e.key === 'ArrowDown') {
        e.preventDefault()
        setSuggestIndex((i) => (i + 1) % terminalSuggestions.length)
        return
      }
      if (e.key === 'ArrowUp') {
        e.preventDefault()
        setSuggestIndex((i) => (i - 1 + terminalSuggestions.length) % terminalSuggestions.length)
        return
      }
      if (e.key === 'Tab' || (e.key === 'Enter' && !e.shiftKey)) {
        e.preventDefault()
        const pick = terminalSuggestions[suggestIndex] || terminalSuggestions[0]
        if (pick) applyTerminalSuggestion(pick.name)
        return
      }
    }
    if (e.key === 'Tab' && terminalMode && terminalEntries.length > 0) {
      e.preventDefault()
      if (terminalSuggestions[0]) {
        applyTerminalSuggestion(terminalSuggestions[0].name)
      } else {
        setSuggestDismissed(false)
        setSuggestForceAll(true)
      }
      return
    }
    if (e.key === 'Enter' && !e.shiftKey) {
      e.preventDefault()
      if (Date.now() < suppressSendUntilRef.current) return
      handleSend()
    }
  }

  const handleComposerPaste = (e: React.ClipboardEvent) => {
    const files = e.clipboardData?.files
    if (!files?.length) return
    const images = Array.from(files).filter((f) => f.type.startsWith('image/'))
    if (!images.length) return
    e.preventDefault()
    void uploadFiles(images)
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
          <span className="chat-header-title">对话</span>
          <div className="chat-header-actions">
            {isHome && homeAgents && homeAgents.length > 0 ? (
              <SearchableChipSelect
                value={agentId ?? ''}
                options={homeAgents.map((a) => ({
                  value: a.id,
                  label: a.name,
                }))}
                onChange={(id) => onAgentChange?.(id)}
                placeholder="请选择 Agent"
                searchPlaceholder="搜索 Agent…"
                className={`chat-chip--agent${!agentId ? ' chat-chip--agent-empty' : ''}`}
                leading={(
                  <>
                    <span className="chat-chip__label">Agent</span>
                    <span className="chat-chip__dot breathing-dot" aria-hidden />
                  </>
                )}
              />
            ) : agent ? (
              <span className="chat-chip chat-chip--agent">
                <span className="chat-chip__label">Agent</span>
                <span className="chat-chip__dot breathing-dot" aria-hidden />
                <span className="chat-chip__text">{agent.name}</span>
              </span>
            ) : null}
          </div>
        </div>
        {error && (
          <div className="chat-error-banner">
            <span>{error}</span>
            <button type="button" className="chat-error-dismiss" onClick={() => setError('')}>x</button>
          </div>
        )}
        {cancelledNotice && (
          <div className="chat-cancelled-banner">
            <span>Generation stopped. The partial reply was saved.</span>
            <button type="button" className="chat-error-dismiss" onClick={() => setCancelledNotice(false)}>x</button>
          </div>
        )}
        {terminalMode ? (
          <div className="terminal-output-area" ref={terminalAreaRef}>
            <div className="terminal-output-area__head">
              <span className="terminal-output-area__title">
                终端 VM {terminalVMID}
                <code className="terminal-cwd" title="当前目录">{terminalWorkDir}</code>
              </span>
              <button
                type="button"
                className="terminal-output-area__exit"
                title="退出终端 (Esc)"
                onClick={exitTerminalMode}
              >
                退出终端 <kbd className="terminal-kbd">Esc</kbd>
              </button>
            </div>
            {terminalOutputs.map((entry) => (
              <div key={entry.key} className={`terminal-line terminal-line--${entry.role}`}>
                {entry.role === 'user' && <span className="terminal-prompt">&gt; </span>}
                {entry.role === 'cmd' && <span className="terminal-prompt">$ </span>}
                {entry.role === 'stdout' ? (
                  <TerminalStdoutView
                    content={entry.content}
                    disabled={terminalBusy}
                    onEnterDir={(name) => handleEnterTerminalDir(name, entry.cwd)}
                  />
                ) : (
                  <pre>{entry.content}</pre>
                )}
              </div>
            ))}
            {terminalBusy && <div className="terminal-line terminal-line--busy">执行中…</div>}
            {terminalOutputs.length === 0 && !terminalBusy && (
              <div className="terminal-line" style={{ color: '#808080' }}>
                输入自然语言执行命令；目录名可点击进入。按 Esc 可退出终端。
              </div>
            )}
            <div ref={terminalEndRef} />
          </div>
        ) : null}
        <div className="chat-messages">
          <div className="chat-messages-inner">
            {!hasAgent ? (
              <div className="chat-welcome">
                <h2>开始对话</h2>
                <p>先在右上角选择一个 Agent，再发送消息。</p>
                <span className="chat-welcome__hint">右上角 · 选择 Agent</span>
              </div>
            ) : !sessionId ? (
              <div className="chat-welcome">
                <p>开始一段新对话。</p>
              </div>
            ) : loadingHistory ? (
              <div className="chat-welcome">
                <div className="loading-spinner" />
                <p>正在加载历史…</p>
              </div>
            ) : (
              <>
                {historyCursor && (
                  <div className="chat-load-earlier">
                    <button type="button" onClick={loadEarlier} disabled={loadingEarlier}>
                      {loadingEarlier ? 'Loading earlier…' : 'Load earlier messages'}
                    </button>
                  </div>
                )}
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
                  const timelineNodes = messageTimelines[messageKey] ?? m.metadata?.timeline ?? []
                  const showTime = !(streaming && idx === messages.length - 1) && formatMessageTime(m.created_at)
                  const actions = m.id && sessionId && !streaming && (m.role === 'user' || m.role === 'assistant') ? (
                    <div className="chat-msg-actions">
                      <button
                        type="button"
                        className="chat-rewind-btn"
                        title="隐藏这条及之后的消息，从更早的上下文继续"
                        disabled={rewinding || forking}
                        onClick={() => handleRewind(m.id)}
                      >
                        {rewinding ? '回溯中…' : '回溯'}
                      </button>
                      <button
                        type="button"
                        className="chat-rewind-btn"
                        title="复制到此为止的历史，开一个新会话"
                        disabled={rewinding || forking}
                        onClick={() => handleFork(m.id)}
                      >
                        {forking ? '分叉中…' : '分叉'}
                      </button>
                    </div>
                  ) : null
                  if (m.role === 'user') {
                    const userAtts = m.metadata?.attachments ?? []
                    return (
                      <div key={messageKey} className="chat-msg chat-msg-user">
                        <div className="chat-user-block">
                          <div className="chat-user-block__head">
                            <span className="chat-user-block__tag">#user_input</span>
                            <span className="chat-user-block__role">role: user</span>
                          </div>
                          <div className="chat-user-block__body">
                            <div className="chat-user-block__avatar" aria-hidden>U</div>
                            <div className="chat-user-block__main">
                              {sessionId && userAtts.length > 0 ? (
                                <MessageAttachmentList sessionId={sessionId} attachments={userAtts} />
                              ) : null}
                              {m.content ? <pre className="chat-user-block__text">{m.content}</pre> : null}
                            </div>
                            <div className="chat-user-block__wave" aria-hidden>
                              <span /><span /><span /><span />
                            </div>
                          </div>
                          <div className="chat-user-block__foot">
                            {actions}
                            {showTime ? (
                              <span className="chat-msg-time" title={m.created_at}>
                                {formatMessageTime(m.created_at)}
                              </span>
                            ) : null}
                          </div>
                        </div>
                      </div>
                    )
                  }
                  return (
                    <div key={messageKey} className={`chat-msg chat-msg-${m.role}`}>
                      <div className="chat-msg-avatar">AI</div>
                      <div className="chat-msg-stack">
                        {timelineNodes.length > 0 ? <TimelineCard nodes={timelineNodes} /> : null}
                        <SourcesPanel sources={sources} />
                        <div className="chat-msg-content">
                          <>
                            {messagePlans[messageKey] && <PlanPanel plan={messagePlans[messageKey]} />}
                            <AssistantReplyBody
                              sessionId={sessionId}
                              content={m.content}
                              nodes={timelineNodes}
                              showCursor={streaming && idx === messages.length - 1}
                              interrupted={
                                !!m.metadata?.interrupted ||
                                timelineNodes.some((n) => n.phase === 'interrupted')
                              }
                              emptyReply={!!m.metadata?.empty_reply}
                            />
                            {m.metadata?.interrupted && !!(m.content ?? '').trim() && (
                              <div className="chat-interrupted-marker">Interrupted — showing the partial reply</div>
                            )}
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
                          {showTime ? (
                            <div className="chat-msg-time" title={m.created_at}>
                              {formatMessageTime(m.created_at)}
                            </div>
                          ) : null}
                          {actions}
                        </div>
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
          <div
            className={`chat-composer${composerDragOver ? ' chat-composer--drag' : ''}`}
            onDragEnter={(e) => {
              e.preventDefault()
              e.stopPropagation()
              if (!hasAgent || streaming) return
              setComposerDragOver(true)
            }}
            onDragOver={(e) => {
              e.preventDefault()
              e.stopPropagation()
              if (!hasAgent || streaming) return
              setComposerDragOver(true)
            }}
            onDragLeave={(e) => {
              e.preventDefault()
              if (e.currentTarget.contains(e.relatedTarget as Node)) return
              setComposerDragOver(false)
            }}
            onDrop={(e) => {
              e.preventDefault()
              e.stopPropagation()
              setComposerDragOver(false)
              if (!hasAgent || streaming) return
              if (e.dataTransfer.files?.length) void uploadFiles(e.dataTransfer.files)
            }}
          >
            {hasAgent ? (
              <div className="chat-composer__toolbar">
                <SearchableChipSelect
                  className="chat-chip--compact"
                  value={modelChoice}
                  disabled={streaming || !sessionId}
                  placement="up"
                  searchPlaceholder="搜索模型…"
                  placeholder="选择模型"
                  leading={<span className="chat-chip__ai" aria-hidden>AI</span>}
                  options={modelChoices.map((item) => {
                    if (item.id === 'agent_default') {
                      const model = item.model || agent?.model_config?.model
                      return {
                        value: 'agent_default',
                        label: model ? `默认 · ${model}` : (item.label?.replace(/^Agent\s*/, '') || '默认模型'),
                      }
                    }
                    return {
                      value: `${item.provider_id}::${item.model}`,
                      label: item.display_name || item.model || item.label,
                      group: item.provider_name || '其他',
                    }
                  })}
                  onChange={async (next) => {
                    const prev = modelChoice
                    if (!sessionId || next === prev) return
                    try {
                      if (next === 'agent_default') {
                        await modelCatalogApi.patchSessionModel(sessionId, { choice: 'agent_default' })
                      } else {
                        const sep = next.indexOf('::')
                        await modelCatalogApi.patchSessionModel(sessionId, {
                          model_provider_id: next.slice(0, sep),
                          model: next.slice(sep + 2),
                        })
                      }
                      setModelChoice(next)
                    } catch (err) {
                      setError((err as Error).message)
                      setModelChoice(prev)
                    }
                  }}
                />
                <span className="chat-composer__hint">
                  {terminalMode ? 'Tab 补全文件 · Esc 退出终端' : 'Shift + Enter 换行'}
                </span>
              </div>
            ) : null}
            {(pendingAttachments.length > 0 || uploadingCount > 0) ? (
              <div className="chat-composer__pending">
                {pendingAttachments.map((att) => (
                  <div key={att.id} className="chat-pending-chip">
                    {att.kind === 'image' && sessionId ? (
                      <AttachmentImageThumb
                        sessionId={sessionId}
                        att={att}
                        className="chat-pending-chip__thumb"
                      />
                    ) : (
                      <span className="chat-pending-chip__name">{att.name}</span>
                    )}
                    <button
                      type="button"
                      className="chat-pending-chip__remove"
                      aria-label={`移除 ${att.name}`}
                      disabled={streaming}
                      onClick={() => void removePendingAttachment(att.id)}
                    >
                      ×
                    </button>
                  </div>
                ))}
                {uploadingCount > 0 ? (
                  <span className="chat-pending-uploading">上传中 ({uploadingCount})…</span>
                ) : null}
              </div>
            ) : null}
            <div className="chat-composer__row">
              <input
                ref={fileInputRef}
                type="file"
                className="chat-attach-input"
                multiple
                accept={ATTACH_ACCEPT}
                disabled={streaming || !hasAgent}
                onChange={(e) => {
                  const files = e.target.files
                  if (files?.length) void uploadFiles(files)
                  e.target.value = ''
                }}
              />
              <button
                type="button"
                className="chat-attach"
                aria-label="添加附件"
                title="添加图片或文本文件"
                disabled={streaming || !hasAgent}
                onClick={() => fileInputRef.current?.click()}
              >
                <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" aria-hidden>
                  <path
                    strokeLinecap="round"
                    strokeLinejoin="round"
                    strokeWidth="2"
                    d="M21.44 11.05l-8.49 8.49a5.5 5.5 0 01-7.78-7.78l8.49-8.49a3.5 3.5 0 014.95 4.95l-8.5 8.49a1.5 1.5 0 01-2.12-2.12l7.78-7.78"
                  />
                </svg>
              </button>
              <div className="chat-input-suggest-wrap">
                {suggestOpen ? (
                  <ul className="terminal-suggest" role="listbox" aria-label="当前目录补全">
                    {terminalSuggestions.map((item, idx) => (
                      <li key={`${item.kind}:${item.name}`}>
                        <button
                          type="button"
                          role="option"
                          aria-selected={idx === suggestIndex}
                          className={`terminal-suggest__item${idx === suggestIndex ? ' is-active' : ''}`}
                          onMouseDown={(ev) => {
                            ev.preventDefault()
                            ev.stopPropagation()
                            applyTerminalSuggestion(item.name)
                          }}
                          onClick={(ev) => {
                            ev.preventDefault()
                            ev.stopPropagation()
                          }}
                        >
                          <span className={`terminal-suggest__kind terminal-suggest__kind--${item.kind}`}>
                            {item.kind === 'dir' ? '目录' : '文件'}
                          </span>
                          <span className="terminal-suggest__name">{item.name}</span>
                        </button>
                      </li>
                    ))}
                  </ul>
                ) : null}
                <textarea
                  className="chat-input"
                  placeholder={
                    terminalMode
                      ? '输入命令或自然语言；Tab 补全当前目录文件…'
                      : hasAgent
                        ? '给 AI Agent 发送指令...'
                        : '请先选择 Agent'
                  }
                  value={input}
                  onChange={(e) => {
                    setSuggestDismissed(false)
                    setSuggestForceAll(false)
                    setInput(e.target.value)
                  }}
                  onKeyDown={handleKeyDown}
                  onPaste={handleComposerPaste}
                  disabled={streaming || !hasAgent}
                  rows={1}
                />
              </div>
              {streaming ? (
                <button
                  type="button"
                  className="chat-stop"
                  onClick={() => {
                    abortRef.current?.abort()
                    streamSessionRef.current = null
                    setStreaming(false)
                  }}
                >
                  停止
                </button>
              ) : (
                <button
                  type="button"
                  className="chat-send"
                  aria-label="发送"
                  onClick={() => {
                    if (Date.now() < suppressSendUntilRef.current) return
                    handleSend()
                  }}
                  disabled={
                    (!input.trim() && pendingAttachments.length === 0) ||
                    !hasAgent ||
                    uploadingCount > 0
                  }
                >
                  <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" aria-hidden>
                    <path strokeLinecap="round" strokeLinejoin="round" strokeWidth="2.5" d="M12 19l9 2-9-18-9 18 9-2zm0 0v-8" />
                  </svg>
                </button>
              )}
            </div>
          </div>
        </div>
      </main>
    </div>
  )
}
