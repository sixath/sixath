import { describe, it } from 'node:test'
import assert from 'node:assert/strict'
import { buildAuthHeaders, DEV_BOOTSTRAP_TOKEN } from '../src/api/auth.ts'

describe('buildAuthHeaders', () => {
  it('sets Bearer and X-Org-Id when both present', () => {
    assert.deepEqual(buildAuthHeaders('tok', 'org-1'), {
      Authorization: 'Bearer tok',
      'X-Org-Id': 'org-1',
    })
  })

  it('omits empty fields', () => {
    assert.deepEqual(buildAuthHeaders('', ''), {})
    assert.deepEqual(buildAuthHeaders('  ', '  '), {})
    assert.deepEqual(buildAuthHeaders('a', ''), { Authorization: 'Bearer a' })
    assert.deepEqual(buildAuthHeaders('', 'o'), { 'X-Org-Id': 'o' })
  })

  it('trims values', () => {
    assert.deepEqual(buildAuthHeaders('  t  ', '  o  '), {
      Authorization: 'Bearer t',
      'X-Org-Id': 'o',
    })
  })

  it('exports bootstrap token constant for local defaults', () => {
    assert.equal(DEV_BOOTSTRAP_TOKEN, 'dev-bootstrap-token')
  })
})
