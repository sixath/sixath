# Memory Hub 治理/知识插件化 — P0 Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or executing-plans.  
> `framework/` and `portal/` are nested git repos; run tests and commits inside the touched repo.  
> **Do not commit unless the user asks.**

**Goal:** 落地 `framework/memory/hub` 契约 + LocalGovernance/LocalKnowledge + Resolve/enforceHub/失败分流；Portal 最小接线（Catalog + Resolve 取 Provider），**不**接外部 Adapter、**不**做资产 UI、**不**做 Wiki/CodeGraph。

**Architecture:** Catalog 注册 `local`；全局 `defaults`；Agent 覆盖暂用可选 config 钩子（P1 再落 `RuntimeToolsConfig` proto）。Prefetch **不**因 Loadout 默认灌入全部 units——Loadout 默认仅 skills + 显式 binding。

**Tech Stack:** Go；framework `memory/hub`；Portal chat bootstrap 接线。

**Spec:** [2026-08-07-memory-hub-governance-knowledge-plugins-design.md](../specs/2026-08-07-memory-hub-governance-knowledge-plugins-design.md)  
**Review:** 设计评审通过；本计划吸收评审钉死项（见下）。

**Out of scope (P0):** 外部 Tencent Adapter；Skill 物化（§3.5.3 → P2 前置）；Portal 资产/配装 UI；`RuntimeToolsConfig` proto 字段（→ P1）；Wiki/CodeGraph；双向 sync；`PrefetchHints`；Confidence 过滤。

---

## Locked implementation choices（评审吸收）

| Topic | Choice |
|-------|--------|
| Loadout 默认范围 | **无显式 binding 时**：仅本机 skills（可列表）+ 空 units 列表；**不**自动把全部可见 session/user units 塞进 Loadout。units 召回仍走 `MemoryStore` / `StorePrefetchBackend`。可选 flag `loadout_include_visible_units` 默认 **false**。 |
| Loadout ↔ Prefetch | Prefetch **不**改为「只召回 Loadout units」；P0 至多：Resolve 成功后把治理面挂到上下文供后续 P1 使用；现网 Prefetch 行为默认不变。 |
| Skill 信任 vs procedural | 文档/代码命名用 **SkillTrustGate**（P2）；**禁止**把普通 Hub Skill 变更硬调成 `CommitProceduralRepair`。P0 无外部 Skill，仅留接口注释指向 §3.5.3。 |
| knowledge_search 默认源 | 一期默认 **`transcript` + `workspace` + `graph`（若可用）**；**默认不含 `units`**（避免与 `memory_recall` 重复）。`args.source` 显式传 `units` 时可含。 |
| 交叉配置 | P0 单测必须覆盖 `gov=local, know=fake` / `gov=fake, know=local`；Identity.ExternalIDs 仅外部 Adapter 填充（P0 fake 可空）。 |
| Catalog 热更新 | P0：**进程启动注册**；改 defaults 需重启。 |
| Agent 覆盖 | P0：`AgentHubConfig` 可由测试 / 可选 YAML 试验钩子注入；**正式 proto 字段留 P1**。 |

---

## File map

| File | Responsibility |
|------|----------------|
| Create `framework/memory/hub/types.go` | Identity, AssetKind/Status, AssetRef, Visibility, Capabilities, errors |
| Create `framework/memory/hub/governance.go` | GovernanceProvider, GovernanceWriter, AssetFilter, Page |
| Create `framework/memory/hub/knowledge.go` | KnowledgeProvider, ToolDesc |
| Create `framework/memory/hub/resolve.go` | Catalog, Defaults, AgentHubConfig, Resolve |
| Create `framework/memory/hub/enforce.go` | enforceHub, ErrHubMismatch |
| Create `framework/memory/hub/errors.go` | ErrNotSupported, 装配/运行时错误哨兵（可选分型） |
| Create `framework/memory/hub/*_test.go` | Resolve / enforceHub / 交叉配置 |
| Create `framework/memory/hub/local/governance.go` | LocalGovernance + bindings 存储接口 |
| Create `framework/memory/hub/local/knowledge.go` | LocalKnowledge Call search/read |
| Create `framework/memory/hub/local/status.go` | AssetStatus ↔ units metadata.hub_status |
| Create `framework/memory/hub/local/*_test.go` | status 映射、Loadout 默认不含 units、enforceHub |
| Create `framework/memory/hub/local/binding_store.go` | 内存 BindingStore（P0）；Portal MySQL 实现 P1 |
| Modify `portal/internal/chat/memory_prefetch_bootstrap.go`（或新建 `hub_bootstrap.go`） | 启动注册 local Catalog；defaults=local；**不改变** Prefetch 合并语义 |
| Modify `portal/configs/agent_extra.yaml` | `memory_hub` 注释块（enabled/defaults） |
| Modify `portal/docs/memory-integration.md` | P0 能力边界 + 链到 spec/plan |
| Modify spec §4.1 Loadout 行 | 与本计划 Locked 选择对齐（文档同步，同 PR 或紧随） |

---

### Task 1: hub 核心类型 + Resolve + enforceHub（framework）

**Files:**
- Create: `framework/memory/hub/types.go`, `governance.go`, `knowledge.go`, `resolve.go`, `enforce.go`, `errors.go`
- Test: `framework/memory/hub/resolve_test.go`, `enforce_test.go`

- [x] **Step 1: 写失败测试**

```go
func TestResolve_UnregisteredGovernance(t *testing.T) {
    cat := hub.Catalog{Gov: map[string]hub.GovernanceProvider{}, Know: map[string]hub.KnowledgeProvider{"local": fakeKnow{}}}
    _, _, err := hub.Resolve(cat, hub.Defaults{Governance: "local", Knowledge: "local"}, hub.AgentHubConfig{})
    if err == nil {
        t.Fatal("expected error")
    }
}

func TestResolve_AgentOverridesOneFace(t *testing.T) { /* gov override, know default */ }

func TestEnforceHub_Mismatch(t *testing.T) {
    err := hub.EnforceHub(fakeGov{name: "local"}, hub.AssetRef{ID: "1", Hub: "tencent"})
    if !errors.Is(err, hub.ErrHubMismatch) {
        t.Fatalf("%v", err)
    }
}
```

- [x] **Step 2: 最小实现使测试通过**（含 Capabilities 结构体；Writer 为独立接口）
- [x] **Step 3: `go test ./memory/hub/ -count=1`**
- [ ] **Step 4: Commit if asked**

---

### Task 2: LocalGovernance — status 映射 + BindingStore + Loadout 默认范围

**Files:**
- Create: `framework/memory/hub/local/status.go`, `binding_store.go`, `governance.go`
- Test: `status_test.go`, `governance_test.go`

- [x] **Step 1: 失败测试 — status 映射**

```go
func TestMapUnitToAssetStatus_DraftInMetadata(t *testing.T) {
    // unit status=active, metadata hub_status=draft → AssetDraft
}
func TestLoadoutFilter_ExcludesDraft(t *testing.T) {
    // draft 不进 ResolveLoadout 结果
}
```

- [x] **Step 2: 失败测试 — 默认 Loadout 不含全部 units**

```go
func TestResolveLoadout_DefaultNoUnitsWithoutFlag(t *testing.T) {
    // 有 session units 但无 binding → Loadout 中 kind=unit 为空；可含 skill
}
func TestResolveLoadout_ExplicitBindingIncluded(t *testing.T) {
    // Bind skill → Loadout 含该 skill 且 Hub=="local"
}
```

- [x] **Step 3: 实现 InMemoryBindingStore + LocalGovernance**
  - `BindAssets`/`UnbindAssets`/`SetVisibility`/`SetStatus` 入口先 `EnforceHub`
  - `CheckAccess`：owner / binding 命中；P0 可简化 Org 成员为「同 Identity.OrgID 非空则允许非 private」（完整 Org ACL AND 留 Portal P1）
- [x] **Step 4: `go test ./memory/hub/... -count=1`**
- [ ] **Step 5: Commit if asked**

---

### Task 3: LocalKnowledge — search/read（默认不含 units）

**Files:**
- Create: `framework/memory/hub/local/knowledge.go`, `knowledge_test.go`
- 依赖：可选注入 `MemoryStore` / transcript search / workspace search 的窄接口（避免 hub/local → portal 反向依赖）

- [x] **Step 1: 定义 `KnowledgeBackends` 接口（在 hub/local）**

```go
type UnitSearcher interface { /* optional; only if source includes units */ }
type TranscriptSearcher interface { Search(ctx, q string, limit int) ([]Hit, error) }
// workspace / graph 同理；nil = 该源跳过
```

- [x] **Step 2: 失败测试**

```go
func TestKnowledgeSearch_DefaultSourcesExcludeUnits(t *testing.T) {
    // 未传 source 时不调用 UnitSearcher
}
func TestKnowledgeSearch_ExplicitUnits(t *testing.T) {
    // args source=units 时调用
}
func TestKnowledgeCall_UnknownTool(t *testing.T) {
    // ErrNotSupported or clear error
}
```

- [x] **Step 3: 实现 `DescribeTools`（knowledge_search / knowledge_read）+ `Call`**
- [x] **Step 4: `go test ./memory/hub/... -count=1`**
- [ ] **Step 5: Commit if asked**

---

### Task 4: 运行时读失败分流辅助（framework）

**Files:**
- Create: `framework/memory/hub/runtime.go`（或 `read_policy.go`）
- Test: `runtime_test.go`

- [x] **Step 1: 辅助函数（P0 先测策略，Portal 接线可后挂）**

```go
// ReadLoadout: 调用 gov.ResolveLoadout；若 err 且判定为运输层错误：
//   - FallbackToDefaultOnReadError → 用 defaultGov 再试
//   - 再失败或 fail_open → 返回空 slice, nil（或 skip reason）
// CheckAccess 不可达 → false, nil（拒绝）
```

- [x] **Step 2: 单测 fake 超时/错误 vs 业务错误（业务错误不 fail-open）**
- [x] **Step 3: `go test ./memory/hub/ -count=1`**

---

### Task 5: Portal 启动注册 Catalog（行为不变）

**Files:**
- Create: `portal/internal/chat/hub_bootstrap.go`（或扩展 prefetch bootstrap）
- Modify: `portal/configs/agent_extra.yaml`, `portal/docs/memory-integration.md`
- Test: `hub_bootstrap_test.go`（Resolve defaults=local 成功；未注册名失败）

- [x] **Step 1: 启动时**
  - `Catalog` 注册 `local` gov+know
  - `Defaults{Governance:"local", Knowledge:"local"}`
  - 导出 `ResolveForAgent(agentID)` 供后续工具使用（P0 可仅单测调用）
- [x] **Step 2: 确认 Prefetch 路径零行为 diff**（现有 prefetch 测试仍绿）
- [x] **Step 3: `go test` 相关 portal chat 包**
- [ ] **Step 4: Commit if asked**

---

### Task 6: 规格同步 + 验收清单

**Files:**
- Modify: `docs/superpowers/specs/2026-08-07-memory-hub-governance-knowledge-plugins-design.md` §4.1 Loadout 行、§3.5.3 措辞（SkillTrustGate）、§3.3 knowledge 默认源说明

- [x] **Step 1: 文档与 Locked choices 对齐**（spec 已含 Locked 行；portal `memory-integration.md` + `agent_extra.yaml` 注释已补）
- [x] **Step 2: 按下方验收跑通 checklist**

---

## P0 验收 Checklist

- [x] `Resolve`：默认 / 只覆治理 / 只覆知识 / 全覆；未注册 name → 硬失败  
- [x] `EnforceHub`：Hub 不符 → `ErrHubMismatch`，Bind 不落存储  
- [x] LocalGovernance：draft 不进 Loadout；默认 Loadout **无** bulk units；显式 Bind skill 可见  
- [x] LocalKnowledge：默认 search **不**调 units；显式 source=units 才调  
- [x] 交叉配置单测：gov/know 不同 fake provider 名称可 Resolve  
- [x] CheckAccess 运输错误 → 拒绝（false）  
- [x] Portal：Catalog 注册 local；Prefetch 现网测试仍通过  
- [x] **无**外部 Adapter；**无**远程 Skill 执行路径  

---

## Follow-ups（不在本 plan）

| 项 | 阶段 |
|----|------|
| `RuntimeToolsConfig` hub_* 字段 + Agent UI 下拉 | P1 |
| 资产列表 / 配装 UI；切换 provider 提示清空 Binding | P1 |
| Tencent Adapter 读+写；SkillTrustGate 物化 | P2（物化为上线闸） |
| 本地 Wiki/CodeGraph | P3 |

---

## Test commands

```bash
# framework
cd framework
go test ./memory/hub/... -count=1

# portal（包名以实际为准）
cd portal
go test ./internal/chat/ -count=1 -run 'Hub|Prefetch|Resolve'
```
