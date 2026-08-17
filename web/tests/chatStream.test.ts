import assert from 'node:assert/strict'
import test from 'node:test'
import {
  buildConfirmMessage,
  buildInputSubmitBody,
  parseConfirmRequiredPayload,
  parseConfirmResultPayload,
  parseInputRequiredPayload,
  restoreConfirmationsFromMessages,
  restoreInputsFromMessages,
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

test('restoreConfirmationsFromMessages rebuilds pending skill_manage from timeline', () => {
  const now = Date.parse('2026-08-16T22:44:30+08:00')
  const items = restoreConfirmationsFromMessages(
    [
      {
        id: 'msg-1',
        role: 'assistant',
        created_at: '2026-08-16T22:44:18+08:00',
        metadata: {
          timeline: [
            {
              kind: 'tool',
              id: 'call_1',
              toolName: 'skill_manage',
              phase: 'completed',
              result: {
                status: 'pending',
                token: 'tok_fresh',
                action: 'create',
                name: 'rca-sync-archive-migrate',
                preview: '---\nname: rca-sync-archive-migrate\n---\n# body',
                expires_in: 300,
              },
            },
          ],
        },
      },
    ],
    now,
  )

  assert.equal(items.length, 1)
  assert.equal(items[0]?.kind, 'skill_manage')
  assert.equal(items[0]?.token, 'tok_fresh')
  assert.equal(items[0]?.status, 'pending')
  assert.equal(items[0]?.messageKey, 'msg-1')
  assert.match(items[0]?.title ?? '', /create/)
})

test('restoreConfirmationsFromMessages marks expired skill_manage when TTL elapsed', () => {
  const now = Date.parse('2026-08-16T22:50:00+08:00')
  const items = restoreConfirmationsFromMessages(
    [
      {
        id: 'msg-exp',
        role: 'assistant',
        created_at: '2026-08-16T22:44:18+08:00',
        metadata: {
          timeline: [
            {
              kind: 'tool',
              id: 'call_exp',
              toolName: 'skill_manage',
              phase: 'completed',
              result: {
                status: 'pending',
                token: 'tok_old',
                action: 'create',
                name: 'old-skill',
                preview: 'preview',
                expires_in: 300,
              },
            },
          ],
        },
      },
    ],
    now,
  )

  assert.equal(items.length, 1)
  assert.equal(items[0]?.status, 'expired')
  assert.match(items[0]?.error ?? '', /过期|重新/)
})

test('restoreConfirmationsFromMessages supersedes older same resource_key', () => {
  const now = Date.parse('2026-08-16T22:44:30+08:00')
  const items = restoreConfirmationsFromMessages(
    [
      {
        id: 'msg-a',
        role: 'assistant',
        created_at: '2026-08-16T22:40:00+08:00',
        metadata: {
          timeline: [
            {
              kind: 'tool',
              id: 'c1',
              toolName: 'skill_manage',
              phase: 'completed',
              result: {
                status: 'pending',
                token: 'tok_a',
                action: 'create',
                name: 'same-skill',
                preview: 'a',
                expires_in: 600,
              },
            },
          ],
        },
      },
      {
        id: 'msg-b',
        role: 'assistant',
        created_at: '2026-08-16T22:44:00+08:00',
        metadata: {
          timeline: [
            {
              kind: 'tool',
              id: 'c2',
              toolName: 'skill_manage',
              phase: 'completed',
              result: {
                status: 'pending',
                token: 'tok_b',
                action: 'create',
                name: 'same-skill',
                preview: 'b',
                expires_in: 600,
              },
            },
          ],
        },
      },
    ],
    now,
  )

  assert.equal(items.length, 2)
  const byTok = Object.fromEntries(items.map((i) => [i.token, i]))
  assert.equal(byTok.tok_a?.status, 'superseded')
  assert.equal(byTok.tok_b?.status, 'pending')
})

test('restoreConfirmationsFromMessages marks confirmed after success history', () => {
  const now = Date.parse('2026-08-16T22:55:00+08:00')
  const items = restoreConfirmationsFromMessages(
    [
      {
        id: 'msg-wait',
        role: 'assistant',
        created_at: '2026-08-16T22:54:08+08:00',
        metadata: {
          timeline: [
            {
              kind: 'tool',
              id: 'c1',
              toolName: 'skill_manage',
              phase: 'completed',
              result: {
                status: 'pending',
                token: 'tok_ok',
                action: 'create',
                name: 'rca-sync-archive-migrate',
                preview: 'preview',
                expires_in: 300,
              },
            },
          ],
        },
      },
      {
        id: 'u-ok',
        role: 'user',
        created_at: '2026-08-16T22:54:13+08:00',
        content: '[confirmed: skill_manage]',
      },
      {
        id: 'a-ok',
        role: 'assistant',
        created_at: '2026-08-16T22:54:13+08:00',
        content: '技能操作已确认并执行:\n{\n  "action": "create",\n  "name": "rca-sync-archive-migrate"\n}',
      },
      {
        id: 'u-again',
        role: 'user',
        created_at: '2026-08-16T22:54:16+08:00',
        content: '[confirmed: skill_manage]',
      },
      {
        id: 'a-used',
        role: 'assistant',
        created_at: '2026-08-16T22:54:16+08:00',
        content: '技能确认失败: 该确认已使用过',
      },
    ],
    now,
  )

  assert.equal(items.length, 1)
  assert.equal(items[0]?.status, 'confirmed')
  assert.equal(items[0]?.error, undefined)
})

test('restoreConfirmationsFromMessages keeps later re-propose pending after prior success', () => {
  const now = Date.parse('2026-08-16T23:10:00+08:00')
  const items = restoreConfirmationsFromMessages(
    [
      {
        id: 'msg-old',
        role: 'assistant',
        created_at: '2026-08-16T22:54:08+08:00',
        metadata: {
          timeline: [
            {
              kind: 'tool',
              id: 'c-old',
              toolName: 'skill_manage',
              phase: 'completed',
              result: {
                status: 'pending',
                token: 'tok_old',
                action: 'create',
                name: 'rca-sync-archive-migrate',
                preview: 'old',
                expires_in: 300,
              },
            },
          ],
        },
      },
      {
        id: 'u-ok',
        role: 'user',
        created_at: '2026-08-16T22:54:13+08:00',
        content: '[confirmed: skill_manage]',
      },
      {
        id: 'a-ok',
        role: 'assistant',
        created_at: '2026-08-16T22:54:13+08:00',
        content: '技能操作已确认并执行:\n{"name":"rca-sync-archive-migrate"}',
      },
      {
        id: 'msg-new',
        role: 'assistant',
        created_at: '2026-08-16T23:08:00+08:00',
        metadata: {
          timeline: [
            {
              kind: 'tool',
              id: 'c-new',
              toolName: 'skill_manage',
              phase: 'completed',
              result: {
                status: 'pending',
                token: 'tok_new',
                action: 'create',
                name: 'rca-sync-archive-migrate',
                preview: 'new after timeout',
                expires_in: 300,
              },
            },
          ],
        },
      },
    ],
    now,
  )

  const byTok = Object.fromEntries(items.map((i) => [i.token, i]))
  assert.equal(byTok.tok_old?.status, 'confirmed')
  assert.equal(byTok.tok_new?.status, 'pending')
})

test('restoreConfirmationsFromMessages keeps re-propose pending after prior expired', () => {
  const now = Date.parse('2026-08-16T23:10:00+08:00')
  const items = restoreConfirmationsFromMessages(
    [
      {
        id: 'msg-old',
        role: 'assistant',
        created_at: '2026-08-16T22:50:00+08:00',
        metadata: {
          timeline: [
            {
              kind: 'tool',
              id: 'c-old',
              toolName: 'skill_manage',
              phase: 'completed',
              result: {
                status: 'pending',
                token: 'tok_expired',
                action: 'create',
                name: 'rca-sync-archive-migrate',
                preview: 'old',
                expires_in: 300,
              },
            },
          ],
        },
      },
      {
        id: 'msg-new',
        role: 'assistant',
        created_at: '2026-08-16T23:08:00+08:00',
        metadata: {
          timeline: [
            {
              kind: 'tool',
              id: 'c-new',
              toolName: 'skill_manage',
              phase: 'completed',
              result: {
                status: 'pending',
                token: 'tok_retry',
                action: 'create',
                name: 'rca-sync-archive-migrate',
                preview: 'retry after timeout',
                expires_in: 300,
              },
            },
          ],
        },
      },
    ],
    now,
  )

  const byTok = Object.fromEntries(items.map((i) => [i.token, i]))
  assert.equal(byTok.tok_expired?.status, 'expired')
  assert.equal(byTok.tok_retry?.status, 'pending')
})

test('restoreConfirmationsFromMessages marks failed on already_used without prior success', () => {
  const now = Date.parse('2026-08-16T22:55:00+08:00')
  const items = restoreConfirmationsFromMessages(
    [
      {
        id: 'msg-wait',
        role: 'assistant',
        created_at: '2026-08-16T22:54:08+08:00',
        metadata: {
          timeline: [
            {
              kind: 'tool',
              id: 'c1',
              toolName: 'skill_manage',
              phase: 'completed',
              result: {
                status: 'pending',
                token: 'tok_used',
                action: 'create',
                name: 'x',
                preview: 'p',
                expires_in: 300,
              },
            },
          ],
        },
      },
      {
        role: 'user',
        content: '[confirmed: skill_manage]',
        created_at: '2026-08-16T22:54:16+08:00',
      },
      {
        role: 'assistant',
        content: '技能确认失败: 该确认已使用过',
        created_at: '2026-08-16T22:54:16+08:00',
      },
    ],
    now,
  )

  assert.equal(items[0]?.status, 'failed')
  assert.match(items[0]?.error ?? '', /已使用/)
})

test('restoreInputsFromMessages rebuilds pending ask_user from timeline', () => {
  const now = Date.parse('2026-08-17T09:50:00+08:00')
  const items = restoreInputsFromMessages(
    [
      {
        id: 'msg-ask',
        role: 'assistant',
        created_at: '2026-08-17T09:49:00+08:00',
        metadata: {
          timeline: [
            {
              kind: 'tool',
              id: 'c-ask',
              toolName: 'ask_user',
              phase: 'completed',
              result: {
                status: 'pending',
                token: 'tok_time',
                request_id: 'req_1',
                kind: 'text',
                field: 'time_range',
                title: '时间范围',
                prompt: '请提供时间范围',
                expires_in: 600,
              },
            },
          ],
        },
      },
    ],
    now,
  )

  assert.equal(items.length, 1)
  assert.equal(items[0]?.status, 'pending')
  assert.equal(items[0]?.field, 'time_range')
  assert.equal(items[0]?.token, 'tok_time')
  assert.equal(items[0]?.messageKey, 'msg-ask')
})

test('restoreInputsFromMessages marks submitted after input provided history', () => {
  const now = Date.parse('2026-08-17T09:55:00+08:00')
  const items = restoreInputsFromMessages(
    [
      {
        id: 'msg-ask',
        role: 'assistant',
        created_at: '2026-08-17T09:49:00+08:00',
        metadata: {
          timeline: [
            {
              kind: 'tool',
              id: 'c-ask',
              toolName: 'ask_user',
              phase: 'completed',
              result: {
                status: 'pending',
                token: 'tok_time',
                request_id: 'req_1',
                kind: 'text',
                field: 'time_range',
                title: '时间范围',
                prompt: '请提供时间范围',
                expires_in: 600,
              },
            },
          ],
        },
      },
      {
        id: 'u-in',
        role: 'user',
        created_at: '2026-08-17T09:50:00+08:00',
        content: '[input provided: time_range]',
      },
    ],
    now,
  )

  assert.equal(items[0]?.status, 'submitted')
})
