import { useCallback, useEffect, useState } from 'react'
import { useParams, useNavigate, Link } from 'react-router-dom'
import {
  agentApi,
  channelApi,
  codeRootsApi,
  CODING_ASSISTANT_RUNTIME_TOOLS,
  RUNTIME_TOOL_FIELDS,
  serializeRuntimeTools,
  type Channel,
  type CodeRootBrowseEntry,
  type CreateAgentRequest,
  type ModelConfig,
  type RuntimeToolsConfig,
} from '../api/client'

const emptyRuntimeTools = (): RuntimeToolsConfig => ({})

function joinRootPath(root: string, path: string): string {
  const r = root.replace(/[/\\]+$/, '')
  const p = path.replace(/^[/\\]+/, '').replace(/[/\\]+$/, '')
  if (!p) return r
  return `${r}/${p}`
}

function workspaceUnderCodeRoots(ws: string, roots: string[]): boolean {
  const n = ws.replace(/[/\\]+$/, '').toLowerCase()
  if (!n) return false
  return roots.some((r) => {
    const root = r.replace(/[/\\]+$/, '').toLowerCase()
    return n === root || n.startsWith(`${root}/`) || n.startsWith(`${root}\\`)
  })
}

export default function AgentForm() {
  const { id } = useParams()
  const navigate = useNavigate()
  const isEdit = !!id

  const [name, setName] = useState('')
  const [description, setDescription] = useState('')
  const [systemPrompt, setSystemPrompt] = useState('')
  const [workspace, setWorkspace] = useState('')
  const [selectedTarget, setSelectedTarget] = useState('')
  const [existingLinkTarget, setExistingLinkTarget] = useState('')
  const [codeRoots, setCodeRoots] = useState<string[]>([])
  const [browseRoot, setBrowseRoot] = useState('')
  const [browsePath, setBrowsePath] = useState('')
  const [browseEntries, setBrowseEntries] = useState<CodeRootBrowseEntry[]>([])
  const [browseLoading, setBrowseLoading] = useState(false)
  const [browseError, setBrowseError] = useState('')
  const [modelConfig, setModelConfig] = useState<ModelConfig>({ provider: 'openai', model: 'gpt-4' })
  const [debugRun, setDebugRun] = useState(false)
  const [runtimeTools, setRuntimeTools] = useState<RuntimeToolsConfig>(emptyRuntimeTools())
  const [wecomChannelId, setWecomChannelId] = useState('')
  const [wecomChannels, setWecomChannels] = useState<Channel[]>([])
  const [error, setError] = useState('')
  const [loading, setLoading] = useState(false)

  const loadBrowse = useCallback(async (root: string, path = '') => {
    if (!root) {
      setBrowseEntries([])
      setBrowsePath('')
      return
    }
    setBrowseLoading(true)
    setBrowseError('')
    try {
      const res = await codeRootsApi.browse(root, path)
      setBrowseRoot(res.root || root)
      setBrowsePath(res.path ?? path)
      setBrowseEntries(res.entries || [])
    } catch (e) {
      setBrowseError((e as Error).message)
      setBrowseEntries([])
    } finally {
      setBrowseLoading(false)
    }
  }, [])

  useEffect(() => {
    channelApi.list({ type: 'wecom', page: 1, page_size: 100 })
      .then((res) => setWecomChannels(res.items))
      .catch(() => setWecomChannels([]))
    codeRootsApi.list()
      .then((res) => {
        const roots = res.roots || []
        setCodeRoots(roots)
        if (roots.length > 0) {
          setBrowseRoot(roots[0])
          void loadBrowse(roots[0], '')
        }
      })
      .catch(() => setCodeRoots([]))
  }, [loadBrowse])

  useEffect(() => {
    if (isEdit && id) {
      agentApi.get(id).then((a) => {
        setName(a.name)
        setDescription(a.description || '')
        setSystemPrompt(a.system_prompt || '')
        setWorkspace(a.workspace)
        setModelConfig(a.model_config || { provider: 'openai', model: 'gpt-4' })
        setDebugRun(a.debug_run ?? false)
        setRuntimeTools(a.runtime_tools ?? emptyRuntimeTools())
        setWecomChannelId(a.wecom_channel_id || '')
      }).catch((e) => setError(e.message))
      agentApi
        .workspaceLinkStatus(id)
        .then((st) => {
          const target = (st.target || '').trim()
          if (st.exists && target) {
            setExistingLinkTarget(target)
            setSelectedTarget((prev) => prev || target)
          } else {
            setExistingLinkTarget('')
          }
        })
        .catch(() => setExistingLinkTarget(''))
    }
  }, [id, isEdit])

  const toggleRuntimeTool = (key: keyof RuntimeToolsConfig, checked: boolean) => {
    setRuntimeTools((prev) => ({ ...prev, [key]: checked }))
  }

  const applyCodingPreset = () => {
    setRuntimeTools({ ...CODING_ASSISTANT_RUNTIME_TOOLS })
  }

  const breadcrumbParts = browsePath
    ? browsePath.split(/[/\\]/).filter(Boolean)
    : []

  const selectCurrentDir = () => {
    if (!browseRoot) return
    setSelectedTarget(joinRootPath(browseRoot, browsePath))
  }

  const retiredWholeRepo = isEdit && workspaceUnderCodeRoots(workspace, codeRoots)

  const handleSubmit = async (e: React.FormEvent) => {
    e.preventDefault()
    setError('')
    if (!name.trim()) {
      setError('请输入 Agent 名称')
      return
    }
    if (!modelConfig.provider || !modelConfig.model) {
      setError('请输入模型配置')
      return
    }
    setLoading(true)
    try {
      const data: CreateAgentRequest = {
        name: name.trim(),
        description: description.trim() || undefined,
        system_prompt: systemPrompt.trim() || undefined,
        workspace: retiredWholeRepo ? '' : workspace.trim(),
        model_config: modelConfig,
        debug_run: debugRun,
        runtime_tools: serializeRuntimeTools(runtimeTools),
        wecom_channel_id: wecomChannelId || (isEdit ? '' : undefined),
      }
      if (isEdit && id) {
        await agentApi.update(id, data)
        if (selectedTarget.trim()) {
          const next = selectedTarget.trim()
          const prev = existingLinkTarget.trim()
          const same =
            prev !== '' &&
            next.replace(/[/\\]+$/, '').toLowerCase() === prev.replace(/[/\\]+$/, '').toLowerCase()
          if (!same) {
            try {
              await agentApi.workspaceLink(id, next)
            } catch (linkErr) {
              setError(`Agent 已更新，但 workspace/code 链接失败：${(linkErr as Error).message}`)
              return
            }
          }
        }
      } else {
        const created = await agentApi.create(data)
        if (selectedTarget.trim()) {
          try {
            await agentApi.workspaceLink(created.id, selectedTarget.trim())
          } catch (linkErr) {
            setError(
              `Agent 已创建（id=${created.id}），但 workspace/code 链接失败：${(linkErr as Error).message}。可编辑该 Agent 后重试链接。`,
            )
            return
          }
        }
      }
      navigate('/agents')
    } catch (e) {
      setError((e as Error).message)
    } finally {
      setLoading(false)
    }
  }

  return (
    <div>
      <div className="page-header">
        <h1>{isEdit ? '编辑 Agent' : '新建 Agent'}</h1>
        <Link to="/agents" className="btn btn-secondary">返回</Link>
      </div>
      <div className="section-card" style={{ maxWidth: 640 }}>
        <form onSubmit={handleSubmit}>
          <section className="form-section">
            <h2 className="form-section__title">基本信息</h2>
            <div className="form-group">
              <label>名称 *</label>
              <input value={name} onChange={(e) => setName(e.target.value)} placeholder="如 my-agent" />
            </div>
            <div className="form-group">
              <label>描述</label>
              <textarea value={description} onChange={(e) => setDescription(e.target.value)} rows={2} placeholder="用途说明" />
            </div>
            <div className="form-group">
              <label>系统提示词</label>
              <textarea value={systemPrompt} onChange={(e) => setSystemPrompt(e.target.value)} rows={4} placeholder="角色与行为设定" />
            </div>

            <div className="form-group">
              <label>Workspace</label>
              <small style={{ display: 'block', marginBottom: 8 }}>
                可写目录由平台默认为 data_root/agents/{'{id}'}；代码根可选，保存后挂到 workspace/code。
                {isEdit ? ' 编辑时可不重选，将保留已有挂载。' : ' 新建可不选代码目录。'}
              </small>
              {retiredWholeRepo ? (
                <small style={{ color: 'var(--warning, #b45309)', display: 'block', marginBottom: 8 }}>
                  整仓作 Workspace 已退役。保存会改成默认可写根；可选再挂载 workspace/code。当前路径在保存前不能跑对话。
                </small>
              ) : null}
            </div>

            <div className="form-group">
              <label>浏览代码根</label>
              {codeRoots.length === 0 ? (
                <p className="form-panel__desc">未配置 code_roots，请手动填写工作空间路径。</p>
              ) : (
                <div className="form-panel">
                  <div className="form-group" style={{ marginBottom: 8 }}>
                    <label>代码根</label>
                    <select
                      value={browseRoot}
                      onChange={(e) => {
                        const root = e.target.value
                        setBrowseRoot(root)
                        void loadBrowse(root, '')
                      }}
                    >
                      {codeRoots.map((r) => (
                        <option key={r} value={r}>{r}</option>
                      ))}
                    </select>
                  </div>
                  <div style={{ fontSize: '0.85rem', marginBottom: 8, wordBreak: 'break-all' }}>
                    <button
                      type="button"
                      className="btn btn-secondary btn-sm"
                      style={{ marginRight: 6 }}
                      disabled={!browseRoot || browseLoading}
                      onClick={() => void loadBrowse(browseRoot, '')}
                    >
                      {browseRoot || '根'}
                    </button>
                    {breadcrumbParts.map((seg, i) => {
                      const sub = breadcrumbParts.slice(0, i + 1).join('/')
                      return (
                        <span key={sub}>
                          <span style={{ margin: '0 4px', color: 'var(--muted)' }}>/</span>
                          <button
                            type="button"
                            className="btn btn-secondary btn-sm"
                            disabled={browseLoading}
                            onClick={() => void loadBrowse(browseRoot, sub)}
                          >
                            {seg}
                          </button>
                        </span>
                      )
                    })}
                  </div>
                  {browseLoading ? (
                    <p className="form-panel__desc">加载中…</p>
                  ) : browseError ? (
                    <div className="error">{browseError}</div>
                  ) : (
                    <ul style={{ listStyle: 'none', padding: 0, margin: '0 0 8px', maxHeight: 200, overflow: 'auto' }}>
                      {browseEntries.length === 0 ? (
                        <li style={{ color: 'var(--muted)', fontSize: '0.85rem' }}>（空目录）</li>
                      ) : (
                        browseEntries.map((ent) => (
                          <li key={ent.path}>
                            <button
                              type="button"
                              className="btn btn-secondary btn-sm"
                              style={{ marginBottom: 4, width: '100%', textAlign: 'left' }}
                              onClick={() => void loadBrowse(browseRoot, ent.path)}
                            >
                              {ent.name}/
                            </button>
                          </li>
                        ))
                      )}
                    </ul>
                  )}
                  <button
                    type="button"
                    className="btn btn-secondary"
                    disabled={!browseRoot || browseLoading}
                    onClick={selectCurrentDir}
                  >
                    选择当前目录
                  </button>
                  {selectedTarget ? (
                    <p style={{ marginTop: 8, fontSize: '0.85rem', wordBreak: 'break-all' }}>
                      已选：{selectedTarget}
                      {existingLinkTarget &&
                      selectedTarget.replace(/[/\\]+$/, '').toLowerCase() ===
                        existingLinkTarget.replace(/[/\\]+$/, '').toLowerCase()
                        ? '（当前已挂载）'
                        : ''}
                    </p>
                  ) : isEdit && existingLinkTarget ? (
                    <p style={{ marginTop: 8, fontSize: '0.85rem', wordBreak: 'break-all' }}>
                      当前已挂载：{existingLinkTarget}（保存时可不改）
                    </p>
                  ) : null}
                </div>
              )}
            </div>

            <div className="form-group">
              <label>工作空间路径（高级 / 可留空）</label>
              <input
                value={workspace}
                onChange={(e) => setWorkspace(e.target.value)}
                placeholder="留空则服务端默认 data_root/agents/{id}"
              />
              <small>技能包解压到可写 workspace/skills/；不要把代码根填成 workspace。</small>
            </div>
          </section>

          <section className="form-section">
            <h2 className="form-section__title">模型配置</h2>
            <div className="form-row">
              <div className="form-group">
                <label>模型 Provider *</label>
                <input value={modelConfig.provider} onChange={(e) => setModelConfig((c) => ({ ...c, provider: e.target.value }))} placeholder="openai" />
              </div>
              <div className="form-group">
                <label>模型名称 *</label>
                <input value={modelConfig.model} onChange={(e) => setModelConfig((c) => ({ ...c, model: e.target.value }))} placeholder="gpt-4" />
              </div>
            </div>
            <div className="form-group">
              <label>API Key</label>
              <input type="password" value={modelConfig.api_key || ''} onChange={(e) => setModelConfig((c) => ({ ...c, api_key: e.target.value }))} placeholder="可选，加密存储" />
            </div>
            <div className="form-group">
              <label>Base URL</label>
              <input value={modelConfig.base_url || ''} onChange={(e) => setModelConfig((c) => ({ ...c, base_url: e.target.value }))} placeholder="可选" />
            </div>
            <div className="form-group">
              <label>最大输出 Token</label>
              <input
                type="number"
                min={256}
                max={32768}
                step={256}
                value={modelConfig.max_output_tokens ?? ''}
                onChange={(e) => {
                  const v = e.target.value.trim()
                  setModelConfig((c) => ({
                    ...c,
                    max_output_tokens: v === '' ? undefined : Math.max(0, parseInt(v, 10) || 0),
                  }))
                }}
                placeholder={`默认 ${8192}（长报告 / 多轮 web_search 建议 8192+）`}
              />
              <small>控制单次模型回复长度；留空使用服务端默认 8192。过小会导致报告写到一半截断。</small>
            </div>
          </section>

          <section className="form-section">
            <h2 className="form-section__title">运行与集成</h2>
            <div className="form-group">
              <label className="checkbox-field">
                <input type="checkbox" checked={debugRun} onChange={(e) => setDebugRun(e.target.checked)} />
                <span>调试运行（详细日志、request_id 等）</span>
              </label>
            </div>
            <div className="form-group">
              <label>企业微信群</label>
              <select value={wecomChannelId} onChange={(e) => setWecomChannelId(e.target.value)}>
                <option value="">不绑定</option>
                {wecomChannels.map((ch) => (
                  <option key={ch.id} value={ch.id}>{ch.channel_id}</option>
                ))}
              </select>
            </div>
            <div className="form-group">
              <div className="form-panel">
                <div className="form-panel__header">
                  <label>运行时工具（Hermes P0）</label>
                  <button type="button" className="btn btn-secondary btn-sm" data-testid="coding-assistant-preset" onClick={applyCodingPreset}>
                    编码助手预设
                  </button>
                </div>
                <p className="form-panel__desc">
                  与全局环境变量按 OR 合并；未勾选时仍可能因 env 启用。
                </p>
                <div className="checkbox-list">
                  {RUNTIME_TOOL_FIELDS.map(({ key, label, hint }) => (
                    <div key={key} className="checkbox-list__item">
                      <label className="checkbox-field">
                        <input
                          type="checkbox"
                          data-testid={`runtime-tool-${key}`}
                          checked={!!runtimeTools[key]}
                          onChange={(e) => toggleRuntimeTool(key, e.target.checked)}
                        />
                        <span>{label}</span>
                      </label>
                      {hint ? <small className="checkbox-list__hint">{hint}</small> : null}
                    </div>
                  ))}
                </div>
                <div className="form-group" style={{ marginTop: 16 }}>
                  <label>Memory Hub 治理面</label>
                  <select
                    data-testid="hub-governance"
                    value={runtimeTools.hub_governance || ''}
                    onChange={(e) =>
                      setRuntimeTools((prev) => {
                        const next = { ...prev }
                        if (!e.target.value) delete next.hub_governance
                        else next.hub_governance = e.target.value
                        return next
                      })
                    }
                  >
                    <option value="">跟随默认（local）</option>
                    <option value="local">local</option>
                  </select>
                </div>
                <div className="form-group">
                  <label>Memory Hub 知识面</label>
                  <select
                    data-testid="hub-knowledge"
                    value={runtimeTools.hub_knowledge || ''}
                    onChange={(e) =>
                      setRuntimeTools((prev) => {
                        const next = { ...prev }
                        if (!e.target.value) delete next.hub_knowledge
                        else next.hub_knowledge = e.target.value
                        return next
                      })
                    }
                  >
                    <option value="">跟随默认（local）</option>
                    <option value="local">local</option>
                  </select>
                </div>
                <div className="checkbox-list__item" style={{ marginTop: 8 }}>
                  <label className="checkbox-field">
                    <input
                      type="checkbox"
                      data-testid="hub-fallback"
                      checked={runtimeTools.hub_fallback_to_default_on_read_error === true}
                      onChange={(e) =>
                        setRuntimeTools((prev) => ({
                          ...prev,
                          hub_fallback_to_default_on_read_error: e.target.checked,
                        }))
                      }
                    />
                    <span>读失败时回落默认治理面</span>
                  </label>
                  <small className="checkbox-list__hint">对应 hub_fallback_to_default_on_read_error</small>
                </div>
              </div>
            </div>
          </section>

          {error && <div className="error">{error}</div>}
          <div className="form-actions">
            <button type="submit" className="btn" disabled={loading}>{loading ? '提交中...' : '保存'}</button>
            <Link to="/agents" className="btn btn-secondary">取消</Link>
          </div>
        </form>
      </div>
    </div>
  )
}
