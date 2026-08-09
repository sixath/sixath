/** Public auth endpoints — no Bearer required. */

const API_BASE = '/api/v1'

export interface OrgMembership {
  id: string
  name: string
  role: string
}

export interface AuthSession {
  token: string
  user_id: string
  email: string
  orgs: OrgMembership[]
  email_verified: boolean
}

export interface InvitePreview {
  org_name: string
  valid: boolean
}

function parseErrorBody(body: string): string {
  const raw = body.trim()
  if (!raw) return '请求失败'
  try {
    const j = JSON.parse(raw) as { message?: string; reason?: string }
    return j.message || j.reason || raw
  } catch {
    return raw
  }
}

async function authFetch<T>(path: string, init?: RequestInit): Promise<T> {
  const res = await fetch(`${API_BASE}${path}`, {
    ...init,
    headers: {
      'Content-Type': 'application/json',
      ...init?.headers,
    },
  })
  if (!res.ok) {
    throw new Error(parseErrorBody(await res.text()))
  }
  return res.json() as Promise<T>
}

export async function login(email: string, password: string): Promise<AuthSession> {
  return authFetch<AuthSession>('/auth/login', {
    method: 'POST',
    body: JSON.stringify({ email, password }),
  })
}

export async function register(
  email: string,
  password: string,
  invite: string
): Promise<AuthSession> {
  return authFetch<AuthSession>('/auth/register', {
    method: 'POST',
    body: JSON.stringify({ email, password, invite }),
  })
}

export async function previewInvite(token: string): Promise<InvitePreview> {
  return authFetch<InvitePreview>(`/auth/invites/${encodeURIComponent(token)}`)
}

export async function verifyEmail(token: string): Promise<{ ok: boolean }> {
  return authFetch<{ ok: boolean }>('/auth/verify-email', {
    method: 'POST',
    body: JSON.stringify({ token }),
  })
}
