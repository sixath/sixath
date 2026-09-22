import { useEffect, useMemo, useState } from 'react'
import { orgApi, type PortalUser } from '../api/orgApi'
import { SearchableChipSelect } from './SearchableChipSelect'

function formatUserLabel(u: PortalUser): string {
  const name = u.name?.trim() || u.id
  const parts = [name]
  if (u.name?.trim() && u.name.trim() !== u.id) parts.push(`(${u.id})`)
  if (u.email?.trim()) parts.push(`· ${u.email.trim()}`)
  return parts.join(' ')
}

export function UserPicker({
  value,
  onChange,
  disabled = false,
  excludeIds,
  placeholder = '选择用户…',
  className = '',
}: {
  value: string
  onChange: (userId: string) => void
  disabled?: boolean
  /** Hide these user ids (e.g. already org members). */
  excludeIds?: Iterable<string>
  placeholder?: string
  className?: string
}) {
  const [users, setUsers] = useState<PortalUser[]>([])
  const [loading, setLoading] = useState(true)
  const [error, setError] = useState('')

  useEffect(() => {
    let cancelled = false
    ;(async () => {
      setLoading(true)
      setError('')
      try {
        const list = await orgApi.listUsers('', 200)
        if (!cancelled) setUsers(list)
      } catch (e) {
        if (!cancelled) {
          setUsers([])
          setError(e instanceof Error ? e.message : '加载用户失败')
        }
      } finally {
        if (!cancelled) setLoading(false)
      }
    })()
    return () => {
      cancelled = true
    }
  }, [])

  const excluded = useMemo(() => {
    const s = new Set<string>()
    if (excludeIds) {
      for (const id of excludeIds) {
        if (id) s.add(id)
      }
    }
    return s
  }, [excludeIds])

  const options = useMemo(
    () =>
      users
        .filter((u) => u.id && !excluded.has(u.id))
        .map((u) => ({
          value: u.id,
          label: formatUserLabel(u),
        })),
    [users, excluded]
  )

  return (
    <div className={className} style={{ minWidth: '16rem' }}>
      <SearchableChipSelect
        value={value}
        onChange={onChange}
        options={options}
        disabled={disabled || loading}
        placeholder={loading ? '加载用户…' : placeholder}
        searchPlaceholder="搜索名称 / ID / 邮箱"
        emptyText={error || '没有可添加的用户'}
      />
      {error ? (
        <p className="muted" style={{ margin: '0.35rem 0 0', fontSize: '0.8rem' }}>
          {error}
        </p>
      ) : null}
    </div>
  )
}

export function formatPortalUserLabel(u: PortalUser): string {
  return formatUserLabel(u)
}
