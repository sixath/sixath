import { type FormEvent, useEffect, useState } from 'react'
import { Link, useNavigate } from 'react-router-dom'
import {
  DEV_BOOTSTRAP_TOKEN,
  getApiToken,
  getSessionEmail,
  getStoredOrgId,
  getStoredToken,
  hasApiToken,
  isSessionEmailUnverified,
  logout,
  markSessionEmailVerified,
  saveCredentials,
} from '../api/auth'
import { resendVerifyEmail, verifyEmail } from '../api/sessionAuth'

export default function SettingsPage() {
  const navigate = useNavigate()
  const [token, setToken] = useState('')
  const [orgId, setOrgId] = useState('')
  const [saved, setSaved] = useState(false)
  const [emailUnverified, setEmailUnverified] = useState(false)
  const [sessionEmail, setSessionEmail] = useState('')
  const [resendBusy, setResendBusy] = useState(false)
  const [resendMsg, setResendMsg] = useState('')
  const [resendErr, setResendErr] = useState('')
  const [pasteToken, setPasteToken] = useState('')
  const [pasteBusy, setPasteBusy] = useState(false)
  const [pasteMsg, setPasteMsg] = useState('')
  const [pasteErr, setPasteErr] = useState('')

  useEffect(() => {
    setToken(getStoredToken())
    setOrgId(getStoredOrgId())
    setEmailUnverified(isSessionEmailUnverified())
    setSessionEmail(getSessionEmail())
    if (window.location.hash === '#email-verify') {
      window.requestAnimationFrame(() => {
        document.getElementById('email-verify')?.scrollIntoView({ behavior: 'smooth', block: 'start' })
      })
    }
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

  const onResend = async () => {
    const bearer = getApiToken()
    if (!bearer) {
      setResendErr('当前没有有效登录 Token，请先登录。')
      return
    }
    setResendBusy(true)
    setResendErr('')
    setResendMsg('')
    try {
      const result = await resendVerifyEmail(bearer)
      if (result.already_verified) {
        markSessionEmailVerified()
        setEmailUnverified(false)
        setResendMsg('邮箱已验证，无需再发。')
        return
      }
      if (result.verification_disabled) {
        setResendErr('服务端未配置 SMTP，无法发送验证邮件。请联系管理员，或使用下方粘贴验证链接。')
        return
      }
      if (result.sent) {
        setResendMsg('验证邮件已重新发送，请查收邮箱并点击邮件中的链接。')
        return
      }
      setResendErr('未能发送验证邮件，请稍后重试。')
    } catch (e) {
      setResendErr(e instanceof Error ? e.message : '发送失败')
    } finally {
      setResendBusy(false)
    }
  }

  const extractToken = (raw: string): string => {
    const trimmed = raw.trim()
    if (!trimmed) return ''
    try {
      if (trimmed.includes('token=')) {
        const url = new URL(trimmed, window.location.origin)
        const t = url.searchParams.get('token')
        if (t) return t.trim()
      }
    } catch {
      /* not a URL */
    }
    const match = trimmed.match(/[?&]token=([^&\s]+)/)
    if (match?.[1]) {
      try {
        return decodeURIComponent(match[1]).trim()
      } catch {
        return match[1].trim()
      }
    }
    return trimmed
  }

  const onPasteVerify = async (e: FormEvent) => {
    e.preventDefault()
    const t = extractToken(pasteToken)
    if (!t) {
      setPasteErr('请粘贴邮件中的验证链接，或 token 字符串。')
      return
    }
    setPasteBusy(true)
    setPasteErr('')
    setPasteMsg('')
    try {
      await verifyEmail(t)
      markSessionEmailVerified()
      setEmailUnverified(false)
      setPasteMsg('邮箱验证成功。')
      setPasteToken('')
    } catch (err) {
      setPasteErr(err instanceof Error ? err.message : '验证失败')
    } finally {
      setPasteBusy(false)
    }
  }

  return (
    <div>
      <div className="page-header">
        <div>
          <div className="page-title-row">
            <h1>设置</h1>
          </div>
          <p className="page-sub">管理当前会话的鉴权凭证与组织上下文。</p>
        </div>
      </div>
      <p className="muted" style={{ marginBottom: '1.25rem' }}>
        Portal 已启用 Bearer 鉴权。未配置 Token 时会进入登录页，API 也会返回 401。本地可与 portal{' '}
        <code>auth.bootstrap_token</code>（默认 <code>dev-bootstrap-token</code>）对齐；也可用环境变量{' '}
        <code>VITE_API_TOKEN</code> / <code>VITE_ORG_ID</code>。首次访问请先到登录页填写 Token。
        组织与邀请请前往 <Link to="/orgs">组织管理</Link>。
      </p>

      <section id="email-verify" className="section-card" style={{ padding: '1.25rem', marginBottom: '1.25rem' }}>
        <h2 style={{ margin: '0 0 0.5rem', fontSize: '1.05rem' }}>邮箱验证</h2>
        <p className="muted" style={{ marginTop: 0 }}>
          {sessionEmail ? (
            <>当前邮箱：<code>{sessionEmail}</code></>
          ) : (
            <>当前会话未记录邮箱（可能是本地 Token / bootstrap 登录）。</>
          )}
          {' · '}
          {emailUnverified ? (
            <span style={{ color: 'var(--warn, #b45309)', fontWeight: 600 }}>未验证</span>
          ) : (
            <span style={{ color: 'var(--ok)', fontWeight: 600 }}>已验证 / 无需验证</span>
          )}
        </p>

        {emailUnverified ? (
          <>
            <p style={{ margin: '0 0 0.75rem', lineHeight: 1.5 }}>
              注册时系统会发送验证邮件。请打开邮件中的链接完成验证；若未收到，可重新发送，或把邮件里的验证链接粘贴到下方。
            </p>
            <div className="form-actions" style={{ marginBottom: '1rem' }}>
              <button type="button" className="btn btn-primary" disabled={resendBusy} onClick={onResend}>
                {resendBusy ? '发送中…' : '重新发送验证邮件'}
              </button>
            </div>
            {resendMsg && <p style={{ color: 'var(--ok)', fontWeight: 600 }}>{resendMsg}</p>}
            {resendErr && <p className="error">{resendErr}</p>}

            <form onSubmit={onPasteVerify} style={{ marginTop: '0.75rem' }}>
              <div className="form-group">
                <label htmlFor="verify-paste">粘贴验证链接或 token</label>
                <input
                  id="verify-paste"
                  type="text"
                  value={pasteToken}
                  onChange={(e) => setPasteToken(e.target.value)}
                  placeholder="https://…/verify-email?token=… 或直接粘贴 token"
                  autoComplete="off"
                />
              </div>
              <button type="submit" className="btn btn-secondary" disabled={pasteBusy || !pasteToken.trim()}>
                {pasteBusy ? '验证中…' : '提交验证'}
              </button>
              {pasteMsg && <p style={{ color: 'var(--ok)', fontWeight: 600, marginTop: '0.5rem' }}>{pasteMsg}</p>}
              {pasteErr && <p className="error" style={{ marginTop: '0.5rem' }}>{pasteErr}</p>}
            </form>
          </>
        ) : (
          <p className="muted" style={{ margin: 0 }}>
            无需额外操作。若状态未刷新，可退出后重新登录。
          </p>
        )}
      </section>

      <form className="form-panel section-card" onSubmit={onSave}>
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
