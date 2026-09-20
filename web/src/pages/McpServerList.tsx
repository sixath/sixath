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
        <div>
          <div className="page-title-row">
            <h1>MCP 服务</h1>
            <span className="page-count">{total}</span>
          </div>
          <p className="page-sub">管理已接入的 MCP 服务与传输方式。</p>
        </div>
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
                <th className="col-actions">操作</th>
              </tr>
            </thead>
            <tbody>
              {servers.map((server) => (
                <tr key={server.id}>
                  <td>
                    <code>{server.id}</code>
                  </td>
                  <td>
                    <span className="cell-name">
                      <span className="cell-dot cell-dot--purple" aria-hidden />
                      {server.name}
                    </span>
                  </td>
                  <td>
                    <span className={`badge badge-${server.transport === 'stdio' ? 'mcp' : 'builtin'}`}>
                      {server.transport}
                    </span>
                  </td>
                  <td className="cell-desc">{server.description}</td>
                  <td className="col-actions">
                    <div className="row-actions">
                      <Link to={`/mcp-servers/${server.id}/edit`} className="btn btn-ghost btn-sm">
                        编辑
                      </Link>
                      <button
                        type="button"
                        className="btn btn-ghost btn-sm btn-danger"
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
