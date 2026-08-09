import { beforeEach, describe, it } from 'node:test'
import assert from 'node:assert/strict'
import { pickOrgIdAfterLogin } from '../src/api/auth.ts'

describe('pickOrgIdAfterLogin', () => {
  const orgs = [
    { id: 'org-a', name: 'A', role: 'owner' },
    { id: 'org-b', name: 'B', role: 'member' },
  ]

  beforeEach(() => {
    /* pure function — no storage */
  })

  it('returns empty when user has no orgs', () => {
    assert.equal(pickOrgIdAfterLogin([], 'org-a'), '')
  })

  it('auto-selects when exactly one org', () => {
    assert.equal(pickOrgIdAfterLogin([orgs[0]], ''), 'org-a')
    assert.equal(pickOrgIdAfterLogin([orgs[0]], 'stale'), 'org-a')
  })

  it('keeps stored org when still in list', () => {
    assert.equal(pickOrgIdAfterLogin(orgs, 'org-b'), 'org-b')
    assert.equal(pickOrgIdAfterLogin(orgs, '  org-a  '), 'org-a')
  })

  it('clears stored org when not in list', () => {
    assert.equal(pickOrgIdAfterLogin(orgs, 'org-gone'), '')
    assert.equal(pickOrgIdAfterLogin(orgs, ''), '')
  })
})
