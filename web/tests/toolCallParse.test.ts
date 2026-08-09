import assert from 'node:assert/strict'
import test from 'node:test'
import { parseToolCallPayload, parseModelCallPayload } from '../src/api/chatStream.ts'

test('parseToolCallPayload accepts a valid tool_call event', () => {
  const p = parseToolCallPayload({ tool_call: { id: 'c1', step: 1, phase: 'completed', tool_name: 'read_file', allowed: true, duration_ms: 5 } })
  assert.equal(p?.id, 'c1')
  assert.equal(p?.phase, 'completed')
  assert.equal(p?.tool_name, 'read_file')
})

test('parseToolCallPayload rejects malformed events', () => {
  assert.equal(parseToolCallPayload({ tool_call: { id: 'c1' } }), null)
  assert.equal(parseToolCallPayload({}), null)
})

test('parseModelCallPayload accepts a valid model_call event', () => {
  const p = parseModelCallPayload({ model_call: { step: 2, phase: 'responded', model: 'gpt-4o', input_tokens: 10, output_tokens: 5 } })
  assert.equal(p?.phase, 'responded')
  assert.equal(p?.step, 2)
  assert.equal(p?.output_tokens, 5)
})

test('parseModelCallPayload rejects malformed events', () => {
  assert.equal(parseModelCallPayload({ model_call: { phase: 'responded' } }), null) // missing step
  assert.equal(parseModelCallPayload({}), null)
})
