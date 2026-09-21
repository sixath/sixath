import { useEffect, useState } from 'react'
import { getResourceByPayload, listGrants, createGrant, deleteGrant, type ResourceGrant, type ResourceInfo } from '../api/resource'

interface ResourceGrantPanelProps {
  resourceType: string
  payloadRef: string
}

const PERMS = ['view', 'use', 'edit', 'admin'] as const

export default function ResourceGrantPanel({ resourceType, payloadRef }: ResourceGrantPanelProps) {
  const [resource, setResource] = useState<ResourceInfo | null>(null)
  const [grants, setGrants] = useState<ResourceGrant[]>([])
  const [loading, setLoading] = useState(true)
  const [error, setError] = useState('')
  const [granteeType, setGranteeType] = useState<'user' | 'org'>('user')
  const [granteeId, setGranteeId] = useState('')
  const [perm, setPerm] = useState<string>('view')
  const [adding, setAdding] = useState(false)
  const [addError, setAddError] = useState('')
  const [revokingKey, setRevokingKey] = useState<string | null>(null)

  const load = async () => {
    setLoading(true)
    setError('')
    try {
      const info = await getResourceByPayload(resourceType, payloadRef)
      setResource(info)
      const g = await listGrants(info.id)
      setGrants(g)
    } catch (e) {
      setError(e instanceof Error ? e.message : 'Failed to load')
      setResource(null)
      setGrants([])
    } finally {
      setLoading(false)
    }
  }

  useEffect(() => { load() }, [resourceType, payloadRef])

  const handleAdd = async () => {
    if (!resource || !granteeId.trim()) return
    setAdding(true)
    setAddError('')
    try {
      await createGrant(resource.id, granteeType, granteeId.trim(), perm)
      setGranteeId('')
      load()
    } catch (e) {
      setAddError(e instanceof Error ? e.message : 'Grant failed')
    } finally {
      setAdding(false)
    }
  }

  const handleRevoke = async (g: ResourceGrant) => {
    if (!resource) return
    const key = `${g.grantee_type}:${g.grantee_id}`
    setRevokingKey(key)
    try {
      await deleteGrant(resource.id, g.grantee_type, g.grantee_id)
      load()
    } catch (e) {
      setAddError(e instanceof Error ? e.message : 'Revoke failed')
    } finally {
      setRevokingKey(null)
    }
  }

  const grantKey = (g: ResourceGrant) => `${g.grantee_type}:${g.grantee_id}`

  if (loading) return <div className="section-card"><p>Loading grants...</p></div>
  if (error) return <div className="section-card"><p className="muted">{error}</p></div>
  if (!resource) return null

  return (
    <section className="section">
      <h2 className="section-title">权限管理</h2>
      <div className="section-card" style={{ padding: '1.25rem', marginBottom: '1rem' }}>
        <div style={{ display: 'flex', gap: '1rem', marginBottom: '0.5rem', fontSize: '0.9rem', color: 'var(--muted)' }}>
          <span>可见性: <strong>{resource.visibility}</strong></span>
          {resource.home_org_id ? <span>Org: <code>{resource.home_org_id}</code></span> : null}
          <span>Owner: <code>{resource.owner_user_id}</code></span>
        </div>
      </div>

      <div className="section-card" style={{ padding: '1.25rem', marginBottom: '1rem' }}>
        <h3 style={{ margin: '0 0 0.75rem', fontSize: '1rem' }}>添加授权</h3>
        <div style={{ display: 'flex', gap: '0.5rem', alignItems: 'flex-end', flexWrap: 'wrap' }}>
          <div className="form-group" style={{ margin: 0 }}>
            <label style={{ fontSize: '0.8rem' }}>类型</label>
            <select value={granteeType} onChange={(e) => setGranteeType(e.target.value as 'user' | 'org')}>
              <option value="user">User</option>
              <option value="org">Org</option>
            </select>
          </div>
          <div className="form-group" style={{ margin: 0 }}>
            <label style={{ fontSize: '0.8rem' }}>{granteeType === 'user' ? 'User ID' : 'Org ID'}</label>
            <input
              type="text"
              value={granteeId}
              onChange={(e) => setGranteeId(e.target.value)}
              placeholder={granteeType === 'user' ? 'user ID' : 'org ID'}
              style={{ width: '12rem' }}
            />
          </div>
          <div className="form-group" style={{ margin: 0 }}>
            <label style={{ fontSize: '0.8rem' }}>权限</label>
            <select value={perm} onChange={(e) => setPerm(e.target.value)}>
              {PERMS.map((p) => <option key={p} value={p}>{p}</option>)}
            </select>
          </div>
          <button type="button" className="btn btn-primary btn-sm" disabled={adding || !granteeId.trim()} onClick={handleAdd}>
            {adding ? '添加中…' : '添加'}
          </button>
        </div>
        {addError && <p className="error" style={{ marginTop: '0.5rem' }}>{addError}</p>}
      </div>

      <div className="table-card">
        <table>
          <thead>
            <tr>
              <th>类型</th>
              <th>ID</th>
              <th>权限</th>
              <th>操作</th>
            </tr>
          </thead>
          <tbody>
            {grants.length === 0 ? (
              <tr><td colSpan={4} className="muted">暂无授权</td></tr>
            ) : (
              grants.map((g) => {
                const key = grantKey(g)
                const revoking = revokingKey === key
                return (
                  <tr key={key}>
                    <td><span className={`badge badge-${g.grantee_type === 'org' ? 'mcp' : 'builtin'}`}>{g.grantee_type}</span></td>
                    <td><code>{g.grantee_id}</code></td>
                    <td><span className="badge">{g.perm}</span></td>
                    <td>
                      <button
                        type="button"
                        className="btn btn-danger btn-sm"
                        disabled={revoking}
                        onClick={() => handleRevoke(g)}
                      >
                        {revoking ? '撤销中…' : '撤销'}
                      </button>
                    </td>
                  </tr>
                )
              })
            )}
          </tbody>
        </table>
      </div>
    </section>
  )
}