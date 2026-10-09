import { useMemo, useState } from 'react'
import { Link } from 'react-router-dom'
import { repoApi } from '../api/repoRegistry'
import type { LegacyLinkMigrationItem } from '../api/repoRegistryTypes'
import { ConfirmDialog } from '../components/ConfirmDialog'
import { RepoRegistryTabs } from '../components/RepoRegistryTabs'
import {
  LEGACY_ACTIONS,
  LEGACY_ACTION_LABELS,
  isAutoApplyAction,
  pendingAutoApplyCount,
  summarizeMigration,
} from '../utils/repoRegistry'
import './RepoRegistry.css'

const AFTER_ROOTS_PREVIEW = 5

function AfterRoots({ roots }: { roots?: string[] }) {
  if (!roots || roots.length === 0) return <span className="muted">—</span>
  const shown = roots.slice(0, AFTER_ROOTS_PREVIEW)
  return (
    <div className="cell-list" title={roots.join('\n')}>
      {shown.map((r) => (
        <code key={r}>{r}</code>
      ))}
      {roots.length > shown.length ? <span className="muted">等 {roots.length} 个</span> : null}
    </div>
  )
}

function itemState(it: LegacyLinkMigrationItem): string {
  if (it.applied) return '已写入'
  if (it.error) return '失败'
  return isAutoApplyAction(it.action) ? '可自动写入' : '需人工处理'
}

export default function RepoMigrationPage() {
  const [items, setItems] = useState<LegacyLinkMigrationItem[] | null>(null)
  const [running, setRunning] = useState(false)
  const [error, setError] = useState('')
  const [confirmOpen, setConfirmOpen] = useState(false)

  const summary = useMemo(() => summarizeMigration(items ?? []), [items])
  const pending = useMemo(() => pendingAutoApplyCount(items ?? []), [items])

  const run = async (apply: boolean) => {
    setRunning(true)
    setError('')
    try {
      const res = await repoApi.migrateLegacyLinks(apply)
      setItems(res.items)
    } catch (e) {
      setError((e as Error).message)
    } finally {
      setRunning(false)
      setConfirmOpen(false)
    }
  }

  return (
    <div>
      <div className="page-header">
        <div>
          <h1>旧链接迁移</h1>
          <p className="page-sub">
            把 Agent 的 workspace/code 链接换成仓库绑定。只列出你有编辑权限的 Agent；生成报告不会写入任何数据。
          </p>
        </div>
        <div className="row-actions">
          <button type="button" className="btn btn-secondary" disabled={running} onClick={() => void run(false)}>
            {running ? '处理中…' : '生成报告'}
          </button>
          <button
            type="button"
            className="btn"
            disabled={running || pending === 0}
            onClick={() => setConfirmOpen(true)}
            data-testid="migration-apply"
          >
            应用自动迁移（{pending}）
          </button>
        </div>
      </div>
      <RepoRegistryTabs />

      {error ? <div className="error">{error}</div> : null}
      {items === null ? (
        <div className="section-card empty-state">
          <p>点「生成报告」查看每个 Agent 的迁移方案。精确命中一个仓库或一个目录组的 Agent 可以自动写入，其余需要在 Agent 详情页手动绑定。</p>
        </div>
      ) : items.length === 0 ? (
        <div className="section-card empty-state">
          <p>没有需要处理的 Agent。</p>
        </div>
      ) : (
        <>
          <div className="filter-bar" data-testid="migration-summary">
            {LEGACY_ACTIONS.filter((a) => summary[a] > 0).map((a) => (
              <span key={a} className="badge">
                {LEGACY_ACTION_LABELS[a]} {summary[a]}
              </span>
            ))}
          </div>
          <div className="table-card">
            <table>
              <thead>
                <tr>
                  <th>Agent</th>
                  <th>链接目标</th>
                  <th>方案</th>
                  <th>迁移后可见</th>
                  <th>说明</th>
                  <th>状态</th>
                </tr>
              </thead>
              <tbody>
                {items.map((it) => (
                  <tr key={it.agent_id}>
                    <td>
                      <Link to={`/agents/${it.agent_id}`}>{it.agent_id}</Link>
                    </td>
                    <td>
                      <code>{it.target || '—'}</code>
                    </td>
                    <td>{LEGACY_ACTION_LABELS[it.action] ?? it.action}</td>
                    <td>
                      <AfterRoots roots={it.after_roots} />
                    </td>
                    <td className={it.error ? 'error' : 'cell-desc'} title={it.error || it.reason || ''}>
                      {it.error || it.reason || '—'}
                    </td>
                    <td>{itemState(it)}</td>
                  </tr>
                ))}
              </tbody>
            </table>
          </div>
        </>
      )}

      <ConfirmDialog
        open={confirmOpen}
        title="应用自动迁移"
        description={`将为 ${pending} 个 Agent 写入仓库绑定。写入后它们的 RCA 代码工具改用绑定的仓库，不再读取 workspace/code 链接。`}
        confirmLabel="应用"
        cancelLabel="取消"
        loading={running}
        onCancel={() => setConfirmOpen(false)}
        onConfirm={() => void run(true)}
      />
    </div>
  )
}
