import { useEffect, useMemo, useState } from 'react'
import { Link } from 'react-router-dom'
import { agentApi, cronApi, type Agent, type CronTask } from '../api/client'
import { ConfirmDialog } from '../components/ConfirmDialog'

export default function CronTaskList() {
  const [tasks, setTasks] = useState<CronTask[]>([])
  const [agents, setAgents] = useState<Agent[]>([])
  const [total, setTotal] = useState(0)
  const [enabled, setEnabled] = useState('')
  const [loading, setLoading] = useState(true)
  const [error, setError] = useState('')
  const [pendingDelete, setPendingDelete] = useState<{ id: string; name: string } | null>(null)
  const [pendingRun, setPendingRun] = useState<{ id: string; name: string } | null>(null)
  const [confirmLoading, setConfirmLoading] = useState(false)

  const agentNames = useMemo(() => new Map(agents.map((agent) => [agent.id, agent.name])), [agents])

  const loadTasks = () => {
    setLoading(true)
    setError('')
    Promise.all([
      cronApi.list({ page: 1, page_size: 50, enabled: enabled === '' ? undefined : enabled === 'true' }),
      agentApi.list({ page: 1, page_size: 100 }).catch(() => ({ items: [], total: 0 })),
    ])
      .then(([taskRes, agentRes]) => {
        setTasks(taskRes.items)
        setTotal(taskRes.total)
        setAgents(agentRes.items)
      })
      .catch((e) => setError(e.message))
      .finally(() => setLoading(false))
  }

  useEffect(() => {
    loadTasks()
  }, [])

  const confirmDelete = async () => {
    if (!pendingDelete) return
    setConfirmLoading(true)
    try {
      await cronApi.delete(pendingDelete.id)
      setTasks((prev) => prev.filter((task) => task.id !== pendingDelete.id))
      setTotal((prev) => Math.max(0, prev - 1))
      setPendingDelete(null)
    } catch (e) {
      alert((e as Error).message)
    } finally {
      setConfirmLoading(false)
    }
  }

  const confirmRun = async () => {
    if (!pendingRun) return
    setConfirmLoading(true)
    try {
      await cronApi.run(pendingRun.id)
      setPendingRun(null)
      alert('Task run requested.')
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
          <h1>Cron Tasks</h1>
          <div className="page-sub">{total} configured</div>
        </div>
        <Link to="/cron/new" className="btn">New Task</Link>
      </div>

      <div className="section-card" style={{ marginBottom: '1.25rem', padding: '1rem 1.25rem' }}>
        <div className="filter-bar">
          <span className="filter-label">Filter</span>
          <select value={enabled} onChange={(e) => setEnabled(e.target.value)}>
            <option value="">All states</option>
            <option value="true">Enabled</option>
            <option value="false">Disabled</option>
          </select>
          <button type="button" className="btn btn-secondary" onClick={loadTasks}>Apply</button>
        </div>
      </div>

      {total === 0 ? (
        <div className="section-card empty-state">
          <p>No cron tasks yet.</p>
          <Link to="/cron/new" className="btn">New Task</Link>
        </div>
      ) : (
        <div className="table-card">
          <table>
            <thead>
              <tr>
                <th>Task</th>
                <th>Agent</th>
                <th>Schedule</th>
                <th>Delivery</th>
                <th>Next Run</th>
                <th>Actions</th>
              </tr>
            </thead>
            <tbody>
              {tasks.map((task) => (
                <tr key={task.id}>
                  <td>
                    <Link to={`/cron/${task.id}`} className="link">{task.name}</Link>
                    <div className="page-sub">{task.enabled ? 'Enabled' : 'Disabled'}</div>
                  </td>
                  <td>{agentNames.get(task.agent_id) || task.agent_id}</td>
                  <td>
                    <span className="badge badge-api">{task.schedule_kind}</span>
                    <div><code>{task.schedule_expr}</code></div>
                  </td>
                  <td>{task.delivery_mode}</td>
                  <td>{task.next_run_at || '-'}</td>
                  <td>
                    <div className="actions">
                      <button className="btn btn-sm" onClick={() => setPendingRun({ id: task.id, name: task.name })}>Run</button>
                      <Link to={`/cron/${task.id}`} className="btn btn-secondary btn-sm">Detail</Link>
                      <Link to={`/cron/${task.id}/edit`} className="btn btn-secondary btn-sm">Edit</Link>
                      <button className="btn btn-danger btn-sm" onClick={() => setPendingDelete({ id: task.id, name: task.name })}>Delete</button>
                    </div>
                  </td>
                </tr>
              ))}
            </tbody>
          </table>
        </div>
      )}
      <ConfirmDialog
        open={!!pendingRun}
        title="Run task now"
        description={pendingRun ? `Run "${pendingRun.name}" now?` : ''}
        confirmLabel="Run"
        loading={confirmLoading}
        onCancel={() => setPendingRun(null)}
        onConfirm={confirmRun}
      />
      <ConfirmDialog
        open={!!pendingDelete}
        title="Delete cron task"
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
