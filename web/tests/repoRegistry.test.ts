import { describe, it } from 'node:test'
import assert from 'node:assert/strict'
import type {
  AgentRepoBinding,
  LegacyLinkMigrationItem,
  RepoGroupView,
  Repository,
} from '../src/api/repoRegistryTypes.ts'
import {
  bindingsEqual,
  groupNamesByRepo,
  includeRepo,
  isGroupIncluded,
  isRepoExcluded,
  parseSubPaths,
  parseTags,
  pendingAutoApplyCount,
  previewEffective,
  removeRepoBinding,
  repoFilterQuery,
  shortCommit,
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
