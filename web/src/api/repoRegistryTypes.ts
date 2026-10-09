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
  handbook_commit?: string
  handbook_version?: number
  handbook_stats?: HandbookStats | null
  handbook_lease_until?: string
  /** '' inherits the global model, 'off' disables the LLM layer for this repo. */
  handbook_model?: string
  handbook_llm?: HandbookLLMStats | null
  handbook_llm_lease_until?: string
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
  handbook_model?: string
}

export interface RepoFilter {
  status?: string
  q?: string
  group_id?: string
  code_root?: string
}

export interface HandbookStats {
  generator_version?: string
  built_at?: string
  duration_ms?: number
  files?: number
  go_files?: number
  packages?: number
  areas?: number
  symbols?: number
  registers?: number
  truncated?: boolean
  cards?: number
  stale_cards?: number
  stages?: number
  llm_rev?: string
  last_error?: string
  failed_commit?: string
}

export type HandbookFallbackReason = 'bad_reply' | 'too_large' | 'few_stages' | 'unassigned' | 'no_model' | ''

/** LLM layer run state; state is absent when only an infrastructure error was recorded. */
export interface HandbookLLMStats {
  state?: 'partial' | 'complete' | 'failed'
  model?: string
  commit?: string
  prompt_version?: string
  cards_total?: number
  cards_done?: number
  cards_new?: number
  card_errors?: number
  card_transport_errors?: number
  stages?: number
  fallback?: boolean
  fallback_reason?: HandbookFallbackReason
  skeleton_rebuilt?: boolean
  rebuild_reason?: string
  skeleton_built_at?: string
  tokens_in?: number
  tokens_out?: number
  run_at?: string
  duration_ms?: number
  rev?: string
  /** May be set without a failure; only state === 'failed' is a failure. */
  last_error?: string
  failed_commit?: string
}

export interface HandbookView {
  repo_id: string
  status: string
  commit: string
  head_commit: string
  version: number
  stats?: HandbookStats | null
  pages: string[]
  llm?: HandbookLLMStats | null
  /** '' when the LLM layer is off for this repo. */
  llm_model: string
  llm_running: boolean
}

export interface HandbookConfigView {
  model: string
  enabled: boolean
  concurrency?: number
  max_cards_per_run?: number
  max_file_kb?: number
  max_run_minutes?: number
  skeleton_rebuild_days?: number
}
