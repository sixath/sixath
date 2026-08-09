import { useEffect, useState } from 'react'
import { useParams, useNavigate, Link } from 'react-router-dom'
import {
  agentApi,
  channelApi,
  memoryHubApi,
  CODING_ASSISTANT_RUNTIME_TOOLS,
  RUNTIME_TOOL_FIELDS,
  serializeRuntimeTools,
  type Channel,
  type CreateAgentRequest,
  type MemoryHubCatalog,
  type ModelConfig,
  type RuntimeToolsConfig,
} from '../api/client'

const emptyRuntimeTools = (): RuntimeToolsConfig => ({})

export default function AgentForm() {
  const { id } = useParams()
  const navigate = useNavigate()
  const isEdit = !!id

  const [name, setName] = useState('')
  const [description, setDescription] = useState('')
  const [systemPrompt, setSystemPrompt] = useState('')
  const [workspace, setWorkspace] = useState('')
  const [modelConfig, setModelConfig] = useState<ModelConfig>({ provider: 'openai', model: 'gpt-4' })
  const [debugRun, setDebugRun] = useState(false)
  const [runtimeTools, setRuntimeTools] = useState<RuntimeToolsConfig>(emptyRuntimeTools())
  const [wecomChannelId, setWecomChannelId] = useState('')
  const [wecomChannels, setWecomChannels] = useState<Channel[]>([])
  const [hubCatalog, setHubCatalog] = useState<MemoryHubCatalog | null>(null)
  const [initialHubGov, setInitialHubGov] = useState<string>('')
  const [clearBindingsOnSave, setClearBindingsOnSave] = useState(false)
  const [error, setError] = useState('')
  const [loading, setLoading] = useState(false)

  useEffect(() => {
    channelApi.list({ type: 'wecom', page: 1, page_size: 100 })
      .then((res) => setWecomChannels(res.items))
      .catch(() => setWecomChannels([]))
    memoryHubApi.catalog()
      .then(setHubCatalog)
      .catch(() => setHubCatalog({ defaults: { governance: 'local', knowledge: 'local' }, governance: ['local'], knowledge: ['local'] }))
  }, [])

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
        setInitialHubGov(a.runtime_tools?.hub_governance || '')
        setClearBindingsOnSave(false)
        setWecomChannelId(a.wecom_channel_id || '')
      }).catch((e) => setError(e.message))
    }
  }, [id, isEdit])

  const toggleRuntimeTool = (key: keyof RuntimeToolsConfig, checked: boolean) => {
    setRuntimeTools((prev) => ({ ...prev, [key]: checked }))
  }

  const applyCodingPreset = () => {
    setRuntimeTools({ ...CODING_ASSISTANT_RUNTIME_TOOLS })
  }

  const handleSubmit = async (e: React.FormEvent) => {
    e.preventDefault()
    setError('')
    if (!name.trim()) {
      setError('请输入 Agent 名称')
      return
    }
    if (!workspace.trim()) {
      setError('请输入工作空间路径')
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
        workspace: workspace.trim(),
        model_config: modelConfig,
        debug_run: debugRun,
        runtime_tools: serializeRuntimeTools(runtimeTools),
        wecom_channel_id: wecomChannelId || (isEdit ? '' : undefined),
      }
      if (isEdit && id) {
        await agentApi.update(id, data)
        if (clearBindingsOnSave) {
          await memoryHubApi.clearBindings(id)
        }
      } else {
        await agentApi.create(data)
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
              <label>工作空间路径 *</label>
              <input value={workspace} onChange={(e) => setWorkspace(e.target.value)} placeholder="/data/agents/my-agent" />
              <small>技能包将解压到 workspace/skills/</small>
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
                    <option value="">跟随默认（{hubCatalog?.defaults.governance || 'local'}）</option>
                    {(hubCatalog?.governance || ['local']).map((name) => (
                      <option key={name} value={name}>{name}</option>
                    ))}
                  </select>
                  {isEdit && (runtimeTools.hub_governance || '') !== initialHubGov ? (
                    <div style={{ marginTop: 8 }}>
                      <p style={{ color: 'var(--muted)', fontSize: '0.85rem', marginBottom: 4 }}>
                        治理面已变更：旧 Loadout Binding 可能失效。
                      </p>
                      <label className="checkbox-field">
                        <input
                          type="checkbox"
                          data-testid="hub-clear-on-save"
                          checked={clearBindingsOnSave}
                          onChange={(e) => setClearBindingsOnSave(e.target.checked)}
                        />
                        <span>保存时清空该 Agent 的显式 Hub Binding</span>
                      </label>
                    </div>
                  ) : null}
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
                    <option value="">跟随默认（{hubCatalog?.defaults.knowledge || 'local'}）</option>
                    {(hubCatalog?.knowledge || ['local']).map((name) => (
                      <option key={name} value={name}>{name}</option>
                    ))}
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
