import { authHeaders } from './auth'

const API_BASE = '/api/v1'

export interface TerminalSession {
  session_id: string
  host: string
  port: number
  workdir: string
}

export async function createTerminalSession(vmid: number, port?: number, workdir?: string): Promise<TerminalSession> {
  const res = await fetch(`${API_BASE}/terminal/sessions`, {
    method: 'POST',
    headers: { ...authHeaders(), 'Content-Type': 'application/json' },
    body: JSON.stringify({ vmid, port: port ?? 53000, workdir: workdir ?? 'C:\\' }),
  })
  if (!res.ok) {
    const text = await res.text()
    throw new Error(text || `create session failed: ${res.status}`)
  }
  return res.json()
}

export async function getTerminalSession(sessionId: string): Promise<TerminalSession> {
  const res = await fetch(`${API_BASE}/terminal/sessions/${encodeURIComponent(sessionId)}`, {
    headers: authHeaders(),
  })
  if (!res.ok) throw new Error(`session not found: ${res.status}`)
  return res.json()
}

export async function deleteTerminalSession(sessionId: string): Promise<void> {
  const res = await fetch(`${API_BASE}/terminal/sessions/${encodeURIComponent(sessionId)}`, {
    method: 'DELETE',
    headers: authHeaders(),
  })
  if (!res.ok) throw new Error(`delete session failed: ${res.status}`)
}

export function terminalWebSocketURL(sessionId: string): string {
  const proto = window.location.protocol === 'https:' ? 'wss:' : 'ws:'
  return `${proto}//${window.location.host}${API_BASE}/terminal/sessions/${encodeURIComponent(sessionId)}/ws`
}

export interface TerminalChatRequest {
  agent_id: string
  vmid: number
  content: string
  session_id?: string
  /** When set, skip LLM and run this CMD directly (UI actions like click-to-cd). */
  cmd?: string
}

export interface TerminalChatResponse {
  session_id: string
  cmd: string
  stdout: string
  stderr?: string
  workdir?: string
}

export async function sendTerminalChat(req: TerminalChatRequest): Promise<TerminalChatResponse> {
  const res = await fetch(`${API_BASE}/terminal/chat`, {
    method: 'POST',
    headers: { ...authHeaders(), 'Content-Type': 'application/json' },
    body: JSON.stringify(req),
  })
  if (!res.ok) {
    const text = await res.text()
    throw new Error(formatTerminalAPIError(text, res.status))
  }
  return res.json()
}

function formatTerminalAPIError(text: string, status: number): string {
  const raw = (text || '').trim()
  if (!raw) return `terminal chat failed: ${status}`
  try {
    const j = JSON.parse(raw) as {
      message?: string
      reason?: string
      ret?: { code?: number; message?: string; reason?: string }
    }
    const reason = j.ret?.reason || j.reason || ''
    const message = j.ret?.message || j.message || ''
    if (reason === 'NO_CMD' || message.includes('did not produce a command')) {
      return '无法把这句话翻译成命令，请换种说法，或直接输入 CMD（例如：powershell -Command "Get-Content cgvmagent.log -Tail 20"）'
    }
    if (message && reason) return `${message} (${reason})`
    if (message) return message
  } catch {
    /* not JSON */
  }
  return raw
}