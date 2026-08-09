import { useEffect, useState } from 'react'
import { Link, useNavigate, useParams } from 'react-router-dom'
import { mcpServerApi, type CreateMcpServerRequest } from '../api/client'

type EnvRow = { key: string; value: string }

function argsToLines(args?: string[]): string {
  return (args || []).join('\n')
}

function linesToArgs(text: string): string[] {
  return text
    .split(/\r?\n/)
    .map((s) => s.trim())
    .filter(Boolean)
}

function envToRows(env?: Record<string, string>): EnvRow[] {
  if (!env || Object.keys(env).length === 0) return [{ key: '', value: '' }]
  return Object.entries(env).map(([key, value]) => ({ key, value }))
}

function rowsToEnv(rows: EnvRow[]): Record<string, string> {
  const out: Record<string, string> = {}
  for (const row of rows) {
    const k = row.key.trim()
    if (!k) continue
    out[k] = row.value
  }
  return out
}

export default function McpServerForm() {
  const { id: routeId } = useParams()
  const navigate = useNavigate()
  const isEdit = !!routeId

  const [id, setId] = useState('')
  const [name, setName] = useState('')
  const [description, setDescription] = useState('')
  const [transport, setTransport] = useState<'http' | 'stdio'>('stdio')
  const [endpoint, setEndpoint] = useState('')
  const [backend, setBackend] = useState('mark3labs')
  const [command, setCommand] = useState('')
  const [argsText, setArgsText] = useState('')
  const [envRows, setEnvRows] = useState<EnvRow[]>([{ key: '', value: '' }])
  const [timeoutSec, setTimeoutSec] = useState(30)
  const [error, setError] = useState('')
  const [loading, setLoading] = useState(false)
  const [testLoading, setTestLoading] = useState(false)
  const [testMsg, setTestMsg] = useState('')
  const [savedId, setSavedId] = useState(routeId || '')

  useEffect(() => {
    if (!isEdit || !routeId) return
    mcpServerApi
      .get(routeId)
      .then((s) => {
        setId(s.id)
        setSavedId(s.id)
        setName(s.name)
        setDescription(s.description || '')
        setTransport(s.transport === 'http' ? 'http' : 'stdio')
        setEndpoint(s.endpoint || '')
        setBackend(s.backend || 'mark3labs')
        setCommand(s.command || '')
        setArgsText(argsToLines(s.args))
        setEnvRows(envToRows(s.env))
        setTimeoutSec(s.timeout_sec && s.timeout_sec > 0 ? s.timeout_sec : 30)
      })
      .catch((e) => setError(e.message))
  }, [isEdit, routeId])

  const knownId = savedId || (isEdit ? routeId : '') || ''

  const buildPayload = (): CreateMcpServerRequest => {
    const payload: CreateMcpServerRequest = {
      id: (isEdit ? knownId : id).trim(),
      name: name.trim(),
      description: description.trim(),
      transport,
      timeout_sec: timeoutSec > 0 ? timeoutSec : 30,
    }
    if (transport === 'http') {
      payload.endpoint = endpoint.trim()
      payload.backend = backend.trim() || 'mark3labs'
    } else {
      payload.command = command.trim()
      payload.args = linesToArgs(argsText)
      payload.env = rowsToEnv(envRows)
      payload.backend = 'mark3labs'
    }
    return payload
  }

  const handleSubmit = async (e: React.FormEvent) => {
    e.preventDefault()
    setError('')
    setTestMsg('')
    if (!isEdit && !id.trim()) {
      setError('请填写 ID（slug，如 confluence）')
      return
    }
    if (!name.trim()) {
      setError('请填写名称')
      return
    }
    if (transport === 'http' && !endpoint.trim()) {
      setError('HTTP transport 需要填写 endpoint')
      return
    }
    if (transport === 'stdio' && !command.trim()) {
      setError('stdio transport 需要填写 command')
      return
    }

    setLoading(true)
    try {
      const data = buildPayload()
      if (isEdit && knownId) {
        await mcpServerApi.update(knownId, data)
        setSavedId(knownId)
        setTestMsg('已保存')
      } else {
        const created = await mcpServerApi.create(data)
        setSavedId(created.id)
        navigate(`/mcp-servers/${created.id}/edit`, { replace: true })
      }
    } catch (err) {
      setError((err as Error).message)
    } finally {
      setLoading(false)
    }
  }

  const handleTest = async () => {
    if (!knownId) {
      setTestMsg('请先保存后再测试连接')
      return
    }
    setTestLoading(true)
    setTestMsg('')
    setError('')
    try {
      const res = await mcpServerApi.test(knownId)
      const names = res.tool_names || []
      setTestMsg(
        names.length > 0
          ? `连接成功，发现 ${names.length} 个工具：${names.join(', ')}`
          : '连接成功，未发现工具',
      )
    } catch (err) {
      setError((err as Error).message)
    } finally {
      setTestLoading(false)
    }
  }

  const updateEnvRow = (index: number, field: 'key' | 'value', value: string) => {
    setEnvRows((prev) => prev.map((row, i) => (i === index ? { ...row, [field]: value } : row)))
  }

  const addEnvRow = () => setEnvRows((prev) => [...prev, { key: '', value: '' }])

  const removeEnvRow = (index: number) => {
    setEnvRows((prev) => {
      const next = prev.filter((_, i) => i !== index)
      return next.length ? next : [{ key: '', value: '' }]
    })
  }

  return (
    <div>
      <div className="page-header">
        <h1>{isEdit ? '编辑 MCP 服务' : '新建 MCP 服务'}</h1>
        <Link to="/mcp-servers" className="btn btn-secondary">
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
              placeholder="如 confluence（创建后不可改）"
            />
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
              placeholder="服务用途说明"
            />
          </div>
          <div className="form-group">
            <label>Transport *</label>
            <select
              value={transport}
              onChange={(e) => setTransport(e.target.value as 'http' | 'stdio')}
            >
              <option value="stdio">stdio</option>
              <option value="http">http</option>
            </select>
          </div>
          <div className="form-group">
            <label>超时（秒）</label>
            <input
              type="number"
              min={1}
              value={timeoutSec}
              onChange={(e) => setTimeoutSec(parseInt(e.target.value, 10) || 30)}
            />
          </div>

          {transport === 'http' && (
            <>
              <div className="form-group">
                <label>Endpoint *</label>
                <input
                  value={endpoint}
                  onChange={(e) => setEndpoint(e.target.value)}
                  placeholder="http://localhost:3000/mcp"
                />
              </div>
              <div className="form-group">
                <label>Backend</label>
                <input
                  value={backend}
                  onChange={(e) => setBackend(e.target.value)}
                  placeholder="mark3labs"
                />
              </div>
            </>
          )}

          {transport === 'stdio' && (
            <>
              <div className="form-group">
                <label>Command *</label>
                <input
                  value={command}
                  onChange={(e) => setCommand(e.target.value)}
                  placeholder="npx / node / npm"
                />
              </div>
              <div className="form-group">
                <label>Args（每行一个）</label>
                <textarea
                  value={argsText}
                  onChange={(e) => setArgsText(e.target.value)}
                  rows={4}
                  placeholder={'-y\n@atlassian-dc-mcp/confluence'}
                  style={{ fontFamily: 'monospace', fontSize: '0.9em' }}
                />
              </div>
              <div className="form-group">
                <label>Env</label>
                <p style={{ fontSize: '0.82em', color: 'var(--muted)', margin: '0 0 0.5rem' }}>
                  敏感值（TOKEN/SECRET/PASSWORD/KEY）加载后显示为 ***；提交时保持 *** 则不覆盖原值。
                </p>
                {envRows.map((row, index) => (
                  <div
                    key={index}
                    style={{ display: 'flex', gap: '0.5rem', marginBottom: '0.5rem', flexWrap: 'wrap' }}
                  >
                    <input
                      value={row.key}
                      onChange={(e) => updateEnvRow(index, 'key', e.target.value)}
                      placeholder="KEY"
                      style={{ flex: '1 1 140px' }}
                    />
                    <input
                      type="password"
                      autoComplete="new-password"
                      value={row.value}
                      onChange={(e) => updateEnvRow(index, 'value', e.target.value)}
                      placeholder="value 或 ***"
                      style={{ flex: '1 1 160px' }}
                    />
                    <button type="button" className="btn btn-danger btn-sm" onClick={() => removeEnvRow(index)}>
                      删除
                    </button>
                  </div>
                ))}
                <button type="button" className="btn btn-secondary btn-sm" onClick={addEnvRow}>
                  添加环境变量
                </button>
              </div>
            </>
          )}

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
              title={!knownId ? '请先保存' : '测试连接并列出工具'}
            >
              {testLoading ? '测试中...' : '测试连接'}
            </button>
            <Link to="/mcp-servers" className="btn btn-secondary">
              取消
            </Link>
          </div>
        </form>
      </div>
    </div>
  )
}
