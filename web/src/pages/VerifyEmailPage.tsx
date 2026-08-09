import { useEffect, useMemo, useState } from 'react'
import { Link } from 'react-router-dom'
import { verifyEmail } from '../api/sessionAuth'
import './LoginPage.css'

type VerifyState = 'idle' | 'loading' | 'success' | 'error'

export default function VerifyEmailPage() {
  const token = useMemo(() => {
    const params = new URLSearchParams(window.location.search)
    return (params.get('token') ?? '').trim()
  }, [])

  const [state, setState] = useState<VerifyState>(token ? 'loading' : 'error')
  const [message, setMessage] = useState(token ? '' : '缺少验证参数 token')

  useEffect(() => {
    if (!token) return
    let cancelled = false
    verifyEmail(token)
      .then(() => {
        if (cancelled) return
        setState('success')
        setMessage('邮箱已验证，可以登录了。')
      })
      .catch((err) => {
        if (cancelled) return
        setState('error')
        setMessage(err instanceof Error ? err.message : '验证失败')
      })
    return () => {
      cancelled = true
    }
  }, [token])

  return (
    <div className="login-page">
      <div className="login-card">
        <h1>邮箱验证</h1>
        {state === 'loading' && <p className="login-muted">正在验证…</p>}
        {state === 'success' && <p className="login-success">{message}</p>}
        {state === 'error' && <p className="login-error">{message}</p>}
        {state !== 'loading' && (
          <p className="login-footer-link">
            <Link to="/login">前往登录</Link>
          </p>
        )}
      </div>
    </div>
  )
}
