import { type FormEvent, useEffect, useMemo, useState } from 'react'
import { Link, useParams } from 'react-router-dom'
import { getStoredOrgId, setStoredOrgId } from '../api/auth'
import {
  inviteFullUrl,
  inviteStatusLabel,
  maxUsesLabel,
  orgApi,
  type CreateInviteResult,
  type OrgInvite,
} from '../api/orgApi'
import type { OrgMembership } from '../api/sessionAuth'

type MaxUsesMode = 'single' | 'unlimited' | 'limited'

export default function OrgDetailPage() {
  const { id = '' } = useParams()
  const [org, setOrg] = useState<OrgMembership | null>(null)
  const [invites, setInvites] = useState<OrgInvite[]>([])
  const [loading, setLoading] = useState(true)
  const [error, setError] = useState('')
  const [maxUsesMode, setMaxUsesMode] = useState<MaxUsesMode>('single')
  const [limitedUses, setLimitedUses] = useState('5')
  const [expiresHours, setExpiresHours] = useState('')
  const [creating, setCreating] = useState(false)
  const [createdInvite, setCreatedInvite] = useState<CreateInviteResult | null>(null)
  const [copied, setCopied] = useState(false)
  const [revokingId, setRevokingId] = useState<string | null>(null)

  const isOwner = org?.role === 'owner'
  const currentOrgId = getStoredOrgId()

  const load = () => {
    if (!id) return
    setLoading(true)
    setError('')
    orgApi
      .list()
      .then(async (orgs) => {
        const found = orgs.find((item) => item.id === id) ?? null
        setOrg(found)
        if (!found) {
          setInvites([])
          return
        }
        if (found.role === 'owner') {
          const items = await orgApi.listInvites(id)
          setInvites(items)
        } else {
          setInvites([])
        }
      })
      .catch((e) => setError(e instanceof Error ? e.message : '加载失败'))
      .finally(() => setLoading(false))
  }

  useEffect(() => {
    load()
  }, [id])

  const resolvedMaxUses = useMemo(() => {
    if (maxUsesMode === 'single') return 1
    if (maxUsesMode === 'unlimited') return 0
    const n = parseInt(limitedUses, 10)
    return Number.isFinite(n) && n > 1 ? n : 2
  }, [maxUsesMode, limitedUses])

  const onCreateInvite = async (e: FormEvent) => {
    e.preventDefault()
    if (!id || !isOwner) return
    setCreating(true)
    setError('')
    setCreatedInvite(null)
    try {
      const hours = expiresHours.trim() ? parseInt(expiresHours, 10) : undefined
      const result = await orgApi.createInvite(id, {
        max_uses: resolvedMaxUses,
        expires_in_hours: hours && hours > 0 ? hours : undefined,
      })
      setCreatedInvite(result)
      setInvites((prev) => [result, ...prev])
    } catch (err) {
      setError(err instanceof Error ? err.message : '创建邀请失败')
    } finally {
      setCreating(false)
    }
  }

  const copyInviteLink = async (invitePath: string) => {
    const url = inviteFullUrl(invitePath)
    try {
      await navigator.clipboard.writeText(url)
      setCopied(true)
      window.setTimeout(() => setCopied(false), 2000)
    } catch {
      window.prompt('复制邀请链接', url)
    }
  }

  const revokeInvite = async (inviteId: string) => {
    if (!id || !isOwner) return
    setRevokingId(inviteId)
    setError('')
    try {
      await orgApi.revokeInvite(id, inviteId)
      setInvites((prev) =>
        prev.map((item) =>
          item.id === inviteId
            ? { ...item, revoked_at: new Date().toISOString() }
            : item
        )
      )
    } catch (err) {
      setError(err instanceof Error ? err.message : '撤销失败')
    } finally {
      setRevokingId(null)
    }
  }

  if (loading) {
    return (
      <div className="loading">
        <div className="loading-spinner" />
        <span style={{ marginLeft: '0.75rem' }}>加载中…</span>
      </div>
    )
  }

  if (!org) {
    return (
      <div>
        <div className="error">未找到该组织，或您不是成员。</div>
        <Link to="/orgs" className="btn btn-secondary" style={{ marginTop: '1rem' }}>
          返回组织列表
        </Link>
      </div>
    )
  }

  return (
    <div>
      <div className="page-header">
        <div>
          <h1 className="page-title">{org.name}</h1>
          <div className="page-sub">
            {org.role === 'owner' ? '所有者' : org.role || '成员'} ·{' '}
            <code>{org.id}</code>
            {currentOrgId === org.id && ' · 当前组织'}
          </div>
        </div>
        <div className="actions">
          <Link to="/orgs" className="btn btn-secondary">
            返回列表
          </Link>
          {currentOrgId !== org.id && (
            <button
              type="button"
              className="btn btn-primary"
              onClick={() => setStoredOrgId(org.id)}
            >
              设为当前组织
            </button>
          )}
        </div>
      </div>

      {error && <div className="error" style={{ marginBottom: '1rem' }}>{error}</div>}

      {!isOwner ? (
        <div className="section-card" style={{ padding: '1.25rem' }}>
          <p style={{ margin: 0 }}>
            您是该组织的成员，仅所有者可创建与管理邀请链接。
          </p>
        </div>
      ) : (
        <>
          <div className="section-card" style={{ marginBottom: '1.25rem', padding: '1.25rem' }}>
            <h2 style={{ marginTop: 0, fontSize: '1.05rem' }}>创建邀请</h2>
            <form className="form-panel" onSubmit={onCreateInvite} style={{ marginBottom: 0 }}>
              <div className="form-group">
                <label>使用次数</label>
                <div style={{ display: 'flex', flexWrap: 'wrap', gap: '0.75rem' }}>
                  <label style={{ display: 'flex', alignItems: 'center', gap: '0.35rem' }}>
                    <input
                      type="radio"
                      name="max-uses"
                      checked={maxUsesMode === 'single'}
                      onChange={() => setMaxUsesMode('single')}
                    />
                    单次（1）
                  </label>
                  <label style={{ display: 'flex', alignItems: 'center', gap: '0.35rem' }}>
                    <input
                      type="radio"
                      name="max-uses"
                      checked={maxUsesMode === 'unlimited'}
                      onChange={() => setMaxUsesMode('unlimited')}
                    />
                    无限（0）
                  </label>
                  <label style={{ display: 'flex', alignItems: 'center', gap: '0.35rem' }}>
                    <input
                      type="radio"
                      name="max-uses"
                      checked={maxUsesMode === 'limited'}
                      onChange={() => setMaxUsesMode('limited')}
                    />
                    有限
                    <input
                      type="number"
                      min={2}
                      value={limitedUses}
                      onChange={(e) => setLimitedUses(e.target.value)}
                      disabled={maxUsesMode !== 'limited'}
                      style={{ width: '4.5rem', marginLeft: '0.25rem' }}
                    />
                    次
                  </label>
                </div>
              </div>
              <div className="form-group">
                <label htmlFor="expires-hours">过期时间（小时，可选）</label>
                <input
                  id="expires-hours"
                  type="number"
                  min={1}
                  placeholder="留空表示不过期"
                  value={expiresHours}
                  onChange={(e) => setExpiresHours(e.target.value)}
                  disabled={creating}
                />
              </div>
              <div className="form-actions">
                <button type="submit" className="btn btn-primary" disabled={creating}>
                  {creating ? '创建中…' : '生成邀请链接'}
                </button>
              </div>
            </form>

            {createdInvite && (
              <div style={{ marginTop: '1rem', padding: '1rem', background: 'var(--bg-accent)', borderRadius: 'var(--radius-md)' }}>
                <p style={{ margin: '0 0 0.5rem', fontWeight: 600 }}>邀请已创建（链接仅显示一次）</p>
                <code style={{ display: 'block', wordBreak: 'break-all', fontSize: '0.85rem' }}>
                  {inviteFullUrl(createdInvite.invite_path)}
                </code>
                <button
                  type="button"
                  className="btn btn-secondary btn-sm"
                  style={{ marginTop: '0.75rem' }}
                  onClick={() => copyInviteLink(createdInvite.invite_path)}
                >
                  {copied ? '已复制' : '复制链接'}
                </button>
              </div>
            )}
          </div>

          <div className="table-card">
            <table>
              <thead>
                <tr>
                  <th>状态</th>
                  <th>次数</th>
                  <th>过期</th>
                  <th>创建时间</th>
                  <th>操作</th>
                </tr>
              </thead>
              <tbody>
                {invites.length === 0 ? (
                  <tr>
                    <td colSpan={5} className="muted">
                      暂无邀请
                    </td>
                  </tr>
                ) : (
                  invites.map((invite) => {
                    const status = inviteStatusLabel(invite)
                    const canRevoke = status === '有效'
                    return (
                      <tr key={invite.id}>
                        <td>{status}</td>
                        <td>
                          {maxUsesLabel(invite.max_uses)} · 已用 {invite.used_count}
                        </td>
                        <td>
                          {invite.expires_at
                            ? new Date(invite.expires_at).toLocaleString()
                            : '不过期'}
                        </td>
                        <td>{invite.created_at ? new Date(invite.created_at).toLocaleString() : '-'}</td>
                        <td>
                          {canRevoke ? (
                            <button
                              type="button"
                              className="btn btn-danger btn-sm"
                              disabled={revokingId === invite.id}
                              onClick={() => revokeInvite(invite.id)}
                            >
                              {revokingId === invite.id ? '撤销中…' : '撤销'}
                            </button>
                          ) : (
                            '-'
                          )}
                        </td>
                      </tr>
                    )
                  })
                )}
              </tbody>
            </table>
          </div>
        </>
      )}
    </div>
  )
}
