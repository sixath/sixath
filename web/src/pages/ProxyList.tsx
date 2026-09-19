import { useEffect, useState } from 'react'
import { Link } from 'react-router-dom'
import { isProxyInUseError, proxyApi, type Proxy } from '../api/client'
import { ConfirmDialog } from '../components/ConfirmDialog'

export default function ProxyList() {
  const [proxies, setProxies] = useState<Proxy[]>([])
  const [total, setTotal] = useState(0)
  const [loading, setLoading] = useState(true)
  const [error, setError] = useState('')
  const [pendingDelete, setPendingDelete] = useState<{ id: string; name: string } | null>(null)
  const [confirmLoading, setConfirmLoading] = useState(false)

  useEffect(() => {
    proxyApi
      .list({ page: 1, page_size: 50 })
      .then((res) => {
        setProxies(res.items)
        setTotal(res.total)
      })
      .catch((e) => setError(e.message))
      .finally(() => setLoading(false))
  }, [])

  const confirmDelete = async () => {
    if (!pendingDelete) return
    setConfirmLoading(true)
    try {
      await proxyApi.remove(pendingDelete.id)
      setProxies((prev) => prev.filter((p) => p.id !== pendingDelete.id))
      setTotal((prev) => Math.max(0, prev - 1))
      setPendingDelete(null)
      setError('')
    } catch (e) {
      if (isProxyInUseError(e)) {
        setError(e.message)
        setPendingDelete(null)
      } else {
        alert((e as Error).message)
      }
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
  if (error && proxies.length === 0 && total === 0 && !pendingDelete) {
    return <div className="error">Load failed: {error}</div>
  }

  return (
    <div>
      <div className="page-header">
        <h1>代理</h1>
        <Link to="/proxies/new" className="btn">
          新建代理
        </Link>
      </div>
      {error ? (
        <div className="error" style={{ whiteSpace: 'pre-wrap', marginBottom: '1rem' }}>
          {error}
        </div>
      ) : null}
      {total === 0 ? (
        <div className="section-card empty-state">
          <p>暂无代理。</p>
          <Link to="/proxies/new" className="btn">
            新建代理
          </Link>
        </div>
      ) : (
        <div className="table-card">
          <table>
            <thead>
              <tr>
                <th>ID</th>
                <th>名称</th>
                <th>类型</th>
                <th>地址</th>
                <th>操作</th>
              </tr>
            </thead>
            <tbody>
              {proxies.map((proxy) => (
                <tr key={proxy.id}>
                  <td>
                    <code>{proxy.id}</code>
                  </td>
                  <td>
                    <strong>{proxy.name}</strong>
                  </td>
                  <td>
                    <span className={`badge badge-${proxy.type === 'socks5' ? 'mcp' : 'builtin'}`}>
                      {proxy.type}
                    </span>
                  </td>
                  <td>
                    <code>
                      {proxy.host}:{proxy.port}
                    </code>
                  </td>
                  <td>
                    <div className="actions">
                      <Link to={`/proxies/${proxy.id}/edit`} className="btn btn-secondary btn-sm">
                        编辑
                      </Link>
                      <button
                        className="btn btn-danger btn-sm"
                        onClick={() => {
                          setError('')
                          setPendingDelete({ id: proxy.id, name: proxy.name })
                        }}
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
        title="删除代理"
        description={
          pendingDelete
            ? `删除「${pendingDelete.name}」？仍被 Agent 或工具引用时无法删除。`
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
