import { useEffect, useMemo, useState } from 'react'
import { Link, useNavigate, useSearchParams } from 'react-router-dom'
import { applyLoginSession } from '../api/auth'
import { exchangeWecomTicket } from '../api/sessionAuth'
import './LoginPage.css'
import ThemeToggle from '../components/ThemeToggle'

const ERROR_MESSAGES: Record<string, string> = {
  INVALID_STATE: '登录状态已失效，请重试',
  INVALID_CODE: '企业微信授权失败，请重试',
  INVALID_WECOM_USER: '非企业成员，无法登录',
  WECOM_LOGIN_DISABLED: '企业微信登录未配置',
  INTERNAL: '登录失败，请稍后重试',
}

function wecomErrorMessage(code: string): string {
  return ERROR_MESSAGES[code] ?? '登录失败，请稍后重试'
}

/** Survives StrictMode remount — one exchange per ticket per page load. */
const inflightTickets = new Set<string>()

type CallbackState = 'loading' | 'error'

export default function WecomCallbackPage() {
  const navigate = useNavigate()
  const [params] = useSearchParams()

  const error = params.get('error')?.trim() || ''
  const ticket = params.get('ticket')?.trim() || ''
  const next = useMemo(() => {
    const n = params.get('next') || '/'
    return n.startsWith('/') && !n.startsWith('//') ? n : '/'
  }, [params])

  const [state, setState] = useState<CallbackState>(() => {
    if (error || !ticket) return 'error'
    return 'loading'
  })
  const [message, setMessage] = useState(() => {
    if (error) return wecomErrorMessage(error)
    if (!ticket) return '登录失败，请稍后重试'
    return ''
  })

  useEffect(() => {
    if (!ticket || error) return
    if (inflightTickets.has(ticket)) return
    inflightTickets.add(ticket)

    let cancelled = false
    exchangeWecomTicket(ticket)
      .then((session) => {
        if (cancelled) return
        applyLoginSession(session)
        navigate(next, { replace: true })
      })
      .catch((err) => {
        if (cancelled) return
        inflightTickets.delete(ticket)
        setState('error')
        setMessage(err instanceof Error ? err.message : '登录失败，请稍后重试')
      })
    return () => {
      cancelled = true
    }
  }, [ticket, error, next, navigate])

  return (
    <div className="login-page">
      <ThemeToggle />
      <div className="login-card">
        <h1>企业微信登录</h1>
        {state === 'loading' && <p className="login-muted">正在登录…</p>}
        {state === 'error' && (
          <>
            <p className="login-error">{message}</p>
            <p className="login-footer-link">
              <Link to="/login">前往登录</Link>
            </p>
          </>
        )}
      </div>
    </div>
  )
}
