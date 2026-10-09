import { Fragment, useCallback, useEffect, useMemo, useState } from 'react'
import { repoApi, repoGroupApi } from '../api/repoRegistry'
import type { RepoGroupKind, RepoGroupView, Repository } from '../api/repoRegistryTypes'
import { ConfirmDialog } from '../components/ConfirmDialog'
import { FormDialog } from '../components/FormDialog'
import { RepoPicker } from '../components/RepoPicker'
import { RepoRegistryTabs } from '../components/RepoRegistryTabs'
import { GROUP_KIND_LABELS, REPO_STATUS_LABELS } from '../utils/repoRegistry'
import './RepoRegistry.css'

type DialogState =
  | { mode: 'create'; name: string; repoIds: string[] }
  | { mode: 'edit'; group: RepoGroupView; repoIds: string[] }

function ruleText(g: RepoGroupView): string {
  if (!g.rule?.rel_prefix) return '—'
  return g.rule.code_root ? `${g.rule.code_root} / ${g.rule.rel_prefix}` : g.rule.rel_prefix
}

export default function RepoGroupListPage() {
  const [groups, setGroups] = useState<RepoGroupView[]>([])
  const [repos, setRepos] = useState<Repository[]>([])
  const [kind, setKind] = useState<RepoGroupKind | ''>('')
  const [loading, setLoading] = useState(true)
  const [error, setError] = useState('')
  const [notice, setNotice] = useState('')
  const [expanded, setExpanded] = useState<Set<string>>(new Set())
  const [dialog, setDialog] = useState<DialogState | null>(null)
  const [dialogSaving, setDialogSaving] = useState(false)
  const [dialogError, setDialogError] = useState('')
  const [pendingDelete, setPendingDelete] = useState<RepoGroupView | null>(null)
  const [deleting, setDeleting] = useState(false)

  const load = useCallback(async () => {
    setLoading(true)
    setError('')
    try {
      const [g, r] = await Promise.all([repoGroupApi.list(kind || undefined), repoApi.list()])
      setGroups(g.items)
      setRepos(r.items)
    } catch (e) {
      setError((e as Error).message)
    } finally {
      setLoading(false)
    }
  }, [kind])

  useEffect(() => {
    void load()
  }, [load])

  const repoById = useMemo(() => new Map(repos.map((r) => [r.id, r])), [repos])

  const toggleExpanded = (id: string) =>
    setExpanded((prev) => {
      const next = new Set(prev)
      if (next.has(id)) next.delete(id)
      else next.add(id)
      return next
    })

  const openCreate = () => {
    setDialogError('')
    setDialog({ mode: 'create', name: '', repoIds: [] })
  }

  const openEdit = (group: RepoGroupView) => {
    setDialogError('')
    setDialog({ mode: 'edit', group, repoIds: [...group.repo_ids] })
  }

  const saveDialog = async () => {
    if (!dialog) return
    setDialogSaving(true)
    setDialogError('')
    try {
      if (dialog.mode === 'create') await repoGroupApi.create(dialog.name.trim(), dialog.repoIds)
      else await repoGroupApi.setMembers(dialog.group.id, dialog.repoIds)
      setDialog(null)
      await load()
    } catch (e) {
      setDialogError((e as Error).message)
    } finally {
      setDialogSaving(false)
    }
  }

  const confirmDelete = async () => {
    if (!pendingDelete) return
    setDeleting(true)
    setNotice('')
    try {
      await repoGroupApi.remove(pendingDelete.id)
      setGroups((prev) => prev.filter((g) => g.id !== pendingDelete.id))
    } catch (e) {
      setNotice(`删除失败：${(e as Error).message}`)
    } finally {
      setDeleting(false)
      setPendingDelete(null)
    }
  }

  return (
    <div>
      <div className="page-header">
        <div>
          <div className="page-title-row">
            <h1>仓库分组</h1>
            <span className="page-count">{groups.length}</span>
          </div>
          <p className="page-sub">目录组按仓库的直接父目录自动生成、随扫描更新；手工组可自由挑选成员。</p>
        </div>
        <button type="button" className="btn" onClick={openCreate}>
          新建手工组
        </button>
      </div>
      <RepoRegistryTabs />

      <div className="filter-bar">
        <select value={kind} onChange={(e) => setKind(e.target.value as RepoGroupKind | '')}>
          <option value="">全部类型</option>
          <option value="dir">{GROUP_KIND_LABELS.dir}</option>
          <option value="manual">{GROUP_KIND_LABELS.manual}</option>
        </select>
      </div>

      {notice ? <div className="error">{notice}</div> : null}
      {error ? <div className="error">加载失败：{error}</div> : null}
      {loading ? (
        <div className="loading">
          <div className="loading-spinner" />
          <span style={{ marginLeft: '0.75rem' }}>加载中…</span>
        </div>
      ) : groups.length === 0 ? (
        <div className="section-card empty-state">
          <p>暂无分组。扫描仓库后会自动生成目录组。</p>
        </div>
      ) : (
        <div className="table-card">
          <table>
            <thead>
              <tr>
                <th>名称</th>
                <th>类型</th>
                <th>目录</th>
                <th>成员</th>
                <th className="col-actions">操作</th>
              </tr>
            </thead>
            <tbody>
              {groups.map((g) => (
                <Fragment key={g.id}>
                  <tr>
                    <td>{g.name}</td>
                    <td>
                      <span className="badge">{GROUP_KIND_LABELS[g.kind]}</span>
                    </td>
                    <td className="cell-desc">{ruleText(g)}</td>
                    <td>{g.repo_ids.length}</td>
                    <td className="col-actions">
                      <div className="row-actions">
                        <button type="button" className="btn btn-ghost btn-sm" onClick={() => toggleExpanded(g.id)}>
                          {expanded.has(g.id) ? '收起成员' : '查看成员'}
                        </button>
                        {g.kind === 'manual' ? (
                          <>
                            <button type="button" className="btn btn-ghost btn-sm" onClick={() => openEdit(g)}>
                              编辑成员
                            </button>
                            <button
                              type="button"
                              className="btn btn-ghost btn-sm btn-danger"
                              onClick={() => setPendingDelete(g)}
                            >
                              删除
                            </button>
                          </>
                        ) : null}
                      </div>
                    </td>
                  </tr>
                  {expanded.has(g.id) ? (
                    <tr>
                      <td colSpan={5}>
                        {g.repo_ids.length === 0 ? (
                          <span className="muted">没有成员</span>
                        ) : (
                          <div className="cell-list">
                            {g.repo_ids.map((id) => {
                              const r = repoById.get(id)
                              return (
                                <span key={id} className="cell-list">
                                  <code>{r?.rel_path ?? id}</code>
                                  {r && r.status !== 'active' ? (
                                    <span className={`badge badge-repo-${r.status}`}>{REPO_STATUS_LABELS[r.status]}</span>
                                  ) : null}
                                </span>
                              )
                            })}
                          </div>
                        )}
                      </td>
                    </tr>
                  ) : null}
                </Fragment>
              ))}
            </tbody>
          </table>
        </div>
      )}

      <FormDialog
        open={!!dialog}
        title={dialog?.mode === 'edit' ? `编辑成员：${dialog.group.name}` : '新建手工组'}
        loading={dialogSaving}
        error={dialogError}
        confirmDisabled={dialog?.mode === 'create' && !dialog.name.trim()}
        onCancel={() => setDialog(null)}
        onConfirm={() => void saveDialog()}
      >
        {dialog ? (
          <>
            {dialog.mode === 'create' ? (
              <div className="form-group">
                <label>名称</label>
                <input value={dialog.name} onChange={(e) => setDialog({ ...dialog, name: e.target.value })} />
              </div>
            ) : null}
            <RepoPicker
              repos={repos}
              selected={dialog.repoIds}
              onChange={(ids) => setDialog({ ...dialog, repoIds: ids })}
            />
          </>
        ) : null}
      </FormDialog>

      <ConfirmDialog
        open={!!pendingDelete}
        title="删除手工组"
        description={
          pendingDelete ? `删除「${pendingDelete.name}」。仍被 Agent 绑定的组不能删除，需先在这些 Agent 中解除绑定。` : ''
        }
        confirmLabel="删除"
        cancelLabel="取消"
        variant="danger"
        loading={deleting}
        onCancel={() => setPendingDelete(null)}
        onConfirm={() => void confirmDelete()}
      />
    </div>
  )
}
