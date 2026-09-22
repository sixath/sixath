import { useEffect, useState } from 'react'
import { Link } from 'react-router-dom'
import { listProjects, createProject, type ProjectInfo } from '../api/resource'

export default function ProjectListPage() {
  const [projects, setProjects] = useState<ProjectInfo[]>([])
  const [loading, setLoading] = useState(true)
  const [error, setError] = useState('')

  const load = () => {
    setLoading(true)
    setError('')
    listProjects()
      .then(setProjects)
      .catch((e) => setError(e instanceof Error ? e.message : '加载失败'))
      .finally(() => setLoading(false))
  }

  useEffect(() => { load() }, [])

  const handleCreate = async () => {
    const name = window.prompt('项目名称')
    if (!name || !name.trim()) return
    setError('')
    try {
      const p = await createProject(name.trim())
      setProjects((prev) => [p, ...prev])
    } catch (err) {
      setError(err instanceof Error ? err.message : '创建失败')
    }
  }

  if (loading) return (
    <div className="loading">
      <div className="loading-spinner" />
      <span style={{ marginLeft: '0.75rem' }}>加载中…</span>
    </div>
  )
  if (error) return <div className="error">加载失败：{error}</div>

  return (
    <div>
      <div className="page-header">
        <div>
          <div className="page-title-row">
            <h1>项目</h1>
            <span className="page-count">{projects.length}</span>
          </div>
          <p className="page-sub">管理项目，对资源进行分组和授权。</p>
        </div>
        <button type="button" className="btn" onClick={handleCreate}>+ 创建项目</button>
      </div>
      {projects.length === 0 ? (
        <div className="section-card empty-state">
          <p>还没有项目。</p>
          <button type="button" className="btn" onClick={handleCreate}>+ 创建项目</button>
        </div>
      ) : (
        <div className="table-card">
          <table>
            <thead>
              <tr>
                <th>名称</th>
                <th>可见性</th>
                <th>Owner</th>
                <th>操作</th>
              </tr>
            </thead>
            <tbody>
              {projects.map((p) => (
                <tr key={p.id}>
                  <td><Link to={`/projects/${p.id}`}>{p.name}</Link></td>
                  <td><span className="badge">{p.visibility}</span></td>
                  <td><code>{p.owner_user_id}</code></td>
                  <td>
                    <Link to={`/projects/${p.id}`} className="btn btn-secondary btn-sm">查看</Link>
                  </td>
                </tr>
              ))}
            </tbody>
          </table>
        </div>
      )}
    </div>
  )
}