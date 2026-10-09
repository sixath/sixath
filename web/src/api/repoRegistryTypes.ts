export type RepoStatus = 'active' | 'missing' | 'archived'
export type RepoGroupKind = 'dir' | 'tag' | 'manual'
export type BindingTargetKind = 'repo' | 'repo_group'
export type BindingMode = 'include' | 'exclude'
export type LegacyAction =
  | 'bind_repo'
  | 'bind_group'
  | 'manual_multi'
  | 'manual_subdir'
  | 'unresolved'
  | 'skip_has_bindings'

export interface Repository {
  id: string
  code_root: string
  rel_path: string
  name: string
  description: string
  tags: string[]
  git_remote: string
  git_branch: string
  head_commit: string
  sync_mode: string
  status: RepoStatus
  handbook_status: string
  owner_id: string
  last_scanned_at?: string
  created_at: string
  updated_at: string
}

export interface RepoGroupRule {
  code_root?: string
  rel_prefix?: string
  all_of?: string[]
  any_of?: string[]
}

export interface RepoGroupView {
  id: string
  name: string
  kind: RepoGroupKind
  rule?: RepoGroupRule
  auto_apply_new: boolean
  owner_id: string
  created_at: string
  updated_at: string
  repo_ids: string[]
}

export interface AgentRepoBinding {
  agent_id?: string
  target_kind: BindingTargetKind
  target_id: string
  mode: BindingMode
  sub_paths?: string[]
  priority?: number
  created_by?: string
}

export interface BindingRef {
  kind: BindingTargetKind
  id: string
}

export interface EffectiveRepoView {
  agent_id: string
  repo_id: string
  via: BindingRef[]
  sub_paths?: string[]
  computed_at: string
  repository?: Repository
}

export interface AgentRepoBindingsView {
  bindings: AgentRepoBinding[]
  effective: EffectiveRepoView[]
}

export interface RepoDetail {
  repository: Repository
  agent_ids: string[]
}

export interface RepoScanReport {
  roots: number
  found: number
  added: number
  restored: number
  missing: number
  errors?: string[]
}

export interface LegacyLinkMigrationItem {
  agent_id: string
  target: string
  action: LegacyAction
  reason?: string
  bindings?: AgentRepoBinding[]
  after_roots?: string[]
  applied: boolean
  error?: string
}

export interface RepoMetaPatch {
  name?: string
  description?: string
  tags?: string[]
  owner_id?: string
  status?: 'active' | 'archived'
}

export interface RepoFilter {
  status?: string
  q?: string
  group_id?: string
  code_root?: string
}
