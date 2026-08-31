import assert from 'node:assert/strict'
import test from 'node:test'
import type { TimelineNode } from '../src/pages/timelineReducer.ts'
import {
  dropIncompleteMarkdownTable,
  parseSpillItems,
  pickSpillPath,
} from '../src/utils/spillTable.ts'

test('pickSpillPath prefers run_result_script near claimed count', () => {
  const sid = '7d069a05-0699-42ea-a3af-2407e0a1aa20'
  const nodes: TimelineNode[] = [
    {
      kind: 'tool',
      id: 't1',
      step: 0,
      seq: 0,
      phase: 'completed',
      toolName: 'es_log_query',
      allowed: true,
      result: { spilled: true, path: `tmp/results/${sid}/es.jsonl`, count: 500 },
    },
    {
      kind: 'tool',
      id: 't2',
      step: 1,
      seq: 1,
      phase: 'completed',
      toolName: 'run_result_script',
      allowed: true,
      result: JSON.stringify({
        spilled: true,
        path: `tmp/results/${sid}/1787986133618_run_result_script_28.jsonl`,
        count: 480,
      }),
    },
  ]
  assert.equal(
    pickSpillPath(sid, 468, nodes),
    `tmp/results/${sid}/1787986133618_run_result_script_28.jsonl`,
  )
})

test('parseSpillItems extracts flowId vmid gid rows', () => {
  const items = [
    { line: 'DiscardUserArchive flowIds: 475' },
    { line: '4103_03KgXZVYE2HH → vmid=219332, gid=46646' },
    { line: '4103_zzc5dler485c \ufffd\ufffd vmid=234509, gid=32745' },
  ]
  const table = parseSpillItems(items)
  assert.deepEqual(table.columns, ['flowId', 'vmid', 'gid'])
  assert.equal(table.rows.length, 2)
  assert.deepEqual(table.rows[0], ['4103_03KgXZVYE2HH', '219332', '46646'])
})

test('dropIncompleteMarkdownTable keeps summary without pipe rows', () => {
  const content = `成功匹配 **468 个 flowId**\n\n### 完整映射表（468 条）\n\n| flowId | vmid | gid |\n|--------|------|-----|\n| 4103_a | 1 | 2 |\n| 4103_b | 3 | 4 |\n`
  const out = dropIncompleteMarkdownTable(content)
  assert.match(out, /468 个 flowId/)
  assert.match(out, /完整映射表/)
  assert.doesNotMatch(out, /4103_a/)
})
