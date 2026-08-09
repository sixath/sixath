import { useEffect, useState } from 'react'
import { Link } from 'react-router-dom'
import { mcpServerApi, type McpServer } from '../api/client'
import { ConfirmDialog } from '../components/ConfirmDialog'

export default function McpServerList() {
  const [servers, setServers] = useState<McpServer[]>([])
  const [total, setTotal] = useState(0)
  const [loading, setLoading] = useState(true)
  const [error, setError] = useState('')
  const [pendingDelete, setPendingDelete] = useState<{ id: string; name: string } | null>(null)
  const [confirmLoading, setConfirmLoading] = useState(false)

  useEffect(() => {
    mcpServerApi
      .list({ page: 1, page_size: 50 })
      .then((res) => {
        setServers(res.items)
        setTotal(res.total)
      })
      .catch((e) => setError(e.message))
      .finally(() => setLoading(false))
  }, [])

  const confirmDelete = async () => {
    if (!pendingDelete) return
    setConfirmLoading(true)
    try {
      await mcpServerApi.remove(pendingDelete.id)
      setServers((prev) => prev.filter((s) => s.id !== pendingDelete.id))
      setTotal((prev) => Math.max(0, prev - 1))
      setPendingDelete(null)
    } catch (e) {
      alert((e as Error).message)
    } finally {
      setConfirmLoading(false)
    }
  }

  if (loading) {
    return (
      <div className="loading">
        <div className="loading-spinner" />
        <span style={{ marginLeft: '0.75rem' }}>Loading...</span>
      </div>
    )
  }
  if (error) return <div className="error">Load failed: {error}</div>

  return (
    <div>
      <div className="page-header">
        <h1>MCP 服务</h1>
        <Link to="/mcp-servers/new" className="btn">
          新建 MCP 服务
        </Link>
      </div>
      {total === 0 ? (
        <div className="section-card empty-state">
          <p>暂无 MCP 服务。</p>
          <Link to="/mcp-servers/new" className="btn">
            新建 MCP 服务
          </Link>
        </div>
      ) : (
        <div className="table-card">
          <table>
            <thead>
              <tr>
                <th>ID</th>
                <th>名称</th>
                <th>Transport</th>
                <th>描述</th>
                <th>操作</th>
              </tr>
            </thead>
            <tbody>
              {servers.map((server) => (
                <tr key={server.id}>
                  <td>
                    <code>{server.id}</code>
                  </td>
                  <td>
                    <strong>{server.name}</strong>
                  </td>
                  <td>
                    <span className={`badge badge-${server.transport === 'stdio' ? 'mcp' : 'builtin'}`}>
                      {server.transport}
                    </span>
                  </td>
                  <td style={{ color: 'var(--muted)', maxWidth: 320 }}>{server.description}</td>
                  <td>
                    <div className="actions">
                      <Link to={`/mcp-servers/${server.id}/edit`} className="btn btn-secondary btn-sm">
                        编辑
                      </Link>
                      <button
                        className="btn btn-danger btn-sm"
                        onClick={() => setPendingDelete({ id: server.id, name: server.name })}
                      >
                        删除
                      </button>
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
        title="删除 MCP 服务"
        description={
          pendingDelete
            ? `删除「${pendingDelete.name}」？绑定关系会一并解除，且不可恢复。`
            : ''
        }
        confirmLabel="删除"
        variant="danger"
        loading={confirmLoading}
        onCancel={() => setPendingDelete(null)}
        onConfirm={confirmDelete}
      />
    </div>
  )
}
