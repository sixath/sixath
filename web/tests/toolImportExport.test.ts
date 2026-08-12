import assert from 'node:assert/strict'
import { describe, it } from 'node:test'
import {
  buildExportFile,
  parseToolsImportJson,
  toExportItem,
} from '../src/utils/toolExportFormat.ts'

describe('toolExportFormat', () => {
  it('round-trips export envelope', () => {
    const item = toExportItem({
      name: 'echo',
      description: 'd',
      type: 'builtin',
      config: { func_path: 'echo', parameters: {} },
    })
    const doc = buildExportFile([item])
    assert.equal(doc.kind, 'sixath.tools')
    assert.equal(doc.version, 1)
    assert.equal(doc.tools.length, 1)

    const parsed = parseToolsImportJson(JSON.stringify(doc))
    assert.equal(parsed[0].name, 'echo')
    assert.equal(parsed[0].type, 'builtin')
  })

  it('accepts bare array and single object', () => {
    const arr = parseToolsImportJson(
      JSON.stringify([{ name: 'a', type: 'mcp', description: '', config: {} }]),
    )
    assert.equal(arr.length, 1)
    const one = parseToolsImportJson(
      JSON.stringify({ name: 'b', type: 'datasource', config: { datasource: { id: 'x' } } }),
    )
    assert.equal(one[0].name, 'b')
  })

  it('rejects bad type', () => {
    assert.throws(
      () => parseToolsImportJson(JSON.stringify([{ name: 'x', type: 'nope', config: {} }])),
      /type/,
    )
  })
})
