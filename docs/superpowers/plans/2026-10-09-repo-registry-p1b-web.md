# 仓库注册与分组绑定（P1b Web UI）Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** 在 web 前端提供仓库列表/扫描、仓库分组管理、旧链接迁移报告，以及 Agent 详情页的"代码仓库"绑定编辑区，使 30 个仓库的 agent 能一次勾选完成绑定。

**Architecture:** 新增 `api/repoRegistryTypes.ts`（纯类型）与 `api/repoRegistry.ts`（调用 P1 后端接口）；绑定编辑与有效仓库预览逻辑放在纯函数模块 `utils/repoRegistry.ts`（node:test 单测，规则与后端 `ExpandRepoBindings` 一致）；三张页面 `/repos`、`/repo-groups`、`/repos/migration` 共用一个侧栏入口和页内标签；绑定编辑做成 `RepoBindingsPanel` 组件放进 Agent 详情页（沿用 MCP 绑定"勾选 + 保存全量替换"的交互），Agent 编辑页只在已有绑定时提示链接被覆盖。

**Tech Stack:** React 19、react-router-dom v7、TypeScript（strict、`verbatimModuleSyntax`、`erasableSyntaxOnly`）、Vite、手写 CSS（无组件库）、`node --test --experimental-strip-types` 单测、Playwright e2e（`page.route` mock）。

**Spec:** `docs/superpowers/specs/2026-10-09-repo-registry-and-rca-handbook-design.md` §11（API）、§12（前端）、§14（迁移）、§15 P1 行；后端计划 `docs/superpowers/plans/2026-10-09-repo-registry-p1.md`（"实施偏差"与"运行时行为约定"两节）。

---

## 范围与取舍

- **做**：仓库列表（筛选、扫描、编辑名称/描述/标签、归档/恢复、查看使用该仓库的 agent）；分组列表（dir 组只读、manual 组新建/改成员/删除）；迁移报告（dry-run、应用自动迁移）；Agent 详情页绑定编辑（勾选组、组内排除、单独添加仓库含子目录、可见数预览与明细、清空、从其他 agent 复制）；Agent 编辑页的覆盖提示。
- **不做**：handbook 相关列（状态、落后天数、冻结比例、漏召回率）与 handbook 抽屉，属 P2；仓库批量操作（后端无 `/repos/batch`）；tag 组与 `pending_confirm` 确认（P4）；分页（后端列表接口不分页）；权限门控（前端无此机制，沿用"接口出错显示错误"）。
- **与设计 §12 的偏差**（Task 8 同步设计文档）：
  - 绑定编辑区放在 Agent **详情页**而不是编辑表单。理由：绑定是独立的全量 PUT，需要已有 agent id（新建表单没有）；详情页的 MCP 绑定已是同样的交互。编辑表单保留旧的 workspace/code 选择，已有绑定时显示覆盖提示。
  - 仓库表不做"使用 agent 数"列：列表接口不返回该计数，逐个请求详情代价太高；改为在编辑弹窗里列出使用该仓库的 agent。
  - "从其他 Agent 复制"的候选只取前 100 个 agent（后端单页上限），不支持搜索；agent 更多时再改为可搜索分页。
  - 读取绑定需要该 agent 的编辑权限（后端 `GetForEdit`），只有查看权限的用户在详情页会看到"无法加载代码仓库绑定"的提示，而不是绑定内容。

## 前端约定（实现前必读）

- 单测只覆盖纯函数：`web/tests/*.test.ts` 用 `node:test`，导入路径带 `.ts` 后缀（如 `'../src/utils/repoRegistry.ts'`）。被测模块**不能**有运行时导入 `client.ts`（node 无法解析无后缀导入）；只允许 `import type`（会被擦除）。
- `request<T>(path, init)` 从 `web/src/api/client.ts` 导出，路径相对 `/api/v1`，自动带认证头；非 2xx 抛 `Error(message)`，message 取 kratos 错误体的 `message`。仓库相关接口直接返回 JSON（没有 `ret` 包装），不要调用 `checkRet`。
- dev 代理与 nginx 已把 `/api/v1/repos*`、`/api/v1/repo-groups*`、`/api/v1/agents/{id}/repo-bindings*` 转发到 portal，无需改配置。
- 样式：沿用 `App.css` 的 `.page-header`、`.page-title-row`、`.page-count`、`.page-sub`、`.filter-bar`、`.table-card`、`.col-actions`、`.row-actions`、`.section`、`.section-title`、`.section-card`、`.section-card__footer`、`.detail-kv__hint`、`.empty-state`、`.badge`、`.btn`/`.btn-secondary`/`.btn-ghost`/`.btn-danger`/`.btn-sm`、`.form-group`、`.error`、`.success`、`.loading`/`.loading-spinner`。`App.css` **没有**全局 `.muted`，本功能在 `RepoRegistry.css` 里定义；变量 `--border`、`--muted` 在 `App.css` 中已存在。新样式写在 `web/src/pages/RepoRegistry.css`。注意 `App.tsx` 最后才导入 `App.css`，`RepoRegistry.css` 先进 bundle，与 `App.css` 同优先级的选择器会被覆盖，需要覆盖 `App.css` 时用组合选择器提高优先级。
- 类型检查与构建：`npm --prefix web run build`（`tsc && vite build`）。构建产物 `web/dist` 不要提交（先确认 `.gitignore` 已忽略）。
- 文案全部中文，硬编码；枚举中文名用常量映射表。
- 每个任务一次提交；只暂存该任务的文件，不要暂存 `evals/`。

## 文件结构

| 文件 | 动作 | 职责 |
|------|------|------|
| `web/src/api/repoRegistryTypes.ts` | 新建 | 与后端 JSON 一致的类型（纯类型，无运行时代码） |
| `web/src/utils/repoRegistry.ts` | 新建 | 纯函数：查询串、解析输入、绑定编辑、有效仓库预览、标签与迁移汇总 |
| `web/tests/repoRegistry.test.ts` | 新建 | 上述纯函数单测 |
| `web/src/api/repoRegistry.ts` | 新建 | `repoApi` / `repoGroupApi` / `repoBindingApi` |
| `web/src/api/client.ts` | 改 | `agentApi.workspaceLink` 返回类型补 `repo_bindings_override`、`warning` |
| `web/src/components/FormDialog.tsx` | 新建 | 带表单内容的模态框（复用 ConfirmDialog 样式） |
| `web/src/components/RepoRegistryTabs.tsx` | 新建 | 三个仓库页面之间的标签导航 |
| `web/src/components/RepoPicker.tsx` | 新建 | 可搜索的仓库多选（复选框列表） |
| `web/src/pages/RepoRegistry.css` | 新建 | 仓库相关页面与组件样式 |
| `web/src/pages/RepoListPage.tsx` | 新建 | `/repos` |
| `web/src/pages/RepoGroupListPage.tsx` | 新建 | `/repo-groups` |
| `web/src/pages/RepoMigrationPage.tsx` | 新建 | `/repos/migration` |
| `web/src/components/RepoBindingsPanel.tsx` | 新建 | Agent 详情页"代码仓库"区块 |
| `web/src/pages/AgentDetail.tsx` | 改 | 插入 `RepoBindingsPanel` |
| `web/src/pages/AgentForm.tsx` | 改 | 已有绑定时的覆盖提示 |
| `web/src/App.tsx` | 改 | 路由、侧栏入口、面包屑 |
| `web/e2e/repo-registry.spec.ts` | 新建 | Playwright（mock API） |
| `docs/superpowers/specs/2026-10-09-repo-registry-and-rca-handbook-design.md` | 改 | §12 同步 P1b 实际交互 |

---

## Task 1: 类型与纯函数（TDD）

**Files:**
- Create: `web/src/api/repoRegistryTypes.ts`
- Create: `web/src/utils/repoRegistry.ts`
- Test: `web/tests/repoRegistry.test.ts`

- [ ] **Step 1: 核对后端 JSON**

逐项对照 `portal/internal/biz/repo_registry.go` 与 `portal/internal/biz/repo_registry_usecase.go` 中以下结构体的 json tag：`Repository`、`RepoGroupRule`、`RepoGroup`、`RepoGroupView`、`AgentRepoBinding`、`BindingRef`、`AgentEffectiveRepo`、`EffectiveRepoView`、`AgentRepoBindingsView`、`RepoDetail`、`RepoScanReport`、`LegacyLinkMigrationItem`、`RepoMetaPatch`。下面的类型按已知 tag 写成；若有出入，以 Go 代码为准修改类型，并在提交说明里列出。

- [ ] **Step 2: 写类型文件**

`web/src/api/repoRegistryTypes.ts`:

```ts
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
```

- [ ] **Step 3: 写失败测试**

`web/tests/repoRegistry.test.ts`:

```ts
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
```

- [ ] **Step 4: 运行确认失败**

Run: `npm --prefix web test`
Expected: `repoRegistry.test.ts` 失败（找不到 `../src/utils/repoRegistry.ts`）；其他测试照常通过。

- [ ] **Step 5: 实现**

`web/src/utils/repoRegistry.ts`:

```ts
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
```

- [ ] **Step 6: 运行确认通过**

Run: `npm --prefix web test`
Expected: 全部 PASS（含 `repoRegistry.test.ts` 的所有用例）。

- [ ] **Step 7: 类型检查**

Run: `npm --prefix web run build`
Expected: `tsc` 无错误，vite 构建成功（新模块暂未被引用，构建只验证类型）。

- [ ] **Step 8: Commit**

```bash
git add web/src/api/repoRegistryTypes.ts web/src/utils/repoRegistry.ts web/tests/repoRegistry.test.ts
git commit -m "feat(web): repo registry types and binding helpers"
```

---

## Task 2: API 模块与共用组件

**Files:**
- Create: `web/src/api/repoRegistry.ts`
- Modify: `web/src/api/client.ts`（`agentApi.workspaceLink` 返回类型）
- Create: `web/src/components/FormDialog.tsx`
- Create: `web/src/components/RepoRegistryTabs.tsx`
- Create: `web/src/components/RepoPicker.tsx`
- Create: `web/src/pages/RepoRegistry.css`

- [ ] **Step 1: API 模块**

`web/src/api/repoRegistry.ts`:

```ts
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
```

- [ ] **Step 2: workspaceLink 返回类型**

`web/src/api/client.ts` 中 `agentApi.workspaceLink` 的泛型改为：

```ts
    request<{ link?: string; target?: string; repo_bindings_override?: boolean; warning?: string }>(
```

（其余不变。）

- [ ] **Step 3: FormDialog**

`web/src/components/FormDialog.tsx`:

```tsx
import type { ReactNode } from 'react'
import './ConfirmDialog.css'

export interface FormDialogProps {
  open: boolean
  title: string
  children: ReactNode
  confirmLabel?: string
  cancelLabel?: string
  loading?: boolean
  confirmDisabled?: boolean
  error?: string
  onConfirm: () => void
  onCancel: () => void
}

export function FormDialog({
  open,
  title,
  children,
  confirmLabel = '保存',
  cancelLabel = '取消',
  loading = false,
  confirmDisabled = false,
  error = '',
  onConfirm,
  onCancel,
}: FormDialogProps) {
  if (!open) return null
  return (
    <div className="confirm-dialog-backdrop" role="presentation">
      <div className="confirm-dialog form-dialog" role="dialog" aria-modal="true" aria-labelledby="form-dialog-title">
        <h2 id="form-dialog-title">{title}</h2>
        <div className="form-dialog__body">{children}</div>
        {error ? <div className="error form-dialog__error">{error}</div> : null}
        <div className="confirm-dialog-actions">
          <button type="button" className="btn btn-secondary" disabled={loading} onClick={onCancel}>
            {cancelLabel}
          </button>
          <button type="button" className="btn" disabled={loading || confirmDisabled} onClick={onConfirm}>
            {loading ? '处理中…' : confirmLabel}
          </button>
        </div>
      </div>
    </div>
  )
}
```

`.form-dialog` 样式写在 `RepoRegistry.css`，由使用方页面导入。

- [ ] **Step 4: RepoRegistryTabs**

`web/src/components/RepoRegistryTabs.tsx`:

```tsx
import { NavLink } from 'react-router-dom'

const TABS = [
  { to: '/repos', label: '仓库', end: true },
  { to: '/repo-groups', label: '分组', end: false },
  { to: '/repos/migration', label: '旧链接迁移', end: false },
]

export function RepoRegistryTabs() {
  return (
    <div className="repo-tabs">
      {TABS.map((t) => (
        <NavLink
          key={t.to}
          to={t.to}
          end={t.end}
          className={({ isActive }) => `btn btn-sm ${isActive ? '' : 'btn-ghost'}`}
        >
          {t.label}
        </NavLink>
      ))}
    </div>
  )
}
```

- [ ] **Step 5: RepoPicker**

`web/src/components/RepoPicker.tsx`:

```tsx
import { useMemo, useState } from 'react'
import type { Repository } from '../api/repoRegistryTypes'
import { REPO_STATUS_LABELS } from '../utils/repoRegistry'

export function RepoPicker({
  repos,
  selected,
  onChange,
}: {
  repos: Repository[]
  selected: string[]
  onChange: (ids: string[]) => void
}) {
  const [query, setQuery] = useState('')
  const chosen = useMemo(() => new Set(selected), [selected])
  const visible = useMemo(() => {
    const q = query.trim().toLowerCase()
    if (!q) return repos
    return repos.filter((r) => `${r.rel_path} ${r.name}`.toLowerCase().includes(q))
  }, [repos, query])

  const toggle = (id: string) => onChange(chosen.has(id) ? selected.filter((x) => x !== id) : [...selected, id])

  return (
    <div className="repo-picker">
      <div className="repo-picker__bar">
        <input value={query} onChange={(e) => setQuery(e.target.value)} placeholder="搜索仓库路径或名称" />
        <span className="muted">已选 {selected.length} 个</span>
      </div>
      <ul className="repo-picker__list">
        {visible.map((r) => (
          <li key={r.id}>
            <label className="repo-picker__item">
              <input type="checkbox" checked={chosen.has(r.id)} onChange={() => toggle(r.id)} />
              <code>{r.rel_path}</code>
              {r.name && r.name !== r.rel_path ? <span className="muted">{r.name}</span> : null}
              {r.status !== 'active' ? (
                <span className={`badge badge-repo-${r.status}`}>{REPO_STATUS_LABELS[r.status]}</span>
              ) : null}
            </label>
          </li>
        ))}
        {visible.length === 0 ? <li className="muted">没有匹配的仓库</li> : null}
      </ul>
    </div>
  )
}
```

- [ ] **Step 6: 样式**

`web/src/pages/RepoRegistry.css`:

```css
.muted {
  color: var(--muted);
}

.repo-tabs {
  display: flex;
  gap: 0.5rem;
  margin-bottom: 1rem;
}

.form-dialog {
  width: min(640px, 92vw);
  max-height: 86vh;
  display: flex;
  flex-direction: column;
}

.form-dialog__body {
  overflow: auto;
  display: flex;
  flex-direction: column;
  gap: 0.75rem;
  margin: 0.5rem 0 1rem;
}

.form-dialog__error {
  margin-bottom: 0.75rem;
}

.repo-picker {
  display: flex;
  flex-direction: column;
  gap: 0.5rem;
}

.repo-picker__bar {
  display: flex;
  gap: 0.75rem;
  align-items: center;
}

.repo-picker__bar input {
  flex: 1;
}

.repo-picker__list {
  list-style: none;
  margin: 0;
  padding: 0.25rem 0.5rem;
  max-height: 320px;
  overflow: auto;
  border: 1px solid var(--border);
  border-radius: 8px;
}

.repo-picker__item {
  display: flex;
  gap: 0.5rem;
  align-items: center;
  padding: 0.25rem 0;
  font-size: 0.875rem;
}

.repo-scan-report {
  margin-bottom: 1rem;
}

.repo-scan-report ul {
  margin: 0.25rem 0 0 1.25rem;
}

.cell-list {
  display: flex;
  flex-wrap: wrap;
  gap: 0.25rem;
}

.badge-repo-active {
  background: color-mix(in srgb, #16a34a 15%, transparent);
  color: #15803d;
}

.badge-repo-missing {
  background: color-mix(in srgb, #dc2626 15%, transparent);
  color: #b91c1c;
}

.badge-repo-archived {
  background: color-mix(in srgb, #6b7280 15%, transparent);
  color: #4b5563;
}

.repo-bindings__summary {
  display: flex;
  gap: 0.75rem;
  align-items: center;
  flex-wrap: wrap;
  margin-bottom: 0.75rem;
}

.repo-bindings__title {
  font-size: 0.95rem;
  margin: 1rem 0 0.5rem;
}

.repo-bindings__groups {
  list-style: none;
  margin: 0.5rem 0 0;
  padding: 0;
  display: flex;
  flex-direction: column;
  gap: 0.25rem;
  max-height: 420px;
  overflow: auto;
}

.repo-bindings__group {
  border: 1px solid var(--border);
  border-radius: 8px;
  padding: 0.4rem 0.6rem;
}

.repo-bindings__group-head {
  display: flex;
  gap: 0.5rem;
  align-items: center;
}

.repo-bindings__group-head label {
  flex: 1;
  display: flex;
  gap: 0.4rem;
  align-items: center;
}

.repo-bindings__members {
  list-style: none;
  margin: 0.4rem 0 0 1.5rem;
  padding: 0;
  display: flex;
  flex-direction: column;
  gap: 0.2rem;
  font-size: 0.85rem;
}

.repo-bindings__members li,
.repo-bindings__members label {
  display: flex;
  gap: 0.4rem;
  align-items: center;
}

.repo-bindings__add {
  display: flex;
  gap: 0.5rem;
  align-items: center;
  flex-wrap: wrap;
  margin: 0.5rem 0 1rem;
}

.repo-bindings__warn,
.detail-kv__hint.repo-bindings__warn {
  color: #b45309;
}
```

- [ ] **Step 7: 类型检查与单测**

Run: `npm --prefix web run build` 和 `npm --prefix web test`
Expected: 均成功。

- [ ] **Step 8: Commit**

```bash
git add web/src/api/repoRegistry.ts web/src/api/client.ts web/src/components/FormDialog.tsx web/src/components/RepoRegistryTabs.tsx web/src/components/RepoPicker.tsx web/src/pages/RepoRegistry.css
git commit -m "feat(web): repo registry api client and shared components"
```

---

## Task 3: 仓库列表页 `/repos` 与导航

**Files:**
- Create: `web/src/pages/RepoListPage.tsx`
- Modify: `web/src/App.tsx`

- [ ] **Step 1: 页面**

`web/src/pages/RepoListPage.tsx`:

```tsx
import { useCallback, useEffect, useMemo, useState, type FormEvent } from 'react'
import { Link } from 'react-router-dom'
import { repoApi, repoGroupApi } from '../api/repoRegistry'
import type { RepoFilter, RepoGroupView, RepoScanReport, Repository } from '../api/repoRegistryTypes'
import { ConfirmDialog } from '../components/ConfirmDialog'
import { FormDialog } from '../components/FormDialog'
import { RepoRegistryTabs } from '../components/RepoRegistryTabs'
import { REPO_STATUS_LABELS, groupNamesByRepo, parseTags, shortCommit } from '../utils/repoRegistry'
import './RepoRegistry.css'

interface EditState {
  repo: Repository
  name: string
  description: string
  tags: string
  agentIds: string[] | null
}

function formatTime(iso?: string): string {
  if (!iso) return '—'
  const d = new Date(iso)
  return Number.isNaN(d.getTime()) ? iso : d.toLocaleString()
}

export default function RepoListPage() {
  const [repos, setRepos] = useState<Repository[]>([])
  const [groups, setGroups] = useState<RepoGroupView[]>([])
  const [knownRoots, setKnownRoots] = useState<string[]>([])
  const [draft, setDraft] = useState<RepoFilter>({})
  const [filter, setFilter] = useState<RepoFilter>({})
  const [loading, setLoading] = useState(true)
  const [error, setError] = useState('')
  const [scanning, setScanning] = useState(false)
  const [scanReport, setScanReport] = useState<RepoScanReport | null>(null)
  const [scanError, setScanError] = useState('')
  const [edit, setEdit] = useState<EditState | null>(null)
  const [editSaving, setEditSaving] = useState(false)
  const [editError, setEditError] = useState('')
  const [pendingStatus, setPendingStatus] = useState<Repository | null>(null)
  const [statusSaving, setStatusSaving] = useState(false)

  const loadRepos = useCallback(async (f: RepoFilter) => {
    setLoading(true)
    setError('')
    try {
      const res = await repoApi.list(f)
      setRepos(res.items)
      setKnownRoots((prev) => [...new Set([...prev, ...res.items.map((r) => r.code_root)])].sort())
    } catch (e) {
      setError((e as Error).message)
    } finally {
      setLoading(false)
    }
  }, [])

  const loadGroups = useCallback(() => {
    repoGroupApi
      .list()
      .then((r) => setGroups(r.items))
      .catch(() => setGroups([]))
  }, [])

  useEffect(() => {
    void loadRepos(filter)
  }, [filter, loadRepos])

  useEffect(() => {
    loadGroups()
  }, [loadGroups])

  const groupNames = useMemo(() => groupNamesByRepo(groups), [groups])

  const submitFilter = (e: FormEvent) => {
    e.preventDefault()
    setFilter({ ...draft })
  }

  const resetFilter = () => {
    setDraft({})
    setFilter({})
  }

  const runScan = async () => {
    setScanning(true)
    setScanError('')
    setScanReport(null)
    try {
      setScanReport(await repoApi.scan())
      await loadRepos(filter)
      loadGroups()
    } catch (e) {
      setScanError((e as Error).message)
    } finally {
      setScanning(false)
    }
  }

  const openEdit = (repo: Repository) => {
    setEditError('')
    setEdit({ repo, name: repo.name, description: repo.description, tags: repo.tags.join(', '), agentIds: null })
    repoApi
      .get(repo.id)
      .then((d) => setEdit((prev) => (prev && prev.repo.id === repo.id ? { ...prev, agentIds: d.agent_ids } : prev)))
      .catch(() => {})
  }

  const saveEdit = async () => {
    if (!edit) return
    setEditSaving(true)
    setEditError('')
    try {
      const updated = await repoApi.patch(edit.repo.id, {
        name: edit.name.trim(),
        description: edit.description.trim(),
        tags: parseTags(edit.tags),
      })
      setRepos((prev) => prev.map((r) => (r.id === updated.id ? updated : r)))
      setEdit(null)
    } catch (e) {
      setEditError((e as Error).message)
    } finally {
      setEditSaving(false)
    }
  }

  const confirmStatus = async () => {
    if (!pendingStatus) return
    setStatusSaving(true)
    try {
      const updated = await repoApi.patch(pendingStatus.id, {
        status: pendingStatus.status === 'archived' ? 'active' : 'archived',
      })
      setRepos((prev) => prev.map((r) => (r.id === updated.id ? updated : r)))
      setPendingStatus(null)
    } catch (e) {
      alert((e as Error).message)
    } finally {
      setStatusSaving(false)
    }
  }

  const restoring = pendingStatus?.status === 'archived'

  return (
    <div>
      <div className="page-header">
        <div>
          <div className="page-title-row">
            <h1>代码仓库</h1>
            <span className="page-count">{repos.length}</span>
          </div>
          <p className="page-sub">code root 下自动发现的 git 仓库。启动时和每 10 分钟扫描一次，也可以手动扫描。</p>
        </div>
        <button type="button" className="btn" onClick={() => void runScan()} disabled={scanning}>
          {scanning ? '扫描中…' : '立即扫描'}
        </button>
      </div>
      <RepoRegistryTabs />

      {scanError ? <div className="error repo-scan-report">扫描失败：{scanError}</div> : null}
      {scanReport ? (
        <div className="success repo-scan-report" data-testid="repo-scan-report">
          扫描完成：code root {scanReport.roots} 个，发现 {scanReport.found}，新增 {scanReport.added}，恢复{' '}
          {scanReport.restored}，缺失 {scanReport.missing}
          {scanReport.errors?.length ? (
            <ul>
              {scanReport.errors.map((msg) => (
                <li key={msg} className="error">
                  {msg}
                </li>
              ))}
            </ul>
          ) : null}
        </div>
      ) : null}

      <form className="filter-bar" onSubmit={submitFilter}>
        <select value={draft.status ?? ''} onChange={(e) => setDraft({ ...draft, status: e.target.value })}>
          <option value="">全部状态</option>
          <option value="active">{REPO_STATUS_LABELS.active}</option>
          <option value="missing">{REPO_STATUS_LABELS.missing}</option>
          <option value="archived">{REPO_STATUS_LABELS.archived}</option>
        </select>
        <select value={draft.code_root ?? ''} onChange={(e) => setDraft({ ...draft, code_root: e.target.value })}>
          <option value="">全部 code root</option>
          {knownRoots.map((r) => (
            <option key={r} value={r}>
              {r}
            </option>
          ))}
        </select>
        <select value={draft.group_id ?? ''} onChange={(e) => setDraft({ ...draft, group_id: e.target.value })}>
          <option value="">全部分组</option>
          {groups.map((g) => (
            <option key={g.id} value={g.id}>
              {g.name}
            </option>
          ))}
        </select>
        <input
          value={draft.q ?? ''}
          onChange={(e) => setDraft({ ...draft, q: e.target.value })}
          placeholder="搜索路径或名称"
        />
        <button type="submit" className="btn btn-sm">
          查询
        </button>
        <button type="button" className="btn btn-ghost btn-sm" onClick={resetFilter}>
          重置
        </button>
      </form>

      {error ? <div className="error">加载失败：{error}</div> : null}
      {loading ? (
        <div className="loading">
          <div className="loading-spinner" />
          <span style={{ marginLeft: '0.75rem' }}>加载中…</span>
        </div>
      ) : repos.length === 0 ? (
        <div className="section-card empty-state">
          <p>没有符合条件的仓库。新部署时先点「立即扫描」。</p>
        </div>
      ) : (
        <div className="table-card">
          <table>
            <thead>
              <tr>
                <th>仓库</th>
                <th>分组</th>
                <th>标签</th>
                <th>分支 / 提交</th>
                <th>状态</th>
                <th>最近扫描</th>
                <th className="col-actions">操作</th>
              </tr>
            </thead>
            <tbody>
              {repos.map((r) => (
                <tr key={r.id}>
                  <td>
                    <code>{r.rel_path}</code>
                    {r.name && r.name !== r.rel_path ? <div className="muted">{r.name}</div> : null}
                    <div className="muted" title={r.code_root}>
                      {r.code_root}
                    </div>
                  </td>
                  <td>
                    <div className="cell-list">
                      {(groupNames.get(r.id) ?? []).map((n) => (
                        <span key={n} className="badge">
                          {n}
                        </span>
                      ))}
                    </div>
                  </td>
                  <td>
                    <div className="cell-list">
                      {r.tags.map((t) => (
                        <span key={t} className="badge">
                          {t}
                        </span>
                      ))}
                    </div>
                  </td>
                  <td>
                    {r.git_branch || '—'} <code title={r.head_commit}>{shortCommit(r.head_commit)}</code>
                  </td>
                  <td>
                    <span className={`badge badge-repo-${r.status}`}>{REPO_STATUS_LABELS[r.status]}</span>
                  </td>
                  <td>{formatTime(r.last_scanned_at)}</td>
                  <td className="col-actions">
                    <div className="row-actions">
                      <button type="button" className="btn btn-ghost btn-sm" onClick={() => openEdit(r)}>
                        编辑
                      </button>
                      <button
                        type="button"
                        className={`btn btn-ghost btn-sm ${r.status === 'archived' ? '' : 'btn-danger'}`}
                        onClick={() => setPendingStatus(r)}
                      >
                        {r.status === 'archived' ? '恢复' : '归档'}
                      </button>
                    </div>
                  </td>
                </tr>
              ))}
            </tbody>
          </table>
        </div>
      )}

      <FormDialog
        open={!!edit}
        title={edit ? `编辑仓库 ${edit.repo.rel_path}` : ''}
        loading={editSaving}
        error={editError}
        onCancel={() => setEdit(null)}
        onConfirm={() => void saveEdit()}
      >
        {edit ? (
          <>
            <div className="form-group">
              <label>名称</label>
              <input value={edit.name} onChange={(e) => setEdit({ ...edit, name: e.target.value })} />
            </div>
            <div className="form-group">
              <label>描述</label>
              <textarea
                rows={3}
                value={edit.description}
                onChange={(e) => setEdit({ ...edit, description: e.target.value })}
              />
            </div>
            <div className="form-group">
              <label>标签（逗号分隔）</label>
              <input value={edit.tags} onChange={(e) => setEdit({ ...edit, tags: e.target.value })} />
            </div>
            <div className="form-group">
              <label>使用该仓库的 Agent</label>
              {edit.agentIds === null ? (
                <span className="muted">加载中…</span>
              ) : edit.agentIds.length === 0 ? (
                <span className="muted">无</span>
              ) : (
                <div className="cell-list">
                  {edit.agentIds.map((id) => (
                    <Link key={id} to={`/agents/${id}`} className="badge">
                      {id}
                    </Link>
                  ))}
                </div>
              )}
            </div>
          </>
        ) : null}
      </FormDialog>

      <ConfirmDialog
        open={!!pendingStatus}
        title={restoring ? '恢复仓库' : '归档仓库'}
        description={
          pendingStatus
            ? restoring
              ? `恢复「${pendingStatus.rel_path}」后，绑定了它的 Agent 会重新看到它（目录仍存在时）。`
              : `归档「${pendingStatus.rel_path}」后，它会从所有 Agent 的有效仓库中移除，扫描不会自动恢复。`
            : ''
        }
        confirmLabel={restoring ? '恢复' : '归档'}
        cancelLabel="取消"
        variant={restoring ? 'default' : 'danger'}
        loading={statusSaving}
        onCancel={() => setPendingStatus(null)}
        onConfirm={() => void confirmStatus()}
      />
    </div>
  )
}
```

- [ ] **Step 2: 路由与导航**

`web/src/App.tsx`：

1. 在 `import EvolutionReviewPage ...` 之后加：

```tsx
import RepoListPage from './pages/RepoListPage'
```

2. `Breadcrumb()` 的 `evolution-review` 分支之后、闭合 `}` 之前加：

```tsx
  } else if (segments[0] === 'repos') {
    current = segments[1] === 'migration' ? '旧链接迁移' : '代码仓库'
    icon = '📦'
  } else if (segments[0] === 'repo-groups') {
    current = '仓库分组'
    icon = '📦'
```

3. 侧栏 `Agent 管理` 的 `NavLink` 之后加（`/repo-groups` 也要高亮，所以用 `loc.pathname` 判断）：

```tsx
            <NavLink
              to="/repos"
              className={() => `nav-item ${loc.pathname.startsWith('/repo') ? 'active' : ''}`}
            >
              <span className="nav-item__icon">📦</span>
              代码仓库
            </NavLink>
```

4. `<Routes>` 中 `/evolution-review` 之前加：

```tsx
            <Route path="/repos" element={<RepoListPage />} />
```

- [ ] **Step 3: 类型检查**

Run: `npm --prefix web run build`
Expected: 成功。

- [ ] **Step 4: 手工检查（可选）**

`npm --prefix web run dev` 后访问 `http://localhost:5173/repos`（portal 未启动时显示加载失败即可；页头、标签、筛选栏应正常渲染）。

- [ ] **Step 5: Commit**

```bash
git add web/src/pages/RepoListPage.tsx web/src/App.tsx
git commit -m "feat(web): repository list page with scan and archive"
```

---

## Task 4: 分组页 `/repo-groups`

**Files:**
- Create: `web/src/pages/RepoGroupListPage.tsx`
- Modify: `web/src/App.tsx`

- [ ] **Step 1: 页面**

`web/src/pages/RepoGroupListPage.tsx`:

```tsx
import { Fragment, useCallback, useEffect, useMemo, useState } from 'react'
import { repoApi, repoGroupApi } from '../api/repoRegistry'
import type { RepoGroupKind, RepoGroupView, Repository } from '../api/repoRegistryTypes'
import { ConfirmDialog } from '../components/ConfirmDialog'
import { FormDialog } from '../components/FormDialog'
import { RepoPicker } from '../components/RepoPicker'
import { RepoRegistryTabs } from '../components/RepoRegistryTabs'
import { GROUP_KIND_LABELS, REPO_STATUS_LABELS } from '../utils/repoRegistry'
import './RepoRegistry.css'

type DialogState =
  | { mode: 'create'; name: string; repoIds: string[] }
  | { mode: 'edit'; group: RepoGroupView; repoIds: string[] }

function ruleText(g: RepoGroupView): string {
  if (!g.rule?.rel_prefix) return '—'
  return g.rule.code_root ? `${g.rule.code_root} / ${g.rule.rel_prefix}` : g.rule.rel_prefix
}

export default function RepoGroupListPage() {
  const [groups, setGroups] = useState<RepoGroupView[]>([])
  const [repos, setRepos] = useState<Repository[]>([])
  const [kind, setKind] = useState<RepoGroupKind | ''>('')
  const [loading, setLoading] = useState(true)
  const [error, setError] = useState('')
  const [notice, setNotice] = useState('')
  const [expanded, setExpanded] = useState<Set<string>>(new Set())
  const [dialog, setDialog] = useState<DialogState | null>(null)
  const [dialogSaving, setDialogSaving] = useState(false)
  const [dialogError, setDialogError] = useState('')
  const [pendingDelete, setPendingDelete] = useState<RepoGroupView | null>(null)
  const [deleting, setDeleting] = useState(false)

  const load = useCallback(async () => {
    setLoading(true)
    setError('')
    try {
      const [g, r] = await Promise.all([repoGroupApi.list(kind || undefined), repoApi.list()])
      setGroups(g.items)
      setRepos(r.items)
    } catch (e) {
      setError((e as Error).message)
    } finally {
      setLoading(false)
    }
  }, [kind])

  useEffect(() => {
    void load()
  }, [load])

  const repoById = useMemo(() => new Map(repos.map((r) => [r.id, r])), [repos])

  const toggleExpanded = (id: string) =>
    setExpanded((prev) => {
      const next = new Set(prev)
      if (next.has(id)) next.delete(id)
      else next.add(id)
      return next
    })

  const openCreate = () => {
    setDialogError('')
    setDialog({ mode: 'create', name: '', repoIds: [] })
  }

  const openEdit = (group: RepoGroupView) => {
    setDialogError('')
    setDialog({ mode: 'edit', group, repoIds: [...group.repo_ids] })
  }

  const saveDialog = async () => {
    if (!dialog) return
    setDialogSaving(true)
    setDialogError('')
    try {
      if (dialog.mode === 'create') await repoGroupApi.create(dialog.name.trim(), dialog.repoIds)
      else await repoGroupApi.setMembers(dialog.group.id, dialog.repoIds)
      setDialog(null)
      await load()
    } catch (e) {
      setDialogError((e as Error).message)
    } finally {
      setDialogSaving(false)
    }
  }

  const confirmDelete = async () => {
    if (!pendingDelete) return
    setDeleting(true)
    setNotice('')
    try {
      await repoGroupApi.remove(pendingDelete.id)
      setGroups((prev) => prev.filter((g) => g.id !== pendingDelete.id))
    } catch (e) {
      setNotice(`删除失败：${(e as Error).message}`)
    } finally {
      setDeleting(false)
      setPendingDelete(null)
    }
  }

  return (
    <div>
      <div className="page-header">
        <div>
          <div className="page-title-row">
            <h1>仓库分组</h1>
            <span className="page-count">{groups.length}</span>
          </div>
          <p className="page-sub">目录组按仓库的直接父目录自动生成、随扫描更新；手工组可自由挑选成员。</p>
        </div>
        <button type="button" className="btn" onClick={openCreate}>
          新建手工组
        </button>
      </div>
      <RepoRegistryTabs />

      <div className="filter-bar">
        <select value={kind} onChange={(e) => setKind(e.target.value as RepoGroupKind | '')}>
          <option value="">全部类型</option>
          <option value="dir">{GROUP_KIND_LABELS.dir}</option>
          <option value="manual">{GROUP_KIND_LABELS.manual}</option>
        </select>
      </div>

      {notice ? <div className="error">{notice}</div> : null}
      {error ? <div className="error">加载失败：{error}</div> : null}
      {loading ? (
        <div className="loading">
          <div className="loading-spinner" />
          <span style={{ marginLeft: '0.75rem' }}>加载中…</span>
        </div>
      ) : groups.length === 0 ? (
        <div className="section-card empty-state">
          <p>暂无分组。扫描仓库后会自动生成目录组。</p>
        </div>
      ) : (
        <div className="table-card">
          <table>
            <thead>
              <tr>
                <th>名称</th>
                <th>类型</th>
                <th>目录</th>
                <th>成员</th>
                <th className="col-actions">操作</th>
              </tr>
            </thead>
            <tbody>
              {groups.map((g) => (
                <Fragment key={g.id}>
                  <tr>
                    <td>{g.name}</td>
                    <td>
                      <span className="badge">{GROUP_KIND_LABELS[g.kind]}</span>
                    </td>
                    <td className="cell-desc">{ruleText(g)}</td>
                    <td>{g.repo_ids.length}</td>
                    <td className="col-actions">
                      <div className="row-actions">
                        <button type="button" className="btn btn-ghost btn-sm" onClick={() => toggleExpanded(g.id)}>
                          {expanded.has(g.id) ? '收起成员' : '查看成员'}
                        </button>
                        {g.kind === 'manual' ? (
                          <>
                            <button type="button" className="btn btn-ghost btn-sm" onClick={() => openEdit(g)}>
                              编辑成员
                            </button>
                            <button
                              type="button"
                              className="btn btn-ghost btn-sm btn-danger"
                              onClick={() => setPendingDelete(g)}
                            >
                              删除
                            </button>
                          </>
                        ) : null}
                      </div>
                    </td>
                  </tr>
                  {expanded.has(g.id) ? (
                    <tr>
                      <td colSpan={5}>
                        {g.repo_ids.length === 0 ? (
                          <span className="muted">没有成员</span>
                        ) : (
                          <div className="cell-list">
                            {g.repo_ids.map((id) => {
                              const r = repoById.get(id)
                              return (
                                <span key={id} className="cell-list">
                                  <code>{r?.rel_path ?? id}</code>
                                  {r && r.status !== 'active' ? (
                                    <span className={`badge badge-repo-${r.status}`}>{REPO_STATUS_LABELS[r.status]}</span>
                                  ) : null}
                                </span>
                              )
                            })}
                          </div>
                        )}
                      </td>
                    </tr>
                  ) : null}
                </Fragment>
              ))}
            </tbody>
          </table>
        </div>
      )}

      <FormDialog
        open={!!dialog}
        title={dialog?.mode === 'edit' ? `编辑成员：${dialog.group.name}` : '新建手工组'}
        loading={dialogSaving}
        error={dialogError}
        confirmDisabled={dialog?.mode === 'create' && !dialog.name.trim()}
        onCancel={() => setDialog(null)}
        onConfirm={() => void saveDialog()}
      >
        {dialog ? (
          <>
            {dialog.mode === 'create' ? (
              <div className="form-group">
                <label>名称</label>
                <input value={dialog.name} onChange={(e) => setDialog({ ...dialog, name: e.target.value })} />
              </div>
            ) : null}
            <RepoPicker
              repos={repos}
              selected={dialog.repoIds}
              onChange={(ids) => setDialog({ ...dialog, repoIds: ids })}
            />
          </>
        ) : null}
      </FormDialog>

      <ConfirmDialog
        open={!!pendingDelete}
        title="删除手工组"
        description={
          pendingDelete ? `删除「${pendingDelete.name}」。仍被 Agent 绑定的组不能删除，需先在这些 Agent 中解除绑定。` : ''
        }
        confirmLabel="删除"
        cancelLabel="取消"
        variant="danger"
        loading={deleting}
        onCancel={() => setPendingDelete(null)}
        onConfirm={() => void confirmDelete()}
      />
    </div>
  )
}
```

- [ ] **Step 2: 路由**

`web/src/App.tsx`：加 `import RepoGroupListPage from './pages/RepoGroupListPage'`，并在 `/repos` 路由之后加：

```tsx
            <Route path="/repo-groups" element={<RepoGroupListPage />} />
```

- [ ] **Step 3: 类型检查**

Run: `npm --prefix web run build`
Expected: 成功。

- [ ] **Step 4: Commit**

```bash
git add web/src/pages/RepoGroupListPage.tsx web/src/App.tsx
git commit -m "feat(web): repository groups page"
```

---

## Task 5: 迁移页 `/repos/migration`

**Files:**
- Create: `web/src/pages/RepoMigrationPage.tsx`
- Modify: `web/src/App.tsx`

- [ ] **Step 1: 页面**

`web/src/pages/RepoMigrationPage.tsx`:

```tsx
import { useMemo, useState } from 'react'
import { Link } from 'react-router-dom'
import { repoApi } from '../api/repoRegistry'
import type { LegacyLinkMigrationItem } from '../api/repoRegistryTypes'
import { ConfirmDialog } from '../components/ConfirmDialog'
import { RepoRegistryTabs } from '../components/RepoRegistryTabs'
import {
  LEGACY_ACTIONS,
  LEGACY_ACTION_LABELS,
  isAutoApplyAction,
  pendingAutoApplyCount,
  summarizeMigration,
} from '../utils/repoRegistry'
import './RepoRegistry.css'

const AFTER_ROOTS_PREVIEW = 5

function AfterRoots({ roots }: { roots?: string[] }) {
  if (!roots || roots.length === 0) return <span className="muted">—</span>
  const shown = roots.slice(0, AFTER_ROOTS_PREVIEW)
  return (
    <div className="cell-list" title={roots.join('\n')}>
      {shown.map((r) => (
        <code key={r}>{r}</code>
      ))}
      {roots.length > shown.length ? <span className="muted">等 {roots.length} 个</span> : null}
    </div>
  )
}

function itemState(it: LegacyLinkMigrationItem): string {
  if (it.applied) return '已写入'
  if (it.error) return '失败'
  return isAutoApplyAction(it.action) ? '可自动写入' : '需人工处理'
}

export default function RepoMigrationPage() {
  const [items, setItems] = useState<LegacyLinkMigrationItem[] | null>(null)
  const [running, setRunning] = useState(false)
  const [error, setError] = useState('')
  const [confirmOpen, setConfirmOpen] = useState(false)

  const summary = useMemo(() => summarizeMigration(items ?? []), [items])
  const pending = useMemo(() => pendingAutoApplyCount(items ?? []), [items])

  const run = async (apply: boolean) => {
    setRunning(true)
    setError('')
    try {
      const res = await repoApi.migrateLegacyLinks(apply)
      setItems(res.items)
    } catch (e) {
      setError((e as Error).message)
    } finally {
      setRunning(false)
      setConfirmOpen(false)
    }
  }

  return (
    <div>
      <div className="page-header">
        <div>
          <h1>旧链接迁移</h1>
          <p className="page-sub">
            把 Agent 的 workspace/code 链接换成仓库绑定。只列出你有编辑权限的 Agent；生成报告不会写入任何数据。
          </p>
        </div>
        <div className="row-actions">
          <button type="button" className="btn btn-secondary" disabled={running} onClick={() => void run(false)}>
            {running ? '处理中…' : '生成报告'}
          </button>
          <button
            type="button"
            className="btn"
            disabled={running || pending === 0}
            onClick={() => setConfirmOpen(true)}
            data-testid="migration-apply"
          >
            应用自动迁移（{pending}）
          </button>
        </div>
      </div>
      <RepoRegistryTabs />

      {error ? <div className="error">{error}</div> : null}
      {items === null ? (
        <div className="section-card empty-state">
          <p>点「生成报告」查看每个 Agent 的迁移方案。精确命中一个仓库或一个目录组的 Agent 可以自动写入，其余需要在 Agent 详情页手动绑定。</p>
        </div>
      ) : items.length === 0 ? (
        <div className="section-card empty-state">
          <p>没有需要处理的 Agent。</p>
        </div>
      ) : (
        <>
          <div className="filter-bar" data-testid="migration-summary">
            {LEGACY_ACTIONS.filter((a) => summary[a] > 0).map((a) => (
              <span key={a} className="badge">
                {LEGACY_ACTION_LABELS[a]} {summary[a]}
              </span>
            ))}
          </div>
          <div className="table-card">
            <table>
              <thead>
                <tr>
                  <th>Agent</th>
                  <th>链接目标</th>
                  <th>方案</th>
                  <th>迁移后可见</th>
                  <th>说明</th>
                  <th>状态</th>
                </tr>
              </thead>
              <tbody>
                {items.map((it) => (
                  <tr key={it.agent_id}>
                    <td>
                      <Link to={`/agents/${it.agent_id}`}>{it.agent_id}</Link>
                    </td>
                    <td>
                      <code>{it.target || '—'}</code>
                    </td>
                    <td>{LEGACY_ACTION_LABELS[it.action] ?? it.action}</td>
                    <td>
                      <AfterRoots roots={it.after_roots} />
                    </td>
                    <td className={it.error ? 'error' : 'cell-desc'} title={it.error || it.reason || ''}>
                      {it.error || it.reason || '—'}
                    </td>
                    <td>{itemState(it)}</td>
                  </tr>
                ))}
              </tbody>
            </table>
          </div>
        </>
      )}

      <ConfirmDialog
        open={confirmOpen}
        title="应用自动迁移"
        description={`将为 ${pending} 个 Agent 写入仓库绑定。写入后它们的 RCA 代码工具改用绑定的仓库，不再读取 workspace/code 链接。`}
        confirmLabel="应用"
        cancelLabel="取消"
        loading={running}
        onCancel={() => setConfirmOpen(false)}
        onConfirm={() => void run(true)}
      />
    </div>
  )
}
```

- [ ] **Step 2: 路由**

`web/src/App.tsx`：加 `import RepoMigrationPage from './pages/RepoMigrationPage'`，并在 `/repos` 路由之后加：

```tsx
            <Route path="/repos/migration" element={<RepoMigrationPage />} />
```

- [ ] **Step 3: 类型检查**

Run: `npm --prefix web run build`
Expected: 成功。

- [ ] **Step 4: Commit**

```bash
git add web/src/pages/RepoMigrationPage.tsx web/src/App.tsx
git commit -m "feat(web): legacy workspace link migration page"
```

---

## Task 6: Agent 详情页"代码仓库"区块

**Files:**
- Create: `web/src/components/RepoBindingsPanel.tsx`
- Modify: `web/src/pages/AgentDetail.tsx`

- [ ] **Step 1: 确认 `agentApi.list` 返回结构**

读 `web/src/api/client.ts` 中 `agentApi.list`（约第 934 行），确认返回 `{ items: Agent[] ... }`；若字段不同，相应调整下面 `setAgents` 一行。

- [ ] **Step 2: 组件**

`web/src/components/RepoBindingsPanel.tsx`:

```tsx
import { useCallback, useEffect, useMemo, useState } from 'react'
import { agentApi, type Agent } from '../api/client'
import { repoApi, repoBindingApi, repoGroupApi } from '../api/repoRegistry'
import type { AgentRepoBinding, AgentRepoBindingsView, RepoGroupView, Repository } from '../api/repoRegistryTypes'
import { ConfirmDialog } from './ConfirmDialog'
import { SearchableChipSelect } from './SearchableChipSelect'
import {
  GROUP_KIND_LABELS,
  REPO_STATUS_LABELS,
  bindingsEqual,
  includeRepo,
  isGroupIncluded,
  isRepoExcluded,
  parseSubPaths,
  previewEffective,
  removeRepoBinding,
  toggleGroup,
  toggleRepoExclude,
  viaLabel,
} from '../utils/repoRegistry'
import '../pages/RepoRegistry.css'

const AGENT_PAGE_SIZE = 100

export default function RepoBindingsPanel({ agentId }: { agentId: string }) {
  const [view, setView] = useState<AgentRepoBindingsView | null>(null)
  const [draft, setDraft] = useState<AgentRepoBinding[]>([])
  const [groups, setGroups] = useState<RepoGroupView[]>([])
  const [repos, setRepos] = useState<Repository[]>([])
  const [agents, setAgents] = useState<Agent[]>([])
  const [loadError, setLoadError] = useState('')
  const [groupQuery, setGroupQuery] = useState('')
  const [expanded, setExpanded] = useState<Set<string>>(new Set())
  const [addRepoId, setAddRepoId] = useState('')
  const [addSubPaths, setAddSubPaths] = useState('')
  const [copyFromId, setCopyFromId] = useState('')
  const [confirm, setConfirm] = useState<'clear' | 'copy' | null>(null)
  const [saving, setSaving] = useState(false)
  const [msg, setMsg] = useState<{ ok: boolean; text: string } | null>(null)
  const [showDetail, setShowDetail] = useState(false)

  const load = useCallback(async () => {
    setLoadError('')
    try {
      const [v, g, r] = await Promise.all([repoBindingApi.get(agentId), repoGroupApi.list(), repoApi.list()])
      setView(v)
      setDraft(v.bindings)
      setGroups(g.items)
      setRepos(r.items)
    } catch (e) {
      setLoadError(`无法加载代码仓库绑定（需要该 Agent 的编辑权限）：${(e as Error).message}`)
    }
  }, [agentId])

  useEffect(() => {
    void load()
  }, [load])

  useEffect(() => {
    agentApi
      .list({ page: 1, page_size: AGENT_PAGE_SIZE })
      .then((r) => setAgents(r.items.filter((a) => a.id !== agentId)))
      .catch(() => setAgents([]))
  }, [agentId])

  const repoById = useMemo(() => new Map(repos.map((r) => [r.id, r])), [repos])
  const preview = useMemo(() => previewEffective(draft, groups, repos), [draft, groups, repos])
  const dirty = view ? !bindingsEqual(draft, view.bindings) : false
  const visibleGroups = useMemo(() => {
    const q = groupQuery.trim().toLowerCase()
    const list = q ? groups.filter((g) => g.name.toLowerCase().includes(q)) : groups
    return [...list].sort((a, b) => a.name.localeCompare(b.name))
  }, [groups, groupQuery])
  const repoOptions = useMemo(
    () =>
      repos
        .filter((r) => r.status === 'active')
        .map((r) => ({ value: r.id, label: r.rel_path, group: r.code_root })),
    [repos],
  )
  const singleBindings = draft.filter((b) => b.target_kind === 'repo')

  const repoLabel = (id: string) => repoById.get(id)?.rel_path ?? id

  const edit = (next: AgentRepoBinding[]) => {
    setDraft(next)
    setMsg(null)
  }

  const toggleExpanded = (id: string) =>
    setExpanded((prev) => {
      const next = new Set(prev)
      if (next.has(id)) next.delete(id)
      else next.add(id)
      return next
    })

  const addRepo = () => {
    if (!addRepoId) return
    edit(includeRepo(draft, addRepoId, parseSubPaths(addSubPaths)))
    setAddRepoId('')
    setAddSubPaths('')
  }

  const applyView = (v: AgentRepoBindingsView, text: string) => {
    setView(v)
    setDraft(v.bindings)
    setMsg({ ok: true, text })
  }

  const save = async (bindings: AgentRepoBinding[]) => {
    setSaving(true)
    setMsg(null)
    try {
      applyView(await repoBindingApi.replace(agentId, bindings), '已保存')
    } catch (e) {
      setMsg({ ok: false, text: (e as Error).message })
    } finally {
      setSaving(false)
      setConfirm(null)
    }
  }

  const copy = async () => {
    if (!copyFromId) return
    setSaving(true)
    setMsg(null)
    try {
      applyView(await repoBindingApi.copyFrom(agentId, copyFromId), '已复制')
      setCopyFromId('')
    } catch (e) {
      setMsg({ ok: false, text: (e as Error).message })
    } finally {
      setSaving(false)
      setConfirm(null)
    }
  }

  return (
    <section className="section" data-testid="repo-bindings-section">
      <h2 className="section-title">代码仓库</h2>
      <div className="section-card">
        {loadError ? <div className="error">{loadError}</div> : null}
        {!view && !loadError ? <p className="muted">加载中…</p> : null}
        {view ? (
          <>
            <p className="detail-kv__hint">
              {view.bindings.length === 0
                ? '尚未绑定仓库：RCA 代码工具使用 workspace/code 链接（旧方式）。保存绑定后以这里为准。'
                : 'RCA 代码工具只读取下列绑定展开后的仓库，不再使用 workspace/code 链接。'}
            </p>

            <div className="repo-bindings__summary" data-testid="repo-bindings-summary">
              <strong>
                {dirty ? '保存后' : '当前'}可见 {preview.length} 个仓库
              </strong>
              {dirty ? <span className="muted">（有未保存的修改）</span> : null}
              {draft.length > 0 && preview.length === 0 ? (
                <span className="repo-bindings__warn">当前绑定没有可用仓库，保存后该 Agent 的 RCA 代码工具不可用。</span>
              ) : null}
              <button type="button" className="btn btn-ghost btn-sm" onClick={() => setShowDetail((s) => !s)}>
                {showDetail ? '收起明细' : '查看明细'}
              </button>
            </div>
            {showDetail ? (
              <div className="table-card">
                <table>
                  <thead>
                    <tr>
                      <th>仓库</th>
                      <th>范围</th>
                      <th>来源</th>
                    </tr>
                  </thead>
                  <tbody>
                    {preview.map((p) => (
                      <tr key={p.repo.id}>
                        <td>
                          <code>{p.repo.rel_path}</code>
                        </td>
                        <td>{p.subPaths ? p.subPaths.join(', ') : '整个仓库'}</td>
                        <td>{viaLabel(p.via, groups)}</td>
                      </tr>
                    ))}
                  </tbody>
                </table>
              </div>
            ) : null}

            <h3 className="repo-bindings__title">按分组绑定</h3>
            <input value={groupQuery} onChange={(e) => setGroupQuery(e.target.value)} placeholder="搜索分组" />
            <ul className="repo-bindings__groups">
              {visibleGroups.map((g) => {
                const included = isGroupIncluded(draft, g.id)
                return (
                  <li key={g.id} className="repo-bindings__group">
                    <div className="repo-bindings__group-head">
                      <label>
                        <input
                          type="checkbox"
                          checked={included}
                          onChange={() => edit(toggleGroup(draft, g.id))}
                          data-testid={`repo-group-${g.id}`}
                        />
                        {g.name}
                      </label>
                      <span className="badge">{GROUP_KIND_LABELS[g.kind]}</span>
                      <span className="muted">{g.repo_ids.length} 个仓库</span>
                      <button type="button" className="btn btn-ghost btn-sm" onClick={() => toggleExpanded(g.id)}>
                        {expanded.has(g.id) ? '收起' : '展开'}
                      </button>
                    </div>
                    {expanded.has(g.id) ? (
                      <ul className="repo-bindings__members">
                        {g.repo_ids.map((id) => (
                          <li key={id}>
                            <label>
                              <input
                                type="checkbox"
                                disabled={!included}
                                checked={included && !isRepoExcluded(draft, id)}
                                onChange={() => edit(toggleRepoExclude(draft, id))}
                                data-testid={`repo-member-${g.id}-${id}`}
                              />
                              <code>{repoLabel(id)}</code>
                            </label>
                          </li>
                        ))}
                      </ul>
                    ) : null}
                  </li>
                )
              })}
              {visibleGroups.length === 0 ? <li className="muted">没有分组。先到「代码仓库」页扫描。</li> : null}
            </ul>

            <h3 className="repo-bindings__title">单独绑定与排除</h3>
            {singleBindings.length === 0 ? (
              <p className="muted">无</p>
            ) : (
              <ul className="repo-bindings__members">
                {singleBindings.map((b) => {
                  const repo = repoById.get(b.target_id)
                  return (
                    <li key={b.target_id}>
                      <span className={`badge ${b.mode === 'exclude' ? 'badge-repo-missing' : 'badge-repo-active'}`}>
                        {b.mode === 'exclude' ? '排除' : '包含'}
                      </span>
                      <code>{repoLabel(b.target_id)}</code>
                      {b.sub_paths?.length ? <span className="muted">子目录：{b.sub_paths.join(', ')}</span> : null}
                      {repo && repo.status !== 'active' ? (
                        <span className="muted">（{REPO_STATUS_LABELS[repo.status]}）</span>
                      ) : null}
                      <button
                        type="button"
                        className="btn btn-ghost btn-sm"
                        onClick={() => edit(removeRepoBinding(draft, b.target_id))}
                      >
                        移除
                      </button>
                    </li>
                  )
                })}
              </ul>
            )}
            <div className="repo-bindings__add">
              <SearchableChipSelect
                value={addRepoId}
                options={repoOptions}
                onChange={setAddRepoId}
                placeholder="选择仓库"
                searchPlaceholder="搜索仓库路径"
              />
              <input
                value={addSubPaths}
                onChange={(e) => setAddSubPaths(e.target.value)}
                placeholder="子目录（可选，逗号分隔）"
              />
              <button type="button" className="btn btn-sm" disabled={!addRepoId} onClick={addRepo}>
                添加
              </button>
            </div>

            <div className="section-card__footer">
              <button
                type="button"
                className="btn btn-sm"
                disabled={!dirty || saving}
                onClick={() => void save(draft)}
                data-testid="repo-bindings-save"
              >
                {saving ? '保存中...' : '保存绑定'}
              </button>
              <button
                type="button"
                className="btn btn-ghost btn-sm"
                disabled={!dirty || saving}
                onClick={() => edit(view.bindings)}
              >
                还原
              </button>
              <button
                type="button"
                className="btn btn-ghost btn-sm btn-danger"
                disabled={saving || view.bindings.length === 0}
                onClick={() => setConfirm('clear')}
              >
                清空绑定
              </button>
              <select value={copyFromId} onChange={(e) => setCopyFromId(e.target.value)}>
                <option value="">从其他 Agent 复制…</option>
                {agents.map((a) => (
                  <option key={a.id} value={a.id}>
                    {a.name}
                  </option>
                ))}
              </select>
              <button
                type="button"
                className="btn btn-ghost btn-sm"
                disabled={!copyFromId || saving}
                onClick={() => setConfirm('copy')}
              >
                复制
              </button>
              {msg ? (
                <span className={msg.ok ? 'success' : 'error'} style={{ fontSize: '0.875rem' }}>
                  {msg.text}
                </span>
              ) : null}
            </div>
          </>
        ) : null}
      </div>

      <ConfirmDialog
        open={confirm === 'clear'}
        title="清空仓库绑定"
        description="清空后该 Agent 回到旧方式：RCA 代码工具使用 workspace/code 链接。"
        confirmLabel="清空"
        cancelLabel="取消"
        variant="danger"
        loading={saving}
        onCancel={() => setConfirm(null)}
        onConfirm={() => void save([])}
      />
      <ConfirmDialog
        open={confirm === 'copy'}
        title="复制仓库绑定"
        description="用所选 Agent 的绑定整体替换当前绑定，未保存的修改会丢失。"
        confirmLabel="复制"
        cancelLabel="取消"
        loading={saving}
        onCancel={() => setConfirm(null)}
        onConfirm={() => void copy()}
      />
    </section>
  )
}
```

- [ ] **Step 3: 插入详情页**

`web/src/pages/AgentDetail.tsx`：

1. 导入区加 `import RepoBindingsPanel from '../components/RepoBindingsPanel'`。
2. 在 MCP 服务区块（`data-testid="mcp-servers-bind-section"` 的 `</section>`）之后插入：

```tsx
      {id ? <RepoBindingsPanel agentId={id} /> : null}
```

- [ ] **Step 4: 类型检查与单测**

Run: `npm --prefix web run build` 和 `npm --prefix web test`
Expected: 均成功。

- [ ] **Step 5: Commit**

```bash
git add web/src/components/RepoBindingsPanel.tsx web/src/pages/AgentDetail.tsx
git commit -m "feat(web): agent repo bindings panel"
```

---

## Task 7: Agent 编辑页覆盖提示

**Files:**
- Modify: `web/src/pages/AgentForm.tsx`

- [ ] **Step 1: 加载绑定数**

1. 导入区加 `import { repoBindingApi } from '../api/repoRegistry'`；若尚未导入 `Link`，在 `react-router-dom` 的导入里补上。
2. 状态区加：

```tsx
  const [boundCount, setBoundCount] = useState(0)
```

3. 在编辑模式回填 workspace-link 状态的 `useEffect`（调用 `agentApi.workspaceLinkStatus(id)` 的那个，约第 118–129 行）旁边新增：

```tsx
  useEffect(() => {
    if (!isEdit || !id) return
    repoBindingApi
      .get(id)
      .then((v) => setBoundCount(v.bindings.length))
      .catch(() => setBoundCount(0))
  }, [isEdit, id])
```

（`isEdit` 的实际变量名以文件为准。）

- [ ] **Step 2: 提示文案**

在 `<label>浏览代码根</label>`（约第 259 行）之后、`{codeRoots.length === 0 ? (` 之前插入（放在这个分支之外，没配置 code_roots 时提示也要显示）：

```tsx
          {boundCount > 0 && id ? (
            <p className="detail-kv__hint repo-bindings__warn" data-testid="workspace-link-override-hint">
              该 Agent 已有 {boundCount} 条代码仓库绑定，RCA 代码工具使用绑定的仓库；这里的 workspace/code 链接只影响工作区文件浏览。到{' '}
              <Link to={`/agents/${id}`}>Agent 详情</Link> 管理代码仓库。
            </p>
          ) : null}
```

`repo-bindings__warn` 样式在 `RepoRegistry.css`，在本文件顶部加 `import './RepoRegistry.css'`（路径相对 `web/src/pages/`）。

- [ ] **Step 3: 类型检查**

Run: `npm --prefix web run build`
Expected: 成功。

- [ ] **Step 4: Commit**

```bash
git add web/src/pages/AgentForm.tsx
git commit -m "feat(web): warn when repo bindings override the workspace link"
```

---

## Task 8: e2e、全量验证与文档

**Files:**
- Create: `web/e2e/repo-registry.spec.ts`
- Modify: `docs/superpowers/specs/2026-10-09-repo-registry-and-rca-handbook-design.md`（§12）

- [ ] **Step 1: e2e 用例**

`web/e2e/repo-registry.spec.ts`:

```ts
import { expect, test, type Page, type Route } from '@playwright/test'
import { mockAgentDetailDeps, mockAgentList, sampleAgent } from './helpers/mock-api'

const now = '2026-10-01T00:00:00Z'
const repoA = {
  id: 'r-a',
  code_root: '/codes',
  rel_path: 'cloudgame/svc-a',
  name: 'svc-a',
  description: '',
  tags: [],
  git_remote: '',
  git_branch: 'main',
  head_commit: 'abcdef1234567890',
  sync_mode: 'registry_only',
  status: 'active',
  handbook_status: 'none',
  owner_id: '',
  last_scanned_at: now,
  created_at: now,
  updated_at: now,
}
const repoB = { ...repoA, id: 'r-b', rel_path: 'cloudgame/svc-b', name: 'svc-b' }
const group = {
  id: 'g-1',
  name: 'cloudgame',
  kind: 'dir',
  rule: { code_root: '/codes', rel_prefix: 'cloudgame' },
  auto_apply_new: true,
  owner_id: '',
  created_at: now,
  updated_at: now,
  repo_ids: ['r-a', 'r-b'],
}

async function mockRepoRegistry(
  page: Page,
  opts: { onPutBindings?: (body: { bindings: unknown[] }) => void; onMigrate?: (apply: boolean) => void } = {},
) {
  await page.route(/\/api\/v1\/repos(\/[^?]*)?(\?.*)?$/, async (route: Route) => {
    const url = new URL(route.request().url())
    const method = route.request().method()
    if (url.pathname === '/api/v1/repos/scan' && method === 'POST') {
      await route.fulfill({ json: { roots: 1, found: 2, added: 0, restored: 0, missing: 0 } })
      return
    }
    if (url.pathname === '/api/v1/repos/migrate-legacy-links' && method === 'POST') {
      const apply = url.searchParams.get('apply') === 'true'
      opts.onMigrate?.(apply)
      await route.fulfill({
        json: {
          apply,
          items: [
            {
              agent_id: sampleAgent.id,
              target: '/codes/cloudgame',
              action: 'bind_group',
              bindings: [{ target_kind: 'repo_group', target_id: 'g-1', mode: 'include' }],
              after_roots: ['cloudgame/svc-a', 'cloudgame/svc-b'],
              applied: apply,
            },
            { agent_id: 'agent-2', target: '/codes', action: 'manual_multi', reason: '目标下有多个仓库', applied: false },
          ],
        },
      })
      return
    }
    if (url.pathname === '/api/v1/repos' && method === 'GET') {
      await route.fulfill({ json: { items: [repoA, repoB], total: 2 } })
      return
    }
    await route.continue()
  })
  await page.route(/\/api\/v1\/repo-groups(\?.*)?$/, async (route: Route) => {
    await route.fulfill({ json: { items: [group] } })
  })
  await page.route(`**/api/v1/agents/${sampleAgent.id}/repo-bindings`, async (route: Route) => {
    if (route.request().method() === 'PUT') {
      const body = route.request().postDataJSON() as { bindings: unknown[] }
      opts.onPutBindings?.(body)
      await route.fulfill({ json: { bindings: body.bindings, effective: [] } })
      return
    }
    await route.fulfill({ json: { bindings: [], effective: [] } })
  })
}

test.describe('Repo registry UI', () => {
  test('仓库列表展示仓库并可手动扫描', async ({ page }) => {
    await mockRepoRegistry(page)
    await page.goto('/repos')
    await expect(page.getByText('cloudgame/svc-a')).toBeVisible()
    await page.getByRole('button', { name: '立即扫描' }).click()
    await expect(page.getByTestId('repo-scan-report')).toContainText('发现 2')
  })

  test('Agent 详情页绑定目录组并排除一个仓库', async ({ page }) => {
    let put: { bindings: unknown[] } | null = null
    await mockAgentList(page, [sampleAgent])
    await mockAgentDetailDeps(page, sampleAgent)
    await mockRepoRegistry(page, { onPutBindings: (b) => (put = b) })

    await page.goto(`/agents/${sampleAgent.id}`)
    const section = page.getByTestId('repo-bindings-section')
    await expect(section).toBeVisible()
    await section.getByTestId('repo-group-g-1').check()
    await expect(section.getByTestId('repo-bindings-summary')).toContainText('可见 2 个仓库')
    await section.getByRole('button', { name: '展开' }).click()
    await section.getByTestId('repo-member-g-1-r-b').uncheck()
    await expect(section.getByTestId('repo-bindings-summary')).toContainText('可见 1 个仓库')
    await section.getByTestId('repo-bindings-save').click()
    await expect(section.getByText('已保存')).toBeVisible()
    expect(put).not.toBeNull()
    expect(put!.bindings).toEqual([
      { target_kind: 'repo_group', target_id: 'g-1', mode: 'include' },
      { target_kind: 'repo', target_id: 'r-b', mode: 'exclude' },
    ])
  })

  test('迁移页先出报告再应用自动迁移', async ({ page }) => {
    const calls: boolean[] = []
    await mockRepoRegistry(page, { onMigrate: (apply) => calls.push(apply) })
    await page.goto('/repos/migration')
    await page.getByRole('button', { name: '生成报告' }).click()
    await expect(page.getByRole('cell', { name: '需人工：多个仓库', exact: true })).toBeVisible()
    await expect(page.getByTestId('migration-apply')).toContainText('（1）')
    await page.getByTestId('migration-apply').click()
    await page.getByRole('button', { name: '应用', exact: true }).click()
    await expect(page.getByText('已写入')).toBeVisible()
    expect(calls).toEqual([false, true])
  })
})
```

- [ ] **Step 2: 运行 e2e**

Run（在 `web` 目录下）：`npx playwright test e2e/repo-registry.spec.ts`
Expected: 3 个用例 PASS。若提示浏览器未安装，运行 `npx playwright install chromium` 后重试；若仍无法运行（离线等），记录原因并在报告中说明，不阻塞后续步骤。

- [ ] **Step 3: 全量单测与构建**

Run: `npm --prefix web test` 和 `npm --prefix web run build`
Expected: 均成功；`git status` 中不出现 `web/dist`。

- [ ] **Step 4: 同步设计文档 §12**

把 `docs/superpowers/specs/2026-10-09-repo-registry-and-rca-handbook-design.md` 的 §12 改为：

```markdown
## 12. 前端

P1b 已实现（侧栏「代码仓库」入口，页内标签切换）：

- **仓库页** `/repos`：按状态、code root、分组、关键字筛选；列出路径、所属分组、标签、分支与提交、状态、最近扫描时间；手动扫描并显示报告；编辑名称、描述、标签并查看使用该仓库的 agent；归档与恢复。
- **分组页** `/repo-groups`：目录组只读（随扫描更新）；手工组新建、编辑成员、删除（仍被绑定时提示 409 信息）。
- **迁移页** `/repos/migration`：生成迁移报告（不写入），展示方案、迁移后可见的逻辑名、原因；确认后应用自动迁移。
- **Agent 详情页「代码仓库」区块**：勾选分组；展开分组逐个排除；单独添加仓库（可填子目录）；本地预览"保存后可见 N 个仓库"及每个仓库的来源；清空绑定（回到旧链接）；从其他 agent 复制。绑定需要已有 agent id，因此放在详情页而不是编辑表单。
- **Agent 编辑页**：已有绑定时，在 workspace/code 选择处提示"RCA 代码工具使用绑定的仓库，链接只影响工作区文件浏览"。

P1b 的限制：仓库表没有"使用 agent 数"列（列表接口不返回，改在编辑弹窗列出使用该仓库的 agent）；"从其他 agent 复制"只列前 100 个 agent；读取绑定需要 agent 编辑权限，只有查看权限时区块显示无法加载。

后续阶段：仓库页的 handbook 状态、落后天数、冻结比例、漏召回率列与 handbook 浏览抽屉（P2）；仓库批量操作；tag 组与 pending 成员确认（P4）。
```

- [ ] **Step 5: Commit**

```bash
git add web/e2e/repo-registry.spec.ts docs/superpowers/specs/2026-10-09-repo-registry-and-rca-handbook-design.md
git commit -m "test(web): repo registry e2e; docs: sync frontend section"
```
