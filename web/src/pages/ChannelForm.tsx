import { type FormEvent, useEffect, useState } from 'react'
import { Link, useNavigate, useParams } from 'react-router-dom'
import {
  agentApi,
  channelApi,
  type Agent,
  type ChannelRuntimeStatus,
  type CreateChannelRequest,
} from '../api/client'

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

/** When allowlist is non-empty, default agent must be included (Portal validation). */
function withDefaultInAllowed(list: string[], defaultId: string): string[] {
  const trimmed = defaultId.trim()
  if (!list.length || !trimmed || list.includes(trimmed)) return list
  return [...list, trimmed]
}

function formatRelativeTime(iso?: string): string {
  if (!iso) return '—'
  const d = new Date(iso)
  const t = d.getTime()
  if (Number.isNaN(t)) return '—'
  const now = Date.now()
  let diffSec = Math.round((now - t) / 1000)
  const rtf = new Intl.RelativeTimeFormat('en', { numeric: 'auto' })
  if (Math.abs(diffSec) < 45) return rtf.format(-Math.max(diffSec, 1), 'second')
  const diffMin = Math.round(diffSec / 60)
  if (Math.abs(diffMin) < 60) return rtf.format(-diffMin, 'minute')
  const diffHour = Math.round(diffMin / 60)
  if (Math.abs(diffHour) < 24) return rtf.format(-diffHour, 'hour')
  const diffDay = Math.round(diffHour / 24)
  if (Math.abs(diffDay) < 30) return rtf.format(-diffDay, 'day')
  const diffMonth = Math.round(diffDay / 30)
  if (Math.abs(diffMonth) < 12) return rtf.format(-diffMonth, 'month')
  const diffYear = Math.round(diffDay / 365)
  return rtf.format(-diffYear, 'year')
}

/** Five Admin-facing states: connected|disconnected|reconnecting|disabled|unknown */
const RUNTIME_DOT_COLORS: Record<string, string> = {
  connected: '#16a34a',
  disconnected: '#dc2626',
  reconnecting: '#d97706',
  disabled: '#64748b',
  unknown: '#94a3b8',
}

function RuntimePanel({ status }: { status?: ChannelRuntimeStatus }) {
  const state = status?.state || 'unknown'
  const color = RUNTIME_DOT_COLORS[state] ?? RUNTIME_DOT_COLORS.unknown
  const reconnectInSec =
    status?.reconnect_in_ms != null && status.reconnect_in_ms > 0
      ? Math.round(status.reconnect_in_ms / 1000)
      : null

  return (
    <div className="form-panel" style={{ marginBottom: '1.25rem' }}>
      <label>Runtime Status</label>
      <p className="form-panel__desc">Gateway connection status for this WeCom Bot channel.</p>
      <div style={{ display: 'grid', gap: '0.5rem', fontSize: '0.95rem' }}>
        <div style={{ display: 'inline-flex', alignItems: 'center', gap: '0.4rem' }}>
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
          <strong>{state}</strong>
        </div>
        <div>
          <span style={{ color: 'var(--muted)' }}>Last heartbeat: </span>
          {formatRelativeTime(status?.last_heartbeat_at)}
        </div>
        <div>
          <span style={{ color: 'var(--muted)' }}>Last error: </span>
          {status?.last_error || '—'}
        </div>
        <div>
          <span style={{ color: 'var(--muted)' }}>Reconnect: </span>
          attempt {status?.reconnect_attempt ?? 0}
          {reconnectInSec != null ? ` · in ${reconnectInSec}s` : ''}
        </div>
      </div>
    </div>
  )
}

export default function ChannelForm() {
  const { id } = useParams()
  const navigate = useNavigate()
  const isEdit = !!id

  const [agents, setAgents] = useState<Agent[]>([])
  const [channelId, setChannelId] = useState('')
  const [type, setType] = useState<CreateChannelRequest['type']>('web')
  const [defaultAgent, setDefaultAgent] = useState('')
  const [allowedAgents, setAllowedAgents] = useState<string[]>([])
  const [enabled, setEnabled] = useState(true)
  const [autoRouteEnabled, setAutoRouteEnabled] = useState(true)
  const [autoRouteMention, setAutoRouteMention] = useState(true)
  const [autoRouteClassifier, setAutoRouteClassifier] = useState(true)
  const [webhookPath, setWebhookPath] = useState('')
  const [webhookSecret, setWebhookSecret] = useState('')
  const [ipWhitelist, setIpWhitelist] = useState('')
  const [appToken, setAppToken] = useState('')
  const [webhookUrl, setWebhookUrl] = useState('')
  const [webhookUrlMasked, setWebhookUrlMasked] = useState('')
  const [defaultUids, setDefaultUids] = useState('')
  const [defaultReplyMode, setDefaultReplyMode] = useState<'async' | 'sync'>('async')
  const [botId, setBotId] = useState('')
  const [botSecret, setBotSecret] = useState('')
  const [secretSet, setSecretSet] = useState(false)
  const [botNames, setBotNames] = useState('')
  const [wsUrl, setWsUrl] = useState('')
  const [corpId, setCorpId] = useState('')
  const [corpSecret, setCorpSecret] = useState('')
  const [runtimeStatus, setRuntimeStatus] = useState<ChannelRuntimeStatus | undefined>()
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
        setAllowedAgents(channel.allowed_agents ?? [])
        setEnabled(channel.enabled)
        setAutoRouteEnabled(channel.auto_route_enabled ?? true)
        setAutoRouteMention(channel.auto_route_mention ?? true)
        setAutoRouteClassifier(channel.auto_route_classifier ?? true)
        setWebhookPath(channel.webhook_path || '')
        setIpWhitelist(joinList(channel.ip_whitelist))
        setDefaultUids(joinList(channel.default_uids))
        setWebhookUrlMasked(channel.webhook_url_masked || '')
        setDefaultReplyMode(
          channel.default_reply_mode === 'sync' ? 'sync' : 'async',
        )
        setBotId(channel.bot_id || '')
        setSecretSet(!!channel.secret_set)
        setBotNames(joinList(channel.bot_names))
        setWsUrl(channel.ws_url || '')
        setCorpId(channel.corp_id || '')
        setRuntimeStatus(channel.runtime_status)
      })
      .catch((e) => setError(e.message))
  }, [id, isEdit])

  useEffect(() => {
    if (!defaultAgent.trim() || !allowedAgents.length) return
    setAllowedAgents((prev) => withDefaultInAllowed(prev, defaultAgent))
  }, [defaultAgent, allowedAgents.length])

  const toggleAllowedAgent = (agentId: string) => {
    setAllowedAgents((prev) => {
      const next = prev.includes(agentId)
        ? prev.filter((id) => id !== agentId)
        : [...prev, agentId]
      return withDefaultInAllowed(next, defaultAgent)
    })
  }

  const resolveAllowedAgentsForSubmit = (): string[] | undefined => {
    const normalized = withDefaultInAllowed(allowedAgents, defaultAgent)
    return normalized.length ? normalized : undefined
  }

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
    if (type === 'wecom_bot') {
      if (!botId.trim()) {
        setError('Bot ID is required for WeCom Bot channels.')
        return
      }
      if (!botSecret.trim() && (!isEdit || !secretSet)) {
        setError('Secret is required for WeCom Bot channels.')
        return
      }
    }
    const allowedForSubmit = resolveAllowedAgentsForSubmit()
    if (allowedForSubmit?.length && !defaultAgent.trim()) {
      setError('Default Agent is required when Allowed Agents is set.')
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
          allowed_agents: allowedForSubmit ?? [],
          auto_route_enabled: autoRouteEnabled,
          auto_route_mention: autoRouteMention,
          auto_route_classifier: autoRouteClassifier,
        }
        if (type === 'webhook' || type === 'api') {
          updates.webhook_path = webhookPath.trim() || undefined
          if (webhookSecret.trim()) updates.webhook_secret = webhookSecret.trim()
          updates.ip_whitelist = parseList(ipWhitelist)
        }
        if (type === 'webhook') {
          updates.default_reply_mode = defaultReplyMode
        }
        if (type === 'wxpusher') {
          if (appToken.trim()) updates.app_token = appToken.trim()
          updates.default_uids = parseList(defaultUids)
        }
        if (type === 'wecom' && webhookUrl.trim()) {
          updates.webhook_url = webhookUrl.trim()
        }
        if (type === 'wecom_bot') {
          updates.bot_id = botId.trim()
          if (botSecret.trim()) updates.secret = botSecret.trim()
          updates.bot_names = parseList(botNames) ?? []
          updates.ws_url = wsUrl.trim() || undefined
          updates.corp_id = corpId.trim() || undefined
          if (corpSecret.trim()) updates.corp_secret = corpSecret.trim()
        }
        await channelApi.update(id, updates)
      } else {
        const data: CreateChannelRequest = {
          channel_id: channelId.trim(),
          type,
          default_agent: defaultAgent || undefined,
          allowed_agents: allowedForSubmit,
          enabled,
          auto_route_enabled: autoRouteEnabled,
          auto_route_mention: autoRouteMention,
          auto_route_classifier: autoRouteClassifier,
          webhook_path: webhookPath.trim() || undefined,
          webhook_secret: webhookSecret.trim() || undefined,
          ip_whitelist: parseList(ipWhitelist),
          app_token: appToken.trim() || undefined,
          webhook_url: webhookUrl.trim() || undefined,
          default_uids: parseList(defaultUids),
        }
        if (type === 'webhook') {
          data.default_reply_mode = defaultReplyMode
        }
        if (type === 'wecom_bot') {
          data.bot_id = botId.trim()
          data.secret = botSecret.trim() || undefined
          data.bot_names = parseList(botNames)
          data.ws_url = wsUrl.trim() || undefined
          data.corp_id = corpId.trim() || undefined
          data.corp_secret = corpSecret.trim() || undefined
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
        {isEdit && type === 'wecom_bot' && <RuntimePanel status={runtimeStatus} />}

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
                <option value="wecom_bot">WeCom Bot</option>
              </select>
            </div>
            <div className="form-group">
              <label>Default Agent</label>
              <select
                value={defaultAgent}
                onChange={(e) => setDefaultAgent(e.target.value)}
              >
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
            <div className="form-panel">
              <label>Allowed Agents</label>
              <p className="form-panel__desc">
                留空表示仅允许 Default Agent；勾选后用户只能绑定列表中的 Agent，且 Default Agent 必须在列表内。
              </p>
              {agents.length > 0 ? (
                <div className="checkbox-list">
                  {agents.map((agent) => {
                    const isDefault = agent.id === defaultAgent
                    const checked = allowedAgents.includes(agent.id)
                    return (
                      <div key={agent.id} className="checkbox-list__item">
                        <label className="checkbox-field">
                          <input
                            type="checkbox"
                            checked={checked}
                            disabled={isDefault && checked && allowedAgents.length > 0}
                            onChange={() => toggleAllowedAgent(agent.id)}
                          />
                          <span>{agent.name}</span>
                          {isDefault ? (
                            <span style={{ color: 'var(--muted)', fontSize: '0.85em' }}>(default)</span>
                          ) : null}
                        </label>
                      </div>
                    )
                  })}
                </div>
              ) : (
                <p style={{ color: 'var(--muted)', margin: 0 }}>暂无 Agent，请先创建 Agent。</p>
              )}
            </div>
          </div>

          <div className="form-group">
            <div className="form-panel">
              <label>Auto-route</label>
              <p className="form-panel__desc">
                控制本渠道是否按消息自动选择 Agent（@提及 / 分类器）。
              </p>
              <div className="checkbox-list">
                <div className="checkbox-list__item">
                  <label className="checkbox-field">
                    <input
                      type="checkbox"
                      checked={autoRouteEnabled}
                      onChange={(e) => setAutoRouteEnabled(e.target.checked)}
                    />
                    <span>Enable auto-route</span>
                  </label>
                </div>
                <div className="checkbox-list__item">
                  <label className="checkbox-field">
                    <input
                      type="checkbox"
                      checked={autoRouteMention}
                      disabled={!autoRouteEnabled}
                      onChange={(e) => setAutoRouteMention(e.target.checked)}
                    />
                    <span>@Agent mention</span>
                  </label>
                </div>
                <div className="checkbox-list__item">
                  <label className="checkbox-field">
                    <input
                      type="checkbox"
                      checked={autoRouteClassifier}
                      disabled={!autoRouteEnabled}
                      onChange={(e) => setAutoRouteClassifier(e.target.checked)}
                    />
                    <span>Classifier (no @)</span>
                  </label>
                </div>
              </div>
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
              {type === 'webhook' && (
                <div className="form-group">
                  <label>Default Reply Mode</label>
                  <select
                    value={defaultReplyMode}
                    onChange={(e) => setDefaultReplyMode(e.target.value as 'async' | 'sync')}
                  >
                    <option value="async">async</option>
                    <option value="sync">sync</option>
                  </select>
                </div>
              )}
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

          {type === 'wecom_bot' && (
            <>
              <div className="form-group">
                <label>Bot ID *</label>
                <input
                  value={botId}
                  onChange={(e) => setBotId(e.target.value)}
                  placeholder="ai_bot_xxx"
                  autoComplete="off"
                />
              </div>
              <div className="form-group">
                <label>Secret{!isEdit || !secretSet ? ' *' : ''}</label>
                {isEdit && secretSet && (
                  <div className="page-sub" style={{ marginBottom: '0.35rem' }}>
                    Secret is set（leave blank to keep existing）
                  </div>
                )}
                <input
                  type="password"
                  value={botSecret}
                  onChange={(e) => setBotSecret(e.target.value)}
                  placeholder={isEdit ? 'Leave blank to keep existing secret' : 'Bot secret'}
                  autoComplete="new-password"
                />
              </div>
              <div className="form-group">
                <label>Bot Names</label>
                <textarea
                  value={botNames}
                  onChange={(e) => setBotNames(e.target.value)}
                  rows={3}
                  placeholder="One bot display name per line"
                />
              </div>
              <div className="form-group">
                <label>WS URL (optional)</label>
                <input
                  value={wsUrl}
                  onChange={(e) => setWsUrl(e.target.value)}
                  placeholder="wss://..."
                  autoComplete="off"
                />
              </div>
              <div className="form-group">
                <label>Corp ID (optional)</label>
                <input
                  value={corpId}
                  onChange={(e) => setCorpId(e.target.value)}
                  placeholder="wwxxxxxxxx"
                  autoComplete="off"
                />
              </div>
              <div className="form-group">
                <label>Corp Secret (optional)</label>
                <input
                  type="password"
                  value={corpSecret}
                  onChange={(e) => setCorpSecret(e.target.value)}
                  placeholder={isEdit ? 'Leave blank to keep existing corp secret' : 'Optional corp secret'}
                  autoComplete="new-password"
                />
              </div>
            </>
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
