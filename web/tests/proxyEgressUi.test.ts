import assert from 'node:assert/strict'
import { describe, it } from 'node:test'
import {
  coerceEgressMode,
  coerceMysqlNamedProxy,
  datasourceForbidsNamedProxy,
  filterProxiesForTool,
  formatProxyInUseMessage,
  parseProxyDeleteConflict,
  toolAllowsNamedProxy,
  toolSupportsEgress,
} from '../src/utils/proxyEgress.ts'

const catalog = [
  { id: 'office', name: '办公 HTTP', type: 'http' },
  { id: 'lab', name: '实验室 SOCKS', type: 'socks5' },
]

describe('tool egress visibility', () => {
  it('shows egress for datasource / mcp / rca, not builtin', () => {
    assert.equal(toolSupportsEgress('datasource'), true)
    assert.equal(toolSupportsEgress('mcp'), true)
    assert.equal(toolSupportsEgress('rca'), true)
    assert.equal(toolSupportsEgress('builtin'), false)
  })

  it('hive cannot pick named proxy; mongodb can', () => {
    assert.equal(datasourceForbidsNamedProxy('hive'), true)
    assert.equal(datasourceForbidsNamedProxy('mongo'), false)
    assert.equal(datasourceForbidsNamedProxy('mongodb'), false)
    assert.equal(datasourceForbidsNamedProxy('mysql'), false)
    assert.equal(datasourceForbidsNamedProxy('elasticsearch'), false)
    assert.equal(toolAllowsNamedProxy('datasource', 'hive'), false)
    assert.equal(toolAllowsNamedProxy('datasource', 'mongodb'), true)
    assert.equal(toolAllowsNamedProxy('datasource', 'mysql'), true)
    assert.equal(toolAllowsNamedProxy('mcp'), true)
    assert.equal(toolAllowsNamedProxy('rca'), true)
  })

  it('coerces hive proxy mode to inherit before submit', () => {
    assert.equal(
      coerceEgressMode('proxy', { toolType: 'datasource', datasourceType: 'hive' }),
      'inherit',
    )
    assert.equal(
      coerceEgressMode('proxy', { toolType: 'datasource', datasourceType: 'mongodb' }),
      'proxy',
    )
    assert.equal(
      coerceEgressMode('off', { toolType: 'datasource', datasourceType: 'hive' }),
      'off',
    )
    assert.equal(
      coerceEgressMode('proxy', { toolType: 'datasource', datasourceType: 'mysql' }),
      'proxy',
    )
    assert.equal(coerceEgressMode('proxy', { toolType: 'mcp' }), 'proxy')
    assert.equal(coerceEgressMode('', { toolType: 'rca' }), 'inherit')
  })
})

describe('mysql proxy filter', () => {
  it('keeps only socks5 for mysql datasource', () => {
    const got = filterProxiesForTool(catalog, { toolType: 'datasource', datasourceType: 'mysql' })
    assert.deepEqual(got.map((p) => p.id), ['lab'])
    const mongo = filterProxiesForTool(catalog, { toolType: 'datasource', datasourceType: 'mongodb' })
    assert.deepEqual(mongo.map((p) => p.id), ['lab'])
  })

  it('does not filter hive/es/mcp/rca catalogs', () => {
    assert.equal(
      filterProxiesForTool(catalog, { toolType: 'datasource', datasourceType: 'hive' }).length,
      2,
    )
    assert.equal(
      filterProxiesForTool(catalog, { toolType: 'datasource', datasourceType: 'elasticsearch' }).length,
      2,
    )
    assert.equal(filterProxiesForTool(catalog, { toolType: 'mcp' }).length, 2)
    assert.equal(filterProxiesForTool(catalog, { toolType: 'rca' }).length, 2)
  })

  it('coerces mysql http / missing proxy_id to inherit before submit', () => {
    assert.deepEqual(
      coerceMysqlNamedProxy('proxy', 'office', catalog, 'mysql'),
      { mode: 'inherit', proxyId: '' },
    )
    assert.deepEqual(
      coerceMysqlNamedProxy('proxy', 'gone', catalog, 'mysql'),
      { mode: 'inherit', proxyId: '' },
    )
    assert.deepEqual(
      coerceMysqlNamedProxy('proxy', 'lab', catalog, 'mysql'),
      { mode: 'proxy', proxyId: 'lab' },
    )
    assert.deepEqual(
      coerceMysqlNamedProxy('proxy', '', catalog, 'mysql'),
      { mode: 'proxy', proxyId: '' },
    )
    assert.deepEqual(
      coerceMysqlNamedProxy('off', 'office', catalog, 'mysql'),
      { mode: 'off', proxyId: '' },
    )
    assert.deepEqual(
      coerceMysqlNamedProxy('proxy', 'office', catalog, 'elasticsearch'),
      { mode: 'proxy', proxyId: 'office' },
    )
    assert.deepEqual(
      coerceMysqlNamedProxy('proxy', 'office', catalog, 'mongodb'),
      { mode: 'inherit', proxyId: '' },
    )
    assert.deepEqual(
      coerceMysqlNamedProxy('proxy', 'lab', catalog, 'mongodb'),
      { mode: 'proxy', proxyId: 'lab' },
    )
  })
})

describe('delete 409 references', () => {
  it('parses PROXY_IN_USE body', () => {
    const parsed = parseProxyDeleteConflict(JSON.stringify({
      ret: { code: 409, reason: 'PROXY_IN_USE', message: 'proxy is referenced by agents or tools' },
      references: [
        { kind: 'agent', id: 'a1', name: '值班' },
        { kind: 'tool', id: 't1', name: 'es-logs' },
      ],
      truncated: true,
    }))
    assert.ok(parsed)
    assert.equal(parsed?.references.length, 2)
    assert.equal(parsed?.references[0].kind, 'agent')
    assert.equal(parsed?.truncated, true)
    const msg = formatProxyInUseMessage(parsed!.references, parsed!.truncated)
    assert.match(msg, /值班/)
    assert.match(msg, /es-logs/)
    assert.match(msg, /还有更多引用未列出/)
  })
})
