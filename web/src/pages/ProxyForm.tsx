import { useEffect, useState } from 'react'
import { Link, useNavigate, useParams } from 'react-router-dom'
import { proxyApi, type CreateProxyRequest, type ProxyType } from '../api/client'

const PROXY_ID_RE = /^[a-z][a-z0-9_-]{0,35}$/

function linesToNoProxy(text: string): string[] {
  return text
    .split(/\r?\n/)
    .map((s) => s.trim())
    .filter(Boolean)
}

function noProxyToLines(items?: string[]): string {
  return (items || []).join('\n')
}

export default function ProxyForm() {
  const { id: routeId } = useParams()
  const navigate = useNavigate()
  const isEdit = !!routeId

  const [id, setId] = useState('')
  const [name, setName] = useState('')
  const [description, setDescription] = useState('')
  const [type, setType] = useState<ProxyType>('http')
  const [host, setHost] = useState('')
  const [port, setPort] = useState('')
  const [user, setUser] = useState('')
  const [password, setPassword] = useState('')
  const [hasPassword, setHasPassword] = useState(false)
  const [noProxyText, setNoProxyText] = useState('')
  const [error, setError] = useState('')
  const [loading, setLoading] = useState(false)
  const [testLoading, setTestLoading] = useState(false)
  const [testMsg, setTestMsg] = useState('')
  const [savedId, setSavedId] = useState(routeId || '')

  useEffect(() => {
    if (!isEdit || !routeId) return
    proxyApi
      .get(routeId)
      .then((p) => {
        setId(p.id)
        setSavedId(p.id)
        setName(p.name)
        setDescription(p.description || '')
        setType(p.type === 'socks5' ? 'socks5' : 'http')
        setHost(p.host || '')
        setPort(p.port ? String(p.port) : '')
        setUser(p.user || '')
        setPassword('')
        setHasPassword(!!p.has_password)
        setNoProxyText(noProxyToLines(p.no_proxy))
      })
      .catch((e) => setError(e.message))
  }, [isEdit, routeId])

  const knownId = savedId || (isEdit ? routeId : '') || ''

  const buildPayload = (): CreateProxyRequest => {
    const payload: CreateProxyRequest = {
      id: (isEdit ? knownId : id).trim(),
      name: name.trim(),
      description: description.trim(),
      type,
      host: host.trim(),
      port: parseInt(port, 10) || 0,
      user: user.trim(),
      no_proxy: linesToNoProxy(noProxyText),
    }
    if (password) {
      payload.password = password
    } else if (isEdit) {
      payload.password = ''
    }
    return payload
  }

  const handleSubmit = async (e: React.FormEvent) => {
    e.preventDefault()
    setError('')
    setTestMsg('')
    const slug = (isEdit ? knownId : id).trim()
    if (!isEdit && !slug) {
      setError('请填写 ID（slug，如 office）')
      return
    }
    if (!isEdit && !PROXY_ID_RE.test(slug)) {
      setError('ID 须为 slug（小写字母开头，仅 a-z、0-9、_、-，最长 36）')
      return
    }
    if (!name.trim()) {
      setError('请填写名称')
      return
    }
    if (!host.trim()) {
      setError('请填写 Host')
      return
    }
    const portNum = parseInt(port, 10)
    if (!portNum || portNum < 1 || portNum > 65535) {
      setError('请填写有效端口（1–65535）')
      return
    }

    setLoading(true)
    try {
      const data = buildPayload()
      if (isEdit && knownId) {
        const updated = await proxyApi.update(knownId, data)
        setSavedId(knownId)
        setHasPassword(updated.has_password)
        setPassword('')
        setTestMsg('已保存')
      } else {
        const created = await proxyApi.create(data)
        setSavedId(created.id)
        setHasPassword(created.has_password)
        setPassword('')
        navigate(`/proxies/${created.id}/edit`, { replace: true })
      }
    } catch (err) {
      setError((err as Error).message)
    } finally {
      setLoading(false)
    }
  }

  const handleTest = async () => {
    if (!knownId) {
      setTestMsg('请先保存后再测连通')
      return
    }
    setTestLoading(true)
    setTestMsg('')
    setError('')
    try {
      await proxyApi.test(knownId)
      setTestMsg('连通成功')
    } catch (err) {
      setError((err as Error).message)
    } finally {
      setTestLoading(false)
    }
  }

  return (
    <div>
      <div className="page-header">
        <h1>{isEdit ? '编辑代理' : '新建代理'}</h1>
        <Link to="/proxies" className="btn btn-secondary">
          返回
        </Link>
      </div>
      <div className="section-card" style={{ maxWidth: 560 }}>
        <form onSubmit={handleSubmit}>
          <div className="form-group">
            <label>ID *</label>
            <input
              value={isEdit ? knownId : id}
              onChange={(e) => setId(e.target.value)}
              disabled={isEdit}
              placeholder="如 office（创建后不可改）"
            />
            <p style={{ fontSize: '0.82em', color: 'var(--muted)', margin: '0.35rem 0 0' }}>
              slug，与 MCP 服务相同：小写字母开头，仅 a-z、0-9、_、-，最长 36。
            </p>
          </div>
          <div className="form-group">
            <label>名称 *</label>
            <input value={name} onChange={(e) => setName(e.target.value)} placeholder="显示名称" />
          </div>
          <div className="form-group">
            <label>描述</label>
            <textarea
              value={description}
              onChange={(e) => setDescription(e.target.value)}
              rows={2}
              placeholder="代理用途说明"
            />
          </div>
          <div className="form-group">
            <label>类型 *</label>
            <select value={type} onChange={(e) => setType(e.target.value as ProxyType)}>
              <option value="http">http</option>
              <option value="socks5">socks5</option>
            </select>
          </div>
          <div className="form-group" style={{ display: 'flex', gap: '0.5rem' }}>
            <div style={{ flex: 2 }}>
              <label>Host *</label>
              <input value={host} onChange={(e) => setHost(e.target.value)} placeholder="127.0.0.1" />
            </div>
            <div style={{ flex: 1 }}>
              <label>Port *</label>
              <input
                type="number"
                min={1}
                max={65535}
                value={port}
                onChange={(e) => setPort(e.target.value)}
                placeholder="1080"
              />
            </div>
          </div>
          <div className="form-group">
            <label>用户</label>
            <input value={user} onChange={(e) => setUser(e.target.value)} placeholder="可选" autoComplete="off" />
          </div>
          <div className="form-group">
            <label>密码</label>
            <input
              type="password"
              autoComplete="new-password"
              value={password}
              onChange={(e) => setPassword(e.target.value)}
              placeholder={hasPassword ? '留空则保持' : '可选'}
            />
          </div>
          <div className="form-group">
            <label>不走代理（每行一个）</label>
            <textarea
              value={noProxyText}
              onChange={(e) => setNoProxyText(e.target.value)}
              rows={4}
              placeholder={'es.local\n.example.com\n10.0.0.0/8'}
              style={{ fontFamily: 'monospace', fontSize: '0.9em' }}
            />
            <p style={{ fontSize: '0.82em', color: 'var(--muted)', margin: '0.35rem 0 0' }}>
              每行一个主机名、后缀或 CIDR。
            </p>
          </div>

          {error && <div className="error">{error}</div>}
          {testMsg && (
            <div className={testMsg.includes('成功') || testMsg === '已保存' ? 'success' : 'error'} style={{ marginTop: '0.5rem' }}>
              {testMsg}
            </div>
          )}

          <div style={{ display: 'flex', gap: '0.5rem', marginTop: '1.5rem', flexWrap: 'wrap' }}>
            <button type="submit" className="btn" disabled={loading}>
              {loading ? '提交中...' : '保存'}
            </button>
            <button
              type="button"
              className="btn btn-secondary"
              disabled={testLoading || !knownId}
              onClick={handleTest}
              title={!knownId ? '请先保存' : '测连通'}
            >
              {testLoading ? '测试中...' : '测连通'}
            </button>
            <Link to="/proxies" className="btn btn-secondary">
              取消
            </Link>
          </div>
        </form>
      </div>
    </div>
  )
}
