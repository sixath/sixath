import { useCallback, useEffect, useRef, useState } from 'react'
import { ilinkApi, type IlinkPeer } from '../api/client'
import { ilinkPeerActivation, ilinkPeerTail, mergeRemarkDrafts } from '../utils/ilink'
import { ConfirmDialog } from './ConfirmDialog'

interface IlinkRosterPanelProps {
  channelId: string
  refreshKey: number
}

function formatTime(iso?: string): string {
  if (!iso) return '—'
  const d = new Date(iso)
  return Number.isNaN(d.getTime()) ? '—' : d.toLocaleString()
}

function errorMessage(e: unknown): string {
  return e instanceof Error ? e.message : String(e)
}

export function IlinkRosterPanel({ channelId, refreshKey }: IlinkRosterPanelProps) {
  const [peers, setPeers] = useState<IlinkPeer[]>([])
  const [remarks, setRemarks] = useState<Record<string, string>>({})
  const [busyPeers, setBusyPeers] = useState<Set<string>>(() => new Set())
  const [pendingRemove, setPendingRemove] = useState<IlinkPeer | null>(null)
  const [error, setError] = useState('')
  const peersRef = useRef<IlinkPeer[]>([])

  useEffect(() => {
    peersRef.current = peers
  }, [peers])

  const load = useCallback(async () => {
    try {
      const items = await ilinkApi.listPeers(channelId)
      const prevPeers = peersRef.current
      setPeers(items)
      setRemarks((prev) => mergeRemarkDrafts(prev, prevPeers, items))
      setError('')
    } catch (e) {
      setError(errorMessage(e))
    }
  }, [channelId])

  useEffect(() => {
    void load()
  }, [load, refreshKey])

  const applyPeer = (updated: IlinkPeer) => {
    setPeers((prev) => prev.map((p) => (p.peer_id === updated.peer_id ? updated : p)))
  }

  const run = async (peerId: string, action: () => Promise<void>) => {
    setBusyPeers((prev) => new Set(prev).add(peerId))
    try {
      await action()
      setError('')
    } catch (e) {
      setError(errorMessage(e))
    } finally {
      setBusyPeers((prev) => {
        const next = new Set(prev)
        next.delete(peerId)
        return next
      })
    }
  }

  // Not routed through run(): a blur-triggered save must not disable the button the user is clicking.
  const saveRemark = async (peer: IlinkPeer) => {
    const peerId = peer.peer_id
    const next = (remarks[peerId] ?? '').trim()
    if (next === peer.remark) {
      setRemarks((prev) => ({ ...prev, [peerId]: peer.remark }))
      return
    }
    try {
      const updated = await ilinkApi.patchPeer(channelId, peerId, { remark: next })
      applyPeer(updated)
      setRemarks((prev) =>
        (prev[peerId] ?? '').trim() === updated.remark ? { ...prev, [peerId]: updated.remark } : prev,
      )
      setError('')
    } catch (e) {
      setError(errorMessage(e))
    }
  }

  const toggleAllowed = (peer: IlinkPeer) =>
    run(peer.peer_id, async () => {
      applyPeer(await ilinkApi.patchPeer(channelId, peer.peer_id, { allowed: !peer.allowed }))
    })

  const confirmRemove = async () => {
    if (!pendingRemove) return
    const peerId = pendingRemove.peer_id
    await run(peerId, async () => {
      await ilinkApi.deletePeer(channelId, peerId)
      setPeers((prev) => prev.filter((p) => p.peer_id !== peerId))
      setRemarks((prev) => {
        const next = { ...prev }
        delete next[peerId]
        return next
      })
    })
    setPendingRemove(null)
  }

  return (
    <div className="section-card" style={{ maxWidth: 640, marginTop: '1rem' }}>
      <h2 style={{ fontSize: '1rem', margin: '0 0 0.5rem' }}>微信名册（白名单）</h2>
      <p className="page-sub">
        给 bot 发过消息的人会出现在这里，默认无权限；放行后约 15 秒生效。「待激活」表示对方还没发过消息（或 bot
        重新登录后还没再发），暂时收不到推送。
      </p>
      <div style={{ margin: '0.5rem 0' }}>
        <button type="button" className="btn btn-secondary btn-sm" onClick={() => void load()}>
          刷新
        </button>
      </div>
      {error && <div className="error">{error}</div>}
      {peers.length === 0 ? (
        <p className="page-sub">暂无对端。让对方给 bot 发一条消息后刷新。</p>
      ) : (
        <table>
          <thead>
            <tr>
              <th>对端</th>
              <th>备注</th>
              <th>权限</th>
              <th>推送</th>
              <th>最近消息</th>
              <th>操作</th>
            </tr>
          </thead>
          <tbody>
            {peers.map((peer) => {
              const busy = busyPeers.has(peer.peer_id)
              return (
                <tr key={peer.peer_id}>
                  <td title={peer.peer_id}>尾号 {ilinkPeerTail(peer.peer_id)}</td>
                  <td>
                    <input
                      value={remarks[peer.peer_id] ?? ''}
                      onChange={(e) => setRemarks((prev) => ({ ...prev, [peer.peer_id]: e.target.value }))}
                      onBlur={() => void saveRemark(peer)}
                      maxLength={64}
                      placeholder="备注"
                    />
                  </td>
                  <td>{peer.allowed ? '已放行' : '无权限'}</td>
                  <td>{ilinkPeerActivation(peer)}</td>
                  <td title={formatTime(peer.last_inbound_at)}>{peer.last_preview || '—'}</td>
                  <td>
                    <div className="row-actions">
                      <button
                        type="button"
                        className="btn btn-ghost btn-sm"
                        disabled={busy}
                        onClick={() => void toggleAllowed(peer)}
                      >
                        {peer.allowed ? '取消放行' : '放行'}
                      </button>
                      <button
                        type="button"
                        className="btn btn-ghost btn-sm"
                        disabled={busy}
                        onClick={() => setPendingRemove(peer)}
                      >
                        移出
                      </button>
                    </div>
                  </td>
                </tr>
              )
            })}
          </tbody>
        </table>
      )}
      <ConfirmDialog
        open={!!pendingRemove}
        title="移出名册"
        description="移出后对方下次发消息会重新入册，且默认无权限。"
        confirmLabel="移出"
        cancelLabel="取消"
        variant="danger"
        loading={!!pendingRemove && busyPeers.has(pendingRemove.peer_id)}
        onCancel={() => setPendingRemove(null)}
        onConfirm={() => void confirmRemove()}
      />
    </div>
  )
}
