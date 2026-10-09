import { request } from './client'
import type {
  AgentRepoBinding,
  AgentRepoBindingsView,
  LegacyLinkMigrationItem,
  RepoDetail,
  RepoFilter,
  RepoGroupKind,
  RepoGroupView,
  RepoMetaPatch,
  RepoScanReport,
  Repository,
} from './repoRegistryTypes'
import { repoFilterQuery, toRequestBindings } from '../utils/repoRegistry'

const enc = encodeURIComponent

function send(method: string, body?: unknown): RequestInit {
  return body === undefined ? { method } : { method, body: JSON.stringify(body) }
}

export const repoApi = {
  list: (f: RepoFilter = {}) =>
    request<{ items: Repository[]; total: number }>(`/repos${repoFilterQuery(f)}`),
  get: (id: string) => request<RepoDetail>(`/repos/${enc(id)}`),
  patch: (id: string, p: RepoMetaPatch) => request<Repository>(`/repos/${enc(id)}`, send('PATCH', p)),
  scan: () => request<RepoScanReport>('/repos/scan', send('POST')),
  migrateLegacyLinks: (apply: boolean) =>
    request<{ apply: boolean; items: LegacyLinkMigrationItem[] }>(
      `/repos/migrate-legacy-links${apply ? '?apply=true' : ''}`,
      send('POST'),
    ),
}

export const repoGroupApi = {
  list: (kind?: RepoGroupKind) =>
    request<{ items: RepoGroupView[] }>(`/repo-groups${kind ? `?kind=${enc(kind)}` : ''}`),
  create: (name: string, repoIds: string[]) =>
    request<RepoGroupView>('/repo-groups', send('POST', { name, repo_ids: repoIds })),
  setMembers: (id: string, repoIds: string[]) =>
    request<{ ok: boolean }>(`/repo-groups/${enc(id)}/members`, send('PUT', { repo_ids: repoIds })),
  remove: (id: string) => request<{ ok: boolean }>(`/repo-groups/${enc(id)}`, send('DELETE')),
}

export const repoBindingApi = {
  get: (agentId: string) => request<AgentRepoBindingsView>(`/agents/${enc(agentId)}/repo-bindings`),
  replace: (agentId: string, bindings: AgentRepoBinding[]) =>
    request<AgentRepoBindingsView>(
      `/agents/${enc(agentId)}/repo-bindings`,
      send('PUT', { bindings: toRequestBindings(bindings) }),
    ),
  copyFrom: (agentId: string, otherId: string) =>
    request<AgentRepoBindingsView>(
      `/agents/${enc(agentId)}/repo-bindings/copy-from/${enc(otherId)}`,
      send('POST'),
    ),
}
