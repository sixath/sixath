import { useEffect, useRef, useState } from 'react'
import { ilinkApi, isApiError, type IlinkLoginStatusReply } from '../api/client'
import { ilinkLoginHint, ilinkPeerTail } from '../utils/ilink'
import './ConfirmDialog.css'
import './IlinkLoginDialog.css'

const STATUS_MAX_RETRIES = 3
const STATUS_RETRY_DELAY_MS = 1500

interface IlinkLoginDialogProps {
  channelId: string
  onClose: () => void
  onConfirmed: () => void
}

function delay(ms: number, signal: AbortSignal): Promise<void> {
  return new Promise((resolve) => {
    const timer = setTimeout(resolve, ms)
    signal.addEventListener(
      'abort',
      () => {
        clearTimeout(timer)
        resolve()
      },
      { once: true },
    )
  })
}

/** Mount only while open so each opening starts from a blank QR/status. */
export function IlinkLoginDialog({ channelId, onClose, onConfirmed }: IlinkLoginDialogProps) {
  const [image, setImage] = useState('')
  const [status, setStatus] = useState<IlinkLoginStatusReply['status'] | ''>('')
  const [error, setError] = useState('')
  const [botId, setBotId] = useState('')
  const [round, setRound] = useState(0)
  const onConfirmedRef = useRef(onConfirmed)

  useEffect(() => {
    onConfirmedRef.current = onConfirmed
  }, [onConfirmed])

  useEffect(() => {
    const controller = new AbortController()
    const { signal } = controller
    setImage('')
    setStatus('')
    setError('')
    setBotId('')
    const run = async () => {
      try {
        const start = await ilinkApi.startLogin(channelId, signal)
        if (signal.aborted) return
        setImage(start.qrcode_image)
        setStatus('wait')
        let failures = 0
        // Each status call is held open by the server for up to ~30s; abort releases it on close.
        while (!signal.aborted) {
          let res: IlinkLoginStatusReply
          try {
            res = await ilinkApi.loginStatus(channelId, start.qrcode, signal)
          } catch (e) {
            if (signal.aborted) return
            // 4xx (expired qrcode, unauthorized) will not recover by retrying.
            if (isApiError(e) && e.status < 500) throw e
            failures += 1
            if (failures > STATUS_MAX_RETRIES) throw e
            await delay(STATUS_RETRY_DELAY_MS, signal)
            continue
          }
          failures = 0
          if (signal.aborted) return
          setStatus(res.status)
          if (res.status === 'confirmed') {
            setBotId(res.bot_id || '')
            onConfirmedRef.current()
            return
          }
          if (res.status === 'expired') return
          await delay(1000, signal)
        }
      } catch (e) {
        if (!signal.aborted) setError(e instanceof Error ? e.message : String(e))
      }
    }
    void run()
    return () => controller.abort()
  }, [channelId, round])

  const canRetry = status === 'expired' || !!error
  const confirmed = status === 'confirmed'
  const expired = status === 'expired'
  const dotClass = confirmed ? ' ilink-login__dot--ok' : expired ? ' ilink-login__dot--expired' : ''
  const statusText = confirmed
    ? `登录成功${botId ? `，bot 尾号 ${ilinkPeerTail(botId)}` : ''}`
    : ilinkLoginHint(status)
  return (
    <div className="confirm-dialog-backdrop" role="presentation">
      <div className="confirm-dialog form-dialog" role="dialog" aria-modal="true" aria-labelledby="ilink-login-title">
        <h2 id="ilink-login-title">微信扫码登录</h2>
        <div className="form-dialog__body ilink-login">
          <div className={`ilink-login__qr${confirmed || expired ? ' ilink-login__qr--done' : ''}`}>
            {image ? (
              <img src={image} alt="微信 iLink 登录二维码" />
            ) : (
              <span className="ilink-login__placeholder">{error ? '二维码获取失败' : '正在获取二维码…'}</span>
            )}
            {confirmed ? (
              <div className="ilink-login__overlay">
                <span className="ilink-login__badge ilink-login__badge--ok" aria-hidden>✓</span>
                已登录
              </div>
            ) : expired ? (
              <div className="ilink-login__overlay">
                <span className="ilink-login__badge ilink-login__badge--expired" aria-hidden>↻</span>
                已过期
              </div>
            ) : null}
          </div>
          {image || confirmed ? (
            <p className="ilink-login__status">
              <span className={`ilink-login__dot${dotClass}`} aria-hidden />
              {statusText}
            </p>
          ) : null}
          {confirmed ? (
            <ul className="ilink-login__steps">
              <li>需要接收推送的人请先给 bot 发一条消息</li>
            </ul>
          ) : !expired && image ? (
            <ul className="ilink-login__steps">
              <li>1. 打开手机微信，扫一扫上方二维码</li>
              <li>2. 在手机上点击确认登录</li>
            </ul>
          ) : null}
        </div>
        {error ? <div className="error form-dialog__error">{error}</div> : null}
        <div className="confirm-dialog-actions">
          {canRetry ? (
            <button type="button" className="btn btn-secondary" onClick={() => setRound((n) => n + 1)}>
              刷新二维码
            </button>
          ) : null}
          <button type="button" className="btn" onClick={onClose}>
            {status === 'confirmed' ? '完成' : '关闭'}
          </button>
        </div>
      </div>
    </div>
  )
}
