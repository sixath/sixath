# Agent Hybrid Recall UI + Sidecar PR 收尾 Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Web Agent 面板三态「混合召回」开关（对齐 `optional bool`）+ 单测/e2e；并将 framework/portal/monorepo 的 `feat/p2e-vector-sidecar` Push + PR。

**Architecture:** 纯函数 `runtimeTools.ts` 负责 normalize/serialize 与 §4.1 提交重映射；`AgentForm` 用 `<select>` 表达 default/on/off；`AgentDetail` 独立展示三态。`hybrid_recall` 永不进入 `RUNTIME_TOOL_FIELDS`。Sidecar 三仓只做 push/PR，不改代码。

**Tech Stack:** React + TypeScript（Vite）；`node --test`；Playwright e2e；`gh pr create`。

**Spec:** `docs/superpowers/specs/2026-07-26-agent-hybrid-recall-ui-design.md`（monorepo worktree）

**Worktrees:**

| 仓 | 路径 | 分支 |
|----|------|------|
| web | `web/.worktrees/hybrid-recall-ui`（本计划 Task 1 创建） | `feat/hybrid-recall-ui` |
| framework | `framework/.worktrees/p2e-vector-sidecar` | `feat/p2e-vector-sidecar` |
| portal | `portal/.worktrees/p2e-vector-sidecar` | `feat/p2e-vector-sidecar` |
| monorepo docs | `.worktrees/p2e-vector-sidecar` | `feat/p2e-vector-sidecar` |

Windows：

```powershell
cd web/.worktrees/hybrid-recall-ui
npm test
npx tsc --noEmit
npm run test:e2e
```

---

## File Structure

| 文件 | 责任 |
|------|------|
| `web/src/api/runtimeTools.ts` | `RuntimeToolsConfig`、`RUNTIME_TOOL_FIELDS`、normalize/serialize、`HybridRecallMode`、提交重映射 |
| `web/src/api/client.ts` | 从 `runtimeTools` re-export；Agent 类型继续用同一 config |
| `web/tests/runtimeToolsSerialize.test.ts` | 序列化 + §4.1 重映射单测 |
| `web/src/pages/AgentForm.tsx` | select + hint；submit 前调用重映射 |
| `web/src/pages/AgentDetail.tsx` | `hybrid-recall-status` 三态文案 |
| `web/e2e/agent-runtime-tools.spec.ts` | Create/Update 分场景 body 断言 |
| `web/e2e/helpers/mock-api.ts` | 可选夹具（显式 false 的 agent） |
| `portal/.../docs/memory-integration.md` | Backlog 去掉前端 UI；补三态说明（portal PR） |
| monorepo E2 §7 | 链到 UI 规格（docs PR） |

**禁止：** 改 portal gate/Update omit；把 `hybrid_recall` 塞进 `RUNTIME_TOOL_FIELDS`；提交 `portal/.../internal/service/data/`。

---

### Task 1: Web worktree

**Files:** none（git only）

- [ ] **Step 1: 创建 worktree**

```powershell
cd D:\workspace\github\sixath\web
git fetch origin
git worktree add .worktrees/hybrid-recall-ui -b feat/hybrid-recall-ui origin/main
```

若 `.worktrees` 未 ignore：加入 `.gitignore` 并 commit 后再创建。

- [ ] **Step 2: 依赖**

```powershell
cd .worktrees/hybrid-recall-ui
npm ci
```

- [ ] **Step 3: 切 workspace（若用 Cursor）**

`move_agent_to_root` → `D:\workspace\github\sixath\web\.worktrees\hybrid-recall-ui`

---

### Task 2: runtimeTools 纯函数 + 失败单测

**Files:**
- Create: `web/src/api/runtimeTools.ts`
- Create: `web/tests/runtimeToolsSerialize.test.ts`
- Modify: `web/src/api/client.ts`（改为 re-export）

- [ ] **Step 1: 写失败测试**

```ts
// tests/runtimeToolsSerialize.test.ts
import { describe, it } from 'node:test'
import assert from 'node:assert/strict'
import {
  serializeRuntimeTools,
  normalizeRuntimeTools,
  applyHybridRecallForSubmit,
  type RuntimeToolsConfig,
} from '../src/api/runtimeTools.ts'

describe('serializeRuntimeTools hybrid_recall', () => {
  it('omits hybrid_recall when undefined', () => {
    const out = serializeRuntimeTools({ memory_write_enabled: true })
    assert.equal(Object.prototype.hasOwnProperty.call(out, 'hybrid_recall'), false)
    assert.equal(out.memory_write_enabled, true)
    assert.equal(out.browser_enabled, false) // !! 压平
  })

  it('preserves explicit false', () => {
    const out = serializeRuntimeTools({ hybrid_recall: false })
    assert.equal(out.hybrid_recall, false)
  })

  it('preserves explicit true', () => {
    const out = serializeRuntimeTools({ hybrid_recall: true })
    assert.equal(out.hybrid_recall, true)
  })
})

describe('normalizeRuntimeTools hybrid_recall', () => {
  it('accepts camelCase and keeps false', () => {
    const n = normalizeRuntimeTools({ hybridRecall: false })
    assert.equal(n.hybrid_recall, false)
  })
})

describe('applyHybridRecallForSubmit', () => {
  it('create default → omit', () => {
    const cfg: RuntimeToolsConfig = {}
    const out = applyHybridRecallForSubmit(cfg, 'default', { isEdit: false, hadExplicit: false })
    assert.equal(out.hybrid_recall, undefined)
  })

  it('edit with prior explicit + default → true', () => {
    const out = applyHybridRecallForSubmit({}, 'default', { isEdit: true, hadExplicit: true })
    assert.equal(out.hybrid_recall, true)
  })

  it('edit without prior + default → omit', () => {
    const out = applyHybridRecallForSubmit({}, 'default', { isEdit: true, hadExplicit: false })
    assert.equal(out.hybrid_recall, undefined)
  })

  it('off → false', () => {
    const out = applyHybridRecallForSubmit({}, 'off', { isEdit: true, hadExplicit: false })
    assert.equal(out.hybrid_recall, false)
  })
})
```

- [ ] **Step 2: RED**

```powershell
npm test -- tests/runtimeToolsSerialize.test.ts
```

Expected: 无法 resolve `../src/api/runtimeTools.ts`

- [ ] **Step 3: 实现 `runtimeTools.ts`**

从 `client.ts` 迁出（或复制后改）`RuntimeToolsConfig`、`RUNTIME_TOOL_FIELDS`、`CODING_ASSISTANT_RUNTIME_TOOLS`、`normalizeRuntimeTools`、`serializeRuntimeTools`，并新增：

```ts
export type HybridRecallMode = 'default' | 'on' | 'off'

export function hybridRecallModeFromValue(v: boolean | undefined): HybridRecallMode {
  if (v === true) return 'on'
  if (v === false) return 'off'
  return 'default'
}

export function hybridRecallLabel(v: boolean | undefined): string {
  if (v === true) return '混合召回：开'
  if (v === false) return '混合召回：关（仅 LIKE）'
  return '混合召回：跟随默认（开）'
}

/** §4.1：在 serialize 之前调用 */
export function applyHybridRecallForSubmit(
  cfg: RuntimeToolsConfig,
  mode: HybridRecallMode,
  opts: { isEdit: boolean; hadExplicit: boolean },
): RuntimeToolsConfig {
  const next = { ...cfg }
  if (mode === 'on') {
    next.hybrid_recall = true
  } else if (mode === 'off') {
    next.hybrid_recall = false
  } else if (opts.isEdit && opts.hadExplicit) {
    next.hybrid_recall = true
  } else {
    delete next.hybrid_recall
  }
  return next
}

function pickBool(cfg: Record<string, unknown>, snake: string, camel: string): boolean | undefined {
  if (typeof cfg[snake] === 'boolean') return cfg[snake] as boolean
  if (typeof cfg[camel] === 'boolean') return cfg[camel] as boolean
  return undefined
}

// normalizeRuntimeTools: hybrid_recall: pickBool(cfg, 'hybrid_recall', 'hybridRecall')
// serialize: after !! loop —
//   if (typeof n.hybrid_recall === 'boolean') out.hybrid_recall = n.hybrid_recall
```

`CODING_ASSISTANT_RUNTIME_TOOLS` **不要**含 `hybrid_recall`。

- [ ] **Step 4: `client.ts` re-export**

删除迁出的定义，改为：

```ts
export {
  RUNTIME_TOOL_FIELDS,
  CODING_ASSISTANT_RUNTIME_TOOLS,
  serializeRuntimeTools,
  normalizeRuntimeTools, // 若外部需要；也可不 export
  type RuntimeToolsConfig,
  type HybridRecallMode,
  hybridRecallModeFromValue,
  hybridRecallLabel,
  applyHybridRecallForSubmit,
} from './runtimeTools'
```

确保 `normalizeAgent` 仍调用 `normalizeRuntimeTools`（同包 import）。

- [ ] **Step 5: GREEN**

```powershell
npm test -- tests/runtimeToolsSerialize.test.ts
npx tsc --noEmit
```

- [ ] **Step 6: Commit**

```powershell
git add src/api/runtimeTools.ts src/api/client.ts tests/runtimeToolsSerialize.test.ts
git commit -m "feat(web): hybrid_recall serialize with three-state submit remap"
```

---

### Task 3: AgentForm select

**Files:**
- Modify: `web/src/pages/AgentForm.tsx`

- [ ] **Step 1: 状态**

```ts
const [hybridMode, setHybridMode] = useState<HybridRecallMode>('default')
const [hadExplicitHybrid, setHadExplicitHybrid] = useState(false)
```

加载编辑：

```ts
const hr = a.runtime_tools?.hybrid_recall
setHadExplicitHybrid(typeof hr === 'boolean')
setHybridMode(hybridRecallModeFromValue(hr))
setRuntimeTools(a.runtime_tools ?? emptyRuntimeTools())
```

`applyCodingPreset`：只覆盖 opt-in 字段，**保留**当前 `hybridMode`（或不改 hybrid）。

- [ ] **Step 2: UI**（运行时工具 panel 闭合 `</div>` 之后、同 `form-group` 内或下一 group）

```tsx
<div className="form-group">
  <label>混合召回</label>
  <select
    data-testid="hybrid-recall-mode"
    value={hybridMode}
    onChange={(e) => setHybridMode(e.target.value as HybridRecallMode)}
  >
    <option value="default">跟随默认（开）</option>
    <option value="on">开</option>
    <option value="off">关（仅 LIKE）</option>
  </select>
  <small>
    关闭后该 Agent 读路径不做向量混合；写索引不受影响。
    {isEdit && hadExplicitHybrid
      ? ' 已显式设置过时，选「跟随默认」将保存为开。'
      : ' 「跟随默认」与后端未设置（开）一致。'}
  </small>
</div>
```

- [ ] **Step 3: submit**

```ts
const tools = applyHybridRecallForSubmit(runtimeTools, hybridMode, {
  isEdit,
  hadExplicit: hadExplicitHybrid,
})
runtime_tools: serializeRuntimeTools(tools),
```

注意：`toggleRuntimeTool` 的 `keyof RuntimeToolsConfig` 含 `hybrid_recall` 后，checkbox 列表仍只遍历 `RUNTIME_TOOL_FIELDS`（无 hybrid）——OK。

- [ ] **Step 4: `tsc` + 手动扫一眼**

```powershell
npx tsc --noEmit
```

- [ ] **Step 5: Commit**

```powershell
git add src/pages/AgentForm.tsx
git commit -m "feat(web): AgentForm hybrid recall three-state select"
```

---

### Task 4: AgentDetail

**Files:**
- Modify: `web/src/pages/AgentDetail.tsx`

- [ ] **Step 1: 在 runtime-tools-section 旁加一行**

```tsx
<p data-testid="hybrid-recall-status">
  <strong>{hybridRecallLabel(agent.runtime_tools?.hybrid_recall)}</strong>
</p>
```

`enabledRuntimeTools` **不要** filter `hybrid_recall`。

- [ ] **Step 2: Commit**

```powershell
git add src/pages/AgentDetail.tsx
git commit -m "feat(web): show hybrid recall status on AgentDetail"
```

---

### Task 5: e2e

**Files:**
- Modify: `web/e2e/helpers/mock-api.ts`
- Modify: `web/e2e/agent-runtime-tools.spec.ts`

- [ ] **Step 1: 夹具**

```ts
export const sampleAgentHybridOff = {
  ...sampleAgent,
  id: 'agent-e2e-hybrid-off',
  runtime_tools: {
    ...sampleAgent.runtime_tools,
    hybrid_recall: false,
  },
}
```

- [ ] **Step 2: 用例（追加到 describe）**

1. Create + 默认 select → `posted.runtime_tools` 无 `hybrid_recall`  
2. Create + 选 off → `hybrid_recall === false`  
3. Update `sampleAgent`（无显式）+ 保持 default → 无 `hybrid_recall`  
4. Update `sampleAgentHybridOff` + 改 default → `hybrid_recall === true`  
5. Update + 选 off → `false`  
6. Detail `sampleAgent`（无字段）→ status 含「跟随默认」  
7. Detail `sampleAgentHybridOff` → 含「关」

选择器：`page.getByTestId('hybrid-recall-mode').selectOption('off')`

- [ ] **Step 3: 跑 e2e**

```powershell
npm run test:e2e -- e2e/agent-runtime-tools.spec.ts
```

Expected: PASS

- [ ] **Step 4: Commit**

```powershell
git add e2e/agent-runtime-tools.spec.ts e2e/helpers/mock-api.ts
git commit -m "test(web): e2e hybrid recall Create/Update body cases"
```

---

### Task 6: 文档回写（portal + monorepo）

**Files:**
- Modify: `portal/.worktrees/p2e-vector-sidecar/docs/memory-integration.md`
- Modify: `.worktrees/p2e-vector-sidecar/docs/superpowers/specs/2026-07-26-memory-store-hybrid-recall-design.md` §7
- Modify: `.worktrees/p2e-vector-sidecar/docs/superpowers/specs/2026-07-26-agent-hybrid-recall-ui-design.md` 状态可保持已定稿；实现后可加「已交付」

- [ ] **Step 1: portal docs**

Backlog 去掉「前端 Agent 面板混合召回 UI」；在 Hybrid Recall 节补：

> Web Agent 编辑：三态 select（跟随默认 / 开 / 关）；详见 monorepo UI 规格。

```powershell
cd D:\workspace\github\sixath\portal\.worktrees\p2e-vector-sidecar
git add docs/memory-integration.md
git commit -m "docs(memory): document hybrid_recall Agent UI"
```

- [ ] **Step 2: E2 §7**

将「前端 Agent 面板…」改为已交付并链到 `2026-07-26-agent-hybrid-recall-ui-design.md`。

```powershell
cd D:\workspace\github\sixath\.worktrees\p2e-vector-sidecar
git add docs/superpowers/specs/2026-07-26-memory-store-hybrid-recall-design.md docs/superpowers/specs/2026-07-26-agent-hybrid-recall-ui-design.md
git commit -m "docs(memory): mark hybrid recall UI delivered in E2 backlog"
```

（若 web 尚未合入，文案用「规格已定稿 / web 实现见 feat/hybrid-recall-ui」——**实现完成后再改「已交付」**。本 Task 放在 Task 5 之后执行时写「已交付」。）

---

### Task 7: Push + PR（四条）

**前置：** web `npm test && npx tsc --noEmit && npm run test:e2e` 绿；portal 无暂存 `internal/service/data/`。

- [ ] **Step 1: web PR**

```powershell
cd D:\workspace\github\sixath\web\.worktrees\hybrid-recall-ui
git push -u origin HEAD
gh pr create --title "feat(web): Agent hybrid_recall three-state UI" --body "$(@'
## Summary
- Three-state hybrid recall select on AgentForm (default/on/off)
- Serialize preserves omit/true/false; §4.1 remap on edit
- AgentDetail status + unit/e2e coverage

## Test plan
- [ ] npm test
- [ ] npx tsc --noEmit
- [ ] npm run test:e2e -- e2e/agent-runtime-tools.spec.ts
'@)"
```

- [ ] **Step 2: framework PR**

```powershell
cd D:\workspace\github\sixath\framework\.worktrees\p2e-vector-sidecar
$env:GOMAXPROCS='1'; go test ./memory -count=1 -p 1 -vet=off
git push -u origin HEAD
gh pr create --title "feat(memory): hybrid RRF recall + vector backfill" --body "..."
```

- [ ] **Step 3: portal PR**（确认 `git status` 无 `internal/service/data/`）

```powershell
cd D:\workspace\github\sixath\portal\.worktrees\p2e-vector-sidecar
$env:GOMAXPROCS='1'; go test ./internal/chat ./internal/service -count=1 -p 1 -vet=off
git push -u origin HEAD
gh pr create --title "feat(memory): hybrid_recall gate + vector backfill job/CLI" --body "..."
```

- [ ] **Step 4: monorepo docs PR**

```powershell
cd D:\workspace\github\sixath\.worktrees\p2e-vector-sidecar
git push -u origin HEAD
gh pr create --title "docs(memory): E2/E2.1/hybrid UI specs" --body "..."
```

- [ ] **Step 5: 把四个 PR URL 交给用户**

---

## 依赖顺序

```text
Task 1 (worktree) → Task 2 (runtimeTools) → Task 3 (Form) → Task 4 (Detail) → Task 5 (e2e)
                                                                      ↓
                                                              Task 6 (docs) → Task 7 (PRs)
framework/portal 代码已在分支上；Task 7 的 sidecar PR 可与 Task 2–5 并行准备，但 docs「已交付」在 Task 5 后。
```

## 验收对照（spec §6）

1. Create 默认无 `hybrid_recall` — Task 5  
2. Edit off；有显式后再 default → `true` — Task 5  
3. 单测 + e2e — Task 2 + 5  
4. 三仓 + web PR — Task 7  
