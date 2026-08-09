/** Org + invite management — Bearer required. */

import { authHeaders, handleUnauthorized } from './auth'
import type { OrgMembership } from './sessionAuth'
import type { OrgInviteSummary } from './orgInviteUtils'

export { inviteFullUrl, inviteStatusLabel, maxUsesLabel } from './orgInviteUtils'
export type { OrgInviteSummary } from './orgInviteUtils'

const API_BASE = '/api/v1'

function parseErrorBody(body: string): string {
  const raw = body.trim()
  if (!raw) return '请求失败'
  try {
    const j = JSON.parse(raw) as { message?: string; reason?: string; ret?: { message?: string; reason?: string } }
    if (j.ret?.message || j.ret?.reason) {
      return j.ret.message || j.ret.reason || raw
    }
    return j.message || j.reason || raw
  } catch {
    return raw
  }
}

async function orgRequest<T>(path: string, options: RequestInit = {}): Promise<T> {
  const res = await fetch(`${API_BASE}${path}`, {
    ...options,
    headers: {
      'Content-Type': 'application/json',
      ...authHeaders(),
      ...options.headers,
    },
  })
  if (!res.ok) {
    const err = await res.text()
    if (res.status === 401) {
      handleUnauthorized()
    }
    throw new Error(parseErrorBody(err))
  }
  return res.json() as Promise<T>
}

export interface OrgInvite extends OrgInviteSummary {
  id: string
  created_at: string
}

export interface CreateInviteResult extends OrgInvite {
  invite_token: string
  invite_path: string
}

function normalizeOrg(raw: Record<string, unknown>): OrgMembership {
  return {
    id: (raw.id as string | undefined) ?? '',
    name: (raw.name as string | undefined) ?? '',
    role: (raw.role as string | undefined) ?? '',
  }
}

function normalizeInvite(raw: Record<string, unknown>): OrgInvite {
  return {
    id: (raw.id as string | undefined) ?? '',
    max_uses: (raw.max_uses as number | undefined) ?? (raw.maxUses as number | undefined) ?? 0,
    used_count: (raw.used_count as number | undefined) ?? (raw.usedCount as number | undefined) ?? 0,
    expires_at:
      (raw.expires_at as string | undefined) ??
      (raw.expiresAt as string | undefined) ??
      null,
    revoked_at:
      (raw.revoked_at as string | undefined) ??
      (raw.revokedAt as string | undefined) ??
      null,
    created_at:
      (raw.created_at as string | undefined) ?? (raw.createdAt as string | undefined) ?? '',
  }
}

export const orgApi = {
  list: async (): Promise<OrgMembership[]> => {
    const data = await orgRequest<{ orgs?: Record<string, unknown>[] }>('/orgs')
    return (data.orgs ?? []).map((item) => normalizeOrg(item))
  },

  create: async (name: string): Promise<OrgMembership> => {
    const data = await orgRequest<Record<string, unknown>>('/orgs', {
      method: 'POST',
      body: JSON.stringify({ name: name.trim() }),
    })
    return normalizeOrg(data)
  },

  listInvites: async (orgId: string): Promise<OrgInvite[]> => {
    const data = await orgRequest<{ invites?: Record<string, unknown>[] }>(
      `/orgs/${encodeURIComponent(orgId)}/invites`
    )
    return (data.invites ?? []).map((item) => normalizeInvite(item))
  },

  createInvite: async (
    orgId: string,
    body: { max_uses: number; expires_in_hours?: number }
  ): Promise<CreateInviteResult> => {
    const payload: Record<string, number> = { max_uses: body.max_uses }
    if (body.expires_in_hours != null && body.expires_in_hours > 0) {
      payload.expires_in_hours = body.expires_in_hours
    }
    const data = await orgRequest<Record<string, unknown>>(
      `/orgs/${encodeURIComponent(orgId)}/invites`,
      { method: 'POST', body: JSON.stringify(payload) }
    )
    const invite = normalizeInvite(data)
    return {
      ...invite,
      invite_token: (data.invite_token as string | undefined) ?? (data.inviteToken as string | undefined) ?? '',
      invite_path: (data.invite_path as string | undefined) ?? (data.invitePath as string | undefined) ?? '',
    }
  },

  revokeInvite: async (orgId: string, inviteId: string): Promise<void> => {
    await orgRequest<{ ok?: boolean }>(
      `/orgs/${encodeURIComponent(orgId)}/invites/${encodeURIComponent(inviteId)}`,
      { method: 'DELETE' }
    )
  },
}
