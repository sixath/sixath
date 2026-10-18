import { describe, it } from 'node:test'
import assert from 'node:assert/strict'
import {
  ilinkLoginHint,
  ilinkPeerActivation,
  ilinkPeerLabel,
  ilinkPeerTail,
  mergeRemarkDrafts,
} from '../src/utils/ilink.ts'

describe('ilinkPeerTail', () => {
  it('returns the last 4 chars of the local part', () => {
    assert.equal(ilinkPeerTail('o9cq800VxswKIZHziZ1yez1Ngkx4@im.wechat'), 'gkx4')
  })
  it('keeps short ids as-is', () => {
    assert.equal(ilinkPeerTail('ab@im.bot'), 'ab')
    assert.equal(ilinkPeerTail(''), '')
  })
})

describe('ilinkPeerLabel', () => {
  it('prefers the remark', () => {
    assert.equal(ilinkPeerLabel({ peer_id: 'xxxxabcd@im.wechat', remark: ' 张三 ' }), '张三（尾号 abcd）')
  })
  it('falls back to the tail', () => {
    assert.equal(ilinkPeerLabel({ peer_id: 'xxxxabcd@im.wechat', remark: '' }), '尾号 abcd')
  })
})

describe('ilinkLoginHint', () => {
  it('asks recipients to message the bot after login', () => {
    assert.match(ilinkLoginHint('confirmed'), /发一条消息/)
  })
  it('reports expiry', () => {
    assert.match(ilinkLoginHint('expired'), /过期/)
  })
  it('defaults to scan instructions without relying on scaned', () => {
    assert.equal(ilinkLoginHint('wait'), ilinkLoginHint('scaned'))
    assert.match(ilinkLoginHint(''), /扫码/)
  })
})

describe('ilinkPeerActivation', () => {
  it('maps context token presence', () => {
    assert.equal(ilinkPeerActivation({ has_context_token: true }), '已激活')
    assert.equal(ilinkPeerActivation({ has_context_token: false }), '待激活')
  })
})

describe('mergeRemarkDrafts', () => {
  it('takes server remarks on first load', () => {
    assert.deepEqual(mergeRemarkDrafts({}, [], [{ peer_id: 'a', remark: '甲' }]), { a: '甲' })
  })
  it('keeps dirty drafts and refreshes clean ones', () => {
    const prevPeers = [
      { peer_id: 'a', remark: '甲' },
      { peer_id: 'b', remark: '乙' },
    ]
    const nextPeers = [
      { peer_id: 'a', remark: '甲2' },
      { peer_id: 'b', remark: '乙2' },
    ]
    assert.deepEqual(mergeRemarkDrafts({ a: '甲', b: '正在编辑' }, prevPeers, nextPeers), { a: '甲2', b: '正在编辑' })
  })
  it('drops removed peers and adds new ones from the server', () => {
    const prevPeers = [{ peer_id: 'a', remark: '甲' }]
    const nextPeers = [{ peer_id: 'c', remark: '丙' }]
    assert.deepEqual(mergeRemarkDrafts({ a: '改了' }, prevPeers, nextPeers), { c: '丙' })
  })
  it('ignores stray drafts for peers the client never saw', () => {
    assert.deepEqual(mergeRemarkDrafts({ c: 'x' }, [], [{ peer_id: 'c', remark: '丙' }]), { c: '丙' })
  })
})
