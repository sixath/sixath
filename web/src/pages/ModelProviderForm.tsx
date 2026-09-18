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
        navigate('/model-providers')
      } else {
        if (!apiKey.trim()) {
          setError('新建供应商需要填写 API Key')
          setLoading(false)
          return
        }
        await modelCatalogApi.createProvider({
          name: name.trim(),
          kind,
          base_url: baseUrl.trim(),
          api_key: apiKey.trim(),
          enabled,
        })
        navigate('/model-providers')
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
    }
  }

  return (
    <div>
      <div className="page-header">
        <h1>{isEdit ? '编辑模型供应商' : '新建模型供应商'}</h1>
        <Link to="/model-providers" className="btn btn-secondary">
          返回列表
        </Link>
      </div>
      {error ? <div className="error">{error}</div> : null}
      <form className="section-card" onSubmit={handleSubmit}>
        <label>
          名称
          <input value={name} onChange={(e) => setName(e.target.value)} required />
        </label>
        <label>
          类型
          <select value={kind} onChange={(e) => setKind(e.target.value as ModelProviderKind)}>
            <option value="openai_compat">openai_compat（OpenAI / 中转站）</option>
            <option value="dashscope">dashscope</option>
          </select>
        </label>
        <label>
          Base URL
          <input
            value={baseUrl}
            onChange={(e) => setBaseUrl(e.target.value)}
            placeholder="https://api.openai.com/v1"
            required={kind === 'openai_compat'}
          />
        </label>
        <label>
          API Key{isEdit && hasApiKey ? '（留空则不修改）' : ''}
          <input
            type="password"
            value={apiKey}
            onChange={(e) => setApiKey(e.target.value)}
            autoComplete="new-password"
          />
        </label>
        <label className="checkbox-row">
          <input type="checkbox" checked={enabled} onChange={(e) => setEnabled(e.target.checked)} />
          启用
        </label>
        <button className="btn" type="submit" disabled={loading}>
          {loading ? '保存中…' : '保存'}
        </button>
      </form>

      {isEdit && routeId ? (
        <div className="section-card" style={{ marginTop: '1.5rem' }}>
          <div className="page-header">
            <h2>模型目录</h2>
            <button className="btn btn-secondary" type="button" onClick={handleSync} disabled={syncing || kind !== 'openai_compat'}>
              {syncing ? '同步中…' : '从上游同步'}
            </button>
          </div>
          {entries.length === 0 ? (
            <p className="empty-state">同步或手工添加模型</p>
          ) : (
            <table>
              <thead>
                <tr>
                  <th>模型 id</th>
                  <th>展示名</th>
                  <th>隐藏</th>
                  <th>来源</th>
                  <th />
                </tr>
              </thead>
              <tbody>
                {entries.map((e) => (
                  <tr key={e.id}>
                    <td>
                      <code>{e.model}</code>
                    </td>
                    <td>
                      <input
                        defaultValue={e.display_name}
                        onBlur={(ev) => {
                          const v = ev.target.value.trim()
                          if (v && v !== e.display_name) {
                            modelCatalogApi.patchCatalog(e.id, { display_name: v }).then(() => loadEntries(routeId)).catch((err) => setError(err.message))
                          }
                        }}
                      />
                    </td>
                    <td>
                      <input
                        type="checkbox"
                        checked={e.hidden}
                        onChange={(ev) => {
                          modelCatalogApi
                            .patchCatalog(e.id, { hidden: ev.target.checked })
                            .then(() => loadEntries(routeId))
                            .catch((err) => setError(err.message))
                        }}
                      />
                    </td>
                    <td>{e.source}</td>
                    <td>
                      <button
                        type="button"
                        className="btn btn-danger btn-sm"
                        onClick={() => {
                          modelCatalogApi.removeCatalog(e.id).then(() => loadEntries(routeId)).catch((err) => setError(err.message))
                        }}
                      >
                        删除
                      </button>
                    </td>
                  </tr>
                ))}
              </tbody>
            </table>
          )}
          <div className="actions" style={{ marginTop: '1rem', display: 'flex', gap: '0.5rem' }}>
            <input placeholder="模型 id" value={newModel} onChange={(e) => setNewModel(e.target.value)} />
            <input placeholder="展示名（可选）" value={newDisplay} onChange={(e) => setNewDisplay(e.target.value)} />
            <button type="button" className="btn btn-secondary" onClick={handleAddModel}>
              手工添加
            </button>
          </div>
        </div>
      ) : null}
    </div>
  )
}
