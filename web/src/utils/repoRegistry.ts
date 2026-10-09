import type { ModelCatalogEntry, ModelProvider } from '../api/client'
import type {
  AgentRepoBinding,
  BindingRef,
  BindingTargetKind,
  HandbookConfigView,
  HandbookLLMStats,
  HandbookView,
  LegacyAction,
  LegacyLinkMigrationItem,
  RepoFilter,
  RepoGroupKind,
  RepoGroupView,
  RepoStatus,
  Repository,
} from '../api/repoRegistryTypes'

export const REPO_STATUS_LABELS: Record<RepoStatus, string> = {
  active: '正常',
  missing: '缺失',
  archived: '已归档',
}

export const GROUP_KIND_LABELS: Record<RepoGroupKind, string> = {
  dir: '目录组',
  tag: '标签组',
  manual: '手工组',
}

export const LEGACY_ACTIONS: LegacyAction[] = [
  'bind_repo',
  'bind_group',
  'manual_multi',
  'manual_subdir',
  'unresolved',
  'skip_has_bindings',
]

export const LEGACY_ACTION_LABELS: Record<LegacyAction, string> = {
  bind_repo: '绑定仓库',
  bind_group: '绑定目录组',
  manual_multi: '需人工：多个仓库',
  manual_subdir: '需人工：仓库子目录',
  unresolved: '无法识别',
  skip_has_bindings: '已有绑定，跳过',
}

export function isAutoApplyAction(action: LegacyAction): boolean {
  return action === 'bind_repo' || action === 'bind_group'
}

const FILTER_KEYS: (keyof RepoFilter)[] = ['status', 'q', 'group_id', 'code_root']

export function repoFilterQuery(f: RepoFilter): string {
  const q = new URLSearchParams()
  for (const k of FILTER_KEYS) {
    const v = f[k]?.trim()
    if (v) q.set(k, v)
  }
  const s = q.toString()
  return s ? `?${s}` : ''
}

export function shortCommit(commit: string): string {
  return commit ? commit.slice(0, 8) : '—'
}

function dedupe(items: string[]): string[] {
  const out: string[] = []
  for (const s of items) {
    if (s && !out.includes(s)) out.push(s)
  }
  return out
}

const LIST_SEPARATOR = /[,，\n]/

export function parseTags(text: string): string[] {
  return dedupe(text.split(LIST_SEPARATOR).map((s) => s.trim()))
}

export function parseSubPaths(text: string): string[] {
  return dedupe(
    text.split(LIST_SEPARATOR).map((s) => s.trim().replace(/\\/g, '/').replace(/^\/+|\/+$/g, '')),
  )
}

function sameTarget(b: AgentRepoBinding, kind: BindingTargetKind, id: string): boolean {
  return b.target_kind === kind && b.target_id === id
}

export function isGroupIncluded(bs: AgentRepoBinding[], groupId: string): boolean {
  return bs.some((b) => sameTarget(b, 'repo_group', groupId))
}

export function toggleGroup(bs: AgentRepoBinding[], groupId: string): AgentRepoBinding[] {
  if (isGroupIncluded(bs, groupId)) return bs.filter((b) => !sameTarget(b, 'repo_group', groupId))
  return [...bs, { target_kind: 'repo_group', target_id: groupId, mode: 'include' }]
}

/** Drops repo excludes whose repo is no longer a member of any included group. */
export function pruneOrphanExcludes(bs: AgentRepoBinding[], groups: RepoGroupView[]): AgentRepoBinding[] {
  const covered = new Set<string>()
  for (const g of groups) {
    if (isGroupIncluded(bs, g.id)) for (const id of g.repo_ids) covered.add(id)
  }
  return bs.filter((b) => !(b.target_kind === 'repo' && b.mode === 'exclude' && !covered.has(b.target_id)))
}

export function isRepoExcluded(bs: AgentRepoBinding[], repoId: string): boolean {
  return bs.some((b) => sameTarget(b, 'repo', repoId) && b.mode === 'exclude')
}

/** Exclude and include of one repo share a single binding row, so excluding drops any include. */
export function toggleRepoExclude(bs: AgentRepoBinding[], repoId: string): AgentRepoBinding[] {
  const rest = removeRepoBinding(bs, repoId)
  if (isRepoExcluded(bs, repoId)) return rest
  return [...rest, { target_kind: 'repo', target_id: repoId, mode: 'exclude' }]
}

export function includeRepo(bs: AgentRepoBinding[], repoId: string, subPaths: string[] = []): AgentRepoBinding[] {
  const b: AgentRepoBinding = { target_kind: 'repo', target_id: repoId, mode: 'include' }
  if (subPaths.length > 0) b.sub_paths = subPaths
  return [...removeRepoBinding(bs, repoId), b]
}

export function removeRepoBinding(bs: AgentRepoBinding[], repoId: string): AgentRepoBinding[] {
  return bs.filter((b) => !sameTarget(b, 'repo', repoId))
}

export function toRequestBindings(bs: AgentRepoBinding[]): AgentRepoBinding[] {
  return bs.map((b) => {
    const out: AgentRepoBinding = { target_kind: b.target_kind, target_id: b.target_id, mode: b.mode }
    if (b.sub_paths && b.sub_paths.length > 0) out.sub_paths = [...b.sub_paths]
    return out
  })
}

function bindingKey(b: AgentRepoBinding): string {
  return [b.target_kind, b.target_id, b.mode, [...(b.sub_paths ?? [])].sort().join('|')].join('\u0000')
}

export function bindingsEqual(a: AgentRepoBinding[], b: AgentRepoBinding[]): boolean {
  if (a.length !== b.length) return false
  const ka = a.map(bindingKey).sort()
  const kb = b.map(bindingKey).sort()
  return ka.every((k, i) => k === kb[i])
}

export interface PreviewRepo {
  repo: Repository
  via: BindingRef[]
  subPaths?: string[]
}

function cmp(a: string, b: string): number {
  return a < b ? -1 : a > b ? 1 : 0
}

/** Mirrors backend ExpandRepoBindings: exclude wins, non-active repos drop, whole repo beats sub_paths. */
export function previewEffective(
  bs: AgentRepoBinding[],
  groups: RepoGroupView[],
  repos: Repository[],
): PreviewRepo[] {
  const repoById = new Map(repos.map((r) => [r.id, r]))
  const membersByGroup = new Map(groups.map((g) => [g.id, g.repo_ids]))
  const excluded = new Set(
    bs.filter((b) => b.target_kind === 'repo' && b.mode === 'exclude').map((b) => b.target_id),
  )
  const acc = new Map<string, { via: BindingRef[]; subs: string[]; whole: boolean }>()
  const add = (repoId: string, ref: BindingRef, subs: string[]) => {
    let a = acc.get(repoId)
    if (!a) {
      a = { via: [], subs: [], whole: false }
      acc.set(repoId, a)
    }
    a.via.push(ref)
    if (subs.length === 0) a.whole = true
    else a.subs.push(...subs)
  }
  for (const b of bs) {
    if (b.mode === 'exclude') continue
    const ref: BindingRef = { kind: b.target_kind, id: b.target_id }
    if (b.target_kind === 'repo') add(b.target_id, ref, b.sub_paths ?? [])
    else for (const id of membersByGroup.get(b.target_id) ?? []) add(id, ref, [])
  }
  const out: PreviewRepo[] = []
  for (const [id, a] of acc) {
    const repo = repoById.get(id)
    if (excluded.has(id) || !repo || repo.status !== 'active') continue
    out.push(a.whole ? { repo, via: a.via } : { repo, via: a.via, subPaths: dedupe(a.subs).sort() })
  }
  return out.sort((x, y) => cmp(x.repo.rel_path, y.repo.rel_path) || cmp(x.repo.code_root, y.repo.code_root))
}

export function viaLabel(via: BindingRef[], groups: RepoGroupView[]): string {
  const names = new Map(groups.map((g) => [g.id, g.name]))
  return via.map((v) => (v.kind === 'repo' ? '单独绑定' : `组 ${names.get(v.id) ?? v.id}`)).join('、')
}

export function groupNamesByRepo(groups: RepoGroupView[]): Map<string, string[]> {
  const out = new Map<string, string[]>()
  for (const g of groups) {
    for (const id of g.repo_ids) {
      const names = out.get(id)
      if (names) names.push(g.name)
      else out.set(id, [g.name])
    }
  }
  return out
}

export function summarizeMigration(items: LegacyLinkMigrationItem[]): Record<LegacyAction, number> {
  const out = Object.fromEntries(LEGACY_ACTIONS.map((a) => [a, 0])) as Record<LegacyAction, number>
  for (const it of items) out[it.action] = (out[it.action] ?? 0) + 1
  return out
}

export function pendingAutoApplyCount(items: LegacyLinkMigrationItem[]): number {
  return items.filter((it) => isAutoApplyAction(it.action) && !it.applied).length
}

export type HandbookState = 'none' | 'building' | 'ready' | 'outdated' | 'failed'

export const HANDBOOK_STATE_LABELS: Record<HandbookState, string> = {
  none: '未生成',
  building: '生成中',
  ready: '最新',
  outdated: '待更新',
  failed: '失败',
}

/** Derives the display state; a ready handbook built from an older commit is outdated. */
export function handbookState(
  r: Pick<Repository, 'handbook_status' | 'head_commit'> & { handbook_commit?: string },
): HandbookState {
  if (r.handbook_status === 'building') return 'building'
  if (r.handbook_status === 'failed') return 'failed'
  if (!r.handbook_commit) return 'none'
  return r.handbook_commit === r.head_commit ? 'ready' : 'outdated'
}

type HandbookBuildFields = Pick<Repository, 'handbook_status' | 'head_commit'> & {
  handbook_commit?: string
  handbook_lease_until?: string
  handbook_llm_lease_until?: string
}

/** A build is active while building under a live lease; an expired lease means it is stuck. */
export function handbookBuildActive(r: HandbookBuildFields, now: number = Date.now()): boolean {
  if (handbookState(r) !== 'building') return false
  if (!r.handbook_lease_until) return true
  const until = Date.parse(r.handbook_lease_until)
  return Number.isNaN(until) || until > now
}

export function llmRunning(r: Pick<Repository, 'handbook_llm_lease_until'>, now: number = Date.now()): boolean {
  if (!r.handbook_llm_lease_until) return false
  const until = Date.parse(r.handbook_llm_lease_until)
  return !Number.isNaN(until) && until > now
}

/** True while any deterministic build or LLM run is in flight, so the list keeps polling. */
export function anyHandbookBuildActive(repos: HandbookBuildFields[], now: number = Date.now()): boolean {
  return repos.some((r) => handbookBuildActive(r, now) || llmRunning(r, now))
}

export type LLMState = 'off' | 'running' | 'pending' | 'partial' | 'complete' | 'failed'

export const LLM_STATE_LABELS: Record<LLMState, string> = {
  off: '未启用',
  running: '增强中',
  pending: '待增强',
  partial: '部分完成',
  complete: '已完成',
  failed: '失败',
}

export const LLM_FALLBACK_REASON_LABELS: Record<string, string> = {
  bad_reply: '模型回复不可用',
  too_large: '仓库过大',
  few_stages: '模型给出的阶段过少',
  unassigned: '未归类目录过多',
  no_model: '未配置模型',
}

export type HandbookLLMAvailability = Pick<HandbookConfigView, 'model' | 'available'>

/**
 * Model used for a repo's LLM layer: the repo override, else the global model; '' when
 * disabled, including when the server has no model resolver at all.
 */
export function effectiveHandbookModel(
  repo: Pick<Repository, 'handbook_model'>,
  cfg: HandbookLLMAvailability,
): string {
  if (!cfg.available) return ''
  const own = (repo.handbook_model ?? '').trim()
  if (own.toLowerCase() === 'off') return ''
  return own || (cfg.model ?? '').trim()
}

/**
 * The backend retries a failed run once the commit or the model changes, so a failure only
 * stands while both still match.
 */
function llmStateOf(
  model: string,
  running: boolean,
  llm: HandbookLLMStats | null | undefined,
  handbookCommit: string | undefined,
): LLMState {
  if (!model) return 'off'
  if (running) return 'running'
  if (
    llm?.state === 'failed' &&
    (!llm.failed_commit || llm.failed_commit === handbookCommit) &&
    (!llm.model || llm.model.trim() === model)
  ) {
    return 'failed'
  }
  if (!llm?.state || llm.state === 'failed' || llm.commit !== handbookCommit) return 'pending'
  return llm.state === 'complete' ? 'complete' : 'partial'
}

export function llmState(repo: Repository, cfg: HandbookLLMAvailability, now: number = Date.now()): LLMState {
  return llmStateOf(effectiveHandbookModel(repo, cfg), llmRunning(repo, now), repo.handbook_llm, repo.handbook_commit)
}

/** The view's llm_model is already '' when the server cannot run the LLM layer for the repo. */
export function handbookViewLLMState(
  view: Pick<HandbookView, 'commit' | 'llm_model' | 'llm_running' | 'llm'>,
): LLMState {
  return llmStateOf((view.llm_model ?? '').trim(), !!view.llm_running, view.llm, view.commit)
}

export function llmProgress(llm: HandbookLLMStats | null | undefined): string {
  if (!llm || llm.cards_total === undefined) return ''
  return `${llm.cards_done ?? 0}/${llm.cards_total}`
}

/** Card counts belong to the current commit's run only in the partial and complete states. */
export function llmStateText(state: LLMState, llm: HandbookLLMStats | null | undefined): string {
  const progress = state === 'partial' || state === 'complete' ? llmProgress(llm) : ''
  return progress ? `${LLM_STATE_LABELS[state]} ${progress}` : LLM_STATE_LABELS[state]
}

export interface ModelOption {
  value: string
  label: string
}

const OFF_OPTION: ModelOption = { value: 'off', label: '禁用该仓库的 LLM 增强' }

const PROVIDER_NAME_PREFIX = /^[^\s/]+$/

/**
 * Catalog choices as `<provider name or id>/<model>`, the form the backend resolves; the id is
 * used when the name could not be parsed back. Disabled or keyless providers are skipped.
 */
export function handbookModelOptions(providers: ModelProvider[], entries: ModelCatalogEntry[]): ModelOption[] {
  const prefix = new Map(
    providers
      .filter((p) => p.enabled && p.has_api_key)
      .map((p) => [p.id, PROVIDER_NAME_PREFIX.test(p.name) ? p.name : p.id]),
  )
  const seen = new Map<string, ModelOption>()
  for (const e of entries) {
    const p = prefix.get(e.provider_id)
    if (e.hidden || !p || !e.model) continue
    const value = `${p}/${e.model}`
    if (!seen.has(value)) seen.set(value, { value, label: e.display_name.trim() || e.model })
  }
  return [OFF_OPTION, ...[...seen.values()].sort((a, b) => cmp(a.value, b.value))]
}

const ENRICH_ERROR_TEXT: Record<string, string> = {
  HANDBOOK_LLM_DISABLED: '该仓库未启用 LLM 增强（未配置模型或已设为 off）',
  HANDBOOK_NOT_READY: 'handbook 不是最新，请先重建 handbook 再增强',
  HANDBOOK_BUILDING: '该仓库正在构建或增强中',
  HANDBOOK_LLM_BUSY: '其他仓库的 LLM 增强正在运行，请稍后再试',
  HANDBOOK_DISABLED: '服务端未配置 handbook 功能',
  INVALID_ARGUMENT: '请求参数无效',
  NOT_FOUND: '仓库不存在或已被删除',
}

/** Maps an enrich failure by its server reason; unknown reasons keep the server message. */
export function enrichErrorMessage(err: { message: string; reason?: string; status?: number }): string {
  const reason = err.reason || (err.status === 404 ? 'NOT_FOUND' : '')
  return ENRICH_ERROR_TEXT[reason] ?? err.message
}

/** Orders pages as SKILL.md, then references/*.md, then area pages. */
export function sortHandbookPages(pages: string[]): string[] {
  const rank = (p: string) => (p === 'SKILL.md' ? 0 : p.startsWith('references/areas/') ? 2 : 1)
  return [...pages].sort((a, b) => rank(a) - rank(b) || a.localeCompare(b))
}
