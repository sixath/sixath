import type {
  AgentRepoBinding,
  BindingRef,
  BindingTargetKind,
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

/** Orders pages as SKILL.md, then references/*.md, then area pages. */
export function sortHandbookPages(pages: string[]): string[] {
  const rank = (p: string) => (p === 'SKILL.md' ? 0 : p.startsWith('references/areas/') ? 2 : 1)
  return [...pages].sort((a, b) => rank(a) - rank(b) || a.localeCompare(b))
}
