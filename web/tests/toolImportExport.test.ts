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

  it('keeps top-level egress_mode and proxy_id', () => {
    const parsed = parseToolsImportJson(JSON.stringify({
      name: 'http-via-office',
      type: 'builtin',
      description: '',
      config: { func_path: 'http_request', egress_mode: 'proxy', proxy_id: 'office' },
    }))
    assert.equal(parsed[0].config.egress_mode, 'proxy')
    assert.equal(parsed[0].config.proxy_id, 'office')
  })

  it('keeps rca vm_run_cmd and optional datasource_id', () => {
    const parsed = parseToolsImportJson(JSON.stringify({
      name: 'vm-runcmd',
      type: 'rca',
      description: '',
      config: { rca: { func_path: 'vm_run_cmd', datasource_id: 'cmdb_mysql' } },
    }))
    assert.equal(parsed[0].config.rca?.func_path, 'vm_run_cmd')
    assert.equal(parsed[0].config.rca?.datasource_id, 'cmdb_mysql')
  })

  it('keeps elasticsearch purpose and default_index from camelCase', () => {
    const parsed = parseToolsImportJson(JSON.stringify({
      name: 'zj-elk',
      type: 'datasource',
      description: '',
      config: { datasource: { type: 'elasticsearch', dsn: 'http://es:9200', defaultIndex: 'app-*', purpose: '应用日志' } },
    }))
    assert.equal(parsed[0].config.datasource?.default_index, 'app-*')
    assert.equal(parsed[0].config.datasource?.purpose, '应用日志')
  })
})
