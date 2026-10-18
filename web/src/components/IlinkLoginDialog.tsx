import { useEffect, useRef, useState } from 'react'
import { ilinkApi, type IlinkLoginStatusReply } from '../api/client'
import { ilinkLoginHint } from '../utils/ilink'
import './ConfirmDialog.css'

interface IlinkLoginDialogProps {
  channelId: string
  open: boolean
  onClose: () => void
  onConfirmed: () => void
}

export function IlinkLoginDialog({ channelId, open, onClose, onConfirmed }: IlinkLoginDialogProps) {
  const [image, setImage] = useState('')
  const [status, setStatus] = useState<IlinkLoginStatusReply['status'] | ''>('')
  const [error, setError] = useState('')
  const [round, setRound] = useState(0)
  const onConfirmedRef = useRef(onConfirmed)

  useEffect(() => {
    onConfirmedRef.current = onConfirmed
  }, [onConfirmed])

  useEffect(() => {
    if (!open) return
    const controller = new AbortController()
    const { signal } = controller
    setImage('')
    setStatus('')
    setError('')
    const run = async () => {
      try {
        const start = await ilinkApi.startLogin(channelId, signal)
        if (signal.aborted) return
        setImage(start.qrcode_image)
        setStatus('wait')
        // Each status call is held open by the server for up to ~30s; abort releases it on close.
        while (!signal.aborted) {
          const res = await ilinkApi.loginStatus(channelId, start.qrcode, signal)
          if (signal.aborted) return
          setStatus(res.status)
          if (res.status === 'confirmed') {
            onConfirmedRef.current()
            return
          }
          if (res.status === 'expired') return
          await new Promise((resolve) => setTimeout(resolve, 1000))
        }
      } catch (e) {
        if (!signal.aborted) setError(e instanceof Error ? e.message : String(e))
      }
    }
    void run()
    return () => controller.abort()
  }, [open, channelId, round])

  if (!open) return null
  const canRetry = status === 'expired' || !!error
  return (
    <div className="confirm-dialog-backdrop" role="presentation">
      <div className="confirm-dialog form-dialog" role="dialog" aria-modal="true" aria-labelledby="ilink-login-title">
        <h2 id="ilink-login-title">微信扫码登录</h2>
        <div className="form-dialog__body" style={{ textAlign: 'center' }}>
          {image ? (
            <img
              src={image}
              alt="微信 iLink 登录二维码"
              width={240}
              height={240}
              style={{ opacity: status === 'expired' || status === 'confirmed' ? 0.3 : 1 }}
            />
          ) : !error ? (
            <p className="page-sub">正在获取二维码…</p>
          ) : null}
          <p className="page-sub">{ilinkLoginHint(status)}</p>
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
