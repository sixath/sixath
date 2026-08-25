import { type FormEvent, useEffect, useState } from 'react'
import { Link, useNavigate } from 'react-router-dom'
import {
  DEV_BOOTSTRAP_TOKEN,
  getApiToken,
  getStoredOrgId,
  getStoredToken,
  hasApiToken,
  logout,
  saveCredentials,
} from '../api/auth'
import { settingsApi, type GlobalCodeModelSettings } from '../api/client'

export default function SettingsPage() {
  const navigate = useNavigate()
  const [token, setToken] = useState('')
  const [orgId, setOrgId] = useState('')
  const [saved, setSaved] = useState(false)
  const [codeModel, setCodeModel] = useState<GlobalCodeModelSettings>({
    provider: '',
    model: '',
    api_key: '',
    base_url: '',
  })
  const [codeSaved, setCodeSaved] = useState(false)
  const [codeError, setCodeError] = useState('')
  const [codeLoading, setCodeLoading] = useState(false)

  useEffect(() => {
    setToken(getStoredToken())
    setOrgId(getStoredOrgId())
  }, [])

  useEffect(() => {
    if (!hasApiToken()) return
    settingsApi
      .getCodeModel()
      .then((s) => {
        setCodeModel({
          provider: s.provider || '',
          model: s.model || '',
          api_key: s.api_key || '',
          base_url: s.base_url || '',
        })
      })
      .catch((err: unknown) => {
        setCodeError(err instanceof Error ? err.message : '加载全局源码模型失败')
      })
  }, [])

  const onSave = (e: FormEvent) => {
    e.preventDefault()
    saveCredentials(token, orgId)
    setSaved(true)
    window.setTimeout(() => setSaved(false), 2000)
  }

  const onSaveCodeModel = (e: FormEvent) => {
    e.preventDefault()
    setCodeError('')
    setCodeLoading(true)
    settingsApi
      .putCodeModel({
        provider: codeModel.provider?.trim() || '',
        model: codeModel.model?.trim() || '',
        api_key: codeModel.api_key?.trim() || '',
        base_url: codeModel.base_url?.trim() || '',
      })
      .then((s) => {
        setCodeModel({
          provider: s.provider || '',
          model: s.model || '',
          api_key: s.api_key || '',
          base_url: s.base_url || '',
        })
        setCodeSaved(true)
        window.setTimeout(() => setCodeSaved(false), 2000)
      })
      .catch((err: unknown) => {
        setCodeError(err instanceof Error ? err.message : '保存失败')
      })
      .finally(() => setCodeLoading(false))
  }

  const useDevDefault = () => {
    setToken(DEV_BOOTSTRAP_TOKEN)
    saveCredentials(DEV_BOOTSTRAP_TOKEN, orgId)
    setSaved(true)
    window.setTimeout(() => setSaved(false), 2000)
  }

  const onLogout = () => {
    logout()
    navigate('/login')
  }

  return (
    <div>
      <div className="page-header">
        <h1 className="page-title">设置</h1>
      </div>
      <p className="muted" style={{ marginBottom: '1.25rem' }}>
        Portal 已启用 Bearer 鉴权。未配置 Token 时会进入登录页，API 也会返回 401。本地可与 portal{' '}
        <code>auth.bootstrap_token</code>（默认 <code>dev-bootstrap-token</code>）对齐；也可用环境变量{' '}
        <code>VITE_API_TOKEN</code> / <code>VITE_ORG_ID</code>。首次访问请先到登录页填写 Token。
        组织与邀请请前往 <Link to="/orgs">组织管理</Link>。
      </p>

      <form className="form-panel" onSubmit={onSave}>
        <div className="form-group">
          <label htmlFor="api-token">API Token</label>
          <input
            id="api-token"
            type="password"
            autoComplete="off"
            placeholder="Bearer token（如 dev-bootstrap-token）"
            value={token}
            onChange={(e) => setToken(e.target.value)}
          />
          <small>写入 localStorage；留空则回退到 VITE_API_TOKEN</small>
        </div>

        <div className="form-group">
          <label htmlFor="org-id">当前 Org ID（可选）</label>
          <input
            id="org-id"
            type="text"
            autoComplete="off"
            placeholder="如 default"
            value={orgId}
            onChange={(e) => setOrgId(e.target.value)}
          />
          <small>请求头 X-Org-Id；留空则回退到 VITE_ORG_ID</small>
        </div>

        <div className="form-actions">
          <button type="submit" className="btn btn-primary">
            保存
          </button>
          <button type="button" className="btn btn-secondary" onClick={useDevDefault}>
            填入本地默认 Token
          </button>
          <button type="button" className="btn btn-secondary" onClick={onLogout}>
            退出登录
          </button>
          {saved && <span style={{ color: 'var(--ok)', fontWeight: 600 }}>已保存</span>}
        </div>

        <p
          className="muted"
          style={{
            marginTop: '1rem',
            color: hasApiToken() ? 'var(--ok)' : 'var(--warn, #f59e0b)',
            fontWeight: 600,
          }}
        >
          {hasApiToken()
            ? `当前生效 Token 已就绪（前缀 ${(getApiToken() || '').slice(0, 8)}…；来源：设置 / 环境变量 / 开发默认）`
            : '当前有效 Token：未配置 — 将跳转登录页，接口将 401'}
        </p>
      </form>

      <form className="form-panel" onSubmit={onSaveCodeModel} style={{ marginTop: '1.5rem' }}>
        <h2 className="form-section__title" style={{ marginBottom: '0.5rem' }}>源码分析模型（全局）</h2>
        <p className="muted" style={{ marginBottom: '1rem' }}>
          code 族（源码 / 调用链）默认使用这套模型。Agent 在表单里单独填写后会覆盖；未填则用这里。
          若此处也为空，可回退环境变量 <code>SATH_CODE_MODEL</code> 等。
        </p>
        {codeError ? <p className="error" style={{ marginBottom: '0.75rem' }}>{codeError}</p> : null}
        <div className="form-row">
          <div className="form-group">
            <label htmlFor="code-provider">Provider</label>
            <input
              id="code-provider"
              value={codeModel.provider || ''}
              onChange={(e) => setCodeModel((c) => ({ ...c, provider: e.target.value }))}
              placeholder="openai"
            />
          </div>
          <div className="form-group">
            <label htmlFor="code-model">模型名称</label>
            <input
              id="code-model"
              value={codeModel.model || ''}
              onChange={(e) => setCodeModel((c) => ({ ...c, model: e.target.value }))}
              placeholder="如 gpt-4o / claude-opus"
            />
          </div>
        </div>
        <div className="form-group">
          <label htmlFor="code-api-key">API Key</label>
          <input
            id="code-api-key"
            type="password"
            autoComplete="off"
            value={codeModel.api_key || ''}
            onChange={(e) => setCodeModel((c) => ({ ...c, api_key: e.target.value }))}
            placeholder="可选"
          />
        </div>
        <div className="form-group">
          <label htmlFor="code-base-url">Base URL</label>
          <input
            id="code-base-url"
            value={codeModel.base_url || ''}
            onChange={(e) => setCodeModel((c) => ({ ...c, base_url: e.target.value }))}
            placeholder="可选，兼容 OpenAI 的网关地址"
          />
        </div>
        <div className="form-actions">
          <button type="submit" className="btn btn-primary" disabled={codeLoading}>
            {codeLoading ? '保存中…' : '保存源码模型'}
          </button>
          {codeSaved && <span style={{ color: 'var(--ok)', fontWeight: 600 }}>已保存</span>}
        </div>
      </form>
    </div>
  )
}

