/** Portal ACL auth: Bearer token + optional X-Org-Id */

import type { AuthSession, OrgMembership } from './sessionAuth'

const TOKEN_KEY = 'sixath-api-token'
const ORG_KEY = 'sixath-org-id'
export const AUTH_GATE_KEY = 'sixath-auth-gate'
const EMAIL_VERIFIED_KEY = 'sixath-email-verified'

/** Matches portal configs auth.bootstrap_token default for local DEV only. */
export const DEV_BOOTSTRAP_TOKEN = 'dev-bootstrap-token'

function isViteDev(): boolean {
  try {
    return !!(import.meta as ImportMeta & { env?: { DEV?: boolean } }).env?.DEV
  } catch {
    return false
  }
}

function envToken(): string {
  try {
    const v = (import.meta as ImportMeta & { env?: Record<string, string> }).env?.VITE_API_TOKEN
    return (v ?? '').trim()
  } catch {
    return ''
  }
}

function envOrgId(): string {
  try {
    const v = (import.meta as ImportMeta & { env?: Record<string, string> }).env?.VITE_ORG_ID
    return (v ?? '').trim()
  } catch {
    return ''
  }
}

function readStorage(key: string): string {
  try {
    return (localStorage.getItem(key) ?? '').trim()
  } catch {
    return ''
  }
}

function writeStorage(key: string, value: string): void {
  try {
    const v = value.trim()
    if (!v) localStorage.removeItem(key)
    else localStorage.setItem(key, v)
  } catch {
    /* ignore quota / private mode */
  }
}

function readSession(key: string): string {
  try {
    return (sessionStorage.getItem(key) ?? '').trim()
  } catch {
    return ''
  }
}

function writeSession(key: string, value: string): void {
  try {
    if (!value) sessionStorage.removeItem(key)
    else sessionStorage.setItem(key, value)
  } catch {
    /* ignore */
  }
}

export function isAuthGateActive(): boolean {
  return readSession(AUTH_GATE_KEY) === '1'
}

export function setAuthGate(): void {
  writeSession(AUTH_GATE_KEY, '1')
}

export function clearAuthGate(): void {
  writeSession(AUTH_GATE_KEY, '')
}

/** Explicit UI/localStorage override; empty means fall back to env. */
export function getStoredToken(): string {
  return readStorage(TOKEN_KEY)
}

export function setStoredToken(token: string): void {
  writeStorage(TOKEN_KEY, token)
}

export function getStoredOrgId(): string {
  return readStorage(ORG_KEY)
}

export function setStoredOrgId(orgId: string): void {
  writeStorage(ORG_KEY, orgId)
}

export function saveCredentials(token: string, orgId: string): void {
  setStoredToken(token)
  setStoredOrgId(orgId)
  clearAuthGate()
  clearSessionEmailVerified()
}

export function logout(): void {
  setStoredToken('')
  setStoredOrgId('')
  setAuthGate()
  clearSessionEmailVerified()
}

function setSessionEmailVerified(verified: boolean): void {
  writeSession(EMAIL_VERIFIED_KEY, verified ? '1' : '0')
}

export function clearSessionEmailVerified(): void {
  writeSession(EMAIL_VERIFIED_KEY, '')
}

/** True after email login/register when portal reports unverified mailbox. */
export function isSessionEmailUnverified(): boolean {
  return readSession(EMAIL_VERIFIED_KEY) === '0'
}

/**
 * Pick org id after login/register (spec §5):
 * - 1 org → auto select
 * - multiple → keep stored if still valid, else clear
 * - zero → clear
 */
export function pickOrgIdAfterLogin(orgs: Pick<OrgMembership, 'id'>[], stored: string): string {
  if (orgs.length === 0) return ''
  if (orgs.length === 1) return orgs[0].id
  const trimmed = stored.trim()
  if (trimmed && orgs.some((o) => o.id === trimmed)) return trimmed
  return ''
}

/** Persist Bearer + org from portal auth session response. */
export function applyLoginSession(session: AuthSession): void {
  const orgId = pickOrgIdAfterLogin(session.orgs, getStoredOrgId())
  saveCredentials(session.token, orgId)
  setSessionEmailVerified(session.email_verified)
}

/**
 * Effective token: localStorage → VITE_API_TOKEN → (dev only) bootstrap default.
 * Production builds never invent a token.
 * When auth gate is active (post-logout), returns empty — no env/bootstrap fallback.
 */
export function getApiToken(): string {
  if (isAuthGateActive()) return ''
  return getStoredToken() || envToken() || (isViteDev() ? DEV_BOOTSTRAP_TOKEN : '')
}

/** Effective org id: localStorage override, else VITE_ORG_ID. */
export function getOrgId(): string {
  return getStoredOrgId() || envOrgId()
}

/** Build ACL headers from resolved credentials (pure; for tests). */
export function buildAuthHeaders(token: string, orgId: string): Record<string, string> {
  const headers: Record<string, string> = {}
  const t = token.trim()
  if (t) headers.Authorization = `Bearer ${t}`
  const o = orgId.trim()
  if (o) headers['X-Org-Id'] = o
  return headers
}

/**
 * Headers for portal ACL.
 * Does not set Content-Type (callers decide JSON vs multipart).
 */
export function authHeaders(): Record<string, string> {
  return buildAuthHeaders(getApiToken(), getOrgId())
}

export function hasApiToken(): boolean {
  return getApiToken().length > 0
}

let redirectingUnauthorized = false

export function handleUnauthorized(nextPath?: string): void {
  setStoredToken('')
  setAuthGate()
  if (typeof window === 'undefined') return
  const path = window.location.pathname
  if (path === '/login' || redirectingUnauthorized) return
  redirectingUnauthorized = true
  const next = nextPath ?? path + window.location.search
  const q = next && next !== '/login' ? `?next=${encodeURIComponent(next)}` : ''
  window.location.assign(`/login${q}`)
}
