import { useEffect, useState } from 'react'
import { Link, useParams } from 'react-router-dom'
import { agentApi, cronApi, type Agent, type CronRun, type CronTask } from '../api/client'
import { ConfirmDialog } from '../components/ConfirmDialog'

export default function CronTaskDetail() {
  const { id } = useParams()
  const [task, setTask] = useState<CronTask | null>(null)
  const [runs, setRuns] = useState<CronRun[]>([])
  const [agent, setAgent] = useState<Agent | null>(null)
  const [loading, setLoading] = useState(true)
  const [error, setError] = useState('')
  const [pendingRun, setPendingRun] = useState(false)
  const [confirmLoading, setConfirmLoading] = useState(false)

  const loadTask = () => {
    if (!id) return
    setLoading(true)
    setError('')
    cronApi.get(id)
      .then(async (taskRes) => {
        setTask(taskRes)
        const [runRes, agentRes] = await Promise.all([
          cronApi.listRuns(id, { page: 1, page_size: 20 }).catch(() => ({ items: [], total: 0 })),
          agentApi.get(taskRes.agent_id).catch(() => null),
        ])
        setRuns(runRes.items)
        setAgent(agentRes)
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
          <h1>{task.name}</h1>
          <div className="page-sub">{task.enabled ? 'Enabled' : 'Disabled'}</div>
        </div>
        <div className="actions">
          <Link to="/cron" className="btn btn-secondary btn-sm">Back</Link>
          <button className="btn btn-sm" onClick={() => setPendingRun(true)}>Run Now</button>
          <Link to={`/cron/${task.id}/edit`} className="btn btn-secondary btn-sm">Edit</Link>
        </div>
      </div>

      <section className="section">
        <h2 className="section-title">Configuration</h2>
        <div className="section-card">
          <div style={{ display: 'grid', gap: '0.75rem' }}>
            <p><strong>Agent: </strong>{agent?.name || task.agent_id}</p>
            <p><strong>Schedule: </strong><span className="badge badge-api">{task.schedule_kind}</span> <code>{task.schedule_expr}</code></p>
            <p><strong>Timezone: </strong>{task.timezone || '-'}</p>
            <p><strong>Payload: </strong>{task.payload_kind}</p>
            <p><strong>Delivery: </strong>{task.delivery_mode}</p>
            <p><strong>Next Run: </strong>{task.next_run_at || '-'}</p>
          </div>
        </div>
      </section>

      <section className="section">
        <h2 className="section-title">Payload</h2>
        <div className="section-card">
          <pre style={{ whiteSpace: 'pre-wrap', margin: 0, fontFamily: 'var(--mono)', fontSize: 13 }}>{task.payload_content || '-'}</pre>
        </div>
      </section>

      <section className="section">
        <h2 className="section-title">Recent Runs</h2>
        {runs.length === 0 ? (
          <div className="section-card empty-state">
            <p>No runs yet.</p>
          </div>
        ) : (
          <div className="table-card">
            <table>
              <thead>
                <tr>
                  <th>Triggered</th>
                  <th>Status</th>
                  <th>Delivery</th>
                  <th>Finished</th>
                  <th>Summary</th>
                </tr>
              </thead>
              <tbody>
                {runs.map((run) => (
                  <tr key={run.id}>
                    <td>{run.triggered_at}</td>
                    <td>{run.status}</td>
                    <td>{run.delivery_ok === undefined ? '-' : run.delivery_ok ? 'OK' : 'Failed'}</td>
                    <td>{run.finished_at || '-'}</td>
                    <td>{run.output_summary || run.error || '-'}</td>
                  </tr>
                ))}
              </tbody>
            </table>
          </div>
        )}
      </section>

      <ConfirmDialog
        open={pendingRun}
        title="Run task now"
        description={`Run "${task.name}" now?`}
        confirmLabel="Run"
        loading={confirmLoading}
        onCancel={() => setPendingRun(false)}
        onConfirm={confirmRun}
      />
    </div>
  )
}
