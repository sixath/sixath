import type { ModelCatalogEntry, ModelProvider } from '../api/client'
import type {
  AgentRepoBinding,
  BindingRef,
  BindingTargetKind,
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

/** Model used for a repo's LLM layer: the repo override, else the global model; '' when disabled. */
export function effectiveHandbookModel(repo: Pick<Repository, 'handbook_model'>, globalModel: string): string {
  const own = (repo.handbook_model ?? '').trim()
  if (own.toLowerCase() === 'off') return ''
  return own || globalModel.trim()
}

function llmStateOf(
  enabled: boolean,
  running: boolean,
  llm: HandbookLLMStats | null | undefined,
  handbookCommit: string | undefined,
): LLMState {
  if (!enabled) return 'off'
  if (running) return 'running'
  if (llm?.state === 'failed' && (!llm.failed_commit || llm.failed_commit === handbookCommit)) return 'failed'
  if (!llm?.state || llm.state === 'failed' || llm.commit !== handbookCommit) return 'pending'
  return llm.state === 'complete' ? 'complete' : 'partial'
}

/** A failure on an older commit is retried after the next build, so it reads as pending. */
export function llmState(repo: Repository, globalModel: string, now: number = Date.now()): LLMState {
  return llmStateOf(
    effectiveHandbookModel(repo, globalModel) !== '',
    llmRunning(repo, now),
    repo.handbook_llm,
    repo.handbook_commit,
  )
}

export function handbookViewLLMState(
  view: Pick<HandbookView, 'commit' | 'llm_model' | 'llm_running' | 'llm'>,
): LLMState {
  return llmStateOf((view.llm_model ?? '').trim() !== '', !!view.llm_running, view.llm, view.commit)
}

export function llmProgress(llm: HandbookLLMStats | null | undefined): string {
  if (!llm || llm.cards_total === undefined) return ''
  return `${llm.cards_done ?? 0}/${llm.cards_total}`
}

export interface ModelOption {
  value: string
  label: string
}

const OFF_OPTION: ModelOption = { value: 'off', label: '禁用该仓库的 LLM 增强' }

/** Catalog choices as `<provider name or id>/<model>`, the form the backend resolves; 'off' first. */
export function handbookModelOptions(providers: ModelProvider[], entries: ModelCatalogEntry[]): ModelOption[] {
  const prefix = new Map(providers.filter((p) => p.enabled).map((p) => [p.id, p.name.trim() || p.id]))
  const seen = new Map<string, ModelOption>()
  for (const e of entries) {
    const p = prefix.get(e.provider_id)
    if (e.hidden || !p || !e.model) continue
    const value = `${p}/${e.model}`
    if (!seen.has(value)) seen.set(value, { value, label: e.display_name.trim() || e.model })
  }
  return [OFF_OPTION, ...[...seen.values()].sort((a, b) => cmp(a.value, b.value))]
}

const ENRICH_ERRORS: [RegExp, string][] = [
  [/LLM layer is disabled|HANDBOOK_LLM_DISABLED/i, '该仓库未启用 LLM 增强（未配置全局模型或已设为 off）'],
  [/not current|HANDBOOK_NOT_READY/i, 'handbook 不是最新，请先重建 handbook 再增强'],
  [/build already running|HANDBOOK_BUILDING/i, 'handbook 正在生成中，请稍后再试'],
  [/another LLM run|HANDBOOK_LLM_BUSY/i, '其他仓库的 LLM 增强正在运行，请稍后再试'],
  [/not configured|HANDBOOK_DISABLED/i, '服务端未配置 handbook 功能'],
  [/full must be a boolean|INVALID_ARGUMENT/i, '请求参数无效'],
  [/not found/i, '仓库不存在或已被删除'],
]

/** The API client only surfaces the server message, so known errors are matched on it. */
export function enrichErrorMessage(message: string): string {
  for (const [re, text] of ENRICH_ERRORS) if (re.test(message)) return text
  return message
}

/** Orders pages as SKILL.md, then references/*.md, then area pages. */
export function sortHandbookPages(pages: string[]): string[] {
  const rank = (p: string) => (p === 'SKILL.md' ? 0 : p.startsWith('references/areas/') ? 2 : 1)
  return [...pages].sort((a, b) => rank(a) - rank(b) || a.localeCompare(b))
}
