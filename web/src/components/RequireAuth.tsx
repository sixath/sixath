import { Navigate, useLocation } from 'react-router-dom'
import { hasApiToken } from '../api/auth'

export default function RequireAuth({ children }: { children: React.ReactNode }) {
  const loc = useLocation()
  if (!hasApiToken()) {
    const next = encodeURIComponent(loc.pathname + loc.search)
    return <Navigate to={`/login?next=${next}`} replace />
  }
  return <>{children}</>
}
