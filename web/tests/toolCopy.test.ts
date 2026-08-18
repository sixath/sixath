import assert from 'node:assert/strict'
import { describe, it } from 'node:test'
import { buildCopiedTool, nextCopyName } from '../src/utils/toolCopy.ts'

describe('nextCopyName', () => {
  it('appends _copy', () => {
    assert.equal(nextCopyName('migu_mongodb', []), 'migu_mongodb_copy')
  })

  it('increments when _copy exists', () => {
    assert.equal(
      nextCopyName('migu_mongodb', ['migu_mongodb', 'migu_mongodb_copy']),
      'migu_mongodb_copy2',
    )
  })

  it('treats existing _copy as same stem', () => {
    assert.equal(
      nextCopyName('migu_mongodb_copy', ['migu_mongodb', 'migu_mongodb_copy']),
      'migu_mongodb_copy2',
    )
  })
})

describe('buildCopiedTool', () => {
  it('clones datasource and retargets id', () => {
    const body = buildCopiedTool(
      {
        name: 'migu_mongodb',
        description: 'migu mongodb',
        type: 'datasource',
        config: {
          datasource: {
            id: 'mongoDB',
            type: 'mongodb',
            host: '10.19.240.106',
            port: 27017,
            user: 'admin',
            password: 'secret',
            dbname: 'appdb',
          },
        },
      },
      ['migu_mongodb'],
    )
    assert.equal(body.name, 'migu_mongodb_copy')
    assert.equal(body.type, 'datasource')
    assert.equal(body.config.datasource?.id, 'migu_mongodb_copy')
    assert.equal(body.config.datasource?.host, '10.19.240.106')
    assert.equal(body.config.datasource?.password, 'secret')
  })
})
