import { type FormEvent, useEffect, useState } from 'react'
import { Link, useParams } from 'react-router-dom'
import {
  getProject, updateProject, deleteProject,
  listProjectResources, removeFromProject, addToProject,
  type ProjectInfo,
  type ResourceInfo,
} from '../api/resource'
import ResourceGrantPanel from '../components/ResourceGrantPanel'

export default function ProjectDetailPage() {
  const { id = '' } = useParams()
  const [project, setProject] = useState<ProjectInfo | null>(null)
  const [loading, setLoading] = useState(true)
  const [error, setError] = useState('')
  const [editingName, setEditingName] = useState(false)
  const [newName, setNewName] = useState('')
  const [saving, setSaving] = useState(false)

  // Resources in this project
  const [resources, setResources] = useState<ResourceInfo[]>([])
  const [resLoading, setResLoading] = useState(false)

  // Add resource to project
  const [addResId, setAddResId] = useState('')
  const [adding, setAdding] = useState(false)

  // Remove resource
  const [removingId, setRemovingId] = useState<string | null>(null)

  const load = async () => {
    if (!id) return
    setLoading(true)
    setError('')
    try {
      const p = await getProject(id)
      setProject(p)
      setNewName(p.name)
    } catch (e) {
      setError(e instanceof Error ? e.message : '加载失败')
      setProject(null)
    } finally {
      setLoading(false)
    }
  }

  const loadResources = async () => {
    if (!id) return
    setResLoading(true)
    try {
      const items = await listProjectResources(id)
      setResources(items)
    } catch {
      setResources([])
    } finally {
      setResLoading(false)
    }
  }

  useEffect(() => { load(); loadResources() }, [id])

  const handleSaveName = async (e: FormEvent) => {
    e.preventDefault()
    if (!id || !newName.trim() || newName.trim() === project?.name) {
      setEditingName(false)
      return
    }
    setSaving(true)
    try {
      const updated = await updateProject(id, newName.trim())
      setProject(updated)
      setEditingName(false)
    } catch (err) {
      setError(err instanceof Error ? err.message : '保存失败')
    } finally {
      setSaving(false)
    }
  }

  const handleDelete = async () => {
    if (!id) return
    try {
      await deleteProject(id)
      window.location.href = '/projects'
    } catch (err) {
      setError(err instanceof Error ? err.message : '删除失败')
    }
  }

  const handleAddResource = async (e: FormEvent) => {
    e.preventDefault()
    if (!id || !addResId.trim()) return
    setAdding(true)
    setError('')
    try {
      await addToProject(addResId.trim(), id)
      setAddResId('')
      loadResources()
    } catch (err) {
      setError(err instanceof Error ? err.message : '添加失败')
    } finally {
      setAdding(false)
    }
  }

  const handleRemoveResource = async (resourceId: string) => {
    setRemovingId(resourceId)
    setError('')
    try {
      await removeFromProject(resourceId)
      setResources((prev) => prev.filter((r) => r.id !== resourceId))
    } catch (err) {
      setError(err instanceof Error ? err.message : '移除失败')
    } finally {
      setRemovingId(null)
    }
  }

  if (loading) return (
    <div className="loading">
      <div className="loading-spinner" />
      <span style={{ marginLeft: '0.75rem' }}>加载中...</span>
    </div>
  )
  if (!project) return (
    <div>
      <div className="error">未找到该项目。</div>
      <Link to="/projects" className="btn btn-secondary" style={{ marginTop: '1rem' }}>返回项目列表</Link>
    </div>
  )

  return (
    <div>
      <div className="page-header">
        <div>
          <div className="page-title-row">
            {editingName ? (
              <form onSubmit={handleSaveName} style={{ display: 'inline-flex', gap: '0.5rem', alignItems: 'center' }}>
                <input
                  value={newName}
                  onChange={(e) => setNewName(e.target.value)}
                  style={{ fontSize: '1.5rem', fontWeight: 700 }}
                  autoFocus
                />
                <button type="submit" className="btn btn-primary btn-sm" disabled={saving}>保存</button>
                <button type="button" className="btn btn-secondary btn-sm" onClick={() => setEditingName(false)}>取消</button>
              </form>
            ) : (
              <h1 style={{ cursor: 'pointer' }} onClick={() => setEditingName(true)} title="点击编辑名称">
                {project.name}
              </h1>
            )}
          </div>
          <p className="page-sub">
            {project.visibility === 'org' ? 'Org 共享' : 'Private'} ·{' '}
            Owner: <code>{project.owner_user_id}</code>
            {project.home_org_id ? <> · Org: <code>{project.home_org_id}</code></> : null}
          </p>
        </div>
        <div className="actions">
          <Link to="/projects" className="btn btn-secondary">返回列表</Link>
          <button type="button" className="btn btn-danger" onClick={handleDelete}>删除项目</button>
        </div>
      </div>

      {error && <div className="error" style={{ marginBottom: '1rem' }}>{error}</div>}

      <ResourceGrantPanel resourceType="project" payloadRef={project.id} />

      <section className="section">
        <h2 className="section-title">项目资源</h2>

        <div className="section-card" style={{ padding: '1.25rem', marginBottom: '1rem' }}>
          <form onSubmit={handleAddResource}>
            <div style={{ display: 'flex', gap: '0.75rem', alignItems: 'flex-end', flexWrap: 'wrap' }}>
              <div className="form-group" style={{ margin: 0, flex: '1 1 250px' }}>
                <label style={{ fontSize: '0.8rem' }}>Resource ID</label>
                <input
                  value={addResId}
                  onChange={(e) => setAddResId(e.target.value)}
                  placeholder="输入资源 ID (resource 表中的 id)"
                  disabled={adding}
                />
              </div>
              <button type="submit" className="btn btn-primary btn-sm" disabled={adding || !addResId.trim()}>
                {adding ? '添加中...' : '加入项目'}
              </button>
            </div>
          </form>
        </div>

        <div className="table-card">
          <table>
            <thead>
              <tr>
                <th>名称</th>
                <th>类型</th>
                <th>可见性</th>
                <th>操作</th>
              </tr>
            </thead>
            <tbody>
              {resLoading ? (
                <tr><td colSpan={4} className="muted">加载中...</td></tr>
              ) : resources.length === 0 ? (
                <tr><td colSpan={4} className="muted">暂无资源</td></tr>
              ) : (
                resources.map((r) => {
                  const isRemoving = removingId === r.id
                  return (
                    <tr key={r.id}>
                      <td>{r.name}</td>
                      <td><span className="badge">{r.type}</span></td>
                      <td><span className="badge">{r.visibility}</span></td>
                      <td>
                        <button
                          type="button"
                          className="btn btn-danger btn-sm"
                          disabled={isRemoving}
                          onClick={() => handleRemoveResource(r.id)}
                        >
                          {isRemoving ? '移除中...' : '移出项目'}
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
    </div>
  )
}