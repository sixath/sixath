import assert from 'node:assert/strict'
import test from 'node:test'
import { toolVerb } from '../src/pages/toolVerbMap.ts'

test('toolVerb maps known tools to Chinese verbs', () => {
  assert.equal(toolVerb('read_file'), '读取文件')
  assert.equal(toolVerb('execute_query'), '数据库查询')
  assert.equal(toolVerb('web_search'), '网页搜索')
})

test('toolVerb falls back to raw name for unknown tools', () => {
  assert.equal(toolVerb('some_custom_tool'), 'some_custom_tool')
})

test('toolVerb handles empty input', () => {
  assert.equal(toolVerb(''), '工具调用')
})
