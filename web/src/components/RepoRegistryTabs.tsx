import { NavLink } from 'react-router-dom'

const TABS = [
  { to: '/repos', label: '仓库', end: true },
  { to: '/repo-groups', label: '分组', end: false },
  { to: '/repos/migration', label: '旧链接迁移', end: false },
]

export function RepoRegistryTabs() {
  return (
    <div className="repo-tabs">
      {TABS.map((t) => (
        <NavLink
          key={t.to}
          to={t.to}
          end={t.end}
          className={({ isActive }) => `btn btn-sm ${isActive ? '' : 'btn-ghost'}`}
        >
          {t.label}
        </NavLink>
      ))}
    </div>
  )
}
