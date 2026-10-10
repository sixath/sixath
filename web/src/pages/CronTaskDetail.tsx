import { useEffect, useState } from 'react'
import { Link, useParams } from 'react-router-dom'
import { agentApi, cronApi, type Agent, type CronRun, type CronTask } from '../api/client'
import { getResourceByPayload, type ResourceInfo } from '../api/resource'
import ResourceGrantPanel from '../components/ResourceGrantPanel'
import { ConfirmDialog } from '../components/ConfirmDialog'

const STATUS_LABELS: Record<string, string> = {
  success: '成功',
  failed: '失败',
  running: '运行中',
  missed: '错过',
  interrupted: '中断',
}

const TRIGGER_LABELS: Record<string, string> = {
  schedule: '定时',
  manual: '手动',
}

function formatTime(iso?: string): string {
  if (!iso) return '-'
  const d = new Date(iso)
  return Number.isNaN(d.getTime()) ? iso : d.toLocaleString('zh-CN', { hour12: false })
}

/** How long after its planned time a scheduled run started, when over a minute. */
function lateBy(run: CronRun): string {
  if (run.trigger !== 'schedule' || run.status === 'missed') return ''
  const ms = new Date(run.started_at).getTime() - new Date(run.triggered_at).getTime()
  if (!(ms > 60_000)) return ''
  const min = Math.round(ms / 60_000)
  return min < 60 ? `${min} 分钟` : `${Math.floor(min / 60)} 小时 ${min % 60} 分钟`
}

export default function CronTaskDetail() {
  const { id } = useParams()
  const [task, setTask] = useState<CronTask | null>(null)
  const [runs, setRuns] = useState<CronRun[]>([])
  const [agent, setAgent] = useState<Agent | null>(null)
  const [loading, setLoading] = useState(true)
  const [error, setError] = useState('')
  const [pendingRun, setPendingRun] = useState(false)
  const [confirmLoading, setConfirmLoading] = useState(false)
  const [resourceInfo, setResourceInfo] = useState<ResourceInfo | null>(null)

  const loadTask = () => {
    if (!id) return
    setLoading(true)
    setError('')
    cronApi.get(id)
      .then(async (taskRes) => {
        setTask(taskRes)
        const [runRes, agentRes] = await Promise.all([
          cronApi.listRuns(id, { page: 1, page_size: 50 }).catch(() => ({ items: [], total: 0 })),
          agentApi.get(taskRes.agent_id).catch(() => null),
        ])
        setRuns(runRes.items)
        setAgent(agentRes)
        getResourceByPayload('cron', id!).then(setResourceInfo).catch(() => setResourceInfo(null))
      })
      .catch((e) => setError(e.message))
      .finally(() => setLoading(false))
  }

  useEffect(() => {
    loadTask()
  }, [id])

  const confirmRun = async () => {
    if (!id) return
    setConfirmLoading(true)
    try {
      await cronApi.run(id)
      setPendingRun(false)
      alert('Task run requested.')
      loadTask()
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
  if (error || !task) return <div className="error">{error || 'Task not found.'}</div>

  return (
    <div>
      <div className="page-header">
        <div>
          <div className="page-title-row">
            <h1>{task.name}</h1>
            <span className="page-count">{task.enabled ? '已启用' : '已停用'}</span>
          </div>
          <p className="page-sub">查看计划、投递方式与最近运行记录。</p>
        </div>
        <div className="actions">
          <Link to="/cron" className="btn btn-secondary">返回</Link>
          <button type="button" className="btn" onClick={() => setPendingRun(true)}>立即运行</button>
          <Link to={`/cron/${task.id}/edit`} className="btn btn-secondary">编辑</Link>
        </div>
      </div>

      <section className="section">
        <h2 className="section-title">配置</h2>
        <div className="section-card">
          <div style={{ display: 'grid', gap: '0.75rem' }}>
            <p><strong>Agent：</strong>{agent?.name || task.agent_id}</p>
            <p><strong>计划：</strong><span className="badge badge-api">{task.schedule_kind}</span> <code>{task.schedule_expr}</code></p>
            {resourceInfo ? (
              <p><strong>可见性：</strong>
                <span className={`badge badge-${resourceInfo.visibility === 'org' ? 'mcp' : 'builtin'}`}>
                  {resourceInfo.visibility === 'org' ? 'Org 共享' : 'Private'}
                </span>
                {resourceInfo.home_org_id ? <code style={{ marginLeft: '0.5rem' }}>{resourceInfo.home_org_id}</code> : null}
              </p>
            ) : null}
            <p><strong>时区：</strong>{task.timezone || '-'}</p>
            <p><strong>载荷：</strong>{task.payload_kind}</p>
            <p><strong>投递：</strong>{task.delivery_mode}</p>
            <p><strong>下次运行：</strong>
              {task.next_run_at
                ? formatTime(task.next_run_at)
                : task.enabled ? '无（不会再自动运行，原因见下方最近运行记录）' : '-'}
            </p>
          </div>
        </div>
      </section>

      <section className="section">
        <h2 className="section-title">载荷内容</h2>
        <div className="section-card">
          <pre style={{ whiteSpace: 'pre-wrap', margin: 0, fontFamily: 'var(--mono)', fontSize: 13 }}>{task.payload_content || '-'}</pre>
        </div>
      </section>

      <section className="section">
        <h2 className="section-title">最近运行</h2>
        {runs.length === 0 ? (
          <div className="section-card empty-state">
            <p>还没有运行记录。</p>
          </div>
        ) : (
          <div className="table-card">
            <table>
              <thead>
                <tr>
                  <th>计划时间</th>
                  <th>触发</th>
                  <th>状态</th>
                  <th>开始 / 结束</th>
                  <th>投递</th>
                  <th>结果 / 错误</th>
                </tr>
              </thead>
              <tbody>
                {runs.map((run) => {
                  const delay = lateBy(run)
                  return (
                    <tr key={run.id}>
                      <td>
                        {formatTime(run.triggered_at)}
                        {delay ? <div className="cron-run-note">晚了 {delay} 才开始</div> : null}
                      </td>
                      <td>{TRIGGER_LABELS[run.trigger] ?? '-'}</td>
                      <td>
                        <span className={`badge badge-run-${run.status}`}>{STATUS_LABELS[run.status] ?? run.status}</span>
                      </td>
                      <td>
                        {run.status === 'missed' ? '-' : formatTime(run.started_at)}
                        <div className="cron-run-note">{run.finished_at ? formatTime(run.finished_at) : run.status === 'running' ? '进行中' : '-'}</div>
                      </td>
                      <td>
                        {run.delivery_ok === undefined ? '-' : run.delivery_ok ? '成功' : '失败'}
                        {run.delivery_error ? <div className="cron-run-error cron-run-detail">{run.delivery_error}</div> : null}
                      </td>
                      <td className="cron-run-detail">
                        {run.error ? <div className="cron-run-error">{run.error}</div> : null}
                        {run.output_summary && run.output_summary !== run.error ? <div>{run.output_summary}</div> : null}
                        {!run.error && !run.output_summary ? '-' : null}
                      </td>
                    </tr>
                  )
                })}
              </tbody>
            </table>
          </div>
        )}
      </section>

      {id ? <ResourceGrantPanel resourceType="cron" payloadRef={id} /> : null}

      <ConfirmDialog
        open={pendingRun}
        title="立即运行"
        description={`现在运行「${task.name}」？`}
        confirmLabel="运行"
        loading={confirmLoading}
        onCancel={() => setPendingRun(false)}
        onConfirm={confirmRun}
      />
    </div>
  )
}
