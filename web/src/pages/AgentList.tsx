import { useEffect, useState } from 'react'
import { Link } from 'react-router-dom'
import { agentApi, type Agent } from '../api/client'
import { ConfirmDialog } from '../components/ConfirmDialog'

export default function AgentList() {
  const [agents, setAgents] = useState<Agent[]>([])
  const [total, setTotal] = useState(0)
  const [loading, setLoading] = useState(true)
  const [error, setError] = useState('')
  const [pendingDelete, setPendingDelete] = useState<{ id: string; name: string } | null>(null)
  const [confirmLoading, setConfirmLoading] = useState(false)

  useEffect(() => {
    agentApi.list({ page: 1, page_size: 50 })
      .then((res) => {
        setAgents(res.items)
        setTotal(res.total)
      })
      .catch((e) => setError(e.message))
      .finally(() => setLoading(false))
  }, [])

  const confirmDelete = async () => {
    if (!pendingDelete) return
    setConfirmLoading(true)
    try {
      await agentApi.delete(pendingDelete.id)
      setAgents((prev) => prev.filter((agent) => agent.id !== pendingDelete.id))
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
        <h1>Agents</h1>
        <Link to="/agents/new" className="btn">New Agent</Link>
      </div>
      {total === 0 ? (
        <div className="section-card empty-state">
          <p>No agents yet.</p>
          <Link to="/agents/new" className="btn">New Agent</Link>
        </div>
      ) : (
        <div className="table-card">
          <table>
            <thead>
              <tr>
                <th>Name</th>
                <th>Model</th>
                <th>Workspace</th>
                <th>Actions</th>
              </tr>
            </thead>
            <tbody>
              {agents.map((agent) => (
                <tr key={agent.id}>
                  <td>
                    <Link to={`/agents/${agent.id}`} className="link" style={{ fontWeight: 600 }}>{agent.name}</Link>
                  </td>
                  <td style={{ color: 'var(--muted)' }}>
                    {agent.model_config?.provider}/{agent.model_config?.model}
                  </td>
                  <td><code style={{ fontSize: '0.8rem' }}>{agent.workspace}</code></td>
                  <td>
                    <div className="actions">
                      <Link to={`/agents/${agent.id}/chat`} className="btn btn-sm">Chat</Link>
                      <Link to={`/agents/${agent.id}`} className="btn btn-secondary btn-sm">Detail</Link>
                      <Link to={`/agents/${agent.id}/edit`} className="btn btn-secondary btn-sm">Edit</Link>
                      <button className="btn btn-danger btn-sm" onClick={() => setPendingDelete({ id: agent.id, name: agent.name })}>Delete</button>
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
        title="Delete agent"
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
