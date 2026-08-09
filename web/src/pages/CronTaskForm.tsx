import { type FormEvent, useEffect, useState } from 'react'
import { Link, useNavigate, useParams } from 'react-router-dom'
import { agentApi, channelApi, cronApi, type Agent, type Channel, type CreateCronTaskRequest } from '../api/client'

function toNumber(value: string, fallback: number): number {
  const parsed = Number(value)
  return Number.isFinite(parsed) ? parsed : fallback
}

export default function CronTaskForm() {
  const { id } = useParams()
  const navigate = useNavigate()
  const isEdit = !!id

  const [agents, setAgents] = useState<Agent[]>([])
  const [channels, setChannels] = useState<Channel[]>([])
  const [name, setName] = useState('')
  const [agentId, setAgentId] = useState('')
  const [scheduleKind, setScheduleKind] = useState<CreateCronTaskRequest['schedule_kind']>('cron')
  const [scheduleExpr, setScheduleExpr] = useState('')
  const [timezone, setTimezone] = useState('Asia/Shanghai')
  const [staggerSec, setStaggerSec] = useState('0')
  const [payloadKind, setPayloadKind] = useState<CreateCronTaskRequest['payload_kind']>('agent_turn')
  const [payloadContent, setPayloadContent] = useState('')
  const [timeoutSec, setTimeoutSec] = useState('120')
  const [retryCount, setRetryCount] = useState('0')
  const [retryIntervalSec, setRetryIntervalSec] = useState('60')
  const [deliveryMode, setDeliveryMode] = useState<CreateCronTaskRequest['delivery_mode']>('none')
  const [deliveryWebhookUrl, setDeliveryWebhookUrl] = useState('')
  const [deliverySecret, setDeliverySecret] = useState('')
  const [deliveryBestEffort, setDeliveryBestEffort] = useState(true)
  const [deliverySessionId, setDeliverySessionId] = useState('')
  const [deliveryChannelId, setDeliveryChannelId] = useState('')
  const [enabled, setEnabled] = useState(true)
  const [error, setError] = useState('')
  const [loading, setLoading] = useState(false)
  const [initialLoading, setInitialLoading] = useState(isEdit)

  const applyTask = (task: Awaited<ReturnType<typeof cronApi.get>>) => {
    setName(task.name)
    setAgentId(task.agent_id)
    setScheduleKind((task.schedule_kind || 'cron') as CreateCronTaskRequest['schedule_kind'])
    setScheduleExpr(task.schedule_expr)
    setTimezone(task.timezone || 'Asia/Shanghai')
    setStaggerSec(String(task.stagger_sec ?? 0))
    setPayloadKind((task.payload_kind || 'agent_turn') as CreateCronTaskRequest['payload_kind'])
    setPayloadContent(task.payload_content || '')
    setTimeoutSec(String(task.timeout_sec ?? 120))
    setRetryCount(String(task.retry_count ?? 0))
    setRetryIntervalSec(String(task.retry_interval_sec ?? 60))
    setDeliveryMode((task.delivery_mode || 'none') as CreateCronTaskRequest['delivery_mode'])
    setDeliveryWebhookUrl(task.delivery_webhook_url || '')
    setDeliverySessionId(task.delivery_session_id || '')
    setDeliveryChannelId(task.delivery_channel_id || '')
    setEnabled(task.enabled)
  }

  useEffect(() => {
    setError('')
    const refsPromise = Promise.all([
      agentApi.list({ page: 1, page_size: 100 }).catch(() => ({ items: [], total: 0 })),
      channelApi.list({ page: 1, page_size: 100 }).catch(() => ({ items: [], total: 0 })),
    ])

    if (isEdit && id) {
      setInitialLoading(true)
      Promise.all([refsPromise, cronApi.get(id)])
        .then(([[agentRes, channelRes], task]) => {
          setAgents(agentRes.items)
          setChannels(channelRes.items)
          applyTask(task)
        })
        .catch((e) => setError((e as Error).message))
        .finally(() => setInitialLoading(false))
      return
    }

    refsPromise
      .then(([agentRes, channelRes]) => {
        setAgents(agentRes.items)
        setChannels(channelRes.items)
        if (agentRes.items[0]) setAgentId(agentRes.items[0].id)
      })
      .catch((e) => setError((e as Error).message))
  }, [id, isEdit])

  const handleSubmit = async (event: FormEvent) => {
    event.preventDefault()
    setError('')
    if (!name.trim()) {
      setError('Task name is required.')
      return
    }
    if (!agentId) {
      setError('Agent is required.')
      return
    }
    if (!scheduleExpr.trim()) {
      setError('Schedule expression is required.')
      return
    }
    if (!payloadContent.trim()) {
      setError('Payload content is required.')
      return
    }

    const data: CreateCronTaskRequest = {
      name: name.trim(),
      agent_id: agentId,
      schedule_kind: scheduleKind,
      schedule_expr: scheduleExpr.trim(),
      timezone: timezone.trim() || undefined,
      stagger_sec: toNumber(staggerSec, 0),
      payload_kind: payloadKind,
      payload_content: payloadContent,
      timeout_sec: toNumber(timeoutSec, 120),
      retry_count: toNumber(retryCount, 0),
      retry_interval_sec: toNumber(retryIntervalSec, 60),
      delivery_mode: deliveryMode,
      delivery_webhook_url: deliveryWebhookUrl.trim() || undefined,
      delivery_secret: deliverySecret.trim() || undefined,
      delivery_best_effort: deliveryBestEffort,
      delivery_session_id: deliverySessionId.trim() || undefined,
      delivery_channel_id: deliveryChannelId || undefined,
      enabled,
    }

    setLoading(true)
    try {
      if (isEdit && id) {
        await cronApi.update(id, { ...data })
      } else {
        await cronApi.create(data)
      }
      navigate('/cron')
    } catch (e) {
      setError((e as Error).message)
    } finally {
      setLoading(false)
    }
  }

  if (initialLoading) {
    return (
      <div className="loading">
        <div className="loading-spinner" />
        <span style={{ marginLeft: '0.75rem' }}>Loading...</span>
      </div>
    )
  }

  return (
    <div>
      <div className="page-header">
        <h1>{isEdit ? 'Edit Cron Task' : 'New Cron Task'}</h1>
        <Link to="/cron" className="btn btn-secondary">Back</Link>
      </div>

      <div className="section-card" style={{ maxWidth: 760 }}>
        <form onSubmit={handleSubmit}>
          <div className="form-row">
            <div className="form-group">
              <label>Name *</label>
              <input value={name} onChange={(e) => setName(e.target.value)} placeholder="daily-summary" />
            </div>
            <div className="form-group">
              <label>Agent *</label>
              <select value={agentId} onChange={(e) => setAgentId(e.target.value)}>
                <option value="">Select agent</option>
                {agents.map((agent) => (
                  <option key={agent.id} value={agent.id}>{agent.name}</option>
                ))}
              </select>
            </div>
          </div>

          <div className="form-row">
            <div className="form-group">
              <label>Schedule Type *</label>
              <select value={scheduleKind} onChange={(e) => setScheduleKind(e.target.value as CreateCronTaskRequest['schedule_kind'])}>
                <option value="cron">Cron</option>
                <option value="every">Every</option>
                <option value="at">At</option>
              </select>
            </div>
            <div className="form-group">
              <label>Schedule Expression *</label>
              <input value={scheduleExpr} onChange={(e) => setScheduleExpr(e.target.value)} placeholder={scheduleKind === 'cron' ? '0 9 * * *' : scheduleKind === 'every' ? '1h' : '2026-04-27T09:00:00+08:00'} />
            </div>
          </div>

          <div className="form-row">
            <div className="form-group">
              <label>Timezone</label>
              <input value={timezone} onChange={(e) => setTimezone(e.target.value)} placeholder="Asia/Shanghai" />
            </div>
            <div className="form-group">
              <label>Stagger Seconds</label>
              <input type="number" min="0" value={staggerSec} onChange={(e) => setStaggerSec(e.target.value)} />
            </div>
          </div>

          <div className="form-group">
            <label>Payload Type *</label>
            <select value={payloadKind} onChange={(e) => setPayloadKind(e.target.value as CreateCronTaskRequest['payload_kind'])}>
              <option value="agent_turn">Agent Turn</option>
              <option value="skill_execute">Skill Execute</option>
            </select>
          </div>

          <div className="form-group">
            <label>Payload Content *</label>
            <textarea value={payloadContent} onChange={(e) => setPayloadContent(e.target.value)} rows={5} placeholder="Prompt or skill payload" />
          </div>

          <div className="form-row">
            <div className="form-group">
              <label>Timeout Seconds</label>
              <input type="number" min="1" value={timeoutSec} onChange={(e) => setTimeoutSec(e.target.value)} />
            </div>
            <div className="form-group">
              <label>Retry Count</label>
              <input type="number" min="0" value={retryCount} onChange={(e) => setRetryCount(e.target.value)} />
            </div>
            <div className="form-group">
              <label>Retry Interval</label>
              <input type="number" min="0" value={retryIntervalSec} onChange={(e) => setRetryIntervalSec(e.target.value)} />
            </div>
          </div>

          <div className="form-group">
            <label>Delivery Mode</label>
            <select value={deliveryMode} onChange={(e) => setDeliveryMode(e.target.value as CreateCronTaskRequest['delivery_mode'])}>
              <option value="none">None</option>
              <option value="webhook">Webhook</option>
              <option value="session">Session</option>
              <option value="channel">Channel</option>
            </select>
          </div>

          {deliveryMode === 'webhook' && (
            <>
              <div className="form-group">
                <label>Webhook URL</label>
                <input value={deliveryWebhookUrl} onChange={(e) => setDeliveryWebhookUrl(e.target.value)} placeholder="https://example.com/hook" />
              </div>
              <div className="form-group">
                <label>Delivery Secret</label>
                <input type="password" value={deliverySecret} onChange={(e) => setDeliverySecret(e.target.value)} placeholder={isEdit ? 'Leave blank to keep existing secret' : 'Optional secret'} />
              </div>
            </>
          )}

          {deliveryMode === 'session' && (
            <div className="form-group">
              <label>Session ID</label>
              <input value={deliverySessionId} onChange={(e) => setDeliverySessionId(e.target.value)} placeholder="Existing chat session ID" />
            </div>
          )}

          {deliveryMode === 'channel' && (
            <div className="form-group">
              <label>Channel</label>
              <select value={deliveryChannelId} onChange={(e) => setDeliveryChannelId(e.target.value)}>
                <option value="">Select channel</option>
                {channels.map((channel) => (
                  <option key={channel.id} value={channel.id}>{channel.channel_id}</option>
                ))}
              </select>
            </div>
          )}

          <div className="form-group">
            <label style={{ display: 'flex', alignItems: 'center', gap: '0.5rem' }}>
              <input type="checkbox" checked={deliveryBestEffort} onChange={(e) => setDeliveryBestEffort(e.target.checked)} />
              Delivery best effort
            </label>
          </div>

          <div className="form-group">
            <label style={{ display: 'flex', alignItems: 'center', gap: '0.5rem' }}>
              <input type="checkbox" checked={enabled} onChange={(e) => setEnabled(e.target.checked)} />
              Enabled
            </label>
          </div>

          {error && <div className="error">{error}</div>}
          <div style={{ display: 'flex', gap: '0.5rem', marginTop: '1.5rem' }}>
            <button type="submit" className="btn" disabled={loading}>{loading ? 'Saving...' : 'Save'}</button>
            <Link to="/cron" className="btn btn-secondary">Cancel</Link>
          </div>
        </form>
      </div>
    </div>
  )
}
