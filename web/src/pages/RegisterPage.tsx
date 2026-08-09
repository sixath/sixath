import { type FormEvent, useEffect, useMemo, useState } from 'react'
import { Link, Navigate, useNavigate, useSearchParams } from 'react-router-dom'
import { applyLoginSession, hasApiToken } from '../api/auth'
import { previewInvite, register } from '../api/sessionAuth'
import './LoginPage.css'

export default function RegisterPage() {
  const navigate = useNavigate()
  const [params] = useSearchParams()
  const invite = useMemo(() => (params.get('invite') ?? '').trim(), [params])

  const [email, setEmail] = useState('')
  const [password, setPassword] = useState('')
  const [confirm, setConfirm] = useState('')
  const [orgName, setOrgName] = useState('')
  const [inviteValid, setInviteValid] = useState<boolean | null>(null)
  const [previewLoading, setPreviewLoading] = useState(!!invite)
  const [error, setError] = useState('')
  const [loading, setLoading] = useState(false)

  useEffect(() => {
    if (!invite) {
      setPreviewLoading(false)
      setInviteValid(null)
      return
    }
    let cancelled = false
    setPreviewLoading(true)
    setError('')
    previewInvite(invite)
      .then((p) => {
        if (cancelled) return
        setOrgName(p.org_name)
        setInviteValid(p.valid)
      })
      .catch((err) => {
        if (cancelled) return
        setInviteValid(false)
        setError(err instanceof Error ? err.message : '无法预览邀请')
      })
      .finally(() => {
        if (!cancelled) setPreviewLoading(false)
      })
    return () => {
      cancelled = true
    }
  }, [invite])

  if (hasApiToken()) {
    return <Navigate to="/" replace />
  }

  if (!invite) {
    return (
      <div className="login-page">
        <div className="login-card">
          <h1>注册</h1>
          <p className="login-error">缺少邀请链接参数 <code>?invite=</code></p>
          <p className="login-muted">请通过组织管理员提供的邀请链接访问此页。</p>
          <p className="login-footer-link">
            已有账号？<Link to="/login">返回登录</Link>
          </p>
        </div>
      </div>
    )
  }

  const onSubmit = async (e: FormEvent) => {
    e.preventDefault()
    if (inviteValid === false) {
      setError('邀请无效或已过期')
      return
    }
    const em = email.trim()
    if (!em || !password) {
      setError('请填写邮箱和密码')
      return
    }
    if (password !== confirm) {
      setError('两次输入的密码不一致')
      return
    }
    setError('')
    setLoading(true)
    try {
      const session = await register(em, password, invite)
      applyLoginSession(session)
      navigate('/', { replace: true })
    } catch (err) {
      setError(err instanceof Error ? err.message : '注册失败')
    } finally {
      setLoading(false)
    }
  }

  return (
    <div className="login-page">
      <form className="login-card" onSubmit={onSubmit}>
        <h1>注册账号</h1>
        {previewLoading && <p className="login-muted">正在验证邀请…</p>}
        {!previewLoading && inviteValid && orgName && (
          <p className="login-invite-preview">加入组织：<strong>{orgName}</strong></p>
        )}
        {!previewLoading && inviteValid === false && (
          <p className="login-error">邀请无效、已过期或已达使用上限</p>
        )}
        {error && <p className="login-error">{error}</p>}
        <label htmlFor="register-email">邮箱</label>
        <input
          id="register-email"
          type="email"
          autoComplete="email"
          value={email}
          onChange={(e) => setEmail(e.target.value)}
          disabled={loading || previewLoading || inviteValid === false}
        />
        <label htmlFor="register-password">密码</label>
        <input
          id="register-password"
          type="password"
          autoComplete="new-password"
          value={password}
          onChange={(e) => setPassword(e.target.value)}
          disabled={loading || previewLoading || inviteValid === false}
        />
        <label htmlFor="register-confirm">确认密码</label>
        <input
          id="register-confirm"
          type="password"
          autoComplete="new-password"
          value={confirm}
          onChange={(e) => setConfirm(e.target.value)}
          disabled={loading || previewLoading || inviteValid === false}
        />
        <button
          type="submit"
          className="btn btn-primary"
          disabled={loading || previewLoading || inviteValid === false}
        >
          {loading ? '注册中…' : '注册并登录'}
        </button>
        <p className="login-footer-link">
          已有账号？<Link to="/login">返回登录</Link>
        </p>
      </form>
    </div>
  )
}
