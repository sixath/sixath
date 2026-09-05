import { useEffect, useState } from 'react'
import { useParams, useNavigate, Link } from 'react-router-dom'
import { toolApi, type CreateToolRequest, type ToolConfig } from '../api/client'
import { copyTool } from '../utils/toolCopy'

function linesToStringArray(text: string): string[] {
  return text.split(/[\r\n,]+/).map((s) => s.trim()).filter(Boolean)
}

function unknownToLines(value: unknown): string {
  if (!Array.isArray(value)) return ''
  return value.map((item) => String(item)).join('\n')
}

function maxFileBytesToMB(bytes: unknown): number {
  if (typeof bytes === 'number' && bytes > 0) return Math.round(bytes / (1024 * 1024))
  return 100
}

export default function ToolForm() {
  const { id } = useParams()
  const navigate = useNavigate()
  const isEdit = !!id

  const [name, setName] = useState('')
  const [description, setDescription] = useState('')
  const [type, setType] = useState<'builtin' | 'mcp' | 'datasource' | 'rca'>('builtin')
  const [config, setConfig] = useState<ToolConfig>({})
  const [openConfigJson, setOpenConfigJson] = useState('')
  const [openConfigError, setOpenConfigError] = useState('')
  const [newKvKey, setNewKvKey] = useState('')
  const [newKvValue, setNewKvValue] = useState('')
  const [error, setError] = useState('')
  const [loading, setLoading] = useState(false)

  const validateJson = (str: string): Record<string, unknown> | null => {
    if (!str.trim()) return {}
    try {
      const parsed = JSON.parse(str)
      if (typeof parsed !== 'object' || parsed === null || Array.isArray(parsed)) {
        return null
      }
      return parsed as Record<string, unknown>
    } catch {
      return null
    }
  }

  const parseValue = (str: string): unknown => {
    const s = str.trim()
    if (!s) return ''
    try {
      return JSON.parse(s)
    } catch {
      return s
    }
  }

  const syncJsonFromParams = (params: Record<string, unknown>) => {
    setOpenConfigJson(Object.keys(params).length ? JSON.stringify(params, null, 2) : '')
    setOpenConfigError('')
  }

  const addKv = (key: string, valueStr: string) => {
    if (!key.trim()) return
    const params = { ...(config.parameters || {}), [key.trim()]: parseValue(valueStr) }
    setConfig((c) => ({ ...c, parameters: params }))
    syncJsonFromParams(params)
  }

  const removeKv = (key: string) => {
    const { [key]: _, ...rest } = config.parameters || {}
    setConfig((c) => ({ ...c, parameters: rest }))
    syncJsonFromParams(rest)
  }

  useEffect(() => {
    if (isEdit && id) {
      toolApi.get(id).then((t) => {
        setName(t.name)
        setDescription(t.description)
        setType((t.type || 'builtin') as 'builtin' | 'mcp' | 'datasource' | 'rca')
        const loaded = t.config || {}
        // 历史数据可能只有 roots、func_path 为空；与下拉默认展示对齐，避免再保存时写回空值。
        if (t.type === 'rca') {
          loaded.rca = {
            ...(loaded.rca || {}),
            func_path: loaded.rca?.func_path || 'rca_code',
          }
        }
        setConfig(loaded)
        if (t.type === 'builtin' && t.config?.parameters && Object.keys(t.config.parameters).length > 0) {
          setOpenConfigJson(JSON.stringify(t.config.parameters, null, 2))
        }
      }).catch((e) => setError(e.message))
    }
  }, [id, isEdit])


  const handleSubmit = async (e: React.FormEvent) => {
    e.preventDefault()
    setError('')
    setOpenConfigError('')
    if (!name.trim()) {
      setError('请输入工具名称')
      return
    }
    if (!description.trim()) {
      setError('请输入工具描述')
      return
    }
    // 开放配置校验：若 JSON 有内容且无效则阻止提交
    if (type === 'builtin' && openConfigJson.trim()) {
      const parsed = validateJson(openConfigJson)
      if (parsed === null) {
        setOpenConfigError('开放配置必须是有效的 JSON 对象格式')
        return
      }
    }
    // 以 config 为数据源，确保各类型配置都被提交
    let submitConfig: ToolConfig = { ...config }
    if (type === 'builtin') {
      submitConfig = { ...config, parameters: config.parameters ?? {} }
    } else if (type === 'mcp') {
      submitConfig = { ...config, mcp: { endpoint: config.mcp_endpoint || config.mcp?.endpoint, id: config.mcp_server_id || config.mcp?.id, backend: config.mcp_backend || config.mcp?.backend } }
    } else if (type === 'datasource') {
      const ds = config.datasource ?? {}
      const dsType = ds.type || 'mysql'
      if (dsType === 'elasticsearch' || dsType === 'es') {
        if (!(ds.default_index || '').trim() || !(ds.purpose || '').trim()) {
          setError('请填写默认索引和用途')
          return
        }
      }
      submitConfig = {
        ...config,
        datasource: {
          ...ds,
          type: dsType, // 与下拉框默认展示一致，确保 type 始终传递
        },
      }
    } else if (type === 'rca') {
      // 下拉框用 || 'rca_code' 展示默认值，但未改动时 config.rca.func_path 可能仍为空；
      // 提交必须显式写入，否则运行时 registerRCATool 会因空 func_path 静默跳过。
      const funcPath = config.rca?.func_path || 'rca_code'
      if (funcPath === 'es_log_query') {
        const ep = (config.rca?.endpoint || '').trim()
        const ds = (config.rca?.datasource_id || '').trim()
        if ((ep && ds) || (!ep && !ds)) {
          setError(ep && ds
            ? 'ES 地址与 datasource 工具名互斥，请只保留其一'
            : '请填写 ES 地址，或填写已绑定的 datasource 工具名（二选一）')
          return
        }
      }
      submitConfig = {
        rca: {
          ...(config.rca || {}),
          func_path: funcPath,
        },
      }
    }
    setLoading(true)
    try {
      const data: CreateToolRequest = { name: name.trim(), description: description.trim(), type, config: submitConfig }
      if (isEdit && id) {
        await toolApi.update(id, data)
      } else {
        await toolApi.create(data)
      }
      navigate('/tools')
    } catch (e) {
      setError((e as Error).message)
    } finally {
      setLoading(false)
    }
  }

  return (
    <div>
      <div className="page-header">
        <h1>{isEdit ? '编辑工具' : '新建工具'}</h1>
        <Link to="/tools" className="btn btn-secondary">返回</Link>
      </div>
      <div className="section-card" style={{ maxWidth: 520 }}>
        <form onSubmit={handleSubmit}>
        <div className="form-group">
          <label>名称 *</label>
          <input value={name} onChange={(e) => setName(e.target.value)} placeholder="如 notion-search" />
        </div>
        <div className="form-group">
          <label>描述 *</label>
          <textarea value={description} onChange={(e) => setDescription(e.target.value)} rows={2} placeholder="工具功能说明" />
        </div>
        <div className="form-group">
          <label>类型 *</label>
          <select
            value={type}
            onChange={(e) => setType(e.target.value as 'builtin' | 'mcp' | 'datasource' | 'rca')}
          >
            <option value="builtin">内置工具</option>
            <option value="mcp">MCP 工具</option>
            <option value="datasource">数据源</option>
            <option value="rca">RCA</option>
          </select>
        </div>
        {type === 'mcp' && (
          <>
            <p
              style={{
                fontSize: '0.85em',
                color: 'var(--muted)',
                margin: '0 0 1rem',
                padding: '0.65rem 0.75rem',
                borderLeft: '3px solid var(--accent, #3b82f6)',
                background: 'var(--surface-2, transparent)',
              }}
            >
              推荐改用「
              <Link to="/mcp-servers">MCP 服务</Link>
              」管理 stdio/HTTP 服务；本表单为 legacy。
            </p>
            <div className="form-group">
              <label>MCP Endpoint（连接地址）</label>
              <input
                value={config.mcp_endpoint || config.mcp?.endpoint || ''}
                onChange={(e) => setConfig((c) => ({ ...c, mcp_endpoint: e.target.value, mcp: { ...(c.mcp || {}), endpoint: e.target.value } }))}
                placeholder="http://localhost:3000/mcp"
              />
            </div>
            <div className="form-group">
              <label>MCP ID（服务标识）</label>
              <input
                value={config.mcp_server_id || config.mcp?.id || ''}
                onChange={(e) => setConfig((c) => ({ ...c, mcp_server_id: e.target.value, mcp: { ...(c.mcp || {}), id: e.target.value } }))}
                placeholder="如 notion"
              />
            </div>
            <div className="form-group">
              <label>MCP Backend</label>
              <input
                value={config.mcp_backend || config.mcp?.backend || ''}
                onChange={(e) => setConfig((c) => ({ ...c, mcp_backend: e.target.value, mcp: { ...(c.mcp || {}), backend: e.target.value } }))}
                placeholder="metoro | mark3labs"
              />
            </div>
            <div className="form-group">
              <label>超时（秒）</label>
              <input
                type="number"
                value={config.timeout_sec ?? 30}
                onChange={(e) => setConfig((c) => ({ ...c, timeout_sec: parseInt(e.target.value) || 30 }))}
              />
            </div>
          </>
        )}
        {type === 'datasource' && (
          <>
            <div className="form-group">
              <label>数据源 ID</label>
              <input
                value={config.datasource?.id || ''}
                onChange={(e) => setConfig((c) => ({ ...c, datasource: { ...(c.datasource || {}), id: e.target.value } }))}
                placeholder="如 ds1"
              />
            </div>
            <div className="form-group">
              <label>类型</label>
              <select
                value={config.datasource?.type || 'mysql'}
                onChange={(e) => setConfig((c) => ({ ...c, datasource: { ...(c.datasource || {}), type: e.target.value } }))}
              >
                <option value="mysql">MySQL</option>
                <option value="mongodb">MongoDB</option>
                <option value="elasticsearch">Elasticsearch</option>
                <option value="hive">Hive</option>
              </select>
            </div>
            <div className="form-group">
              <label>DSN（完整连接串，与 Host/Port/User 二选一）</label>
              <input
                value={config.datasource?.dsn || ''}
                onChange={(e) => setConfig((c) => ({ ...c, datasource: { ...(c.datasource || {}), dsn: e.target.value } }))}
                placeholder="user:pass@tcp(localhost:3306)/dbname"
              />
            </div>
            <div className="form-group" style={{ display: 'flex', gap: '0.5rem' }}>
              <div style={{ flex: 1 }}>
                <label>Host</label>
                <input
                  value={config.datasource?.host || ''}
                  onChange={(e) => setConfig((c) => ({ ...c, datasource: { ...(c.datasource || {}), host: e.target.value } }))}
                  placeholder="localhost"
                />
              </div>
              <div style={{ flex: 1 }}>
                <label>Port</label>
                <input
                  type="number"
                  value={config.datasource?.port ?? ''}
                  onChange={(e) => setConfig((c) => ({ ...c, datasource: { ...(c.datasource || {}), port: parseInt(e.target.value) || 0 } }))}
                  placeholder="3306"
                />
              </div>
            </div>
            <div className="form-group" style={{ display: 'flex', gap: '0.5rem' }}>
              <div style={{ flex: 1 }}>
                <label>User</label>
                <input
                  value={config.datasource?.user || ''}
                  onChange={(e) => setConfig((c) => ({ ...c, datasource: { ...(c.datasource || {}), user: e.target.value } }))}
                  placeholder="root"
                />
              </div>
              <div style={{ flex: 1 }}>
                <label>Password</label>
                <input
                  type="password"
                  value={config.datasource?.password || ''}
                  onChange={(e) => setConfig((c) => ({ ...c, datasource: { ...(c.datasource || {}), password: e.target.value } }))}
                  placeholder="***"
                />
              </div>
            </div>
            <div className="form-group">
              <label>DB Name</label>
              <input
                value={config.datasource?.dbname || ''}
                onChange={(e) => setConfig((c) => ({ ...c, datasource: { ...(c.datasource || {}), dbname: e.target.value } }))}
                placeholder="mydb"
              />
            </div>
            <div className="form-group">
              <label>
                <input
                  type="checkbox"
                  checked={config.datasource?.read_only ?? false}
                  onChange={(e) => setConfig((c) => ({ ...c, datasource: { ...(c.datasource || {}), read_only: e.target.checked } }))}
                />
                {' '}只读
              </label>
            </div>
            {(config.datasource?.type === 'elasticsearch' || config.datasource?.type === 'es') && (
              <>
                <div className="form-group">
                  <label>默认索引 *</label>
                  <input
                    value={config.datasource?.default_index || ''}
                    onChange={(e) => setConfig((c) => ({ ...c, datasource: { ...(c.datasource || {}), default_index: e.target.value } }))}
                    placeholder="app-*"
                  />
                </div>
                <div className="form-group">
                  <label>trace 字段</label>
                  <input
                    value={config.datasource?.trace_id_field || ''}
                    onChange={(e) => setConfig((c) => ({ ...c, datasource: { ...(c.datasource || {}), trace_id_field: e.target.value } }))}
                    placeholder="trace_id"
                  />
                </div>
                <div className="form-group">
                  <label>用途 *</label>
                  <input
                    value={config.datasource?.purpose || ''}
                    onChange={(e) => setConfig((c) => ({ ...c, datasource: { ...(c.datasource || {}), purpose: e.target.value } }))}
                    placeholder="如 应用日志"
                  />
                </div>
              </>
            )}
          </>
        )}
        {type === 'rca' && (
          <>
            <div className="form-group">
              <label>RCA 子工具</label>
              <select
                value={config.rca?.func_path || 'rca_code'}
                onChange={(e) => setConfig((c) => ({ ...c, rca: { ...(c.rca || {}), func_path: e.target.value as 'rca_code' | 'rca_symbol' | 'jaeger_trace' | 'es_log_query' } }))}
              >
                <option value="rca_code">代码检索 (grep/glob/read)</option>
                <option value="rca_symbol">符号导航 (definition/references)</option>
                <option value="jaeger_trace">Jaeger 链路</option>
                {isEdit && config.rca?.func_path === 'es_log_query' ? (
                  <option value="es_log_query">ELK 日志</option>
                ) : null}
              </select>
            </div>

            {(['rca_code', 'rca_symbol'] as const).includes((config.rca?.func_path || 'rca_code') as 'rca_code' | 'rca_symbol') && (
              <div className="form-group">
                <label>仓库根路径(每行一个绝对路径)</label>
                <textarea
                  value={(config.rca?.roots || []).join('\n')}
                  onChange={(e) => setConfig((c) => ({ ...c, rca: { ...(c.rca || {}), roots: e.target.value.split('\n').map((s) => s.trim()).filter(Boolean) } }))}
                  placeholder={'/abs/path/service-a\n/abs/path/service-b'}
                />
                <small>Agent 已挂载 workspace/code 时，运行时以该目录为根；此处仅在无挂载时作为后备。</small>
              </div>
            )}

            {config.rca?.func_path === 'rca_symbol' && (
              <>
                <div className="form-group">
                  <label>gopls 可执行路径(可选，默认 PATH 中 gopls)</label>
                  <input
                    value={config.rca?.gopls_path || ''}
                    onChange={(e) => setConfig((c) => ({ ...c, rca: { ...(c.rca || {}), gopls_path: e.target.value } }))}
                    placeholder="gopls 或 /usr/local/bin/gopls"
                  />
                </div>
                <div className="form-group">
                  <label>gopls 就绪超时(秒)</label>
                  <input
                    type="number"
                    min={1}
                    value={config.rca?.ready_timeout_sec ?? ''}
                    onChange={(e) => setConfig((c) => ({
                      ...c,
                      rca: {
                        ...(c.rca || {}),
                        ready_timeout_sec: e.target.value === '' ? undefined : Number(e.target.value),
                      },
                    }))}
                    placeholder="30"
                  />
                </div>
                <div className="form-group">
                  <label>单次 LSP 请求超时(秒)</label>
                  <input
                    type="number"
                    min={1}
                    value={config.rca?.request_timeout_sec ?? ''}
                    onChange={(e) => setConfig((c) => ({
                      ...c,
                      rca: {
                        ...(c.rca || {}),
                        request_timeout_sec: e.target.value === '' ? undefined : Number(e.target.value),
                      },
                    }))}
                    placeholder="10"
                  />
                </div>
              </>
            )}

            {config.rca?.func_path === 'jaeger_trace' && (
              <div className="form-group">
                <label>Jaeger Query URL</label>
                <input
                  value={config.rca?.query_url || ''}
                  onChange={(e) => setConfig((c) => ({ ...c, rca: { ...(c.rca || {}), query_url: e.target.value } }))}
                  placeholder="http://jaeger-host:16686"
                />
              </div>
            )}

            {config.rca?.func_path === 'es_log_query' && (
              <>
                <div className="form-group">
                  <label>ES 地址（推荐直接填写）</label>
                  <input
                    value={config.rca?.endpoint || ''}
                    onChange={(e) => setConfig((c) => ({ ...c, rca: { ...(c.rca || {}), endpoint: e.target.value } }))}
                    placeholder="http://host:9200"
                  />
                </div>
                <div className="form-group">
                  <label>用户（可选）</label>
                  <input
                    value={config.rca?.user || ''}
                    onChange={(e) => setConfig((c) => ({ ...c, rca: { ...(c.rca || {}), user: e.target.value } }))}
                    placeholder="basic auth user"
                    autoComplete="off"
                  />
                </div>
                <div className="form-group">
                  <label>密码（可选）</label>
                  <input
                    type="password"
                    value={config.rca?.password || ''}
                    onChange={(e) => setConfig((c) => ({ ...c, rca: { ...(c.rca || {}), password: e.target.value } }))}
                    placeholder="basic auth password"
                    autoComplete="new-password"
                  />
                </div>
                <div className="form-group">
                  <label>或：引用已绑定 datasource 工具名（与上方地址二选一）</label>
                  <input
                    value={config.rca?.datasource_id || ''}
                    onChange={(e) => setConfig((c) => ({ ...c, rca: { ...(c.rca || {}), datasource_id: e.target.value } }))}
                    placeholder="es-logs"
                  />
                </div>
                <div className="form-group">
                  <label>默认索引</label>
                  <input
                    value={config.rca?.default_index || ''}
                    onChange={(e) => setConfig((c) => ({ ...c, rca: { ...(c.rca || {}), default_index: e.target.value } }))}
                    placeholder="app-logs-*"
                  />
                </div>
                <div className="form-group">
                  <label>trace_id 字段名</label>
                  <input
                    value={config.rca?.trace_id_field || ''}
                    onChange={(e) => setConfig((c) => ({ ...c, rca: { ...(c.rca || {}), trace_id_field: e.target.value } }))}
                    placeholder="trace_id"
                  />
                </div>
              </>
            )}
          </>
        )}
        {type === 'builtin' && (
          <>
            <div className="form-group">
              <label>函数路径</label>
              <input
                value={config.func_path || ''}
                onChange={(e) => setConfig((c) => ({ ...c, func_path: e.target.value }))}
                placeholder="如 calculator_add、ssh_exec、scp"
              />
            </div>
            {((config.func_path || '').trim() === 'ssh_exec') && (
              <div
                className="form-group"
                style={{
                  borderLeft: '3px solid var(--accent, #3b82f6)',
                  paddingLeft: '0.75rem',
                  marginBottom: '1rem',
                }}
              >
                <div style={{ fontSize: '0.9em', marginBottom: '0.75rem', fontWeight: 600 }}>
                  SSH（ssh_exec）页面配置
                </div>
                <p style={{ fontSize: '0.82em', color: 'var(--text-muted)', margin: '0 0 0.75rem' }}>
                  填写用户名、密码与端口后，将使用内置 SSH 客户端（密码登录）；默认不校验主机密钥（strict_host_key_checking=no）。生产环境可改用密钥并在下方「开放配置」中配置 native 块与 known_hosts。
                </p>
                <div className="form-group">
                  <label>SSH 用户名</label>
                  <input
                    value={(config.parameters?.default_user as string) ?? ''}
                    onChange={(e) => {
                      const v = e.target.value
                      setConfig((c) => {
                        const params = {
                          ...(c.parameters || {}),
                          default_user: v,
                          strict_host_key_checking: 'no',
                        }
                        setOpenConfigJson(JSON.stringify(params, null, 2))
                        return { ...c, parameters: params }
                      })
                    }}
                    placeholder="如 vrviu"
                  />
                </div>
                <div className="form-group">
                  <label>SSH 密码</label>
                  <input
                    type="password"
                    autoComplete="new-password"
                    value={(config.parameters?.password as string) ?? ''}
                    onChange={(e) => {
                      const v = e.target.value
                      setConfig((c) => {
                        const params = {
                          ...(c.parameters || {}),
                          password: v,
                          strict_host_key_checking: 'no',
                        }
                        setOpenConfigJson(JSON.stringify(params, null, 2))
                        return { ...c, parameters: params }
                      })
                    }}
                    placeholder="写入后保存在服务端配置中"
                  />
                </div>
                <div className="form-group">
                  <label>SSH 端口</label>
                  <input
                    type="number"
                    min={1}
                    max={65535}
                    value={
                      typeof config.parameters?.port === 'number'
                        ? config.parameters.port
                        : (config.parameters?.port != null && config.parameters?.port !== ''
                          ? Number(config.parameters.port)
                          : 22)
                    }
                    onChange={(e) => {
                      const n = parseInt(e.target.value, 10) || 22
                      setConfig((c) => {
                        const params = {
                          ...(c.parameters || {}),
                          port: n,
                          strict_host_key_checking: 'no',
                        }
                        setOpenConfigJson(JSON.stringify(params, null, 2))
                        return { ...c, parameters: params }
                      })
                    }}
                  />
                </div>
              </div>
            )}
            {((config.func_path || '').trim() === 'scp') && (
              <div
                className="form-group"
                style={{
                  borderLeft: '3px solid var(--accent, #3b82f6)',
                  paddingLeft: '0.75rem',
                  marginBottom: '1rem',
                }}
              >
                <div style={{ fontSize: '0.9em', marginBottom: '0.75rem', fontWeight: 600 }}>
                  SCP 文件传输页面配置
                </div>
                <p style={{ fontSize: '0.82em', color: 'var(--text-muted)', margin: '0 0 0.75rem' }}>
                  用于 Agent 上传/下载远程文件（direction=upload/download）。填写密码后将走 SFTP；未填密码则使用系统 scp + 本机 SSH 密钥。路径白名单留空表示不限制。
                </p>
                <div className="form-group">
                  <label>SSH 用户名</label>
                  <input
                    value={(config.parameters?.default_user as string) ?? ''}
                    onChange={(e) => {
                      const v = e.target.value
                      setConfig((c) => {
                        const params = {
                          ...(c.parameters || {}),
                          default_user: v,
                          strict_host_key_checking: 'no',
                        }
                        setOpenConfigJson(JSON.stringify(params, null, 2))
                        return { ...c, parameters: params }
                      })
                    }}
                    placeholder="如 root"
                  />
                </div>
                <div className="form-group">
                  <label>SSH 密码</label>
                  <input
                    type="password"
                    autoComplete="new-password"
                    value={(config.parameters?.password as string) ?? ''}
                    onChange={(e) => {
                      const v = e.target.value
                      setConfig((c) => {
                        const params = {
                          ...(c.parameters || {}),
                          password: v,
                          strict_host_key_checking: 'no',
                        }
                        setOpenConfigJson(JSON.stringify(params, null, 2))
                        return { ...c, parameters: params }
                      })
                    }}
                    placeholder="写入后保存在服务端配置中"
                  />
                </div>
                <div className="form-group" style={{ display: 'flex', gap: '0.5rem' }}>
                  <div style={{ flex: 1 }}>
                    <label>SSH 端口</label>
                    <input
                      type="number"
                      min={1}
                      max={65535}
                      value={
                        typeof config.parameters?.port === 'number'
                          ? config.parameters.port
                          : (config.parameters?.port != null && config.parameters?.port !== ''
                            ? Number(config.parameters.port)
                            : 22)
                      }
                      onChange={(e) => {
                        const n = parseInt(e.target.value, 10) || 22
                        setConfig((c) => {
                          const params = {
                            ...(c.parameters || {}),
                            port: n,
                            strict_host_key_checking: 'no',
                          }
                          setOpenConfigJson(JSON.stringify(params, null, 2))
                          return { ...c, parameters: params }
                        })
                      }}
                    />
                  </div>
                  <div style={{ flex: 1 }}>
                    <label>超时（秒）</label>
                    <input
                      type="number"
                      min={1}
                      value={
                        typeof config.parameters?.default_timeout_sec === 'number'
                          ? config.parameters.default_timeout_sec
                          : (config.parameters?.default_timeout_sec != null && config.parameters?.default_timeout_sec !== ''
                            ? Number(config.parameters.default_timeout_sec)
                            : 120)
                      }
                      onChange={(e) => {
                        const n = parseInt(e.target.value, 10) || 120
                        setConfig((c) => {
                          const params = { ...(c.parameters || {}), default_timeout_sec: n }
                          setOpenConfigJson(JSON.stringify(params, null, 2))
                          return { ...c, parameters: params }
                        })
                      }}
                    />
                  </div>
                  <div style={{ flex: 1 }}>
                    <label>单文件上限（MB）</label>
                    <input
                      type="number"
                      min={1}
                      value={maxFileBytesToMB(config.parameters?.max_file_bytes)}
                      onChange={(e) => {
                        const mb = parseInt(e.target.value, 10) || 100
                        setConfig((c) => {
                          const params = { ...(c.parameters || {}), max_file_bytes: mb * 1024 * 1024 }
                          setOpenConfigJson(JSON.stringify(params, null, 2))
                          return { ...c, parameters: params }
                        })
                      }}
                    />
                  </div>
                </div>
                <div className="form-group">
                  <label>允许的主机（每行一个，支持通配如 10.79.240.*）</label>
                  <textarea
                    rows={3}
                    value={unknownToLines(config.parameters?.allowed_hosts)}
                    onChange={(e) => {
                      const allowed_hosts = linesToStringArray(e.target.value)
                      setConfig((c) => {
                        const params = { ...(c.parameters || {}), allowed_hosts }
                        setOpenConfigJson(JSON.stringify(params, null, 2))
                        return { ...c, parameters: params }
                      })
                    }}
                    placeholder={'10.79.240.*\n172.30.240.64'}
                  />
                </div>
                <div className="form-group">
                  <label>允许的本地路径前缀（每行一个）</label>
                  <textarea
                    rows={2}
                    value={unknownToLines(config.parameters?.allowed_local_path_prefixes)}
                    onChange={(e) => {
                      const allowed_local_path_prefixes = linesToStringArray(e.target.value)
                      setConfig((c) => {
                        const params = { ...(c.parameters || {}), allowed_local_path_prefixes }
                        setOpenConfigJson(JSON.stringify(params, null, 2))
                        return { ...c, parameters: params }
                      })
                    }}
                    placeholder={'D:/deploy\nD:\\deploy'}
                  />
                </div>
                <div className="form-group">
                  <label>允许的远程路径前缀（每行一个）</label>
                  <textarea
                    rows={2}
                    value={unknownToLines(config.parameters?.allowed_remote_path_prefixes)}
                    onChange={(e) => {
                      const allowed_remote_path_prefixes = linesToStringArray(e.target.value)
                      setConfig((c) => {
                        const params = { ...(c.parameters || {}), allowed_remote_path_prefixes }
                        setOpenConfigJson(JSON.stringify(params, null, 2))
                        return { ...c, parameters: params }
                      })
                    }}
                    placeholder="/data/tmp"
                  />
                </div>
              </div>
            )}
            <div className="form-group">
              <label>开放配置</label>
              <div style={{ marginBottom: '0.75rem' }}>
                <div style={{ fontSize: '0.85em', color: 'var(--text-muted)', marginBottom: '0.5rem' }}>逐一添加</div>
                <div style={{ display: 'flex', gap: '0.5rem', flexWrap: 'wrap', alignItems: 'flex-start' }}>
                  <input
                    value={newKvKey}
                    onChange={(e) => setNewKvKey(e.target.value)}
                    onKeyDown={(e) => {
                      if (e.key === 'Enter') {
                        e.preventDefault()
                        addKv(newKvKey, newKvValue)
                        setNewKvKey('')
                        setNewKvValue('')
                      }
                    }}
                    placeholder="key"
                    style={{ flex: '1 1 120px', minWidth: 100 }}
                  />
                  <input
                    value={newKvValue}
                    onChange={(e) => setNewKvValue(e.target.value)}
                    onKeyDown={(e) => {
                      if (e.key === 'Enter') {
                        e.preventDefault()
                        addKv(newKvKey, newKvValue)
                        setNewKvKey('')
                        setNewKvValue('')
                      }
                    }}
                    placeholder="value（支持 JSON）"
                    style={{ flex: '1 1 140px', minWidth: 100 }}
                  />
                  <button
                    type="button"
                    className="btn btn-secondary"
                    onClick={() => {
                      addKv(newKvKey, newKvValue)
                      setNewKvKey('')
                      setNewKvValue('')
                    }}
                  >
                    添加
                  </button>
                </div>
                {(config.parameters && Object.keys(config.parameters).length > 0) && (
                  <ul style={{ marginTop: '0.5rem', paddingLeft: '1.25rem' }}>
                    {Object.entries(config.parameters).map(([k, v]) => (
                      <li key={k} style={{ display: 'flex', alignItems: 'center', gap: '0.5rem', marginBottom: '0.25rem' }}>
                        <code style={{ fontSize: '0.85em' }}>{k}</code>
                        <span style={{ color: 'var(--text-muted)' }}>→</span>
                        <code style={{ fontSize: '0.85em', flex: 1, overflow: 'hidden', textOverflow: 'ellipsis' }}>
                          {typeof v === 'object' ? JSON.stringify(v) : String(v)}
                        </code>
                        <button type="button" className="btn btn-danger btn-sm" onClick={() => removeKv(k)}>
                          删除
                        </button>
                      </li>
                    ))}
                  </ul>
                )}
              </div>
              <div>
                <div style={{ fontSize: '0.85em', color: 'var(--text-muted)', marginBottom: '0.5rem' }}>或一次性填入 JSON</div>
                <textarea
                  value={openConfigJson}
                  onChange={(e) => {
                    const v = e.target.value
                    setOpenConfigJson(v)
                    if (v.trim()) {
                      const parsed = validateJson(v)
                      setOpenConfigError(parsed === null ? '请输入有效的 JSON 对象' : '')
                      if (parsed !== null) setConfig((c) => ({ ...c, parameters: parsed }))
                    } else {
                      setOpenConfigError('')
                      setConfig((c) => ({ ...c, parameters: {} }))
                    }
                  }}
                  onBlur={() => {
                    if (openConfigJson.trim()) {
                      const parsed = validateJson(openConfigJson)
                      setOpenConfigError(parsed === null ? '开放配置必须是有效的 JSON 对象格式' : '')
                    }
                  }}
                  placeholder='{"key": "value"}'
                  rows={5}
                  style={{ fontFamily: 'monospace', fontSize: '0.9em' }}
                />
                {openConfigError && <div className="error" style={{ marginTop: '0.25rem' }}>{openConfigError}</div>}
              </div>
            </div>
          </>
        )}
        {error && <div className="error">{error}</div>}
        <div style={{ display: 'flex', gap: '0.5rem', marginTop: '1.5rem' }}>
          <button type="submit" className="btn" disabled={loading}>{loading ? '提交中...' : '保存'}</button>
          {isEdit && id ? (
            <button
              type="button"
              className="btn btn-secondary"
              disabled={loading}
              onClick={async () => {
                setError('')
                setLoading(true)
                try {
                  const created = await copyTool({
                    name,
                    description,
                    type,
                    config,
                  })
                  navigate(`/tools/${created.id}/edit`)
                } catch (e) {
                  setError((e as Error).message)
                } finally {
                  setLoading(false)
                }
              }}
            >
              复制为新工具
            </button>
          ) : null}
          <Link to="/tools" className="btn btn-secondary">取消</Link>
        </div>
        </form>
      </div>
    </div>
  )
}
