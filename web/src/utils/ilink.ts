export type IlinkLoginStatus = 'wait' | 'scaned' | 'confirmed' | 'expired'

/** Last 4 chars of the part before '@' — iLink IDs are opaque, so the tail is what admins compare. */
export function ilinkPeerTail(peerId: string): string {
  const local = peerId.split('@')[0] ?? ''
  return local.length <= 4 ? local : local.slice(-4)
}

export function ilinkPeerLabel(peer: { peer_id: string; remark?: string }): string {
  const remark = peer.remark?.trim()
  const tail = `尾号 ${ilinkPeerTail(peer.peer_id)}`
  return remark ? `${remark}（${tail}）` : tail
}

/** The server may skip "scaned", so wait and scaned share one hint. */
export function ilinkLoginHint(status: IlinkLoginStatus | ''): string {
  switch (status) {
    case 'confirmed':
      return '登录成功。请让需要接收推送的人先给 bot 发一条消息。'
    case 'expired':
      return '二维码已过期，请刷新。'
    default:
      return '请用微信扫码，并在手机上确认登录。'
  }
}

/** Peers without a context token (never messaged, or not since the last login) cannot receive pushes. */
export function ilinkPeerActivation(peer: { has_context_token?: boolean }): '已激活' | '待激活' {
  return peer.has_context_token ? '已激活' : '待激活'
}

type RemarkRow = { peer_id: string; remark: string }

/** A draft is dirty when it differs from the server remark it was based on; dirty drafts survive a reload. */
export function mergeRemarkDrafts(
  prevDrafts: Record<string, string>,
  prevPeers: RemarkRow[],
  nextPeers: RemarkRow[],
): Record<string, string> {
  const prevRemarks = new Map(prevPeers.map((p) => [p.peer_id, p.remark]))
  const out: Record<string, string> = {}
  for (const p of nextPeers) {
    const draft = prevDrafts[p.peer_id]
    const base = prevRemarks.get(p.peer_id)
    out[p.peer_id] = draft !== undefined && base !== undefined && draft !== base ? draft : p.remark
  }
  return out
}
