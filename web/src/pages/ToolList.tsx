import { useEffect, useState } from 'react'
import { Link } from 'react-router-dom'
import { toolApi, type Tool } from '../api/client'
import { ConfirmDialog } from '../components/ConfirmDialog'

export default function ToolList() {
  const [tools, setTools] = useState<Tool[]>([])
  const [total, setTotal] = useState(0)
  const [loading, setLoading] = useState(true)
  const [error, setError] = useState('')
  const [pendingDelete, setPendingDelete] = useState<{ id: string; name: string } | null>(null)
  const [confirmLoading, setConfirmLoading] = useState(false)

  useEffect(() => {
    toolApi.list({ page: 1, page_size: 50 })
      .then((res) => {
        setTools(res.items)
        setTotal(res.total)
      })
      .catch((e) => setError(e.message))
      .finally(() => setLoading(false))
  }, [])

  const confirmDelete = async () => {
    if (!pendingDelete) return
    setConfirmLoading(true)
    try {
      await toolApi.delete(pendingDelete.id)
      setTools((prev) => prev.filter((t) => t.id !== pendingDelete.id))
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
        <h1>Tools</h1>
        <Link to="/tools/new" className="btn">New Tool</Link>
      </div>
      {total === 0 ? (
        <div className="section-card empty-state">
          <p>No tools yet.</p>
          <Link to="/tools/new" className="btn">New Tool</Link>
        </div>
      ) : (
        <div className="table-card">
          <table>
            <thead>
              <tr>
                <th>Name</th>
                <th>Type</th>
                <th>Description</th>
                <th>Actions</th>
              </tr>
            </thead>
            <tbody>
              {tools.map((tool) => (
                <tr key={tool.id}>
                  <td><strong>{tool.name}</strong></td>
                  <td><span className={`badge badge-${tool.type}`}>{tool.type}</span></td>
                  <td style={{ color: 'var(--muted)', maxWidth: 320 }}>{tool.description}</td>
                  <td>
                    <div className="actions">
                      <Link to={`/tools/${tool.id}/edit`} className="btn btn-secondary btn-sm">Edit</Link>
                      <button className="btn btn-danger btn-sm" onClick={() => setPendingDelete({ id: tool.id, name: tool.name })}>Delete</button>
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
        title="Delete tool"
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
