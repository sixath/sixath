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
          <div className="page-title-row">
            <h1>定时任务</h1>
            <span className="page-count">{total}</span>
          </div>
          <p className="page-sub">按计划触发 Agent，并把结果投递到指定通道。</p>
        </div>
        <Link to="/cron/new" className="btn">新建任务</Link>
      </div>

      {total === 0 ? (
        <div className="section-card empty-state">
          <p>还没有定时任务。</p>
          <Link to="/cron/new" className="btn">新建任务</Link>
        </div>
      ) : (
        <div className="table-card">
          <div className="table-toolbar">
            <div className="filter-bar">
              <span className="filter-label">筛选</span>
              <select value={enabled} onChange={(e) => setEnabled(e.target.value)}>
                <option value="">全部状态</option>
                <option value="true">已启用</option>
                <option value="false">已停用</option>
              </select>
              <button type="button" className="btn btn-secondary btn-sm" onClick={loadTasks}>应用</button>
            </div>
          </div>
          <table>
            <thead>
              <tr>
                <th>任务</th>
                <th>Agent</th>
                <th>计划</th>
                <th>投递</th>
                <th>下次运行</th>
                <th className="col-actions">操作</th>
              </tr>
            </thead>
            <tbody>
              {tasks.map((task) => (
                <tr key={task.id}>
                  <td>
                    <span className="cell-name">
                      <span className="cell-dot cell-dot--amber" aria-hidden />
                      <Link to={`/cron/${task.id}`} className="link">{task.name}</Link>
                    </span>
                    <div className="page-sub">{task.enabled ? '已启用' : '已停用'}</div>
                  </td>
                  <td>{agentNames.get(task.agent_id) || task.agent_id}</td>
                  <td>
                    <span className="badge badge-api">{task.schedule_kind}</span>
                    <div><code>{task.schedule_expr}</code></div>
                  </td>
                  <td>{task.delivery_mode}</td>
                  <td>{task.next_run_at || '-'}</td>
                  <td className="col-actions">
                    <div className="row-actions">
                      <button type="button" className="btn btn-ghost btn-sm" onClick={() => setPendingRun({ id: task.id, name: task.name })}>运行</button>
                      <Link to={`/cron/${task.id}`} className="btn btn-ghost btn-sm">详情</Link>
                      <Link to={`/cron/${task.id}/edit`} className="btn btn-ghost btn-sm">编辑</Link>
                      <button type="button" className="btn btn-ghost btn-sm btn-danger" onClick={() => setPendingDelete({ id: task.id, name: task.name })}>删除</button>
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
        title="立即运行"
        description={pendingRun ? `现在运行「${pendingRun.name}」？` : ''}
        confirmLabel="运行"
        loading={confirmLoading}
        onCancel={() => setPendingRun(null)}
        onConfirm={confirmRun}
      />
      <ConfirmDialog
        open={!!pendingDelete}
        title="删除定时任务"
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
