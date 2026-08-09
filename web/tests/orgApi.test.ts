import { describe, it } from 'node:test'
import assert from 'node:assert/strict'
import { inviteStatusLabel, maxUsesLabel } from '../src/api/orgInviteUtils.ts'

describe('maxUsesLabel', () => {
  it('labels common max_uses values', () => {
    assert.equal(maxUsesLabel(0), '无限')
    assert.equal(maxUsesLabel(1), '单次')
    assert.equal(maxUsesLabel(5), '5 次')
  })
})

describe('inviteStatusLabel', () => {
  const base = {
    id: 'inv-1',
    max_uses: 1,
    used_count: 0,
    created_at: '2026-01-01T00:00:00Z',
  }

  it('marks revoked invites', () => {
    assert.equal(
      inviteStatusLabel({ ...base, revoked_at: '2026-01-02T00:00:00Z' }),
      '已撤销'
    )
  })

  it('marks expired invites', () => {
    assert.equal(
      inviteStatusLabel({ ...base, expires_at: '2020-01-01T00:00:00Z' }),
      '已过期'
    )
  })

  it('marks exhausted invites', () => {
    assert.equal(
      inviteStatusLabel({ ...base, max_uses: 2, used_count: 2 }),
      '已用尽'
    )
  })

  it('marks active invites', () => {
    assert.equal(inviteStatusLabel(base), '有效')
  })
})
