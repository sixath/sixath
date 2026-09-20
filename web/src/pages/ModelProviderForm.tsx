import { useEffect, useState } from 'react'
import { Link, useNavigate, useParams } from 'react-router-dom'
import {
  modelCatalogApi,
  type ModelCatalogEntry,
  type ModelProviderKind,
} from '../api/client'

export default function ModelProviderForm() {
  const { id: routeId } = useParams()
  const navigate = useNavigate()
  const isEdit = !!routeId

  const [name, setName] = useState('')
  const [kind, setKind] = useState<ModelProviderKind>('openai_compat')
  const [baseUrl, setBaseUrl] = useState('')
  const [apiKey, setApiKey] = useState('')
  const [enabled, setEnabled] = useState(true)
  const [hasApiKey, setHasApiKey] = useState(false)
  const [error, setError] = useState('')
  const [loading, setLoading] = useState(false)
  const [syncing, setSyncing] = useState(false)
  const [entries, setEntries] = useState<ModelCatalogEntry[]>([])
  const [newModel, setNewModel] = useState('')
  const [newDisplay, setNewDisplay] = useState('')
  const [adding, setAdding] = useState(false)

  const loadEntries = (pid: string) => {
    modelCatalogApi
      .listCatalog(pid)
      .then(setEntries)
      .catch((e) => setError(e.message))
  }

  useEffect(() => {
    if (!isEdit || !routeId) return
    modelCatalogApi
      .getProvider(routeId)
      .then((p) => {
        setName(p.name)
        setKind(p.kind)
        setBaseUrl(p.base_url || '')
        setEnabled(p.enabled)
        setHasApiKey(p.has_api_key)
      })
      .catch((e) => setError(e.message))
    loadEntries(routeId)
  }, [isEdit, routeId])

  const handleSubmit = async (e: React.FormEvent) => {
    e.preventDefault()
    setError('')
    if (!name.trim()) {
      setError('请填写名称')
      return
    }
    if (kind === 'openai_compat' && !baseUrl.trim()) {
      setError('OpenAI 兼容供应商需要填写 Base URL')
      return
    }
    setLoading(true)
    try {
      if (isEdit && routeId) {
        const body: {
          name: string
          kind: ModelProviderKind
          base_url: string
          enabled: boolean
          api_key?: string
        } = { name: name.trim(), kind, base_url: baseUrl.trim(), enabled }
        if (apiKey.trim()) {
          body.api_key = apiKey.trim()
        }
        await modelCatalogApi.updateProvider(routeId, body)
        setHasApiKey((prev) => prev || !!apiKey.trim())
        setApiKey('')
      } else {
        if (!apiKey.trim()) {
          setError('新建供应商需要填写 API Key')
          setLoading(false)
          return
        }
        const created = await modelCatalogApi.createProvider({
          name: name.trim(),
          kind,
          base_url: baseUrl.trim(),
          api_key: apiKey.trim(),
          enabled,
        })
        navigate(`/model-providers/${created.id}`, { replace: true })
      }
    } catch (err) {
      setError((err as Error).message)
    } finally {
      setLoading(false)
    }
  }

  const handleSync = async () => {
    if (!routeId) return
    setSyncing(true)
    setError('')
    try {
      await modelCatalogApi.syncProvider(routeId)
      loadEntries(routeId)
    } catch (err) {
      setError((err as Error).message)
    } finally {
      setSyncing(false)
    }
  }

  const handleAddModel = async () => {
    if (!routeId || !newModel.trim()) return
    setError('')
    setAdding(true)
    try {
      await modelCatalogApi.createCatalog({
        provider_id: routeId,
        model: newModel.trim(),
        display_name: newDisplay.trim() || undefined,
      })
      setNewModel('')
      setNewDisplay('')
      loadEntries(routeId)
    } catch (err) {
      setError((err as Error).message)
    } finally {
      setAdding(false)
    }
  }

  return (
    <div>
      <div className="page-header">
        <div>
          <h1>{isEdit ? '编辑模型供应商' : '新建模型供应商'}</h1>
          <p className="page-sub">登记中转站或直连厂商，并同步可选模型。</p>
        </div>
        <Link to="/model-providers" className="btn btn-secondary">
          返回列表
        </Link>
      </div>

      <div className="section-card" style={{ maxWidth: 560 }}>
        <form onSubmit={handleSubmit}>
          <div className="form-group">
            <label htmlFor="provider-name">名称 *</label>
            <input
              id="provider-name"
              value={name}
              onChange={(e) => setName(e.target.value)}
              placeholder="如 OpenAI 中转、通义千问"
              required
            />
          </div>

          <div className="form-group">
            <label htmlFor="provider-kind">类型 *</label>
            <select
              id="provider-kind"
              value={kind}
              onChange={(e) => setKind(e.target.value as ModelProviderKind)}
            >
              <option value="openai_compat">OpenAI 兼容（官方 / 中转站）</option>
              <option value="dashscope">阿里云 DashScope</option>
            </select>
            <small>
              {kind === 'openai_compat'
                ? '适用于官方 OpenAI，以及 One API / New API 等兼容 /v1 的中转站。'
                : '走现有 DashScope 客户端；不支持从上游同步目录，请手工添加模型。'}
            </small>
          </div>

          <div className="form-group">
            <label htmlFor="provider-base-url">
              Base URL{kind === 'openai_compat' ? ' *' : ''}
            </label>
            <input
              id="provider-base-url"
              value={baseUrl}
              onChange={(e) => setBaseUrl(e.target.value)}
              placeholder={
                kind === 'openai_compat'
                  ? 'https://api.openai.com/v1'
                  : '可留空'
              }
              required={kind === 'openai_compat'}
            />
            <small>
              {kind === 'openai_compat'
                ? '需含 /v1。同步时会请求 {Base URL}/models。'
                : 'DashScope 一般无需填写。'}
            </small>
          </div>

          <div className="form-group">
            <label htmlFor="provider-api-key">
              API Key{isEdit ? '' : ' *'}
            </label>
            <input
              id="provider-api-key"
              type="password"
              value={apiKey}
              onChange={(e) => setApiKey(e.target.value)}
              autoComplete="new-password"
              placeholder={
                isEdit
                  ? hasApiKey
                    ? '已配置，留空则不修改'
                    : '尚未配置，填写后保存'
                  : '必填'
              }
            />
            {isEdit ? (
              <small>{hasApiKey ? '密钥不会回显。留空保存表示保持原密钥。' : '当前没有密钥，该供应商不会出现在聊天下拉中。'}</small>
            ) : (
              <small>密钥只用于发消息，列表里不会再次显示明文。</small>
            )}
          </div>

          <div className="form-group">
            <label className="checkbox-field" htmlFor="provider-enabled">
              <input
                id="provider-enabled"
                type="checkbox"
                checked={enabled}
                onChange={(e) => setEnabled(e.target.checked)}
              />
              <span>启用（关闭后聊天页不可选该供应商下的模型）</span>
            </label>
          </div>

          {error ? <div className="error">{error}</div> : null}

          <div className="form-actions">
            <button className="btn" type="submit" disabled={loading}>
              {loading ? '保存中…' : '保存'}
            </button>
            <Link to="/model-providers" className="btn btn-secondary">
              取消
            </Link>
          </div>
        </form>
      </div>

      {isEdit && routeId ? (
        <div className="section-card" style={{ marginTop: '1.5rem' }}>
          <div className="page-header">
            <h2>模型目录</h2>
            <button
              className="btn btn-secondary"
              type="button"
              onClick={handleSync}
              disabled={syncing || kind !== 'openai_compat'}
              title={kind !== 'openai_compat' ? 'DashScope 不支持同步，请手工添加' : undefined}
            >
              {syncing ? '同步中…' : '从上游同步'}
            </button>
          </div>
          {entries.length === 0 ? (
            <p className="empty-state">同步或手工添加模型后，才会出现在聊天下拉中。</p>
          ) : (
            <div className="table-card">
              <table>
                <thead>
                  <tr>
                    <th>模型 id</th>
                    <th>展示名</th>
                    <th>隐藏</th>
                    <th>来源</th>
                    <th>操作</th>
                  </tr>
                </thead>
                <tbody>
                  {entries.map((entry) => (
                    <tr key={entry.id}>
                      <td>
                        <code>{entry.model}</code>
                      </td>
                      <td>
                        <input
                          defaultValue={entry.display_name}
                          aria-label={`${entry.model} 展示名`}
                          onBlur={(ev) => {
                            const v = ev.target.value.trim()
                            if (v && v !== entry.display_name) {
                              modelCatalogApi
                                .patchCatalog(entry.id, { display_name: v })
                                .then(() => loadEntries(routeId))
                                .catch((err) => setError(err.message))
                            }
                          }}
                        />
                      </td>
                      <td>
                        <input
                          type="checkbox"
                          checked={entry.hidden}
                          aria-label={`隐藏 ${entry.model}`}
                          onChange={(ev) => {
                            modelCatalogApi
                              .patchCatalog(entry.id, { hidden: ev.target.checked })
                              .then(() => loadEntries(routeId))
                              .catch((err) => setError(err.message))
                          }}
                        />
                      </td>
                      <td>{entry.source === 'sync' ? '同步' : '手工'}</td>
                      <td>
                        <button
                          type="button"
                          className="btn btn-danger btn-sm"
                          onClick={() => {
                            modelCatalogApi
                              .removeCatalog(entry.id)
                              .then(() => loadEntries(routeId))
                              .catch((err) => setError(err.message))
                          }}
                        >
                          删除
                        </button>
                      </td>
                    </tr>
                  ))}
                </tbody>
              </table>
            </div>
          )}
          <div className="form-row" style={{ marginTop: '1rem' }}>
            <div className="form-group" style={{ marginBottom: 0 }}>
              <label htmlFor="catalog-model">模型 id</label>
              <input
                id="catalog-model"
                placeholder="如 gpt-4o"
                value={newModel}
                onChange={(e) => setNewModel(e.target.value)}
              />
            </div>
            <div className="form-group" style={{ marginBottom: 0 }}>
              <label htmlFor="catalog-display">展示名（可选）</label>
              <input
                id="catalog-display"
                placeholder="下拉中显示的名称"
                value={newDisplay}
                onChange={(e) => setNewDisplay(e.target.value)}
              />
            </div>
            <button type="button" className="btn btn-secondary" onClick={handleAddModel} disabled={adding || !newModel.trim()}>
              {adding ? '添加中…' : '手工添加'}
            </button>
          </div>
        </div>
      ) : null}
    </div>
  )
}
