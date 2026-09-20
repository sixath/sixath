import { useEffect, useState } from 'react'
import { Link } from 'react-router-dom'
import { modelCatalogApi, type ModelProvider } from '../api/client'
import { ConfirmDialog } from '../components/ConfirmDialog'

export default function ModelProviderList() {
  const [providers, setProviders] = useState<ModelProvider[]>([])
  const [loading, setLoading] = useState(true)
  const [error, setError] = useState('')
  const [pendingDelete, setPendingDelete] = useState<{ id: string; name: string } | null>(null)
  const [confirmLoading, setConfirmLoading] = useState(false)

  useEffect(() => {
    modelCatalogApi
      .listProviders()
      .then(setProviders)
      .catch((e) => setError(e.message))
      .finally(() => setLoading(false))
  }, [])

  const confirmDelete = async () => {
    if (!pendingDelete) return
    setConfirmLoading(true)
    try {
      await modelCatalogApi.removeProvider(pendingDelete.id)
      setProviders((prev) => prev.filter((p) => p.id !== pendingDelete.id))
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
        <span style={{ marginLeft: '0.75rem' }}>加载中…</span>
      </div>
    )
  }
  if (error) return <div className="error">加载失败：{error}</div>

  return (
    <div>
      <div className="page-header">
        <div>
          <div className="page-title-row">
            <h1>模型供应商</h1>
            <span className="page-count">{providers.length}</span>
          </div>
          <p className="page-sub">登记中转站或直连厂商，供 Agent 选择模型。</p>
        </div>
        <Link to="/model-providers/new" className="btn">
          新建供应商
        </Link>
      </div>
      {providers.length === 0 ? (
        <div className="section-card empty-state">
          <p>暂无模型供应商。同步或手工添加模型前，请先登记中转站或直连厂商。</p>
          <Link to="/model-providers/new" className="btn">
            新建供应商
          </Link>
        </div>
      ) : (
        <div className="table-card">
          <table>
            <thead>
              <tr>
                <th>名称</th>
                <th>类型</th>
                <th>Base URL</th>
                <th>密钥</th>
                <th>启用</th>
                <th className="col-actions">操作</th>
              </tr>
            </thead>
            <tbody>
              {providers.map((p) => (
                <tr key={p.id}>
                  <td>
                    <span className="cell-name">
                      <span className="cell-dot cell-dot--purple" aria-hidden />
                      {p.name}
                    </span>
                  </td>
                  <td>
                    {p.kind === 'dashscope' ? 'DashScope' : 'OpenAI 兼容'}
                  </td>
                  <td className="cell-desc">
                    {p.base_url || '—'}
                  </td>
                  <td>{p.has_api_key ? '已配置' : '无'}</td>
                  <td>{p.enabled ? '是' : '否'}</td>
                  <td className="col-actions">
                    <div className="row-actions">
                      <Link to={`/model-providers/${p.id}`} className="btn btn-ghost btn-sm">
                        编辑
                      </Link>
                      <button
                        type="button"
                        className="btn btn-ghost btn-sm btn-danger"
                        onClick={() => setPendingDelete({ id: p.id, name: p.name })}
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
        title="删除模型供应商"
        description={
          pendingDelete
            ? `删除「${pendingDelete.name}」会级联删除其目录，已选该供应商的会话不会清空覆盖。`
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
