import assert from 'node:assert/strict'
import test from 'node:test'
import type { ChatMessage } from '../src/api/client.ts'
import {
  isCompactBoundaryMessage,
  isMessageVisibleAtIndex,
  SIXATH_ORIGIN_COMPACT_BOUNDARY,
} from '../src/utils/compactBoundary.ts'

function msg(partial: Partial<ChatMessage> & Pick<ChatMessage, 'role' | 'content'>): ChatMessage {
  return {
    id: partial.id ?? '',
    session_id: partial.session_id ?? 's1',
    created_at: partial.created_at ?? '2026-05-26T00:00:00Z',
    ...partial,
  }
}

test('isCompactBoundaryMessage detects system compact_boundary metadata', () => {
  assert.equal(
    isCompactBoundaryMessage(
      msg({
        role: 'system',
        content: 'boundary',
        metadata: { sixath_origin: SIXATH_ORIGIN_COMPACT_BOUNDARY },
      }),
    ),
    true,
  )
  assert.equal(isCompactBoundaryMessage(msg({ role: 'system', content: 'other' })), false)
  assert.equal(
    isCompactBoundaryMessage(
      msg({ role: 'assistant', content: 'x', metadata: { sixath_origin: SIXATH_ORIGIN_COMPACT_BOUNDARY } }),
    ),
    false,
  )
})

test('isMessageVisibleAtIndex hides messages above collapsed boundary', () => {
  const messages: ChatMessage[] = [
    msg({ id: 'u1', role: 'user', content: 'old' }),
    msg({ id: 'u2', role: 'user', content: 'older' }),
    msg({
      id: 'b1',
      role: 'system',
      content: 'boundary',
      metadata: { sixath_origin: SIXATH_ORIGIN_COMPACT_BOUNDARY },
    }),
    msg({ id: 'u3', role: 'user', content: 'new' }),
  ]
  const collapsed = new Set(['b1'])

  assert.equal(isMessageVisibleAtIndex(0, messages, collapsed), false)
  assert.equal(isMessageVisibleAtIndex(1, messages, collapsed), false)
  assert.equal(isMessageVisibleAtIndex(2, messages, collapsed), true)
  assert.equal(isMessageVisibleAtIndex(3, messages, collapsed), true)
  assert.equal(isMessageVisibleAtIndex(0, messages, new Set()), true)
})
