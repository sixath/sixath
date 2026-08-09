import { type FormEvent, useEffect, useState } from 'react'
import { Link } from 'react-router-dom'
import { getStoredOrgId, setStoredOrgId } from '../api/auth'
import { orgApi } from '../api/orgApi'
import type { OrgMembership } from '../api/sessionAuth'

export default function OrgListPage() {
  const [orgs, setOrgs] = useState<OrgMembership[]>([])
  const [currentOrgId, setCurrentOrgId] = useState('')
  const [name, setName] = useState('')
  const [loading, setLoading] = useState(true)
  const [creating, setCreating] = useState(false)
  const [error, setError] = useState('')
  const [savedOrgId, setSavedOrgId] = useState<string | null>(null)

  const loadOrgs = () => {
    setLoading(true)
    setError('')
    orgApi
      .list()
      .then((items) => {
        setOrgs(items)
        setCurrentOrgId(getStoredOrgId())
      })
      .catch((e) => setError(e instanceof Error ? e.message : '加载失败'))
      .finally(() => setLoading(false))
  }

  useEffect(() => {
    loadOrgs()
  }, [])

  const onCreate = async (e: FormEvent) => {
    e.preventDefault()
    const trimmed = name.trim()
    if (!trimmed) {
      setError('请填写组织名称')
      return
    }
    setCreating(true)
    setError('')
    try {
      const org = await orgApi.create(trimmed)
      setName('')
      setOrgs((prev) => [...prev, org])
    } catch (err) {
      setError(err instanceof Error ? err.message : '创建失败')
    } finally {
      setCreating(false)
    }
  }

  const selectOrg = (orgId: string) => {
    setStoredOrgId(orgId)
    setCurrentOrgId(orgId)
    setSavedOrgId(orgId)
    window.setTimeout(() => setSavedOrgId(null), 2000)
  }

  if (loading) {
    return (
      <div className="loading">
        <div className="loading-spinner" />
        <span style={{ marginLeft: '0.75rem' }}>加载中…</span>
      </div>
    )
  }

  return (
    <div>
      <div className="page-header">
        <div>
          <h1 className="page-title">组织</h1>
          <div className="page-sub">{orgs.length} 个成员身份</div>
        </div>
      </div>

      {error && <div className="error" style={{ marginBottom: '1rem' }}>{error}</div>}

      <div className="section-card" style={{ marginBottom: '1.25rem', padding: '1.25rem' }}>
        <h2 style={{ marginTop: 0, fontSize: '1.05rem' }}>新建组织</h2>
        <form className="form-panel" onSubmit={onCreate} style={{ marginBottom: 0 }}>
          <div className="form-group">
            <label htmlFor="org-name">组织名称</label>
            <input
              id="org-name"
              type="text"
              value={name}
              onChange={(e) => setName(e.target.value)}
              placeholder="例如：我的团队"
              disabled={creating}
            />
          </div>
          <div className="form-actions">
            <button type="submit" className="btn btn-primary" disabled={creating}>
              {creating ? '创建中…' : '创建'}
            </button>
          </div>
        </form>
      </div>

      {orgs.length === 0 ? (
        <div className="section-card empty-state">
          <p>尚未加入任何组织。创建第一个组织后即可邀请成员。</p>
        </div>
      ) : (
        <div className="table-card">
          <table>
            <thead>
              <tr>
                <th>名称</th>
                <th>角色</th>
                <th>Org ID</th>
                <th>操作</th>
              </tr>
            </thead>
            <tbody>
              {orgs.map((org) => {
                const isCurrent = currentOrgId === org.id
                return (
                  <tr key={org.id}>
                    <td>
                      <Link to={`/orgs/${org.id}`} className="link" style={{ fontWeight: 600 }}>
                        {org.name}
                      </Link>
                      {isCurrent && (
                        <span className="badge" style={{ marginLeft: '0.5rem' }}>
                          当前
                        </span>
                      )}
                    </td>
                    <td>{org.role === 'owner' ? '所有者' : org.role || '成员'}</td>
                    <td>
                      <code style={{ fontSize: '0.8rem' }}>{org.id}</code>
                    </td>
                    <td>
                      <div className="actions">
                        <button
                          type="button"
                          className="btn btn-secondary btn-sm"
                          onClick={() => selectOrg(org.id)}
                          disabled={isCurrent}
                        >
                          {isCurrent ? '已选中' : '设为当前'}
                        </button>
                        <Link to={`/orgs/${org.id}`} className="btn btn-sm">
                          详情
                        </Link>
                      </div>
                    </td>
                  </tr>
                )
              })}
            </tbody>
          </table>
        </div>
      )}

      {savedOrgId && (
        <p className="muted" style={{ marginTop: '1rem', color: 'var(--ok)', fontWeight: 600 }}>
          已设为当前组织（X-Org-Id）
        </p>
      )}

      <p className="muted" style={{ marginTop: '1.25rem' }}>
        当前 Org ID 会写入 localStorage（<code>sixath-org-id</code>），并作为 API 请求头{' '}
        <code>X-Org-Id</code>。多组织时请先选择当前组织。
      </p>
    </div>
  )
}
