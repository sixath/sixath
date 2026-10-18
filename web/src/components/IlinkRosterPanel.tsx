import { useCallback, useEffect, useState } from 'react'
import { ilinkApi, type IlinkPeer } from '../api/client'
import { ilinkPeerActivation, ilinkPeerTail } from '../utils/ilink'

interface IlinkRosterPanelProps {
  channelId: string
  refreshKey: number
}

function formatTime(iso?: string): string {
  if (!iso) return '—'
  const d = new Date(iso)
  return Number.isNaN(d.getTime()) ? '—' : d.toLocaleString()
}

export function IlinkRosterPanel({ channelId, refreshKey }: IlinkRosterPanelProps) {
  const [peers, setPeers] = useState<IlinkPeer[]>([])
  const [remarks, setRemarks] = useState<Record<string, string>>({})
  const [busyPeer, setBusyPeer] = useState('')
  const [error, setError] = useState('')

  const load = useCallback(async () => {
    try {
      const items = await ilinkApi.listPeers(channelId)
      setPeers(items)
      setRemarks(Object.fromEntries(items.map((p) => [p.peer_id, p.remark])))
      setError('')
    } catch (e) {
      setError(e instanceof Error ? e.message : String(e))
    }
  }, [channelId])

  useEffect(() => {
    void load()
  }, [load, refreshKey])

  const run = async (peerId: string, action: () => Promise<unknown>) => {
    setBusyPeer(peerId)
    try {
      await action()
      await load()
    } catch (e) {
      setError(e instanceof Error ? e.message : String(e))
    } finally {
      setBusyPeer('')
    }
  }

  const saveRemark = (peer: IlinkPeer) => {
    const next = (remarks[peer.peer_id] ?? '').trim()
    if (next === peer.remark) return
    void run(peer.peer_id, () => ilinkApi.patchPeer(channelId, peer.peer_id, { remark: next }))
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
              const busy = busyPeer === peer.peer_id
              return (
                <tr key={peer.peer_id}>
                  <td title={peer.peer_id}>尾号 {ilinkPeerTail(peer.peer_id)}</td>
                  <td>
                    <input
                      value={remarks[peer.peer_id] ?? ''}
                      onChange={(e) => setRemarks((prev) => ({ ...prev, [peer.peer_id]: e.target.value }))}
                      onBlur={() => saveRemark(peer)}
                      maxLength={64}
                      placeholder="备注"
                      disabled={busy}
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
                        onClick={() =>
                          void run(peer.peer_id, () =>
                            ilinkApi.patchPeer(channelId, peer.peer_id, { allowed: !peer.allowed }),
                          )
                        }
                      >
                        {peer.allowed ? '取消放行' : '放行'}
                      </button>
                      <button
                        type="button"
                        className="btn btn-ghost btn-sm"
                        disabled={busy}
                        onClick={() => void run(peer.peer_id, () => ilinkApi.deletePeer(channelId, peer.peer_id))}
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
    </div>
  )
}
