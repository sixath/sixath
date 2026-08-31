import assert from 'node:assert/strict'
import test from 'node:test'
import { truncatedTableHint } from '../src/utils/truncatedTable.ts'

test('truncatedTableHint detects claimed 468 vs fewer markdown rows', () => {
  const rows = Array.from({ length: 355 }, (_, i) => `| 4103_id${i} | ${i} | 32745 |`).join('\n')
  const content = `成功匹配 **468 个 flowId**\n\n### 完整映射表（468 条）\n\n| flowId | vmid | gid |\n|--------|------|-----|\n${rows}\n`
  const hint = truncatedTableHint(content)
  assert.ok(hint, 'expected truncation hint')
  assert.match(hint!, /468/)
  assert.match(hint!, /355/)
})

test('truncatedTableHint is silent when table matches claimed size', () => {
  const rows = Array.from({ length: 3 }, (_, i) => `| id${i} | ${i} | x |`).join('\n')
  const content = `### 完整映射表（3 条）\n\n| flowId | vmid | gid |\n|--------|------|-----|\n${rows}\n`
  assert.equal(truncatedTableHint(content), null)
})

test('truncatedTableHint ignores small claimed counts', () => {
  const content = `共（5 条）\n\n| a | b |\n|--|--|\n| 1 | 2 |\n`
  assert.equal(truncatedTableHint(content), null)
})
