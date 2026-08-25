import { useEffect, useState, useCallback } from 'react'
import { useParams, Link } from 'react-router-dom'
import { agentApi, toolApi, mcpServerApi, memoryHubApi, RUNTIME_TOOL_FIELDS, type Agent, type Tool, type McpServer, type SkillMeta, type HubAsset, type HubLoadoutView, type HubBindingsView, type KnowledgeDraftItem } from '../api/client'
import { SearchableToolSelect } from '../components/SearchableToolSelect'

/** 绑定下拉预拉上限；本地模糊过滤，一般足够覆盖常用环境。 */
const TOOL_CATALOG_PAGE_SIZE = 100
const BIND_TOOL_PAGE_SIZE = 20
const MCP_CATALOG_PAGE_SIZE = 100

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
  const [hubLoadout, setHubLoadout] = useState<HubLoadoutView | null>(null)
  const [hubBindings, setHubBindings] = useState<HubBindingsView | null>(null)
  const [bindSkillName, setBindSkillName] = useState('')
  const [hubMsg, setHubMsg] = useState('')
  const [knowledgeDrafts, setKnowledgeDrafts] = useState<KnowledgeDraftItem[]>([])
  const [knowledgeOverwrite, setKnowledgeOverwrite] = useState(false)

  const loadHub = useCallback(async () => {
    if (!id) return
    try {
      const [lo, bi, draftsRes] = await Promise.all([
        memoryHubApi.loadout(id),
        memoryHubApi.bindings(id),
        memoryHubApi.listKnowledgeDrafts(id).catch(() => ({ drafts: [] as KnowledgeDraftItem[] })),
      ])
      setHubLoadout(lo)
      setHubBindings(bi)
      setKnowledgeDrafts(draftsRes.drafts || [])
    } catch {
      setHubLoadout(null)
      setHubBindings(null)
      setKnowledgeDrafts([])
    }
  }, [id])

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

  useEffect(() => {
    if (!id) return
    setLoading(true)
    agentApi
      .get(id)
      .then(async (a) => {
        setAgent(a)
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
      loadHub()
    }
  }, [id, agent, loadSkills, loadHub])

  useEffect(() => {
    if (agent) {
      loadToolCatalog()
      loadMcpCatalog()
    }
  }, [agent, loadToolCatalog, loadMcpCatalog])

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

  const handleBindHubSkill = async () => {
    if (!id || !bindSkillName.trim()) return
    setHubMsg('')
    try {
      const name = bindSkillName.trim()
      await memoryHubApi.bind(id, [{ kind: 'skill', id: name, name, hub: 'local' }])
      setBindSkillName('')
      await loadHub()
      setHubMsg('已绑定')
    } catch (e) {
      setHubMsg((e as Error).message)
    }
  }

  const handleUnbindHub = async (asset: HubAsset) => {
    if (!id) return
    setHubMsg('')
    try {
      await memoryHubApi.unbind(id, [asset])
      await loadHub()
    } catch (e) {
      setHubMsg((e as Error).message)
    }
  }

  const handleClearHubBindings = async () => {
    if (!id) return
    if (!window.confirm('清空全部显式 Hub Binding？切换治理面后旧 Loadout 可能失效。')) return
    setHubMsg('')
    try {
      const res = await memoryHubApi.clearBindings(id)
      await loadHub()
      setHubMsg(`已清除 ${res.cleared ?? 0} 条 Binding`)
    } catch (e) {
      setHubMsg((e as Error).message)
    }
  }

  const handleApproveHub = async (asset: HubAsset) => {
    if (!id) return
    setHubMsg('')
    try {
      await memoryHubApi.setStatus(id, asset, 'active')
      await loadHub()
      setHubMsg(`已确认激活 ${asset.id}`)
    } catch (e) {
      setHubMsg((e as Error).message)
    }
  }

  const handleApproveKnowledgeDraft = async (draft: KnowledgeDraftItem) => {
    if (!id) return
    setHubMsg('')
    try {
      await memoryHubApi.approveKnowledgeDraft(id, {
        source: draft.source,
        id: draft.id,
        overwrite: knowledgeOverwrite,
      })
      await loadHub()
      setHubMsg(`已批准 knowledge draft ${draft.id}`)
    } catch (e) {
      setHubMsg((e as Error).message)
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
        <h1>{agent.name}</h1>
        <div className="actions">
          <Link to="/agents" className="btn btn-secondary btn-sm">返回列表</Link>
          <Link to={`/agents/${id}/chat`} className="btn btn-sm">对话</Link>
          <Link to={`/agents/${id}/insights`} className="btn btn-secondary btn-sm">Insights</Link>
          <Link to={`/agents/${id}/edit`} className="btn btn-secondary btn-sm">编辑</Link>
        </div>
      </div>

      <section className="section">
        <h2 className="section-title">基本信息</h2>
        <div className="section-card">
          <div style={{ display: 'grid', gap: '0.75rem' }}>
            <p><strong>描述：</strong>{agent.description || '-'}</p>
            <p><strong>Workspace：</strong><code>{agent.workspace}</code></p>
            <p><strong>模型：</strong>{agent.model_config?.provider}/{agent.model_config?.model}</p>
            <p>
              <strong>源码分析模型：</strong>
              {agent.model_config?.code_model || agent.model_config?.code_provider
                ? `${agent.model_config?.code_provider || '跟随全局'}/${agent.model_config?.code_model || '跟随全局'}`
                : '跟随全局'}
            </p>
            <p><strong>最大输出 Token：</strong>{agent.model_config?.max_output_tokens && agent.model_config.max_output_tokens > 0 ? agent.model_config.max_output_tokens : '8192（默认）'}</p>
            <p><strong>调试运行：</strong>{agent.debug_run ? '是' : '否'}</p>
            <div data-testid="runtime-tools-section">
              <strong>运行时工具：</strong>
              {enabledRuntimeTools.length > 0 ? (
                <div data-testid="runtime-tools-badges" style={{ display: 'flex', flexWrap: 'wrap', gap: '0.35rem', marginTop: '0.35rem' }}>
                  {enabledRuntimeTools.map(({ key, label }) => (
                    <span key={key} className="badge badge-mcp">{label}</span>
                  ))}
                </div>
              ) : (
                <span style={{ marginLeft: '0.25rem' }}>未启用（仍可能由全局 env 开启）</span>
              )}
            </div>
            <p data-testid="hub-governance-display">
              <strong>Hub 治理：</strong>
              {agent.runtime_tools?.hub_governance || '跟随默认'}
            </p>
            <p data-testid="hub-knowledge-display">
              <strong>Hub 知识：</strong>
              {agent.runtime_tools?.hub_knowledge || '跟随默认'}
            </p>
            <p data-testid="hub-fallback-display">
              <strong>Hub 读回落：</strong>
              {agent.runtime_tools?.hub_fallback_to_default_on_read_error ? '开' : '关/跟随'}
            </p>
          </div>
        </div>
      </section>

      <section className="section" data-testid="hub-loadout-section">
        <h2 className="section-title">Memory Hub 配装</h2>
        <div className="section-card">
          <p style={{ color: 'var(--muted)', fontSize: '0.875rem', marginBottom: '0.75rem' }}>
            Loadout = 本机 skills + 显式 Binding（provider: {hubLoadout?.provider || '-'}）。切换治理面后请清空旧 Binding。
          </p>
          {hubMsg ? <p className="error" style={{ marginBottom: '0.75rem' }}>{hubMsg}</p> : null}
          <h3 style={{ fontSize: '0.95rem', marginBottom: '0.5rem' }}>当前 Loadout（{hubLoadout?.total ?? 0}）</h3>
          {hubLoadout && hubLoadout.items.length > 0 ? (
            <ul style={{ marginBottom: '1rem', paddingLeft: '1.25rem' }}>
              {hubLoadout.items.map((it) => (
                <li key={`${it.kind}:${it.id}`}>
                  <code>{it.kind}</code> {it.name || it.id} <span style={{ color: 'var(--muted)' }}>({it.hub})</span>
                </li>
              ))}
            </ul>
          ) : (
            <p style={{ color: 'var(--muted)', marginBottom: '1rem' }}>暂无 Loadout 项</p>
          )}
          <h3 style={{ fontSize: '0.95rem', marginBottom: '0.5rem' }}>显式 Binding（{hubBindings?.total ?? 0}）</h3>
          {hubBindings && hubBindings.items.length > 0 ? (
            <div className="table-card" style={{ marginBottom: '1rem', border: 'none' }}>
              <table>
                <thead>
                  <tr>
                    <th>Kind</th>
                    <th>ID</th>
                    <th>Hub</th>
                    <th>Status</th>
                    <th>操作</th>
                  </tr>
                </thead>
                <tbody>
                  {hubBindings.items.map((it) => (
                    <tr key={`${it.kind}:${it.id}`}>
                      <td>{it.kind}</td>
                      <td><code>{it.id}</code></td>
                      <td>{it.hub}</td>
                      <td>{it.status || '-'}</td>
                      <td style={{ display: 'flex', gap: '0.35rem', flexWrap: 'wrap' }}>
                        {it.status === 'draft' ? (
                          <button type="button" className="btn btn-sm" data-testid="hub-approve" onClick={() => handleApproveHub(it)}>
                            确认激活
                          </button>
                        ) : null}
                        <button type="button" className="btn btn-danger btn-sm" onClick={() => handleUnbindHub(it)}>解绑</button>
                      </td>
                    </tr>
                  ))}
                </tbody>
              </table>
            </div>
          ) : (
            <p style={{ color: 'var(--muted)', marginBottom: '1rem' }}>暂无显式 Binding</p>
          )}
          <div style={{ display: 'flex', gap: '0.5rem', alignItems: 'center', flexWrap: 'wrap' }}>
            <select
              data-testid="hub-bind-skill"
              value={bindSkillName}
              onChange={(e) => setBindSkillName(e.target.value)}
            >
              <option value="">从本机技能绑定…</option>
              {skills.map((s) => (
                <option key={s.name} value={s.name}>{s.name}</option>
              ))}
            </select>
            <button type="button" className="btn btn-sm" onClick={handleBindHubSkill} disabled={!bindSkillName}>绑定 Skill</button>
            <button type="button" className="btn btn-secondary btn-sm" onClick={handleClearHubBindings}>清空 Binding</button>
          </div>

          <div data-testid="hub-knowledge-drafts" style={{ marginTop: '1.25rem' }}>
            <h3 style={{ fontSize: '0.95rem', marginBottom: '0.5rem' }}>Knowledge drafts（{knowledgeDrafts.length}）</h3>
            <p style={{ color: 'var(--muted)', fontSize: '0.875rem', marginBottom: '0.75rem' }}>
              Wiki / units 草稿；Approve 写入正式知识面（与 skill Binding「确认激活」不同）。
            </p>
            <label style={{ display: 'inline-flex', alignItems: 'center', gap: '0.35rem', marginBottom: '0.75rem', fontSize: '0.875rem' }}>
              <input
                type="checkbox"
                data-testid="hub-knowledge-overwrite"
                checked={knowledgeOverwrite}
                onChange={(e) => setKnowledgeOverwrite(e.target.checked)}
              />
              overwrite（wiki 批准时覆盖已有页）
            </label>
            {knowledgeDrafts.length > 0 ? (
              <div className="table-card" style={{ marginBottom: 0, border: 'none' }}>
                <table>
                  <thead>
                    <tr>
                      <th>Source</th>
                      <th>ID</th>
                      <th>Preview</th>
                      <th>操作</th>
                    </tr>
                  </thead>
                  <tbody>
                    {knowledgeDrafts.map((d) => (
                      <tr key={`${d.source}:${d.id}`}>
                        <td>{d.source}</td>
                        <td><code>{d.id}</code></td>
                        <td style={{ color: 'var(--muted)', maxWidth: '20rem', overflow: 'hidden', textOverflow: 'ellipsis', whiteSpace: 'nowrap' }}>
                          {d.preview || d.title || '-'}
                        </td>
                        <td>
                          <button
                            type="button"
                            className="btn btn-sm"
                            data-testid={`hub-knowledge-approve-${d.source}-${d.id}`}
                            onClick={() => handleApproveKnowledgeDraft(d)}
                          >
                            Approve
                          </button>
                        </td>
                      </tr>
                    ))}
                  </tbody>
                </table>
              </div>
            ) : (
              <p style={{ color: 'var(--muted)', marginBottom: 0 }}>暂无 Knowledge drafts</p>
            )}
          </div>
        </div>
      </section>

      <section className="section">
        <h2 className="section-title">绑定工具</h2>
        <div className="section-card">
          {boundTools.length > 0 ? (
            <div className="table-card" style={{ marginBottom: '1rem', border: 'none' }}>
              <table>
                <thead>
                  <tr>
                    <th>名称</th>
                    <th>类型</th>
                    <th>操作</th>
                  </tr>
                </thead>
                <tbody>
                  {boundTools.map((t) => (
                    <tr key={t.id}>
                      <td>{t.name}</td>
                      <td><span className={`badge badge-${t.type}`}>{t.type}</span></td>
                      <td>
                        <button className="btn btn-danger btn-sm" onClick={() => handleUnbindTool(t.id)}>解绑</button>
                      </td>
                    </tr>
                  ))}
                </tbody>
              </table>
            </div>
          ) : (
            <p style={{ color: 'var(--muted)', marginBottom: '1rem' }}>暂无绑定工具</p>
          )}
          <div style={{ display: 'flex', gap: '0.5rem', alignItems: 'center', flexWrap: 'wrap' }}>
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
          <p style={{ color: 'var(--muted)', fontSize: '0.875rem', marginBottom: '0.75rem' }}>
            勾选要绑定的 MCP 服务后保存（全量替换）。可先到{' '}
            <Link to="/mcp-servers">MCP 服务</Link> 创建 stdio/HTTP 服务。
          </p>
          {mcpCatalog.length > 0 ? (
            <ul style={{ listStyle: 'none', padding: 0, margin: '0 0 1rem' }}>
              {mcpCatalog.map((s) => (
                <li key={s.id} style={{ marginBottom: '0.5rem' }}>
                  <label style={{ display: 'inline-flex', alignItems: 'center', gap: '0.5rem', cursor: 'pointer' }}>
                    <input
                      type="checkbox"
                      checked={selectedMcpIds.includes(s.id)}
                      onChange={() => toggleMcpServer(s.id)}
                    />
                    <strong>{s.name}</strong>
                    <code style={{ fontSize: '0.85em' }}>{s.id}</code>
                    <span className={`badge badge-${s.transport === 'stdio' ? 'mcp' : 'builtin'}`}>{s.transport}</span>
                    {s.description ? (
                      <span style={{ color: 'var(--muted)', fontSize: '0.85em' }}>{s.description}</span>
                    ) : null}
                  </label>
                </li>
              ))}
            </ul>
          ) : (
            <p style={{ color: 'var(--muted)', marginBottom: '1rem' }}>暂无可用 MCP 服务</p>
          )}
          <div style={{ display: 'flex', gap: '0.5rem', alignItems: 'center', flexWrap: 'wrap' }}>
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
            <div className="table-card" style={{ marginBottom: '1rem', border: 'none' }}>
              <table>
                <thead>
                  <tr>
                    <th>名称</th>
                    <th>描述</th>
                    <th>操作</th>
                  </tr>
                </thead>
                <tbody>
                  {skills.map((s) => (
                    <tr key={s.name}>
                      <td><code>{s.name}</code></td>
                      <td>{s.description || '-'}</td>
                      <td>
                        <button className="btn btn-danger btn-sm" onClick={() => handleDeleteSkill(s.name)}>删除</button>
                      </td>
                    </tr>
                  ))}
                </tbody>
              </table>
            </div>
          ) : (
            <p style={{ color: 'var(--muted)', marginBottom: '1rem' }}>暂无技能</p>
          )}
          <p style={{ color: 'var(--muted)', fontSize: '0.875rem', marginBottom: '1rem' }}>
            上传 .zip 压缩包，校验通过后解压到 <code>{agent.workspace}/skills/</code>
          </p>
          <div style={{ display: 'flex', gap: '0.5rem', alignItems: 'center', flexWrap: 'wrap' }}>
            <input
              type="file"
              accept=".zip"
              onChange={(e) => setSkillFile(e.target.files?.[0] || null)}
              style={{ color: 'var(--muted)', fontSize: '0.875rem' }}
            />
            <button className="btn btn-sm" onClick={handleUploadSkill} disabled={!skillFile}>上传</button>
          </div>
          {skillMsg && <div className={skillMsg.includes('成功') ? 'success' : 'error'} style={{ marginTop: '0.5rem' }}>{skillMsg}</div>}
        </div>
      </section>
    </div>
  )
}
