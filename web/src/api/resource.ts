import { authHeaders } from './auth'

const API_BASE = '/api/v1'

export interface ResourceInfo {
  id: string
  type: string
  name: string
  visibility: string
  owner_user_id: string
  home_org_id: string
  payload_ref: string
  project_id?: string
  bound_agent_id?: string
}

export interface ProjectInfo {
  id: string
  type: string
  name: string
  visibility: string
  owner_user_id: string
  home_org_id: string
  payload_ref: string
  project_id?: string
  bound_agent_id?: string
}

export interface ResourceGrant {
  resource_id: string
  grantee_type: string
  grantee_id: string
  perm: string
}

export async function getResourceByPayload(type: string, payloadRef: string): Promise<ResourceInfo> {
  const res = await fetch(`${API_BASE}/resources/by-payload/${encodeURIComponent(type)}/${encodeURIComponent(payloadRef)}`, {
    headers: authHeaders(),
  })
  if (!res.ok) throw new Error(res.status === 404 ? 'not_found' : `load failed: ${res.status}`)
  return res.json()
}

export async function listGrants(resourceId: string): Promise<ResourceGrant[]> {
  const res = await fetch(`${API_BASE}/resources/${encodeURIComponent(resourceId)}/grants`, {
    headers: authHeaders(),
  })
  if (!res.ok) throw new Error(`list grants failed: ${res.status}`)
  const data = await res.json()
  return data.grants ?? []
}

export async function createGrant(resourceId: string, granteeType: string, granteeId: string, perm: string): Promise<void> {
  const res = await fetch(`${API_BASE}/resources/${encodeURIComponent(resourceId)}/grants`, {
    method: 'POST',
    headers: { ...authHeaders(), 'Content-Type': 'application/json' },
    body: JSON.stringify({ grantee_type: granteeType, grantee_id: granteeId, perm }),
  })
  if (!res.ok) throw new Error(`grant failed: ${res.status}`)
}

export async function deleteGrant(resourceId: string, granteeType: string, granteeId: string): Promise<void> {
  const params = new URLSearchParams({ grantee_type: granteeType, grantee_id: granteeId })
  const res = await fetch(`${API_BASE}/resources/${encodeURIComponent(resourceId)}/grants?${params}`, {
    method: 'DELETE',
    headers: authHeaders(),
  })
  if (!res.ok) throw new Error(`revoke failed: ${res.status}`)
}

// ── Project APIs ──

export async function listProjects(): Promise<ProjectInfo[]> {
  const res = await fetch(`${API_BASE}/projects`, {
    headers: authHeaders(),
  })
  if (!res.ok) throw new Error(`加载失败: ${res.status}`)
  const data = await res.json()
  return data.projects ?? []
}

export async function createProject(name: string, description = ''): Promise<ProjectInfo> {
  const res = await fetch(`${API_BASE}/projects`, {
    method: 'POST',
    headers: { ...authHeaders(), 'Content-Type': 'application/json' },
    body: JSON.stringify({ name, description }),
  })
  if (!res.ok) throw new Error(`创建失败: ${res.status}`)
  return res.json()
}

export async function getProject(id: string): Promise<ProjectInfo> {
  const res = await fetch(`${API_BASE}/projects/${encodeURIComponent(id)}`, {
    headers: authHeaders(),
  })
  if (!res.ok) throw new Error(res.status === 404 ? '项目不存在' : `加载失败: ${res.status}`)
  return res.json()
}

export async function updateProject(id: string, name: string): Promise<ProjectInfo> {
  const res = await fetch(`${API_BASE}/projects/${encodeURIComponent(id)}`, {
    method: 'PATCH',
    headers: { ...authHeaders(), 'Content-Type': 'application/json' },
    body: JSON.stringify({ name }),
  })
  if (!res.ok) throw new Error(`更新失败: ${res.status}`)
  return res.json()
}

export async function deleteProject(id: string): Promise<void> {
  const res = await fetch(`${API_BASE}/projects/${encodeURIComponent(id)}`, {
    method: 'DELETE',
    headers: authHeaders(),
  })
  if (!res.ok) throw new Error(`删除失败: ${res.status}`)
}

export async function listProjectResources(projectId: string): Promise<ResourceInfo[]> {
  const res = await fetch(`${API_BASE}/projects/${encodeURIComponent(projectId)}/resources`, {
    headers: authHeaders(),
  })
  if (!res.ok) throw new Error(`加载资源失败: ${res.status}`)
  const data = await res.json()
  return data.resources ?? []
}

export async function addToProject(resourceId: string, projectId: string): Promise<void> {
  const res = await fetch(`${API_BASE}/resources/${encodeURIComponent(resourceId)}/project`, {
    method: 'PATCH',
    headers: { ...authHeaders(), 'Content-Type': 'application/json' },
    body: JSON.stringify({ project_id: projectId }),
  })
  if (!res.ok) throw new Error(`添加失败: ${res.status}`)
}

export async function removeFromProject(resourceId: string): Promise<void> {
  const res = await fetch(`${API_BASE}/resources/${encodeURIComponent(resourceId)}/project`, {
    method: 'DELETE',
    headers: authHeaders(),
  })
  if (!res.ok) throw new Error(`移除失败: ${res.status}`)
}