import { useEffect, useState, useCallback } from 'react'
import { useParams, Link } from 'react-router-dom'
import { agentApi, toolApi, mcpServerApi, proxyApi, RUNTIME_TOOL_FIELDS, type Agent, type Tool, type McpServer, type Proxy, type SkillMeta } from '../api/client'
import { getResourceByPayload, type ResourceInfo } from '../api/resource'
import ResourceGrantPanel from '../components/ResourceGrantPanel'
import { SearchableToolSelect } from '../components/SearchableToolSelect'

/** 绑定下拉预拉上限；本地模糊过滤，一般足够覆盖常用环境。 */
const TOOL_CATALOG_PAGE_SIZE = 100
const BIND_TOOL_PAGE_SIZE = 20
const MCP_CATALOG_PAGE_SIZE = 100

function boundToolIndex(t: Tool): string {
  const ds = t.config?.datasource
  if (ds?.type === 'elasticsearch' || ds?.type === 'es') return ds.default_index || ''
  return t.config?.rca?.default_index || ''
}
function boundToolPurpose(t: Tool): string {
  const ds = t.config?.datasource
  if (ds?.purpose) return ds.purpose
  return t.description || ''
}

export default function AgentDetail() {
  const { id } = useParams()
  const [agent, setAgent] = useState<Agent | null>(null)
  const [boundTools, setBoundTools] = useState<Tool[]>([])
  const [toolCatalog, setToolCatalog] = useState<Tool[]>([])
  const [catalogTotal, setCatalogTotal] = useState(0)
  const [catalogLoading, setCatalogLoading] = useState(false)
  const [toolSearch, setToolSearch] = useState('')
  const [skills, setSkills] = useState<SkillMeta[]>([])
  const [loading, setLoading] = useState(true)
  const [error, setError] = useState('')
  const [skillFile, setSkillFile] = useState<File | null>(null)
  const [skillMsg, setSkillMsg] = useState('')
  const [bindToolId, setBindToolId] = useState('')
  const [mcpCatalog, setMcpCatalog] = useState<McpServer[]>([])
  const [selectedMcpIds, setSelectedMcpIds] = useState<string[]>([])
  const [mcpBindSaving, setMcpBindSaving] = useState(false)
  const [mcpBindMsg, setMcpBindMsg] = useState('')
  const [proxies, setProxies] = useState<Proxy[]>([])
  const [selectedProxyId, setSelectedProxyId] = useState('')
  const [proxySaving, setProxySaving] = useState(false)
  const [proxyMsg, setProxyMsg] = useState('')
  const [resourceInfo, setResourceInfo] = useState<ResourceInfo | null>(null)

  const loadSkills = useCallback(async () => {
    if (!id) return
    try {
      const res = await agentApi.listSkills(id)
      setSkills(res.items || [])
    } catch {
      setSkills([])
    }
  }, [id])

  const loadToolCatalog = useCallback(async () => {
    setCatalogLoading(true)
    try {
      const res = await toolApi.list({ page: 1, page_size: TOOL_CATALOG_PAGE_SIZE })
      setToolCatalog(res.items)
      setCatalogTotal(res.total ?? res.items.length)
    } catch {
      setToolCatalog([])
      setCatalogTotal(0)
    } finally {
      setCatalogLoading(false)
    }
  }, [])

  const loadMcpCatalog = useCallback(async () => {
    try {
      const res = await mcpServerApi.list({ page: 1, page_size: MCP_CATALOG_PAGE_SIZE })
      setMcpCatalog(res.items)
    } catch {
      setMcpCatalog([])
    }
  }, [])

  const loadProxyCatalog = useCallback(async () => {
    try {
      const res = await proxyApi.list({ page: 1, page_size: 100, bindable: true })
      setProxies(res.items)
    } catch {
      setProxies([])
    }
  }, [])

  useEffect(() => {
    if (!id) return
    setLoading(true)
    agentApi
      .get(id)
      .then(async (a) => {
        setAgent(a)
        setSelectedProxyId(a.proxy_id || '')
        getResourceByPayload('agent', id!).then(setResourceInfo).catch(() => setResourceInfo(null))
        const mcpIds = a.mcp_server_ids ?? a.mcpServerIds ?? []
        setSelectedMcpIds(mcpIds)
        const ids = a.tool_ids ?? a.toolIds ?? []
        if (ids.length === 0) {
          setBoundTools([])
          return
        }
        const tools = await Promise.all(ids.map((tid) => toolApi.get(tid).catch(() => null)))
        setBoundTools(tools.filter((t): t is Tool => t != null))
      })
      .catch((e) => setError(e.message))
      .finally(() => setLoading(false))
  }, [id])

  useEffect(() => {
    if (id && agent) {
      loadSkills()
    }
  }, [id, agent, loadSkills])

  useEffect(() => {
    if (agent) {
      loadToolCatalog()
      loadMcpCatalog()
      loadProxyCatalog()
    }
  }, [agent, loadToolCatalog, loadMcpCatalog, loadProxyCatalog])

  const toolIds = agent?.tool_ids ?? agent?.toolIds ?? []

  const handleBindTool = async () => {
    if (!id || !bindToolId) return
    try {
      await agentApi.bindTools(id, [...toolIds, bindToolId])
      const selected = toolCatalog.find((t) => t.id === bindToolId)
      setAgent((prev) => (prev ? { ...prev, tool_ids: [...toolIds, bindToolId] } : null))
      if (selected) {
        setBoundTools((prev) => (prev.some((t) => t.id === selected.id) ? prev : [...prev, selected]))
      }
      setBindToolId('')
      setToolSearch('')
    } catch (e) {
      alert((e as Error).message)
    }
  }

  const handleUnbindTool = async (toolId: string) => {
    if (!id) return
    try {
      await agentApi.unbindTools(id, [toolId])
      setAgent((prev) =>
        prev
          ? { ...prev, tool_ids: (prev.tool_ids ?? prev.toolIds ?? []).filter((t) => t !== toolId) }
          : null,
      )
      setBoundTools((prev) => prev.filter((t) => t.id !== toolId))
    } catch (e) {
      alert((e as Error).message)
    }
  }

  const toggleMcpServer = (serverId: string) => {
    setSelectedMcpIds((prev) =>
      prev.includes(serverId) ? prev.filter((x) => x !== serverId) : [...prev, serverId],
    )
    setMcpBindMsg('')
  }

  const handleSaveDefaultProxy = async () => {
    if (!id) return
    setProxySaving(true)
    setProxyMsg('')
    try {
      const updated = await agentApi.update(id, { proxy_id: selectedProxyId || '' })
      setAgent((prev) => (prev ? { ...prev, proxy_id: updated.proxy_id || '' } : null))
      setSelectedProxyId(updated.proxy_id || '')
      setProxyMsg('默认出网代理已保存')
    } catch (e) {
      setProxyMsg((e as Error).message)
    } finally {
      setProxySaving(false)
    }
  }

  const handleSaveMcpBindings = async () => {
    if (!id) return
    setMcpBindSaving(true)
    setMcpBindMsg('')
    try {
      await agentApi.bindMcpServers(id, selectedMcpIds)
      setAgent((prev) => (prev ? { ...prev, mcp_server_ids: [...selectedMcpIds] } : null))
      setMcpBindMsg('MCP 服务绑定已保存')
    } catch (e) {
      setMcpBindMsg((e as Error).message)
    } finally {
      setMcpBindSaving(false)
    }
  }

  const handleUploadSkill = async () => {
    if (!id || !skillFile) {
      setSkillMsg('请选择技能压缩包')
      return
    }
    setSkillMsg('')
    try {
      const res = await agentApi.uploadSkillPackage(id, skillFile)
      setSkillMsg(res.success ? '上传成功' : res.message || '上传失败')
      if (res.success) {
        setSkillFile(null)
        loadSkills()
      }
    } catch (e) {
      setSkillMsg((e as Error).message)
    }
  }

  const handleDeleteSkill = async (skillName: string) => {
    if (!id) return
    if (!confirm(`确定删除技能「${skillName}」？此操作不可恢复。`)) return
    try {
      await agentApi.deleteSkill(id, skillName)
      setSkills((prev) => prev.filter((s) => s.name !== skillName))
    } catch (e) {
      alert((e as Error).message)
    }
  }

  if (loading) return (
    <div className="loading">
      <div className="loading-spinner" />
      <span style={{ marginLeft: '0.75rem' }}>加载中...</span>
    </div>
  )
  if (error || !agent) return <div className="error">{error || 'Agent 不存在'}</div>

  const boundToolIds = new Set(toolIds)
  const availableTools = toolCatalog.filter((t) => !boundToolIds.has(t.id))
  const enabledRuntimeTools = RUNTIME_TOOL_FIELDS.filter(({ key }) => agent.runtime_tools?.[key])

  return (
    <div>
      <div className="page-header">
        <div>
          <div className="page-title-row">
            <h1>{agent.name}</h1>
          </div>
          <p className="page-sub">{agent.description || '配置模型、工具、MCP 与出网代理。'}</p>
        </div>
        <div className="actions">
          <Link to="/agents" className="btn btn-secondary">返回列表</Link>
          <Link to={`/agents/${id}/chat`} className="btn">对话</Link>
          <Link to={`/agents/${id}/edit`} className="btn btn-secondary">编辑</Link>
        </div>
      </div>

      <section className="section">
        <h2 className="section-title">基本信息</h2>
        <div className="section-card">
          <div className="detail-kv">
            <div className="detail-kv__label">描述</div>
            <div className="detail-kv__value">{agent.description || '-'}</div>

            {resourceInfo ? (
              <>
                <div className="detail-kv__label">可见性</div>
                <div className="detail-kv__value">
                  <span className={`badge badge-${resourceInfo.visibility === 'org' ? 'mcp' : 'builtin'}`}>
                    {resourceInfo.visibility === 'org' ? 'Org 共享' : 'Private'}
                  </span>
                  {resourceInfo.home_org_id ? <code style={{ marginLeft: '0.5rem' }}>{resourceInfo.home_org_id}</code> : null}
                </div>
              </>
            ) : null}

            <div className="detail-kv__label">Workspace</div>
            <div className="detail-kv__value"><code>{agent.workspace}</code></div>

            <div className="detail-kv__label">模型</div>
            <div className="detail-kv__value">
              {agent.model_config?.provider}/{agent.model_config?.model}
              <span style={{ color: 'var(--muted)', marginLeft: '0.5rem' }}>
                · 最大输出 {agent.model_config?.max_output_tokens && agent.model_config.max_output_tokens > 0 ? agent.model_config.max_output_tokens : '8192（默认）'}
              </span>
            </div>

            <div className="detail-kv__label" data-testid="runtime-tools-section">运行时工具</div>
            <div className="detail-kv__value">
              {enabledRuntimeTools.length > 0 ? (
                <div className="detail-badge-row" data-testid="runtime-tools-badges">
                  {enabledRuntimeTools.map(({ key, label }) => (
                    <span key={key} className="badge badge-mcp">{label}</span>
                  ))}
                </div>
              ) : (
                <span style={{ color: 'var(--muted)' }}>未启用（仍可能由全局 env 开启）</span>
              )}
            </div>

            <div className="detail-kv__label" data-testid="default-proxy-section">出网代理</div>
            <div className="detail-kv__value detail-kv__value--stack">
              <p className="detail-kv__hint">
                空为直连。工具可在表单里继承、直连或指定代理。可先到{' '}
                <Link to="/proxies">代理</Link> 创建 HTTP/SOCKS5。
              </p>
              <div className="detail-kv__controls">
                <select
                  value={selectedProxyId}
                  onChange={(e) => {
                    setSelectedProxyId(e.target.value)
                    setProxyMsg('')
                  }}
                >
                  <option value="">直连</option>
                  {proxies.map((p) => (
                    <option key={p.id} value={p.id}>
                      {p.name}（{p.type} {p.host}:{p.port}）
                    </option>
                  ))}
                  {selectedProxyId && !proxies.some((p) => p.id === selectedProxyId) ? (
                    <option value={selectedProxyId}>{selectedProxyId}（当前）</option>
                  ) : null}
                </select>
                <button
                  type="button"
                  className="btn btn-sm"
                  onClick={handleSaveDefaultProxy}
                  disabled={proxySaving}
                >
                  {proxySaving ? '保存中...' : '保存'}
                </button>
                {proxyMsg ? (
                  <span className={proxyMsg.includes('已保存') ? 'success' : 'error'} style={{ fontSize: '0.875rem' }}>
                    {proxyMsg}
                  </span>
                ) : null}
              </div>
            </div>
          </div>
        </div>
      </section>

      <section className="section">
        <h2 className="section-title">绑定工具</h2>
        <div className="section-card">
          {boundTools.length > 0 ? (
            <div className="bind-list">
              {boundTools.map((t) => {
                const index = boundToolIndex(t)
                const purpose = boundToolPurpose(t)
                const meta = [index, purpose].filter(Boolean).join(' · ')
                return (
                  <div key={t.id} className="bind-row">
                    <div className="bind-row__name" title={t.name}>{t.name}</div>
                    <span className={`badge badge-${t.type}`}>{t.type}</span>
                    <div className="bind-row__meta" title={meta || undefined}>{meta || '—'}</div>
                    <div className="bind-row__actions">
                      <button className="btn btn-danger btn-sm" onClick={() => handleUnbindTool(t.id)}>解绑</button>
                    </div>
                  </div>
                )
              })}
            </div>
          ) : (
            <p style={{ color: 'var(--muted)', marginBottom: '1rem' }}>暂无绑定工具</p>
          )}
          <div className="section-card__footer">
            <SearchableToolSelect
              tools={availableTools}
              value={bindToolId}
              loading={catalogLoading}
              total={catalogTotal}
              pageSize={BIND_TOOL_PAGE_SIZE}
              searchValue={toolSearch}
              onSearchChange={(q) => {
                setToolSearch(q)
                setBindToolId('')
              }}
              onChange={setBindToolId}
            />
            <button className="btn btn-sm" onClick={handleBindTool} disabled={!bindToolId}>绑定</button>
          </div>
        </div>
      </section>

      <section className="section" data-testid="mcp-servers-bind-section">
        <h2 className="section-title">MCP 服务</h2>
        <div className="section-card">
          <p className="detail-kv__hint" style={{ marginBottom: '0.85rem' }}>
            勾选要绑定的 MCP 服务后保存（全量替换）。可先到{' '}
            <Link to="/mcp-servers">MCP 服务</Link> 创建 stdio/HTTP 服务。
          </p>
          {mcpCatalog.length > 0 ? (
            <ul className="mcp-bind-list">
              {mcpCatalog.map((s) => (
                <li key={s.id}>
                  <label className="mcp-bind-item">
                    <input
                      type="checkbox"
                      checked={selectedMcpIds.includes(s.id)}
                      onChange={() => toggleMcpServer(s.id)}
                    />
                    <span className="mcp-bind-item__name">{s.name}</span>
                    <span className={`badge badge-${s.transport === 'stdio' ? 'mcp' : 'builtin'}`}>{s.transport}</span>
                    {s.description ? (
                      <span className="mcp-bind-item__desc" title={s.description}>{s.description}</span>
                    ) : (
                      <code className="mcp-bind-item__id">{s.id}</code>
                    )}
                  </label>
                </li>
              ))}
            </ul>
          ) : (
            <p style={{ color: 'var(--muted)', marginBottom: '1rem' }}>暂无可用 MCP 服务</p>
          )}
          <div className="section-card__footer">
            <button
              type="button"
              className="btn btn-sm"
              onClick={handleSaveMcpBindings}
              disabled={mcpBindSaving}
            >
              {mcpBindSaving ? '保存中...' : '保存绑定'}
            </button>
            {mcpBindMsg ? (
              <span className={mcpBindMsg.includes('已保存') ? 'success' : 'error'} style={{ fontSize: '0.875rem' }}>
                {mcpBindMsg}
              </span>
            ) : null}
          </div>
        </div>
      </section>

      <section className="section">
        <h2 className="section-title">技能管理</h2>
        <div className="section-card">
          {skills.length > 0 ? (
            <div className="bind-list">
              {skills.map((s) => (
                <div key={s.name} className="bind-row bind-row--skill">
                  <div className="bind-row__name"><code>{s.name}</code></div>
                  <div className="bind-row__meta" title={s.description || undefined}>{s.description || '—'}</div>
                  <div className="bind-row__actions">
                    <button className="btn btn-danger btn-sm" onClick={() => handleDeleteSkill(s.name)}>删除</button>
                  </div>
                </div>
              ))}
            </div>
          ) : (
            <p style={{ color: 'var(--muted)', marginBottom: '1rem' }}>暂无技能</p>
          )}
          <p className="detail-kv__hint" style={{ marginBottom: '0.75rem' }}>
            上传 .zip 压缩包，校验通过后解压到 <code>{agent.workspace}/skills/</code>
          </p>
          <div className="section-card__footer">
            <input
              type="file"
              accept=".zip"
              onChange={(e) => setSkillFile(e.target.files?.[0] || null)}
              style={{ color: 'var(--muted)', fontSize: '0.875rem' }}
            />
            <button className="btn btn-sm" onClick={handleUploadSkill} disabled={!skillFile}>上传</button>
            {skillMsg ? (
              <span className={skillMsg.includes('成功') ? 'success' : 'error'} style={{ fontSize: '0.875rem' }}>
                {skillMsg}
              </span>
            ) : null}
          </div>
        </div>
      </section>

      {id ? <ResourceGrantPanel resourceType="agent" payloadRef={id} /> : null}
    </div>
  )
}
