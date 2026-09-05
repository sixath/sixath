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

export default function SettingsPage() {
  const navigate = useNavigate()
  const [token, setToken] = useState('')
  const [orgId, setOrgId] = useState('')
  const [saved, setSaved] = useState(false)

  useEffect(() => {
    setToken(getStoredToken())
    setOrgId(getStoredOrgId())
  }, [])

  const onSave = (e: FormEvent) => {
    e.preventDefault()
    saveCredentials(token, orgId)
    setSaved(true)
    window.setTimeout(() => setSaved(false), 2000)
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
    </div>
  )
}
