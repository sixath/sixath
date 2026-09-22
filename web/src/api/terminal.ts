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
}

export interface TerminalChatResponse {
  session_id: string
  cmd: string
  stdout: string
  stderr?: string
}

export async function sendTerminalChat(req: TerminalChatRequest): Promise<TerminalChatResponse> {
  const res = await fetch(`${API_BASE}/terminal/chat`, {
    method: 'POST',
    headers: { ...authHeaders(), 'Content-Type': 'application/json' },
    body: JSON.stringify(req),
  })
  if (!res.ok) {
    const text = await res.text()
    throw new Error(text || `terminal chat failed: ${res.status}`)
  }
  return res.json()
}