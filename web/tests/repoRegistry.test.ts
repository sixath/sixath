import { describe, it } from 'node:test'
import assert from 'node:assert/strict'
import type {
  AgentRepoBinding,
  LegacyLinkMigrationItem,
  RepoGroupView,
  Repository,
} from '../src/api/repoRegistryTypes.ts'
import type { ModelCatalogEntry, ModelProvider } from '../src/api/client.ts'
import {
  anyHandbookBuildActive,
  bindingsEqual,
  effectiveHandbookModel,
  enrichErrorMessage,
  groupNamesByRepo,
  handbookBuildActive,
  handbookModelOptions,
  handbookState,
  handbookViewLLMState,
  includeRepo,
  isGroupIncluded,
  isRepoExcluded,
  llmProgress,
  llmRunning,
  llmState,
  llmStateText,
  parseSubPaths,
  parseTags,
  pendingAutoApplyCount,
  previewEffective,
  pruneOrphanExcludes,
  removeRepoBinding,
  repoFilterQuery,
  shortCommit,
  sortHandbookPages,
  summarizeMigration,
  toRequestBindings,
  toggleGroup,
  toggleRepoExclude,
  viaLabel,
} from '../src/utils/repoRegistry.ts'

function repo(id: string, rel: string, status: Repository['status'] = 'active'): Repository {
  return {
    id,
    code_root: '/codes',
    rel_path: rel,
    name: rel.split('/').pop() ?? rel,
    description: '',
    tags: [],
    git_remote: '',
    git_branch: 'main',
    head_commit: '',
    sync_mode: 'registry_only',
    status,
    handbook_status: 'none',
    owner_id: '',
    created_at: '',
    updated_at: '',
  }
}

function group(id: string, name: string, repoIds: string[]): RepoGroupView {
  return { id, name, kind: 'dir', auto_apply_new: true, owner_id: '', created_at: '', updated_at: '', repo_ids: repoIds }
}

describe('repoFilterQuery', () => {
  it('omits empty values', () => {
    assert.equal(repoFilterQuery({}), '')
    assert.equal(repoFilterQuery({ status: ' ', q: '' }), '')
  })
  it('encodes values in a stable order', () => {
    assert.equal(
      repoFilterQuery({ code_root: '/codes', q: 'svc a', status: 'active' }),
      '?status=active&q=svc+a&code_root=%2Fcodes',
    )
  })
})

describe('parseSubPaths / parseTags', () => {
  it('splits, normalizes slashes and dedupes sub paths', () => {
    assert.deepEqual(parseSubPaths(' api/, \\internal\\rca ,api\n，docs '), ['api', 'internal/rca', 'docs'])
  })
  it('splits and dedupes tags', () => {
    assert.deepEqual(parseTags('go, rca，go ,, core'), ['go', 'rca', 'core'])
  })
})

describe('binding edits', () => {
  it('toggles a group include', () => {
    const on = toggleGroup([], 'g1')
    assert.deepEqual(on, [{ target_kind: 'repo_group', target_id: 'g1', mode: 'include' }])
    assert.equal(isGroupIncluded(on, 'g1'), true)
    assert.deepEqual(toggleGroup(on, 'g1'), [])
  })
  it('exclude replaces an include for the same repo and toggles back off', () => {
    const exc = toggleRepoExclude(includeRepo([], 'r1', ['api']), 'r1')
    assert.deepEqual(exc, [{ target_kind: 'repo', target_id: 'r1', mode: 'exclude' }])
    assert.equal(isRepoExcluded(exc, 'r1'), true)
    assert.deepEqual(toggleRepoExclude(exc, 'r1'), [])
  })
  it('includeRepo omits empty sub_paths and replaces the previous binding', () => {
    const b = includeRepo(includeRepo([], 'r1', ['api']), 'r1')
    assert.deepEqual(b, [{ target_kind: 'repo', target_id: 'r1', mode: 'include' }])
    assert.deepEqual(removeRepoBinding(b, 'r1'), [])
  })
  it('toRequestBindings strips server-owned fields and empty sub_paths', () => {
    assert.deepEqual(
      toRequestBindings([
        { agent_id: 'a', target_kind: 'repo', target_id: 'r1', mode: 'include', sub_paths: [], priority: 0, created_by: 'u' },
        { target_kind: 'repo', target_id: 'r2', mode: 'include', sub_paths: ['api'] },
      ]),
      [
        { target_kind: 'repo', target_id: 'r1', mode: 'include' },
        { target_kind: 'repo', target_id: 'r2', mode: 'include', sub_paths: ['api'] },
      ],
    )
  })
  it('bindingsEqual ignores order, sub_path order and server fields', () => {
    const a: AgentRepoBinding[] = [
      { target_kind: 'repo_group', target_id: 'g1', mode: 'include' },
      { target_kind: 'repo', target_id: 'r1', mode: 'include', sub_paths: ['b', 'a'] },
    ]
    const b: AgentRepoBinding[] = [
      { agent_id: 'x', target_kind: 'repo', target_id: 'r1', mode: 'include', sub_paths: ['a', 'b'], created_by: 'u' },
      { target_kind: 'repo_group', target_id: 'g1', mode: 'include', priority: 0 },
    ]
    assert.equal(bindingsEqual(a, b), true)
    assert.equal(bindingsEqual(a, a.slice(1)), false)
  })
})

describe('pruneOrphanExcludes', () => {
  const groups = [group('g1', 'cg', ['r1', 'r2']), group('g2', 'core', ['r2', 'r3'])]
  it('drops excludes no longer covered by an included group', () => {
    const bs: AgentRepoBinding[] = [
      { target_kind: 'repo_group', target_id: 'g2', mode: 'include' },
      { target_kind: 'repo', target_id: 'r1', mode: 'exclude' },
      { target_kind: 'repo', target_id: 'r2', mode: 'exclude' },
      { target_kind: 'repo', target_id: 'r4', mode: 'include' },
    ]
    assert.deepEqual(pruneOrphanExcludes(bs, groups), [
      { target_kind: 'repo_group', target_id: 'g2', mode: 'include' },
      { target_kind: 'repo', target_id: 'r2', mode: 'exclude' },
      { target_kind: 'repo', target_id: 'r4', mode: 'include' },
    ])
  })
  it('clears everything when only excludes remain', () => {
    assert.deepEqual(pruneOrphanExcludes([{ target_kind: 'repo', target_id: 'r1', mode: 'exclude' }], groups), [])
  })
})

describe('previewEffective', () => {
  const repos = [repo('r1', 'cg/a'), repo('r2', 'cg/b'), repo('r3', 'cg/c', 'missing'), repo('r4', 'mg/x')]
  const groups = [group('g1', 'cg', ['r1', 'r2', 'r3'])]

  it('expands groups and drops excluded and inactive repos', () => {
    const out = previewEffective(
      [
        { target_kind: 'repo_group', target_id: 'g1', mode: 'include' },
        { target_kind: 'repo', target_id: 'r2', mode: 'exclude' },
      ],
      groups,
      repos,
    )
    assert.deepEqual(out.map((p) => p.repo.id), ['r1'])
  })
  it('whole repo wins over sub_paths and via lists every source', () => {
    const out = previewEffective(
      [
        { target_kind: 'repo', target_id: 'r1', mode: 'include', sub_paths: ['api'] },
        { target_kind: 'repo_group', target_id: 'g1', mode: 'include' },
      ],
      groups,
      repos,
    )
    const r1 = out.find((p) => p.repo.id === 'r1')
    assert.ok(r1)
    assert.equal(r1.subPaths, undefined)
    assert.deepEqual(r1.via, [
      { kind: 'repo', id: 'r1' },
      { kind: 'repo_group', id: 'g1' },
    ])
  })
  it('keeps sorted sub_paths and sorts by rel_path', () => {
    const out = previewEffective(
      [
        { target_kind: 'repo', target_id: 'r4', mode: 'include', sub_paths: ['b', 'a'] },
        { target_kind: 'repo', target_id: 'r2', mode: 'include' },
      ],
      groups,
      repos,
    )
    assert.deepEqual(out.map((p) => p.repo.rel_path), ['cg/b', 'mg/x'])
    assert.deepEqual(out[1].subPaths, ['a', 'b'])
  })
  it('ignores unknown groups and repos', () => {
    assert.deepEqual(
      previewEffective(
        [
          { target_kind: 'repo_group', target_id: 'nope', mode: 'include' },
          { target_kind: 'repo', target_id: 'gone', mode: 'include' },
        ],
        groups,
        repos,
      ),
      [],
    )
  })
})

describe('labels and summaries', () => {
  it('shortCommit', () => {
    assert.equal(shortCommit(''), '—')
    assert.equal(shortCommit('0123456789abcdef'), '01234567')
  })
  it('viaLabel names groups', () => {
    assert.equal(
      viaLabel([{ kind: 'repo_group', id: 'g1' }, { kind: 'repo', id: 'r1' }], [group('g1', 'cg', [])]),
      '组 cg、单独绑定',
    )
  })
  it('groupNamesByRepo', () => {
    const m = groupNamesByRepo([group('g1', 'cg', ['r1']), group('g2', 'core', ['r1', 'r2'])])
    assert.deepEqual(m.get('r1'), ['cg', 'core'])
    assert.deepEqual(m.get('r2'), ['core'])
  })
  it('summarizeMigration and pendingAutoApplyCount', () => {
    const items: LegacyLinkMigrationItem[] = [
      { agent_id: 'a', target: '/c/x', action: 'bind_repo', applied: false },
      { agent_id: 'b', target: '/c/y', action: 'bind_group', applied: true },
      { agent_id: 'c', target: '/c', action: 'manual_multi', applied: false },
    ]
    const s = summarizeMigration(items)
    assert.equal(s.bind_repo, 1)
    assert.equal(s.bind_group, 1)
    assert.equal(s.manual_multi, 1)
    assert.equal(s.unresolved, 0)
    assert.equal(pendingAutoApplyCount(items), 1)
  })
})

describe('handbookState', () => {
  const base = { head_commit: 'aaa', handbook_status: 'ready', handbook_commit: 'aaa' }
  it('maps backend status and commit drift', () => {
    assert.equal(handbookState(base), 'ready')
    assert.equal(handbookState({ ...base, handbook_commit: 'old' }), 'outdated')
    assert.equal(handbookState({ ...base, handbook_status: 'building' }), 'building')
    assert.equal(handbookState({ ...base, handbook_status: 'failed' }), 'failed')
    assert.equal(handbookState({ head_commit: 'aaa', handbook_status: 'none' }), 'none')
  })
})

describe('handbookBuildActive', () => {
  const now = Date.parse('2026-10-09T10:00:00Z')
  const building = { head_commit: 'aaa', handbook_status: 'building' }
  it('is active while building under a live or unknown lease', () => {
    assert.equal(handbookBuildActive(building, now), true)
    assert.equal(handbookBuildActive({ ...building, handbook_lease_until: '2026-10-09T10:05:00Z' }, now), true)
  })
  it('is inactive once the lease expired or the build finished', () => {
    assert.equal(handbookBuildActive({ ...building, handbook_lease_until: '2026-10-09T09:59:00Z' }, now), false)
    assert.equal(handbookBuildActive({ head_commit: 'aaa', handbook_status: 'ready', handbook_commit: 'aaa' }, now), false)
  })
  it('counts any active build in a list', () => {
    const ready = { head_commit: 'aaa', handbook_status: 'ready', handbook_commit: 'aaa' }
    assert.equal(anyHandbookBuildActive([ready, building], now), true)
    assert.equal(anyHandbookBuildActive([ready], now), false)
  })
})

describe('effectiveHandbookModel', () => {
  const cfg = (model: string) => ({ model, available: true })
  it('inherits the global model when the repo has no override', () => {
    assert.equal(effectiveHandbookModel({ handbook_model: '' }, cfg(' qwen/qwen-max ')), 'qwen/qwen-max')
    assert.equal(effectiveHandbookModel({}, cfg('qwen/qwen-max')), 'qwen/qwen-max')
    assert.equal(effectiveHandbookModel({}, cfg('')), '')
  })
  it('prefers the repo override and treats off case-insensitively', () => {
    assert.equal(effectiveHandbookModel({ handbook_model: ' ds/deepseek-v3 ' }, cfg('qwen/qwen-max')), 'ds/deepseek-v3')
    assert.equal(effectiveHandbookModel({ handbook_model: 'ds/deepseek-v3' }, cfg('')), 'ds/deepseek-v3')
    assert.equal(effectiveHandbookModel({ handbook_model: 'off' }, cfg('qwen/qwen-max')), '')
    assert.equal(effectiveHandbookModel({ handbook_model: ' OFF ' }, cfg('qwen/qwen-max')), '')
  })
  it('is disabled for every repo without a model resolver', () => {
    assert.equal(effectiveHandbookModel({}, { model: 'qwen/qwen-max', available: false }), '')
    assert.equal(effectiveHandbookModel({ handbook_model: 'ds/deepseek-v3' }, { model: '', available: false }), '')
  })
})

describe('llmState', () => {
  const now = Date.parse('2026-10-09T10:00:00Z')
  const base: Repository = { ...repo('r1', 'cg/a'), head_commit: 'aaa', handbook_status: 'ready', handbook_commit: 'aaa', handbook_model: '' }
  const g = { model: 'qwen/qwen-max', available: true }
  it('is off without an effective model or resolver', () => {
    assert.equal(llmState(base, { model: '', available: true }, now), 'off')
    assert.equal(llmState({ ...base, handbook_model: 'off' }, g, now), 'off')
    assert.equal(llmState({ ...base, handbook_model: 'ds/deepseek-v3' }, { model: '', available: false }, now), 'off')
  })
  it('a failure with another model is retried, so it is pending', () => {
    const failed = { state: 'failed' as const, commit: 'aaa', failed_commit: 'aaa', model: 'qwen/qwen-max' }
    assert.equal(llmState({ ...base, handbook_llm: failed }, g, now), 'failed')
    assert.equal(llmState({ ...base, handbook_model: 'ds/deepseek-v3', handbook_llm: failed }, g, now), 'pending')
  })
  it('is running only under a live lease', () => {
    assert.equal(llmState({ ...base, handbook_llm_lease_until: '2026-10-09T10:05:00Z' }, g, now), 'running')
    assert.equal(llmState({ ...base, handbook_llm_lease_until: '2026-10-09T09:59:00Z' }, g, now), 'pending')
  })
  it('maps stored state and commit drift', () => {
    assert.equal(llmState(base, g, now), 'pending')
    assert.equal(llmState({ ...base, handbook_llm: { state: 'partial', commit: 'aaa' } }, g, now), 'partial')
    assert.equal(llmState({ ...base, handbook_llm: { state: 'complete', commit: 'aaa' } }, g, now), 'complete')
    assert.equal(llmState({ ...base, handbook_llm: { state: 'complete', commit: 'old' } }, g, now), 'pending')
    assert.equal(llmState({ ...base, handbook_llm: { state: 'failed', commit: 'aaa', failed_commit: 'aaa' } }, g, now), 'failed')
    assert.equal(llmState({ ...base, handbook_llm: { state: 'failed', commit: 'aaa' } }, g, now), 'failed')
  })
  it('a failure on an older commit is retried, so it is pending', () => {
    assert.equal(llmState({ ...base, handbook_llm: { state: 'failed', commit: 'old', failed_commit: 'old' } }, g, now), 'pending')
  })
  it('last_error alone is not a failure', () => {
    assert.equal(llmState({ ...base, handbook_llm: { last_error: 'disk full' } }, g, now), 'pending')
    assert.equal(llmState({ ...base, handbook_llm: { state: 'complete', commit: 'aaa', last_error: 'x' } }, g, now), 'complete')
  })
})

describe('handbookViewLLMState', () => {
  const view = { commit: 'aaa', llm_model: 'qwen/qwen-max', llm_running: false }
  it('follows the same rules as llmState', () => {
    assert.equal(handbookViewLLMState({ ...view, llm_model: '' }), 'off')
    assert.equal(handbookViewLLMState({ ...view, llm_running: true }), 'running')
    assert.equal(handbookViewLLMState(view), 'pending')
    assert.equal(handbookViewLLMState({ ...view, llm: { state: 'partial', commit: 'aaa' } }), 'partial')
    assert.equal(handbookViewLLMState({ ...view, llm: { state: 'failed', commit: 'aaa' } }), 'failed')
    assert.equal(
      handbookViewLLMState({ ...view, llm: { state: 'failed', commit: 'aaa', failed_commit: 'aaa', model: 'old/m' } }),
      'pending',
    )
  })
})

describe('llmProgress / llmStateText', () => {
  it('formats done/total', () => {
    assert.equal(llmProgress(undefined), '')
    assert.equal(llmProgress({ state: 'partial' }), '')
    assert.equal(llmProgress({ cards_total: 600 }), '0/600')
    assert.equal(llmProgress({ cards_total: 600, cards_done: 120 }), '120/600')
  })
  it('shows counts only for partial and complete', () => {
    const llm = { cards_total: 600, cards_done: 120 }
    assert.equal(llmStateText('partial', llm), '部分完成 120/600')
    assert.equal(llmStateText('complete', llm), '已完成 120/600')
    assert.equal(llmStateText('pending', llm), '待增强')
    assert.equal(llmStateText('running', llm), '增强中')
    assert.equal(llmStateText('failed', llm), '失败')
    assert.equal(llmStateText('off', llm), '未启用')
    assert.equal(llmStateText('partial', undefined), '部分完成')
  })
})

describe('LLM leases keep polling alive', () => {
  const now = Date.parse('2026-10-09T10:00:00Z')
  const ready = { head_commit: 'aaa', handbook_status: 'ready', handbook_commit: 'aaa' }
  it('llmRunning checks the lease', () => {
    assert.equal(llmRunning(ready, now), false)
    assert.equal(llmRunning({ ...ready, handbook_llm_lease_until: '2026-10-09T10:05:00Z' }, now), true)
    assert.equal(llmRunning({ ...ready, handbook_llm_lease_until: '2026-10-09T09:59:00Z' }, now), false)
    assert.equal(llmRunning({ ...ready, handbook_llm_lease_until: 'bad' }, now), false)
  })
  it('anyHandbookBuildActive counts live LLM leases', () => {
    assert.equal(anyHandbookBuildActive([ready, { ...ready, handbook_llm_lease_until: '2026-10-09T10:05:00Z' }], now), true)
    assert.equal(anyHandbookBuildActive([{ ...ready, handbook_llm_lease_until: '2026-10-09T09:00:00Z' }], now), false)
  })
})

describe('handbookModelOptions', () => {
  const providers = [
    { id: 'p1', name: 'qwen', enabled: true, has_api_key: true },
    { id: 'p2', name: '', enabled: true, has_api_key: true },
    { id: 'p3', name: 'old', enabled: false, has_api_key: true },
    { id: 'p4', name: 'Ali Cloud', enabled: true, has_api_key: true },
    { id: 'p5', name: 'a/b', enabled: true, has_api_key: true },
    { id: 'p6', name: 'nokey', enabled: true, has_api_key: false },
  ] as ModelProvider[]
  const entries = [
    { id: 'e1', provider_id: 'p1', model: 'qwen-max', display_name: '通义千问 Max', hidden: false },
    { id: 'e2', provider_id: 'p1', model: 'qwen-hidden', display_name: '', hidden: true },
    { id: 'e3', provider_id: 'p2', model: 'deepseek-v3', display_name: '', hidden: false },
    { id: 'e4', provider_id: 'p3', model: 'gpt', display_name: '', hidden: false },
    { id: 'e5', provider_id: 'gone', model: 'x', display_name: '', hidden: false },
    { id: 'e6', provider_id: 'p1', model: 'qwen-max', display_name: 'dup', hidden: false },
    { id: 'e7', provider_id: 'p4', model: 'm4', display_name: '', hidden: false },
    { id: 'e8', provider_id: 'p5', model: 'm5', display_name: '', hidden: false },
    { id: 'e9', provider_id: 'p6', model: 'm6', display_name: '', hidden: false },
  ] as ModelCatalogEntry[]
  it('lists off first, then usable providers and visible entries as provider/model', () => {
    assert.deepEqual(handbookModelOptions(providers, entries), [
      { value: 'off', label: '禁用该仓库的 LLM 增强' },
      { value: 'p2/deepseek-v3', label: 'deepseek-v3' },
      { value: 'p4/m4', label: 'm4' },
      { value: 'p5/m5', label: 'm5' },
      { value: 'qwen/qwen-max', label: '通义千问 Max' },
    ])
  })
  it('falls back to off only', () => {
    assert.deepEqual(handbookModelOptions([], []), [{ value: 'off', label: '禁用该仓库的 LLM 增强' }])
  })
})

describe('enrichErrorMessage', () => {
  const err = (reason: string, status = 409) => ({ message: 'server says', reason, status })
  it('maps known reasons to friendly text', () => {
    assert.match(enrichErrorMessage(err('HANDBOOK_LLM_DISABLED', 400)), /未启用/)
    assert.match(enrichErrorMessage(err('HANDBOOK_NOT_READY')), /不是最新/)
    assert.equal(enrichErrorMessage(err('HANDBOOK_BUILDING')), '该仓库正在构建或增强中')
    assert.match(enrichErrorMessage(err('HANDBOOK_LLM_BUSY')), /其他仓库/)
    assert.match(enrichErrorMessage(err('HANDBOOK_DISABLED', 503)), /未配置/)
    assert.match(enrichErrorMessage(err('INVALID_ARGUMENT', 400)), /参数/)
    assert.match(enrichErrorMessage(err('NOT_FOUND', 404)), /不存在/)
    assert.match(enrichErrorMessage({ message: 'x', status: 404 }), /不存在/)
  })
  it('falls back to the server message', () => {
    assert.equal(enrichErrorMessage(err('SOMETHING_ELSE', 500)), 'server says')
    assert.equal(enrichErrorMessage(new Error('boom')), 'boom')
  })
})

describe('sortHandbookPages', () => {
  it('puts SKILL.md first and area pages last', () => {
    assert.deepEqual(
      sortHandbookPages([
        'references/areas/b.md',
        'references/registers.md',
        'SKILL.md',
        'references/areas/a.md',
        'references/index.md',
      ]),
      ['SKILL.md', 'references/index.md', 'references/registers.md', 'references/areas/a.md', 'references/areas/b.md'],
    )
  })
})
