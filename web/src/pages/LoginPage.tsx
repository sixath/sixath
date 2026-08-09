import { type FormEvent, useMemo, useState } from 'react'
import { Link, Navigate, useNavigate, useSearchParams } from 'react-router-dom'
import {
  DEV_BOOTSTRAP_TOKEN,
  applyLoginSession,
  hasApiToken,
  saveCredentials,
} from '../api/auth'
import { login } from '../api/sessionAuth'
import './LoginPage.css'

export default function LoginPage() {
  const navigate = useNavigate()
  const [params] = useSearchParams()
  const next = useMemo(() => {
    const n = params.get('next') || '/'
    return n.startsWith('/') && !n.startsWith('//') ? n : '/'
  }, [params])

  const [email, setEmail] = useState('')
  const [password, setPassword] = useState('')
  const [token, setToken] = useState('')
  const [orgId, setOrgId] = useState('')
  const [error, setError] = useState('')
  const [loading, setLoading] = useState(false)

  if (hasApiToken()) {
    return <Navigate to={next} replace />
  }

  const onEmailSubmit = async (e: FormEvent) => {
    e.preventDefault()
    const em = email.trim()
    if (!em || !password) {
      setError('请填写邮箱和密码')
      return
    }
    setError('')
    setLoading(true)
    try {
      const session = await login(em, password)
      applyLoginSession(session)
      navigate(next, { replace: true })
    } catch (err) {
      setError(err instanceof Error ? err.message : '登录失败')
    } finally {
      setLoading(false)
    }
  }

  const onTokenSubmit = (e: FormEvent) => {
    e.preventDefault()
    const t = token.trim()
    if (!t) {
      setError('请填写 API Token')
      return
    }
    setError('')
    saveCredentials(t, orgId)
    navigate(next, { replace: true })
  }

  return (
    <div className="login-page">
      <form className="login-card" onSubmit={onEmailSubmit}>
        <h1>Sixath 登录</h1>
        <p className="login-muted">使用邮箱与密码登录</p>
        {error && <p className="login-error">{error}</p>}
        <label htmlFor="login-email">邮箱</label>
        <input
          id="login-email"
          type="email"
          autoComplete="email"
          value={email}
          onChange={(e) => setEmail(e.target.value)}
          disabled={loading}
        />
        <label htmlFor="login-password">密码</label>
        <input
          id="login-password"
          type="password"
          autoComplete="current-password"
          value={password}
          onChange={(e) => setPassword(e.target.value)}
          disabled={loading}
        />
        <button type="submit" className="btn btn-primary" disabled={loading}>
          {loading ? '登录中…' : '登录'}
        </button>
        <p className="login-footer-link">
          有邀请链接？<Link to="/register">注册账号</Link>
        </p>
      </form>

      <details className="login-dev-section">
        <summary>开发者 Token 登录</summary>
        <form className="login-card login-dev-card" onSubmit={onTokenSubmit}>
          <p className="login-muted">使用 Portal Bearer Token（Phase 1）</p>
          <label htmlFor="login-token">API Token</label>
          <input
            id="login-token"
            type="password"
            autoComplete="off"
            value={token}
            onChange={(e) => setToken(e.target.value)}
          />
          <label htmlFor="login-org">Org ID（可选）</label>
          <input
            id="login-org"
            type="text"
            autoComplete="off"
            value={orgId}
            onChange={(e) => setOrgId(e.target.value)}
          />
          <button type="submit" className="btn btn-primary">进入</button>
          {import.meta.env.DEV && (
            <button
              type="button"
              className="btn btn-secondary"
              onClick={() => setToken(DEV_BOOTSTRAP_TOKEN)}
            >
              使用本地 bootstrap
            </button>
          )}
        </form>
      </details>
    </div>
  )
}
