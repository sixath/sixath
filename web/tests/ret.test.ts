import { describe, it } from 'node:test'
import assert from 'node:assert/strict'
import { isFailedRet } from '../src/api/ret.ts'

describe('isFailedRet', () => {
  it('treats missing ret as success', () => {
    assert.equal(isFailedRet(undefined), false)
    assert.equal(isFailedRet(null), false)
  })

  it('treats omitted proto3 code (HTTP 200 success) as success', () => {
    assert.equal(isFailedRet({ message: 'ok' }), false)
    assert.equal(isFailedRet({ code: 0, message: 'ok' }), false)
  })

  it('treats non-zero code as failure', () => {
    assert.equal(isFailedRet({ code: 1, message: 'ok' }), true)
    assert.equal(isFailedRet({ code: 404, message: 'not found' }), true)
  })
})
