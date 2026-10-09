import { useCallback, useEffect, useMemo, useState } from 'react'
import { agentApi, type Agent } from '../api/client'
import { repoApi, repoBindingApi, repoGroupApi } from '../api/repoRegistry'
import type { AgentRepoBinding, AgentRepoBindingsView, RepoGroupView, Repository } from '../api/repoRegistryTypes'
import { ConfirmDialog } from './ConfirmDialog'
import { SearchableChipSelect } from './SearchableChipSelect'
import {
  GROUP_KIND_LABELS,
  REPO_STATUS_LABELS,
  bindingsEqual,
  includeRepo,
  isGroupIncluded,
  isRepoExcluded,
  parseSubPaths,
  previewEffective,
  removeRepoBinding,
  toggleGroup,
  toggleRepoExclude,
  viaLabel,
} from '../utils/repoRegistry'
import '../pages/RepoRegistry.css'

const AGENT_PAGE_SIZE = 100

export default function RepoBindingsPanel({ agentId }: { agentId: string }) {
  const [view, setView] = useState<AgentRepoBindingsView | null>(null)
  const [draft, setDraft] = useState<AgentRepoBinding[]>([])
  const [groups, setGroups] = useState<RepoGroupView[]>([])
  const [repos, setRepos] = useState<Repository[]>([])
  const [agents, setAgents] = useState<Agent[]>([])
  const [loadError, setLoadError] = useState('')
  const [groupQuery, setGroupQuery] = useState('')
  const [expanded, setExpanded] = useState<Set<string>>(new Set())
  const [addRepoId, setAddRepoId] = useState('')
  const [addSubPaths, setAddSubPaths] = useState('')
  const [copyFromId, setCopyFromId] = useState('')
  const [confirm, setConfirm] = useState<'clear' | 'copy' | null>(null)
  const [saving, setSaving] = useState(false)
  const [msg, setMsg] = useState<{ ok: boolean; text: string } | null>(null)
  const [showDetail, setShowDetail] = useState(false)

  const load = useCallback(async () => {
    setLoadError('')
    try {
      const [v, g, r] = await Promise.all([repoBindingApi.get(agentId), repoGroupApi.list(), repoApi.list()])
      setView(v)
      setDraft(v.bindings)
      setGroups(g.items)
      setRepos(r.items)
    } catch (e) {
      setLoadError(`无法加载代码仓库绑定（需要该 Agent 的编辑权限）：${(e as Error).message}`)
    }
  }, [agentId])

  useEffect(() => {
    void load()
  }, [load])

  useEffect(() => {
    agentApi
      .list({ page: 1, page_size: AGENT_PAGE_SIZE })
      .then((r) => setAgents(r.items.filter((a) => a.id !== agentId)))
      .catch(() => setAgents([]))
  }, [agentId])

  const repoById = useMemo(() => new Map(repos.map((r) => [r.id, r])), [repos])
  const preview = useMemo(() => previewEffective(draft, groups, repos), [draft, groups, repos])
  const dirty = view ? !bindingsEqual(draft, view.bindings) : false
  const visibleGroups = useMemo(() => {
    const q = groupQuery.trim().toLowerCase()
    const list = q ? groups.filter((g) => g.name.toLowerCase().includes(q)) : groups
    return [...list].sort((a, b) => a.name.localeCompare(b.name))
  }, [groups, groupQuery])
  const repoOptions = useMemo(
    () =>
      repos
        .filter((r) => r.status === 'active')
        .map((r) => ({ value: r.id, label: r.rel_path, group: r.code_root })),
    [repos],
  )
  const singleBindings = draft.filter((b) => b.target_kind === 'repo')

  const repoLabel = (id: string) => repoById.get(id)?.rel_path ?? id

  const edit = (next: AgentRepoBinding[]) => {
    setDraft(next)
    setMsg(null)
  }

  const toggleExpanded = (id: string) =>
    setExpanded((prev) => {
      const next = new Set(prev)
      if (next.has(id)) next.delete(id)
      else next.add(id)
      return next
    })

  const addRepo = () => {
    if (!addRepoId) return
    edit(includeRepo(draft, addRepoId, parseSubPaths(addSubPaths)))
    setAddRepoId('')
    setAddSubPaths('')
  }

  const applyView = (v: AgentRepoBindingsView, text: string) => {
    setView(v)
    setDraft(v.bindings)
    setMsg({ ok: true, text })
  }

  const save = async (bindings: AgentRepoBinding[]) => {
    setSaving(true)
    setMsg(null)
    try {
      applyView(await repoBindingApi.replace(agentId, bindings), '已保存')
    } catch (e) {
      setMsg({ ok: false, text: (e as Error).message })
    } finally {
      setSaving(false)
      setConfirm(null)
    }
  }

  const copy = async () => {
    if (!copyFromId) return
    setSaving(true)
    setMsg(null)
    try {
      applyView(await repoBindingApi.copyFrom(agentId, copyFromId), '已复制')
      setCopyFromId('')
    } catch (e) {
      setMsg({ ok: false, text: (e as Error).message })
    } finally {
      setSaving(false)
      setConfirm(null)
    }
  }

  return (
    <section className="section" data-testid="repo-bindings-section">
      <h2 className="section-title">代码仓库</h2>
      <div className="section-card">
        {loadError ? <div className="error">{loadError}</div> : null}
        {!view && !loadError ? <p className="muted">加载中…</p> : null}
        {view ? (
          <>
            <p className="detail-kv__hint">
              {view.bindings.length === 0
                ? '尚未绑定仓库：RCA 代码工具使用 workspace/code 链接（旧方式）。保存绑定后以这里为准。'
                : 'RCA 代码工具只读取下列绑定展开后的仓库，不再使用 workspace/code 链接。'}
            </p>

            <div className="repo-bindings__summary" data-testid="repo-bindings-summary">
              <strong>
                {dirty ? '保存后' : '当前'}可见 {preview.length} 个仓库
              </strong>
              {dirty ? <span className="muted">（有未保存的修改）</span> : null}
              {draft.length > 0 && preview.length === 0 ? (
                <span className="repo-bindings__warn">当前绑定没有可用仓库，保存后该 Agent 的 RCA 代码工具不可用。</span>
              ) : null}
              <button type="button" className="btn btn-ghost btn-sm" onClick={() => setShowDetail((s) => !s)}>
                {showDetail ? '收起明细' : '查看明细'}
              </button>
            </div>
            {showDetail ? (
              <div className="table-card">
                <table>
                  <thead>
                    <tr>
                      <th>仓库</th>
                      <th>范围</th>
                      <th>来源</th>
                    </tr>
                  </thead>
                  <tbody>
                    {preview.map((p) => (
                      <tr key={p.repo.id}>
                        <td>
                          <code>{p.repo.rel_path}</code>
                        </td>
                        <td>{p.subPaths ? p.subPaths.join(', ') : '整个仓库'}</td>
                        <td>{viaLabel(p.via, groups)}</td>
                      </tr>
                    ))}
                  </tbody>
                </table>
              </div>
            ) : null}

            <h3 className="repo-bindings__title">按分组绑定</h3>
            <input value={groupQuery} onChange={(e) => setGroupQuery(e.target.value)} placeholder="搜索分组" />
            <ul className="repo-bindings__groups">
              {visibleGroups.map((g) => {
                const included = isGroupIncluded(draft, g.id)
                return (
                  <li key={g.id} className="repo-bindings__group">
                    <div className="repo-bindings__group-head">
                      <label>
                        <input
                          type="checkbox"
                          checked={included}
                          onChange={() => edit(toggleGroup(draft, g.id))}
                          data-testid={`repo-group-${g.id}`}
                        />
                        {g.name}
                      </label>
                      <span className="badge">{GROUP_KIND_LABELS[g.kind]}</span>
                      <span className="muted">{g.repo_ids.length} 个仓库</span>
                      <button type="button" className="btn btn-ghost btn-sm" onClick={() => toggleExpanded(g.id)}>
                        {expanded.has(g.id) ? '收起' : '展开'}
                      </button>
                    </div>
                    {expanded.has(g.id) ? (
                      <ul className="repo-bindings__members">
                        {g.repo_ids.map((id) => (
                          <li key={id}>
                            <label>
                              <input
                                type="checkbox"
                                disabled={!included}
                                checked={included && !isRepoExcluded(draft, id)}
                                onChange={() => edit(toggleRepoExclude(draft, id))}
                                data-testid={`repo-member-${g.id}-${id}`}
                              />
                              <code>{repoLabel(id)}</code>
                            </label>
                          </li>
                        ))}
                      </ul>
                    ) : null}
                  </li>
                )
              })}
              {visibleGroups.length === 0 ? <li className="muted">没有分组。先到「代码仓库」页扫描。</li> : null}
            </ul>

            <h3 className="repo-bindings__title">单独绑定与排除</h3>
            {singleBindings.length === 0 ? (
              <p className="muted">无</p>
            ) : (
              <ul className="repo-bindings__members">
                {singleBindings.map((b) => {
                  const repo = repoById.get(b.target_id)
                  return (
                    <li key={b.target_id}>
                      <span className={`badge ${b.mode === 'exclude' ? 'badge-repo-missing' : 'badge-repo-active'}`}>
                        {b.mode === 'exclude' ? '排除' : '包含'}
                      </span>
                      <code>{repoLabel(b.target_id)}</code>
                      {b.sub_paths?.length ? <span className="muted">子目录：{b.sub_paths.join(', ')}</span> : null}
                      {repo && repo.status !== 'active' ? (
                        <span className="muted">（{REPO_STATUS_LABELS[repo.status]}）</span>
                      ) : null}
                      <button
                        type="button"
                        className="btn btn-ghost btn-sm"
                        onClick={() => edit(removeRepoBinding(draft, b.target_id))}
                      >
                        移除
                      </button>
                    </li>
                  )
                })}
              </ul>
            )}
            <div className="repo-bindings__add">
              <SearchableChipSelect
                value={addRepoId}
                options={repoOptions}
                onChange={setAddRepoId}
                placeholder="选择仓库"
                searchPlaceholder="搜索仓库路径"
              />
              <input
                value={addSubPaths}
                onChange={(e) => setAddSubPaths(e.target.value)}
                placeholder="子目录（可选，逗号分隔）"
              />
              <button type="button" className="btn btn-sm" disabled={!addRepoId} onClick={addRepo}>
                添加
              </button>
            </div>

            <div className="section-card__footer">
              <button
                type="button"
                className="btn btn-sm"
                disabled={!dirty || saving}
                onClick={() => void save(draft)}
                data-testid="repo-bindings-save"
              >
                {saving ? '保存中...' : '保存绑定'}
              </button>
              <button
                type="button"
                className="btn btn-ghost btn-sm"
                disabled={!dirty || saving}
                onClick={() => edit(view.bindings)}
              >
                还原
              </button>
              <button
                type="button"
                className="btn btn-ghost btn-sm btn-danger"
                disabled={saving || view.bindings.length === 0}
                onClick={() => setConfirm('clear')}
              >
                清空绑定
              </button>
              <select value={copyFromId} onChange={(e) => setCopyFromId(e.target.value)}>
                <option value="">从其他 Agent 复制…</option>
                {agents.map((a) => (
                  <option key={a.id} value={a.id}>
                    {a.name}
                  </option>
                ))}
              </select>
              <button
                type="button"
                className="btn btn-ghost btn-sm"
                disabled={!copyFromId || saving}
                onClick={() => setConfirm('copy')}
              >
                复制
              </button>
              {msg ? (
                <span className={msg.ok ? 'success' : 'error'} style={{ fontSize: '0.875rem' }}>
                  {msg.text}
                </span>
              ) : null}
            </div>
          </>
        ) : null}
      </div>

      <ConfirmDialog
        open={confirm === 'clear'}
        title="清空仓库绑定"
        description="清空后该 Agent 回到旧方式：RCA 代码工具使用 workspace/code 链接。"
        confirmLabel="清空"
        cancelLabel="取消"
        variant="danger"
        loading={saving}
        onCancel={() => setConfirm(null)}
        onConfirm={() => void save([])}
      />
      <ConfirmDialog
        open={confirm === 'copy'}
        title="复制仓库绑定"
        description="用所选 Agent 的绑定整体替换当前绑定，未保存的修改会丢失。"
        confirmLabel="复制"
        cancelLabel="取消"
        loading={saving}
        onCancel={() => setConfirm(null)}
        onConfirm={() => void copy()}
      />
    </section>
  )
}
