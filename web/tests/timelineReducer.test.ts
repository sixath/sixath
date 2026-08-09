import assert from 'node:assert/strict'
import test from 'node:test'
import { applyToolCall, applyModelCall, finalizeTimeline, normalizeTimeline } from '../src/pages/timelineReducer.ts'
import type { TimelineNode } from '../src/pages/timelineReducer.ts'
import type { ToolCallPayload, ModelCallPayload } from '../src/api/chatStream.ts'

const tc = (o: Partial<ToolCallPayload>): ToolCallPayload => ({
  id: 'c1', step: 0, phase: 'started', tool_name: 'read_file', allowed: true, ...o,
})
const mc = (o: Partial<ModelCallPayload>): ModelCallPayload => ({
  step: 0, phase: 'invoked', ...o,
})

test('upserts a tool node by id, updating in place', () => {
  let nodes: TimelineNode[] = []
  nodes = applyToolCall(nodes, tc({ phase: 'started' }))
  nodes = applyToolCall(nodes, tc({ phase: 'completed', duration_ms: 128, result: { rows: 1 } }))
  const tools = nodes.filter((n) => n.kind === 'tool')
  assert.equal(tools.length, 1)
  assert.equal(tools[0].kind === 'tool' && tools[0].phase, 'completed')
  assert.equal(tools[0].kind === 'tool' && tools[0].durationMs, 128)
})

test('upserts a model node by step, invoked then responded', () => {
  let nodes: TimelineNode[] = []
  nodes = applyModelCall(nodes, mc({ step: 1, phase: 'invoked' }))
  nodes = applyModelCall(nodes, mc({ step: 1, phase: 'responded', input_tokens: 10, output_tokens: 5 }))
  const models = nodes.filter((n) => n.kind === 'model')
  assert.equal(models.length, 1)
  assert.equal(models[0].kind === 'model' && models[0].outputTokens, 5)
})

test('marks in-progress nodes as interrupted on finalize', () => {
  let nodes: TimelineNode[] = []
  nodes = applyToolCall(nodes, tc({ id: 'c9', phase: 'started' }))
  nodes = finalizeTimeline(nodes)
  const t = nodes.find((n) => n.kind === 'tool' && n.id === 'c9')
  assert.equal(t && t.kind === 'tool' && t.phase, 'interrupted')
})

test('sorts by step then arrival', () => {
  let nodes: TimelineNode[] = []
  nodes = applyModelCall(nodes, mc({ step: 2, phase: 'invoked' }))
  nodes = applyToolCall(nodes, tc({ id: 'a', step: 1, phase: 'started' }))
  assert.equal(nodes[0].step, 1)
  assert.equal(nodes[1].step, 2)
})

test('normalizeTimeline restores persisted camelCase nodes', () => {
  const nodes = normalizeTimeline([
    { kind: 'model', step: 0, seq: 1, phase: 'responded', model: 'gpt-4o', inputTokens: 11 },
    { kind: 'tool', id: 'c1', step: 1, seq: 2, phase: 'completed', toolName: 'list_tools', allowed: true, durationMs: 3 },
  ])
  assert.ok(nodes)
  assert.equal(nodes!.length, 2)
  assert.equal(nodes![0].kind, 'model')
  assert.equal(nodes![1].kind === 'tool' && nodes![1].toolName, 'list_tools')
})

test('normalizeTimeline returns undefined for empty/invalid', () => {
  assert.equal(normalizeTimeline(undefined), undefined)
  assert.equal(normalizeTimeline([]), undefined)
  assert.equal(normalizeTimeline([{ kind: 'tool' }]), undefined)
})
