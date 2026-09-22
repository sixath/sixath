import { useEffect, useRef, useState, useCallback } from 'react'
import { Terminal } from '@xterm/xterm'
import { FitAddon } from '@xterm/addon-fit'
import { createTerminalSession, deleteTerminalSession, terminalWebSocketURL, type TerminalSession } from '../api/terminal'
import '@xterm/xterm/css/xterm.css'

export default function TerminalPage() {
  const termRef = useRef<HTMLDivElement>(null)
  const termInstance = useRef<Terminal | null>(null)
  const wsRef = useRef<WebSocket | null>(null)
  const fitAddonRef = useRef<FitAddon | null>(null)

  const [session, setSession] = useState<TerminalSession | null>(null)
  const [connecting, setConnecting] = useState(false)
  const [error, setError] = useState('')
  const [disconnected, setDisconnected] = useState(false)

  const [vmid, setVmid] = useState('')
  const [port, setPort] = useState('53000')
  const [workdir, setWorkdir] = useState('C:\\')

  const connectWS = useCallback((sess: TerminalSession) => {
    if (wsRef.current) {
      wsRef.current.close()
    }
    setDisconnected(false)

    const url = terminalWebSocketURL(sess.session_id)
    const ws = new WebSocket(url)
    wsRef.current = ws

    ws.onopen = () => {
      setDisconnected(false)
    }

    ws.onmessage = (event) => {
      try {
        const msg = JSON.parse(event.data)
        if (msg.type === 'stdout' && msg.data) {
          termInstance.current?.write(msg.data.replace(/\n/g, '\r\n'))
        } else if (msg.type === 'error') {
          termInstance.current?.writeln(`\r\n\x1b[31m[ERROR] ${msg.message}\x1b[0m`)
        } else if (msg.type === 'closed') {
          termInstance.current?.writeln(`\r\n\x1b[33m[连接已关闭: ${msg.reason || 'unknown'}]\x1b[0m`)
          setDisconnected(true)
        }
      } catch {
        termInstance.current?.write(event.data)
      }
    }

    ws.onclose = () => {
      termInstance.current?.writeln('\r\n\x1b[33m[连接已断开]\x1b[0m')
      setDisconnected(true)
    }

    ws.onerror = () => {
      termInstance.current?.writeln('\r\n\x1b[31m[WebSocket 错误]\x1b[0m')
    }
  }, [])

  const handleConnect = async () => {
    const vmidNum = parseInt(vmid, 10)
    if (isNaN(vmidNum) || vmidNum <= 0) {
      setError('请输入有效的 VM ID')
      return
    }
    setConnecting(true)
    setError('')
    try {
      const portNum = parseInt(port, 10) || 53000
      const sess = await createTerminalSession(vmidNum, portNum, workdir)
      setSession(sess)
      setConnecting(false)
    } catch (err) {
      setError(err instanceof Error ? err.message : '创建会话失败')
      setConnecting(false)
    }
  }

  const handleDisconnect = async () => {
    if (wsRef.current) {
      wsRef.current.close()
      wsRef.current = null
    }
    if (session) {
      try {
        await deleteTerminalSession(session.session_id)
      } catch {}
    }
    termInstance.current?.dispose()
    termInstance.current = null
    setSession(null)
    setDisconnected(false)
  }

  const handleReconnect = () => {
    if (!session) return
    connectWS(session)
  }

  useEffect(() => {
    if (!session || !termRef.current) return

    if (termInstance.current) {
      termInstance.current.dispose()
    }

    const term = new Terminal({
      cursorBlink: true,
      fontSize: 14,
      fontFamily: 'Consolas, "Courier New", monospace',
      theme: {
        background: '#1e1e1e',
        foreground: '#d4d4d4',
        cursor: '#ffffff',
        selectionBackground: '#264f78',
      },
    })
    const fitAddon = new FitAddon()
    term.loadAddon(fitAddon)
    term.open(termRef.current)
    fitAddon.fit()
    fitAddonRef.current = fitAddon

    term.onData((data: string) => {
      if (wsRef.current && wsRef.current.readyState === WebSocket.OPEN) {
        wsRef.current.send(data)
      }
    })

    term.write(`Connected to VM ${session.host}:${session.port}\r\n`)
    term.write(`Working directory: ${session.workdir}\r\n\r\n`)

    termInstance.current = term

    connectWS(session)

    const handleResize = () => fitAddon.fit()
    window.addEventListener('resize', handleResize)
    return () => {
      window.removeEventListener('resize', handleResize)
      term.dispose()
      termInstance.current = null
    }
  }, [session, connectWS])

  useEffect(() => {
    return () => {
      if (wsRef.current) {
        wsRef.current.close()
      }
      if (session) {
        deleteTerminalSession(session.session_id).catch(() => {})
      }
    }
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [])

  return (
    <div style={{ display: 'flex', flexDirection: 'column', height: 'calc(100vh - 120px)' }}>
      <h1 className="page-title">远程终端</h1>

      {!session ? (
        <div className="section-card" style={{ maxWidth: 480, margin: '2rem auto', padding: '1.5rem' }}>
          <div className="form-group">
            <label>VM ID</label>
            <input
              type="text"
              value={vmid}
              onChange={(e) => setVmid(e.target.value)}
              placeholder="输入 VM ID"
              disabled={connecting}
            />
          </div>
          <div className="form-group">
            <label>端口</label>
            <input
              type="text"
              value={port}
              onChange={(e) => setPort(e.target.value)}
              placeholder="53000"
              disabled={connecting}
            />
          </div>
          <div className="form-group">
            <label>工作目录</label>
            <input
              type="text"
              value={workdir}
              onChange={(e) => setWorkdir(e.target.value)}
              placeholder="C:\"
              disabled={connecting}
            />
          </div>
          {error && <p className="error">{error}</p>}
          <button
            type="button"
            className="btn btn-primary"
            onClick={handleConnect}
            disabled={connecting}
            style={{ marginTop: '0.5rem', width: '100%' }}
          >
            {connecting ? '连接中…' : '连接'}
          </button>
        </div>
      ) : (
        <>
          <div style={{
            display: 'flex',
            gap: '1rem',
            alignItems: 'center',
            padding: '0.5rem 1rem',
            background: 'var(--bg-accent)',
            borderRadius: 'var(--radius-md)',
            marginBottom: '0.5rem',
            fontSize: '0.85rem',
            flexWrap: 'wrap',
          }}>
            <span>Host: <code>{session.host}</code></span>
            <span>Port: <code>{session.port}</code></span>
            <span>VMID: <code>{vmid}</code></span>
            <span>WorkDir: <code>{workdir}</code></span>
            <span style={{ marginLeft: 'auto', display: 'flex', gap: '0.5rem' }}>
              {disconnected && (
                <button type="button" className="btn btn-primary btn-sm" onClick={handleReconnect}>
                  重新连接
                </button>
              )}
              <button type="button" className="btn btn-sm" onClick={handleDisconnect} style={{ background: 'var(--danger)', color: '#fff' }}>
                断开
              </button>
            </span>
          </div>
          <div
            ref={termRef}
            style={{
              flex: 1,
              borderRadius: 'var(--radius-md)',
              overflow: 'hidden',
              minHeight: 400,
            }}
          />
        </>
      )}
    </div>
  )
}