import { describe, it } from 'node:test'
import assert from 'node:assert/strict'
import {
  cronIlinkPeerError,
  ilinkLoginHint,
  ilinkPeerActivation,
  ilinkPeerLabel,
  ilinkPeerOptions,
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

describe('cronIlinkPeerError', () => {
  it('requires a recipient only for weixin_ilink channel delivery', () => {
    assert.match(cronIlinkPeerError({ deliveryMode: 'channel', channelType: 'weixin_ilink', peerId: ' ' }), /接收人/)
    assert.equal(cronIlinkPeerError({ deliveryMode: 'channel', channelType: 'weixin_ilink', peerId: 'p1' }), '')
    assert.equal(cronIlinkPeerError({ deliveryMode: 'channel', channelType: 'wecom', peerId: '' }), '')
    assert.equal(cronIlinkPeerError({ deliveryMode: 'none', channelType: 'weixin_ilink', peerId: '' }), '')
  })
})

describe('ilinkPeerOptions', () => {
  const peers = [
    { peer_id: 'aaaa1111@im.wechat', allowed: true, remark: '张三' },
    { peer_id: 'bbbb2222@im.wechat', allowed: false, remark: '' },
    { peer_id: 'cccc3333@im.wechat', allowed: true, remark: '' },
  ]
  it('lists allowed peers only', () => {
    assert.deepEqual(ilinkPeerOptions(peers, ''), [
      { value: 'aaaa1111@im.wechat', label: '张三（尾号 1111）' },
      { value: 'cccc3333@im.wechat', label: '尾号 3333' },
    ])
  })
  it('keeps a saved recipient that is no longer allowed, marked', () => {
    const opts = ilinkPeerOptions(peers, 'bbbb2222@im.wechat')
    assert.deepEqual(opts[0], { value: 'bbbb2222@im.wechat', label: '尾号 2222（未放行）' })
    assert.equal(ilinkPeerOptions([], 'gone9999@im.wechat')[0].label, '尾号 9999（未放行）')
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
