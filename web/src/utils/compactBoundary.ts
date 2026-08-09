import type { ChatMessage } from '../api/client'

export const SIXATH_ORIGIN_COMPACT_BOUNDARY = 'compact_boundary'

export function isCompactBoundaryMessage(m: ChatMessage): boolean {
  return m.role === 'system' && m.metadata?.sixath_origin === SIXATH_ORIGIN_COMPACT_BOUNDARY
}

/** 若某条 collapsed boundary 位于 index i，则 index < i 的消息不展示。 */
export function isMessageVisibleAtIndex(
  idx: number,
  messages: ChatMessage[],
  collapsedBoundaryIds: ReadonlySet<string>,
): boolean {
  for (let i = 0; i < messages.length; i++) {
    const m = messages[i]
    if (!isCompactBoundaryMessage(m)) continue
    const id = m.id
    if (!id || !collapsedBoundaryIds.has(id)) continue
    if (idx < i) return false
  }
  return true
}

export function formatCompactBoundaryTime(createdAt: string): string {
  if (!createdAt) return ''
  const d = new Date(createdAt)
  if (Number.isNaN(d.getTime())) return createdAt
  return d.toLocaleString()
}
