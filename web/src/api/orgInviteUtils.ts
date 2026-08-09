export interface OrgInviteSummary {
  max_uses: number
  used_count: number
  expires_at?: string | null
  revoked_at?: string | null
}

export function inviteFullUrl(invitePath: string): string {
  const path = invitePath.startsWith('/') ? invitePath : `/${invitePath}`
  if (typeof window !== 'undefined' && window.location?.origin) {
    return `${window.location.origin}${path}`
  }
  return path
}

export function inviteStatusLabel(invite: OrgInviteSummary): string {
  if (invite.revoked_at) return '已撤销'
  if (invite.expires_at && new Date(invite.expires_at).getTime() < Date.now()) return '已过期'
  if (invite.max_uses > 0 && invite.used_count >= invite.max_uses) return '已用尽'
  return '有效'
}

export function maxUsesLabel(maxUses: number): string {
  if (maxUses === 0) return '无限'
  if (maxUses === 1) return '单次'
  return `${maxUses} 次`
}
