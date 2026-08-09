import { type FormEvent, useEffect, useState } from 'react'
import { Link, useNavigate, useParams } from 'react-router-dom'
import { agentApi, channelApi, type Agent, type CreateChannelRequest } from '../api/client'

function parseList(value: string): string[] | undefined {
  const items = value
    .split(/[\n,]/)
    .map((item) => item.trim())
    .filter(Boolean)
  return items.length ? items : undefined
}

function joinList(value?: string[]): string {
  return value?.join('\n') || ''
}

export default function ChannelForm() {
  const { id } = useParams()
  const navigate = useNavigate()
  const isEdit = !!id

  const [agents, setAgents] = useState<Agent[]>([])
  const [channelId, setChannelId] = useState('')
  const [type, setType] = useState<CreateChannelRequest['type']>('web')
  const [defaultAgent, setDefaultAgent] = useState('')
  const [enabled, setEnabled] = useState(true)
  const [webhookPath, setWebhookPath] = useState('')
  const [webhookSecret, setWebhookSecret] = useState('')
  const [ipWhitelist, setIpWhitelist] = useState('')
  const [appToken, setAppToken] = useState('')
  const [webhookUrl, setWebhookUrl] = useState('')
  const [webhookUrlMasked, setWebhookUrlMasked] = useState('')
  const [defaultUids, setDefaultUids] = useState('')
  const [error, setError] = useState('')
  const [loading, setLoading] = useState(false)

  useEffect(() => {
    agentApi.list({ page: 1, page_size: 100 })
      .then((res) => setAgents(res.items))
      .catch(() => setAgents([]))
  }, [])

  useEffect(() => {
    if (!isEdit || !id) return
    channelApi.get(id)
      .then((channel) => {
        setChannelId(channel.channel_id)
        setType((channel.type || 'web') as CreateChannelRequest['type'])
        setDefaultAgent(channel.default_agent || '')
        setEnabled(channel.enabled)
        setWebhookPath(channel.webhook_path || '')
        setIpWhitelist(joinList(channel.ip_whitelist))
        setDefaultUids(joinList(channel.default_uids))
        setWebhookUrlMasked(channel.webhook_url_masked || '')
      })
      .catch((e) => setError(e.message))
  }, [id, isEdit])

  const handleSubmit = async (event: FormEvent) => {
    event.preventDefault()
    setError('')
    if (!channelId.trim()) {
      setError('Channel ID is required.')
      return
    }
    if (type === 'wecom' && !webhookUrl.trim() && (!isEdit || !webhookUrlMasked)) {
      setError('Webhook URL is required for WeCom channels.')
      return
    }

    setLoading(true)
    try {
      if (isEdit && id) {
        const updates: Partial<CreateChannelRequest> = {
          channel_id: channelId.trim(),
          type,
          default_agent: defaultAgent || undefined,
          enabled,
        }
        if (type === 'webhook' || type === 'api') {
          updates.webhook_path = webhookPath.trim() || undefined
          if (webhookSecret.trim()) updates.webhook_secret = webhookSecret.trim()
          updates.ip_whitelist = parseList(ipWhitelist)
        }
        if (type === 'wxpusher') {
          if (appToken.trim()) updates.app_token = appToken.trim()
          updates.default_uids = parseList(defaultUids)
        }
        if (type === 'wecom' && webhookUrl.trim()) {
          updates.webhook_url = webhookUrl.trim()
        }
        await channelApi.update(id, updates)
      } else {
        const data: CreateChannelRequest = {
          channel_id: channelId.trim(),
          type,
          default_agent: defaultAgent || undefined,
          enabled,
          webhook_path: webhookPath.trim() || undefined,
          webhook_secret: webhookSecret.trim() || undefined,
          ip_whitelist: parseList(ipWhitelist),
          app_token: appToken.trim() || undefined,
          webhook_url: webhookUrl.trim() || undefined,
          default_uids: parseList(defaultUids),
        }
        await channelApi.create(data)
      }
      navigate('/channels')
    } catch (e) {
      setError((e as Error).message)
    } finally {
      setLoading(false)
    }
  }

  return (
    <div>
      <div className="page-header">
        <h1>{isEdit ? 'Edit Channel' : 'New Channel'}</h1>
        <Link to="/channels" className="btn btn-secondary">Back</Link>
      </div>

      <div className="section-card" style={{ maxWidth: 640 }}>
        <form onSubmit={handleSubmit}>
          <div className="form-group">
            <label>Channel ID *</label>
            <input value={channelId} onChange={(e) => setChannelId(e.target.value)} placeholder="web-default" />
          </div>

          <div className="form-row">
            <div className="form-group">
              <label>Type *</label>
              <select value={type} onChange={(e) => setType(e.target.value as CreateChannelRequest['type'])}>
                <option value="web">Web</option>
                <option value="api">API</option>
                <option value="webhook">Webhook</option>
                <option value="wxpusher">WxPusher</option>
                <option value="wecom">WeCom</option>
              </select>
            </div>
            <div className="form-group">
              <label>Default Agent</label>
              <select value={defaultAgent} onChange={(e) => setDefaultAgent(e.target.value)}>
                <option value="">None</option>
                {agents.map((agent) => (
                  <option key={agent.id} value={agent.id}>{agent.name}</option>
                ))}
              </select>
              {type === 'wecom' && (
                <small style={{ display: 'block', marginTop: '0.35rem', color: 'var(--muted, #64748b)' }}>
                  WeCom 渠道：选择 Default Agent 后，该 Agent 对话中将自动启用 send_to_wecom 工具。
                </small>
              )}
            </div>
          </div>

          <div className="form-group">
            <label style={{ display: 'flex', alignItems: 'center', gap: '0.5rem' }}>
              <input type="checkbox" checked={enabled} onChange={(e) => setEnabled(e.target.checked)} />
              Enabled
            </label>
          </div>

          {(type === 'webhook' || type === 'api') && (
            <>
              <div className="form-group">
                <label>Webhook Path</label>
                <input value={webhookPath} onChange={(e) => setWebhookPath(e.target.value)} placeholder="/hooks/incoming" />
              </div>
              <div className="form-group">
                <label>Webhook Secret</label>
                <input type="password" value={webhookSecret} onChange={(e) => setWebhookSecret(e.target.value)} placeholder={isEdit ? 'Leave blank to keep existing secret' : 'Optional secret'} />
              </div>
              <div className="form-group">
                <label>IP Whitelist</label>
                <textarea value={ipWhitelist} onChange={(e) => setIpWhitelist(e.target.value)} rows={3} placeholder="One IP or CIDR per line" />
              </div>
            </>
          )}

          {type === 'wxpusher' && (
            <>
              <div className="form-group">
                <label>App Token</label>
                <input type="password" value={appToken} onChange={(e) => setAppToken(e.target.value)} placeholder={isEdit ? 'Leave blank to keep existing token' : 'WxPusher app token'} />
              </div>
              <div className="form-group">
                <label>Default UIDs</label>
                <textarea value={defaultUids} onChange={(e) => setDefaultUids(e.target.value)} rows={3} placeholder="One UID per line" />
              </div>
            </>
          )}

          {type === 'wecom' && (
            <div className="form-group">
              <label>企业微信群机器人 Webhook URL{!isEdit || !webhookUrlMasked ? ' *' : ''}</label>
              {isEdit && webhookUrlMasked && (
                <div className="page-sub" style={{ marginBottom: '0.35rem' }}>
                  当前已配置：{webhookUrlMasked}（留空输入框表示不修改）
                </div>
              )}
              <input
                type="url"
                value={webhookUrl}
                onChange={(e) => setWebhookUrl(e.target.value)}
                autoComplete="off"
                placeholder={isEdit ? '留空表示不修改' : 'https://qyapi.weixin.qq.com/cgi-bin/webhook/send?key=...'}
              />
            </div>
          )}

          {error && <div className="error">{error}</div>}
          <div style={{ display: 'flex', gap: '0.5rem', marginTop: '1.5rem' }}>
            <button type="submit" className="btn" disabled={loading}>{loading ? 'Saving...' : 'Save'}</button>
            <Link to="/channels" className="btn btn-secondary">Cancel</Link>
          </div>
        </form>
      </div>
    </div>
  )
}
