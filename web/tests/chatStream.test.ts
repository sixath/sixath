import assert from 'node:assert/strict'
import test from 'node:test'
import {
  buildConfirmMessage,
  buildInputSubmitBody,
  parseConfirmRequiredPayload,
  parseConfirmResultPayload,
  parseInputRequiredPayload,
  shouldTreatStreamErrorAsWarning,
} from '../src/api/chatStream.ts'

test('parseConfirmRequiredPayload accepts valid confirmation events', () => {
  const parsed = parseConfirmRequiredPayload({
    confirmation: {
      kind: 'execute_write',
      title: 'Confirm write operation',
      description: 'Review the operation before it is executed.',
      token: 'abc123',
      dsl: 'DELETE FROM users WHERE id = 1',
      expires_in: 300,
      severity: 'danger',
    },
  })

  assert.equal(parsed?.token, 'abc123')
  assert.equal(parsed?.dsl, 'DELETE FROM users WHERE id = 1')
  assert.equal(parsed?.expires_in, 300)
})

test('parseConfirmRequiredPayload rejects malformed events', () => {
  assert.equal(parseConfirmRequiredPayload({ confirmation: { token: 'abc123' } }), null)
  assert.equal(parseConfirmRequiredPayload({}), null)
})

test('parseConfirmRequiredPayload accepts skill_manage confirmation events', () => {
  const parsed = parseConfirmRequiredPayload({
    confirmation: {
      kind: 'skill_manage',
      title: 'Confirm skill create',
      description: 'Review the skill "my-skill" before it is applied.',
      token: 'sm_tok',
      dsl: '---\nname: my-skill\n---\n# body',
      expires_in: 300,
      severity: 'danger',
    },
  })

  assert.equal(parsed?.kind, 'skill_manage')
  assert.equal(parsed?.token, 'sm_tok')
  assert.match(parsed?.dsl ?? '', /my-skill/)
})

test('parseConfirmRequiredPayload passes through resource_key and expires_at', () => {
  const parsed = parseConfirmRequiredPayload({
    confirmation: {
      kind: 'skill_manage',
      title: 'Confirm skill patch',
      description: 'Review the skill before it is applied.',
      token: 'sm_tok2',
      dsl: '---\nname: my-skill\n---\n# body',
      expires_in: 300,
      resource_key: 'patch:my-skill',
      expires_at: '2026-07-13T10:05:00Z',
      severity: 'danger',
    },
  })

  assert.equal(parsed?.resource_key, 'patch:my-skill')
  assert.equal(parsed?.expires_at, '2026-07-13T10:05:00Z')
})

test('parseConfirmResultPayload accepts success and failure payloads', () => {
  const ok = parseConfirmResultPayload({
    confirm_result: { ok: true, kind: 'skill_manage', token: 'tok_ok' },
  })
  assert.equal(ok?.ok, true)
  assert.equal(ok?.kind, 'skill_manage')
  assert.equal(ok?.token, 'tok_ok')

  const fail = parseConfirmResultPayload({
    confirm_result: {
      ok: false,
      kind: 'skill_manage',
      token: 'tok_fail',
      error: '确认已失效：已被更新的提案替换，请确认最新卡片',
      error_code: 'superseded',
    },
  })
  assert.equal(fail?.ok, false)
  assert.equal(fail?.error_code, 'superseded')
  assert.match(fail?.error ?? '', /更新的提案/)
})

test('parseConfirmResultPayload rejects malformed events', () => {
  assert.equal(parseConfirmResultPayload({ confirm_result: { ok: true } }), null)
  assert.equal(parseConfirmResultPayload({}), null)
  assert.equal(parseConfirmResultPayload(null), null)
})

test('buildConfirmMessage includes token and execution intent', () => {
  const message = buildConfirmMessage({
    kind: 'execute_write',
    title: 'Confirm write operation',
    description: 'Review the operation before it is executed.',
    token: 'abc123',
    dsl: 'UPDATE users SET active = 0',
    severity: 'danger',
  })

  assert.match(message, /confirm_token/)
  assert.match(message, /abc123/)
  assert.match(message, /execute/)
})

test('stream parser downgrades context timeout after assistant content', () => {
  assert.equal(shouldTreatStreamErrorAsWarning('context deadline exceeded', true), true)
  assert.equal(shouldTreatStreamErrorAsWarning('context deadline exceeded', false), false)
  assert.equal(shouldTreatStreamErrorAsWarning('invalid connection', true), false)
})

test('parseInputRequiredPayload accepts valid input events', () => {
  const parsed = parseInputRequiredPayload({
    input: {
      request_id: 'req_1',
      token: 'tok_1',
      kind: 'password',
      field: 'ssh_password',
      title: 'SSH Password',
      prompt: 'Enter password',
      expires_in: 600,
      severity: 'warning',
    },
  })

  assert.equal(parsed?.token, 'tok_1')
  assert.equal(parsed?.kind, 'password')
  assert.equal(parsed?.severity, 'warning')
})

test('parseInputRequiredPayload rejects malformed events', () => {
  assert.equal(parseInputRequiredPayload({ input: { token: 'tok_1' } }), null)
  assert.equal(parseInputRequiredPayload({}), null)
})

test('buildInputSubmitBody omits visible content for password submit', () => {
  const body = buildInputSubmitBody({
    request_id: 'req_1',
    token: 'tok_1',
    kind: 'password',
    field: 'ssh_password',
    title: 'SSH Password',
    prompt: 'Enter password',
  }, 'secret')

  assert.equal(body.content, '')
  assert.equal(body.input_response.value, 'secret')
  assert.equal(body.input_response.field, 'ssh_password')
})
