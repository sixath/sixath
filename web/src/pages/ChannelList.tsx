import { useEffect, useState } from 'react'
import { Link } from 'react-router-dom'
import { channelApi, type Channel, type ChannelRuntimeStatus } from '../api/client'
import { ConfirmDialog } from '../components/ConfirmDialog'

/** Five Admin-facing states: connected|disconnected|reconnecting|disabled|unknown */
const RUNTIME_DOT_COLORS: Record<string, string> = {
  connected: '#16a34a',
  disconnected: '#dc2626',
  reconnecting: '#d97706',
  disabled: '#64748b',
  unknown: '#94a3b8',
}

function RuntimeStatusCell({ channel }: { channel: Channel }) {
  if (channel.type !== 'wecom_bot') return <>—</>
  const status: ChannelRuntimeStatus | undefined = channel.runtime_status
  const state = status?.state || 'unknown'
  const color = RUNTIME_DOT_COLORS[state] ?? RUNTIME_DOT_COLORS.unknown
  return (
    <span style={{ display: 'inline-flex', alignItems: 'center', gap: '0.4rem' }}>
      <span
        aria-hidden
        style={{
          width: 8,
          height: 8,
          borderRadius: '50%',
          background: color,
          flexShrink: 0,
        }}
      />
      <span>{state}</span>
    </span>
  )
}

export default function ChannelList() {
  const [channels, setChannels] = useState<Channel[]>([])
  const [total, setTotal] = useState(0)
  const [type, setType] = useState('')
  const [enabled, setEnabled] = useState('')
  const [loading, setLoading] = useState(true)
  const [error, setError] = useState('')
  const [pendingDelete, setPendingDelete] = useState<{ id: string; name: string } | null>(null)
  const [confirmLoading, setConfirmLoading] = useState(false)

  const loadChannels = () => {
    setLoading(true)
    setError('')
    channelApi.list({
      page: 1,
      page_size: 50,
      type: type || undefined,
      enabled: enabled === '' ? undefined : enabled === 'true',
    })
      .then((res) => {
        setChannels(res.items)
        setTotal(res.total)
      })
      .catch((e) => setError(e.message))
      .finally(() => setLoading(false))
  }

  useEffect(() => {
    loadChannels()
  }, [])

  const confirmDelete = async () => {
    if (!pendingDelete) return
    setConfirmLoading(true)
    try {
      await channelApi.delete(pendingDelete.id)
      setChannels((prev) => prev.filter((channel) => channel.id !== pendingDelete.id))
      setTotal((prev) => Math.max(0, prev - 1))
      setPendingDelete(null)
    } catch (e) {
      alert((e as Error).message)
    } finally {
      setConfirmLoading(false)
    }
  }

  if (loading) return (
    <div className="loading">
      <div className="loading-spinner" />
      <span style={{ marginLeft: '0.75rem' }}>Loading...</span>
    </div>
  )
  if (error) return <div className="error">Load failed: {error}</div>

  return (
    <div>
      <div className="page-header">
        <div>
          <div className="page-title-row">
            <h1>Channels</h1>
            <span className="page-count">{total}</span>
          </div>
          <p className="page-sub">配置 Web、Webhook、企业微信等接入通道。</p>
        </div>
        <Link to="/channels/new" className="btn">新建 Channel</Link>
      </div>

      {total === 0 ? (
        <div className="section-card empty-state">
          <p>还没有 Channel。</p>
          <Link to="/channels/new" className="btn">新建 Channel</Link>
        </div>
      ) : (
        <div className="table-card">
          <div className="table-toolbar">
            <div className="filter-bar">
              <span className="filter-label">筛选</span>
              <select value={type} onChange={(e) => setType(e.target.value)}>
                <option value="">全部类型</option>
                <option value="web">Web</option>
                <option value="api">API</option>
                <option value="webhook">Webhook</option>
                <option value="wxpusher">WxPusher</option>
                <option value="wecom">企业微信</option>
                <option value="wecom_bot">企业微信 Bot</option>
              </select>
              <select value={enabled} onChange={(e) => setEnabled(e.target.value)}>
                <option value="">全部状态</option>
                <option value="true">已启用</option>
                <option value="false">已停用</option>
              </select>
              <button type="button" className="btn btn-secondary btn-sm" onClick={loadChannels}>应用</button>
            </div>
          </div>
          <table>
            <thead>
              <tr>
                <th>通道</th>
                <th>类型</th>
                <th>默认 Agent</th>
                <th>状态</th>
                <th>运行时</th>
                <th className="col-actions">操作</th>
              </tr>
            </thead>
            <tbody>
              {channels.map((channel) => (
                <tr key={channel.id}>
                  <td>
                    <span className="cell-name">
                      <span className="cell-dot cell-dot--cyan" aria-hidden />
                      {channel.channel_id}
                    </span>
                    {channel.webhook_path && <div className="page-sub">{channel.webhook_path}</div>}
                  </td>
                  <td><span className={`badge badge-${channel.type}`}>{channel.type}</span></td>
                  <td><code>{channel.default_agent || '-'}</code></td>
                  <td>{channel.enabled ? '已启用' : '已停用'}</td>
                  <td><RuntimeStatusCell channel={channel} /></td>
                  <td className="col-actions">
                    <div className="row-actions">
                      <Link to={`/channels/${channel.id}/edit`} className="btn btn-ghost btn-sm">编辑</Link>
                      <button type="button" className="btn btn-ghost btn-sm btn-danger" onClick={() => setPendingDelete({ id: channel.id, name: channel.channel_id })}>删除</button>
                    </div>
                  </td>
                </tr>
              ))}
            </tbody>
          </table>
        </div>
      )}
      <ConfirmDialog
        open={!!pendingDelete}
        title="删除 Channel"
        description={pendingDelete ? `删除「${pendingDelete.name}」？此操作不可恢复。` : ''}
        confirmLabel="删除"
        variant="danger"
        loading={confirmLoading}
        onCancel={() => setPendingDelete(null)}
        onConfirm={confirmDelete}
      />
    </div>
  )
}
