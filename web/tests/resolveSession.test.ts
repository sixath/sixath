import { describe, it } from 'node:test'
import assert from 'node:assert/strict'
import {
  findLatestSessionId,
  prepareSessionForSend,
  resolveSessionToContinue,
} from '../src/api/resolveSession.ts'

function fakeApi(opts: {
  items?: { id: string }[]
  createdId?: string
  history?: { id: string; role: string; content: string }[]
}) {
  const calls = { list: 0, create: 0, messages: 0 }
  return {
    calls,
    api: {
      listSessions: async () => {
        calls.list++
        return { items: opts.items ?? [] }
      },
      createSession: async () => {
        calls.create++
        return { id: opts.createdId ?? 'sess-created' }
      },
      listMessages: async () => {
        calls.messages++
        return { items: opts.history ?? [], next_cursor: '' }
      },
    },
  }
}

describe('findLatestSessionId', () => {
  it('returns the first list item (updated_at DESC)', async () => {
    const { api } = fakeApi({
      items: [{ id: 'sess-latest' }, { id: 'sess-older' }],
    })
    assert.equal(await findLatestSessionId('agent-1', api), 'sess-latest')
  })

  it('returns undefined when the agent has no sessions', async () => {
    const { api } = fakeApi({ items: [] })
    assert.equal(await findLatestSessionId('agent-1', api), undefined)
  })
})

describe('resolveSessionToContinue', () => {
  it('keeps the currently selected session', async () => {
    const { api, calls } = fakeApi({ items: [{ id: 'sess-latest' }] })
    const got = await resolveSessionToContinue('agent-1', 'sess-open', api)
    assert.deepEqual(got, { id: 'sess-open', created: false })
    assert.equal(calls.list, 0)
    assert.equal(calls.create, 0)
  })

  it('reuses the latest session when none is selected', async () => {
    const { api, calls } = fakeApi({
      items: [{ id: 'sess-latest' }, { id: 'sess-older' }],
    })
    const got = await resolveSessionToContinue('agent-1', undefined, api)
    assert.deepEqual(got, { id: 'sess-latest', created: false })
    assert.equal(calls.list, 1)
    assert.equal(calls.create, 0)
  })

  it('creates a session only when the agent has none', async () => {
    const { api, calls } = fakeApi({ items: [], createdId: 'sess-new' })
    const got = await resolveSessionToContinue('agent-1', '  ', api)
    assert.deepEqual(got, { id: 'sess-new', created: true })
    assert.equal(calls.create, 1)
  })
})

describe('prepareSessionForSend', () => {
  it('does not reload history when a session is already selected', async () => {
    const { api, calls } = fakeApi({
      items: [{ id: 'sess-latest' }],
      history: [{ id: 'm1', role: 'user', content: 'old' }],
    })
    const got = await prepareSessionForSend('agent-1', 'sess-open', api)
    assert.equal(got.id, 'sess-open')
    assert.equal(got.created, false)
    assert.equal(got.history, null)
    assert.equal(calls.messages, 0)
  })

  it('loads history before continuing the latest session', async () => {
    const history = [
      { id: 'm1', role: 'user', content: 'http://10.141.12.13:53000/runC' },
      { id: 'm2', role: 'assistant', content: '你识得对！' },
    ]
    const { api, calls } = fakeApi({
      items: [{ id: 'sess-latest' }],
      history,
    })
    const got = await prepareSessionForSend('agent-1', undefined, api)
    assert.equal(got.id, 'sess-latest')
    assert.equal(got.created, false)
    assert.deepEqual(got.history, history)
    assert.equal(calls.messages, 1)
    assert.equal(calls.create, 0)
  })

  it('skips history load when creating a brand new session', async () => {
    const { api, calls } = fakeApi({ items: [], createdId: 'sess-new' })
    const got = await prepareSessionForSend('agent-1', undefined, api)
    assert.deepEqual(got, { id: 'sess-new', created: true, history: [], nextCursor: '' })
    assert.equal(calls.messages, 0)
  })
})
