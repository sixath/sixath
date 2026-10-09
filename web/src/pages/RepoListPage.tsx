import { useCallback, useEffect, useMemo, useRef, useState, type FormEvent } from 'react'
import { Link } from 'react-router-dom'
import { modelCatalogApi } from '../api/client'
import { handbookConfigApi, repoApi, repoGroupApi } from '../api/repoRegistry'
import type { HandbookConfigView, RepoFilter, RepoGroupView, RepoScanReport, Repository } from '../api/repoRegistryTypes'
import { ConfirmDialog } from '../components/ConfirmDialog'
import { FormDialog } from '../components/FormDialog'
import { HandbookDialog } from '../components/HandbookDialog'
import { RepoRegistryTabs } from '../components/RepoRegistryTabs'
import {
  HANDBOOK_STATE_LABELS,
  LLM_STATE_LABELS,
  REPO_STATUS_LABELS,
  anyHandbookBuildActive,
  groupNamesByRepo,
  handbookModelOptions,
  handbookState,
  llmProgress,
  llmState,
  parseTags,
  shortCommit,
  type ModelOption,
} from '../utils/repoRegistry'
import './RepoRegistry.css'

const HANDBOOK_POLL_MS = 3000
const NO_HANDBOOK_CONFIG: HandbookConfigView = { model: '', enabled: false }

interface EditState {
  repo: Repository
  name: string
  description: string
  tags: string
  handbookModel: string
  agentIds: string[] | null
}

function formatTime(iso?: string): string {
  if (!iso) return '—'
  const d = new Date(iso)
  return Number.isNaN(d.getTime()) ? iso : d.toLocaleString()
}

function LLMBadge({ repo, globalModel }: { repo: Repository; globalModel: string }) {
  const state = llmState(repo, globalModel)
  const progress = state === 'off' ? '' : llmProgress(repo.handbook_llm)
  return (
    <div className="repo-llm-badge">
      <span
        className={`badge badge-llm-${state}`}
        data-testid={`llm-state-${repo.id}`}
        title={state === 'off' ? '' : (repo.handbook_llm?.last_error ?? '')}
      >
        LLM {LLM_STATE_LABELS[state]}
        {progress ? ` ${progress}` : ''}
      </span>
    </div>
  )
}

export default function RepoListPage() {
  const [repos, setRepos] = useState<Repository[]>([])
  const [groups, setGroups] = useState<RepoGroupView[]>([])
  const [knownRoots, setKnownRoots] = useState<string[]>([])
  const [draft, setDraft] = useState<RepoFilter>({})
  const [filter, setFilter] = useState<RepoFilter>({})
  const [loading, setLoading] = useState(true)
  const [error, setError] = useState('')
  const [scanning, setScanning] = useState(false)
  const [scanReport, setScanReport] = useState<RepoScanReport | null>(null)
  const [scanError, setScanError] = useState('')
  const [edit, setEdit] = useState<EditState | null>(null)
  const [editSaving, setEditSaving] = useState(false)
  const [editError, setEditError] = useState('')
  const [pendingStatus, setPendingStatus] = useState<Repository | null>(null)
  const [statusSaving, setStatusSaving] = useState(false)
  const [viewing, setViewing] = useState<Repository | null>(null)
  const [rebuildingId, setRebuildingId] = useState<string | null>(null)
  const [rebuildError, setRebuildError] = useState('')
  const [handbookConfig, setHandbookConfig] = useState<HandbookConfigView>(NO_HANDBOOK_CONFIG)
  const [modelOptions, setModelOptions] = useState<ModelOption[]>(() => handbookModelOptions([], []))

  const filterRef = useRef(filter)
  const loadSeq = useRef(0)

  const loadRepos = useCallback(async (f: RepoFilter, quiet = false) => {
    const seq = ++loadSeq.current
    if (!quiet) {
      setLoading(true)
      setError('')
    }
    try {
      const res = await repoApi.list(f)
      if (seq !== loadSeq.current) return
      setError('')
      setRepos(res.items)
      setKnownRoots((prev) => [...new Set([...prev, ...res.items.map((r) => r.code_root)])].sort())
    } catch (e) {
      if (seq === loadSeq.current) setError((e as Error).message)
    } finally {
      if (seq === loadSeq.current) setLoading(false)
    }
  }, [])

  const loadGroups = useCallback(() => {
    repoGroupApi
      .list()
      .then((r) => setGroups(r.items))
      .catch(() => setGroups([]))
  }, [])

  useEffect(() => {
    filterRef.current = filter
    void loadRepos(filter)
  }, [filter, loadRepos])

  useEffect(() => {
    loadGroups()
  }, [loadGroups])

  useEffect(() => {
    handbookConfigApi
      .get()
      .then(setHandbookConfig)
      .catch(() => setHandbookConfig(NO_HANDBOOK_CONFIG))
  }, [])

  const globalModel = handbookConfig.model ?? ''

  const building = useMemo(() => anyHandbookBuildActive(repos), [repos])
  useEffect(() => {
    if (!building) return
    const timer = window.setInterval(() => void loadRepos(filterRef.current, true), HANDBOOK_POLL_MS)
    return () => window.clearInterval(timer)
  }, [building, loadRepos])

  const groupNames = useMemo(() => groupNamesByRepo(groups), [groups])

  const submitFilter = (e: FormEvent) => {
    e.preventDefault()
    setFilter({ ...draft })
  }

  const resetFilter = () => {
    setDraft({})
    setFilter({})
  }

  const runScan = async () => {
    setScanning(true)
    setScanError('')
    setScanReport(null)
    try {
      setScanReport(await repoApi.scan())
      await loadRepos(filterRef.current)
      loadGroups()
    } catch (e) {
      setScanError((e as Error).message)
    } finally {
      setScanning(false)
    }
  }

  const openEdit = (repo: Repository) => {
    setEditError('')
    setEdit({
      repo,
      name: repo.name,
      description: repo.description,
      tags: repo.tags.join(', '),
      handbookModel: repo.handbook_model ?? '',
      agentIds: null,
    })
    repoApi
      .get(repo.id)
      .then((d) => setEdit((prev) => (prev && prev.repo.id === repo.id ? { ...prev, agentIds: d.agent_ids } : prev)))
      .catch(() => {})
    Promise.all([modelCatalogApi.listProviders(), modelCatalogApi.listCatalog()])
      .then(([providers, entries]) => setModelOptions(handbookModelOptions(providers, entries)))
      .catch(() => setModelOptions(handbookModelOptions([], [])))
  }

  const saveEdit = async () => {
    if (!edit) return
    setEditSaving(true)
    setEditError('')
    try {
      const updated = await repoApi.patch(edit.repo.id, {
        name: edit.name.trim(),
        description: edit.description.trim(),
        tags: parseTags(edit.tags),
        handbook_model: edit.handbookModel.trim(),
      })
      setRepos((prev) => prev.map((r) => (r.id === updated.id ? updated : r)))
      setEdit(null)
    } catch (e) {
      setEditError((e as Error).message)
    } finally {
      setEditSaving(false)
    }
  }

  const confirmStatus = async () => {
    if (!pendingStatus) return
    setStatusSaving(true)
    try {
      await repoApi.patch(pendingStatus.id, {
        status: pendingStatus.status === 'archived' ? 'active' : 'archived',
      })
      setPendingStatus(null)
      await loadRepos(filterRef.current)
    } catch (e) {
      alert((e as Error).message)
    } finally {
      setStatusSaving(false)
    }
  }

  const requestRebuild = async (repo: Repository) => {
    setRebuildingId(repo.id)
    setRebuildError('')
    try {
      await repoApi.rebuildHandbook(repo.id)
      setRepos((prev) =>
        prev.map((r) =>
          r.id === repo.id ? { ...r, handbook_status: 'building', handbook_lease_until: undefined } : r,
        ),
      )
    } catch (e) {
      setRebuildError(`重建 ${repo.rel_path} 失败：${(e as Error).message}`)
    } finally {
      setRebuildingId(null)
    }
  }

  const restoring = pendingStatus?.status === 'archived'

  return (
    <div>
      <div className="page-header">
        <div>
          <div className="page-title-row">
            <h1>代码仓库</h1>
            <span className="page-count">{repos.length}</span>
          </div>
          <p className="page-sub">code root 下自动发现的 git 仓库。启动时和每 10 分钟扫描一次，HEAD 变化后自动重建 handbook。</p>
        </div>
        <button type="button" className="btn" onClick={() => void runScan()} disabled={scanning}>
          {scanning ? '扫描中…' : '立即扫描'}
        </button>
      </div>
      <RepoRegistryTabs />

      {scanError ? <div className="error repo-scan-report">扫描失败：{scanError}</div> : null}
      {scanReport ? (
        <div className="success repo-scan-report" data-testid="repo-scan-report">
          扫描完成：code root {scanReport.roots} 个，发现 {scanReport.found}，新增 {scanReport.added}，恢复{' '}
          {scanReport.restored}，缺失 {scanReport.missing}
          {scanReport.errors?.length ? (
            <ul>
              {scanReport.errors.map((msg) => (
                <li key={msg} className="error">
                  {msg}
                </li>
              ))}
            </ul>
          ) : null}
        </div>
      ) : null}

      <form className="filter-bar" onSubmit={submitFilter}>
        <select value={draft.status ?? ''} onChange={(e) => setDraft({ ...draft, status: e.target.value })}>
          <option value="">全部状态</option>
          <option value="active">{REPO_STATUS_LABELS.active}</option>
          <option value="missing">{REPO_STATUS_LABELS.missing}</option>
          <option value="archived">{REPO_STATUS_LABELS.archived}</option>
        </select>
        <select value={draft.code_root ?? ''} onChange={(e) => setDraft({ ...draft, code_root: e.target.value })}>
          <option value="">全部 code root</option>
          {knownRoots.map((r) => (
            <option key={r} value={r}>
              {r}
            </option>
          ))}
        </select>
        <select value={draft.group_id ?? ''} onChange={(e) => setDraft({ ...draft, group_id: e.target.value })}>
          <option value="">全部分组</option>
          {groups.map((g) => (
            <option key={g.id} value={g.id}>
              {g.name}
            </option>
          ))}
        </select>
        <input
          value={draft.q ?? ''}
          onChange={(e) => setDraft({ ...draft, q: e.target.value })}
          placeholder="搜索路径或名称"
        />
        <button type="submit" className="btn btn-sm">
          查询
        </button>
        <button type="button" className="btn btn-ghost btn-sm" onClick={resetFilter}>
          重置
        </button>
      </form>

      {error ? <div className="error">加载失败：{error}</div> : null}
      {rebuildError ? (
        <div className="error" role="alert" data-testid="repo-rebuild-error">
          {rebuildError}
        </div>
      ) : null}
      {loading ? (
        <div className="loading">
          <div className="loading-spinner" />
          <span style={{ marginLeft: '0.75rem' }}>加载中…</span>
        </div>
      ) : repos.length === 0 ? (
        <div className="section-card empty-state">
          <p>没有符合条件的仓库。新部署时先点「立即扫描」。</p>
        </div>
      ) : (
        <div className="table-card">
          <table>
            <thead>
              <tr>
                <th>仓库</th>
                <th>分组</th>
                <th>标签</th>
                <th>分支 / 提交</th>
                <th>状态</th>
                <th>Handbook</th>
                <th>最近扫描</th>
                <th className="col-actions">操作</th>
              </tr>
            </thead>
            <tbody>
              {repos.map((r) => (
                <tr key={r.id}>
                  <td>
                    <code>{r.rel_path}</code>
                    {r.name && r.name !== r.rel_path ? <div className="muted">{r.name}</div> : null}
                    <div className="muted" title={r.code_root}>
                      {r.code_root}
                    </div>
                  </td>
                  <td>
                    <div className="cell-list">
                      {(groupNames.get(r.id) ?? []).map((n) => (
                        <span key={n} className="badge">
                          {n}
                        </span>
                      ))}
                    </div>
                  </td>
                  <td>
                    <div className="cell-list">
                      {r.tags.map((t) => (
                        <span key={t} className="badge">
                          {t}
                        </span>
                      ))}
                    </div>
                  </td>
                  <td>
                    {r.git_branch || '—'} <code title={r.head_commit}>{shortCommit(r.head_commit)}</code>
                  </td>
                  <td>
                    <span className={`badge badge-repo-${r.status}`}>{REPO_STATUS_LABELS[r.status]}</span>
                  </td>
                  <td>
                    <span
                      className={`badge badge-handbook-${handbookState(r)}`}
                      title={r.handbook_stats?.last_error ?? ''}
                      data-testid={`handbook-state-${r.id}`}
                    >
                      {HANDBOOK_STATE_LABELS[handbookState(r)]}
                    </span>
                    <LLMBadge repo={r} globalModel={globalModel} />
                  </td>
                  <td>{formatTime(r.last_scanned_at)}</td>
                  <td className="col-actions">
                    <div className="row-actions">
                      <button
                        type="button"
                        className="btn btn-ghost btn-sm"
                        aria-label={`查看 ${r.rel_path} 的 handbook`}
                        disabled={!r.handbook_version}
                        onClick={() => setViewing(r)}
                      >
                        Handbook
                      </button>
                      <button
                        type="button"
                        className="btn btn-ghost btn-sm"
                        aria-label={`重建 ${r.rel_path} 的 handbook`}
                        disabled={r.status !== 'active' || rebuildingId === r.id}
                        onClick={() => void requestRebuild(r)}
                      >
                        重建
                      </button>
                      <button type="button" className="btn btn-ghost btn-sm" onClick={() => openEdit(r)}>
                        编辑
                      </button>
                      <button
                        type="button"
                        className={`btn btn-ghost btn-sm ${r.status === 'archived' ? '' : 'btn-danger'}`}
                        onClick={() => setPendingStatus(r)}
                      >
                        {r.status === 'archived' ? '恢复' : '归档'}
                      </button>
                    </div>
                  </td>
                </tr>
              ))}
            </tbody>
          </table>
        </div>
      )}

      <FormDialog
        open={!!edit}
        title={edit ? `编辑仓库 ${edit.repo.rel_path}` : ''}
        loading={editSaving}
        error={editError}
        onCancel={() => setEdit(null)}
        onConfirm={() => void saveEdit()}
      >
        {edit ? (
          <>
            <div className="form-group">
              <label>名称</label>
              <input value={edit.name} onChange={(e) => setEdit({ ...edit, name: e.target.value })} />
            </div>
            <div className="form-group">
              <label>描述</label>
              <textarea
                rows={3}
                value={edit.description}
                onChange={(e) => setEdit({ ...edit, description: e.target.value })}
              />
            </div>
            <div className="form-group">
              <label>标签（逗号分隔）</label>
              <input value={edit.tags} onChange={(e) => setEdit({ ...edit, tags: e.target.value })} />
            </div>
            <div className="form-group">
              <label htmlFor="repo-handbook-model">Handbook 模型</label>
              <input
                id="repo-handbook-model"
                list="handbook-model-options"
                value={edit.handbookModel}
                placeholder={globalModel ? `继承全局（${globalModel}）` : '未配置全局模型'}
                onChange={(e) => setEdit({ ...edit, handbookModel: e.target.value })}
              />
              <datalist id="handbook-model-options">
                {modelOptions.map((o) => (
                  <option key={o.value} value={o.value}>
                    {o.label}
                  </option>
                ))}
              </datalist>
              <span className="muted">留空继承全局，填 off 禁用该仓库的 LLM 增强；格式为模型名或「提供方/模型名」。</span>
            </div>
            <div className="form-group">
              <label>使用该仓库的 Agent</label>
              {edit.agentIds === null ? (
                <span className="muted">加载中…</span>
              ) : edit.agentIds.length === 0 ? (
                <span className="muted">无</span>
              ) : (
                <div className="cell-list">
                  {edit.agentIds.map((id) => (
                    <Link key={id} to={`/agents/${id}`} className="badge">
                      {id}
                    </Link>
                  ))}
                </div>
              )}
            </div>
          </>
        ) : null}
      </FormDialog>

      <ConfirmDialog
        open={!!pendingStatus}
        title={restoring ? '恢复仓库' : '归档仓库'}
        description={
          pendingStatus
            ? restoring
              ? `恢复「${pendingStatus.rel_path}」后，绑定了它的 Agent 会重新看到它（目录仍存在时）。`
              : `归档「${pendingStatus.rel_path}」后，它会从所有 Agent 的有效仓库中移除，扫描不会自动恢复。`
            : ''
        }
        confirmLabel={restoring ? '恢复' : '归档'}
        cancelLabel="取消"
        variant={restoring ? 'default' : 'danger'}
        loading={statusSaving}
        onCancel={() => setPendingStatus(null)}
        onConfirm={() => void confirmStatus()}
      />
      <HandbookDialog
        repo={viewing}
        onClose={() => setViewing(null)}
        onEnrichStarted={() => void loadRepos(filterRef.current, true)}
      />
    </div>
  )
}
