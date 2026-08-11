import { useEffect, useState } from 'react'
import { Link } from 'react-router-dom'
import { channelApi, type Channel, type ChannelRuntimeStatus } from '../api/client'
import { ConfirmDialog } from '../components/ConfirmDialog'

const RUNTIME_DOT_COLORS: Record<string, string> = {
  connected: '#16a34a',
  reconnecting: '#d97706',
  error: '#dc2626',
  unknown: '#94a3b8',
  disabled: '#64748b',
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
          <h1>Channels</h1>
          <div className="page-sub">{total} configured</div>
        </div>
        <Link to="/channels/new" className="btn">New Channel</Link>
      </div>

      <div className="section-card" style={{ marginBottom: '1.25rem', padding: '1rem 1.25rem' }}>
        <div className="filter-bar">
          <span className="filter-label">Filter</span>
          <select value={type} onChange={(e) => setType(e.target.value)}>
            <option value="">All types</option>
            <option value="web">Web</option>
            <option value="api">API</option>
            <option value="webhook">Webhook</option>
            <option value="wxpusher">WxPusher</option>
            <option value="wecom">WeCom</option>
            <option value="wecom_bot">WeCom Bot</option>
          </select>
          <select value={enabled} onChange={(e) => setEnabled(e.target.value)}>
            <option value="">All states</option>
            <option value="true">Enabled</option>
            <option value="false">Disabled</option>
          </select>
          <button type="button" className="btn btn-secondary" onClick={loadChannels}>Apply</button>
        </div>
      </div>

      {total === 0 ? (
        <div className="section-card empty-state">
          <p>No channels yet.</p>
          <Link to="/channels/new" className="btn">New Channel</Link>
        </div>
      ) : (
        <div className="table-card">
          <table>
            <thead>
              <tr>
                <th>Channel</th>
                <th>Type</th>
                <th>Default Agent</th>
                <th>Status</th>
                <th>Runtime Status</th>
                <th>Actions</th>
              </tr>
            </thead>
            <tbody>
              {channels.map((channel) => (
                <tr key={channel.id}>
                  <td>
                    <strong>{channel.channel_id}</strong>
                    {channel.webhook_path && <div className="page-sub">{channel.webhook_path}</div>}
                  </td>
                  <td><span className={`badge badge-${channel.type}`}>{channel.type}</span></td>
                  <td><code>{channel.default_agent || '-'}</code></td>
                  <td>{channel.enabled ? 'Enabled' : 'Disabled'}</td>
                  <td><RuntimeStatusCell channel={channel} /></td>
                  <td>
                    <div className="actions">
                      <Link to={`/channels/${channel.id}/edit`} className="btn btn-secondary btn-sm">Edit</Link>
                      <button className="btn btn-danger btn-sm" onClick={() => setPendingDelete({ id: channel.id, name: channel.channel_id })}>Delete</button>
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
        title="Delete channel"
        description={pendingDelete ? `Delete "${pendingDelete.name}"? This action cannot be undone.` : ''}
        confirmLabel="Delete"
        variant="danger"
        loading={confirmLoading}
        onCancel={() => setPendingDelete(null)}
        onConfirm={confirmDelete}
      />
    </div>
  )
}
