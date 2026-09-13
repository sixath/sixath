# Memory Hub — P1 Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development or executing-plans.  
> Nested repos: `portal/`, `web/`, root `docs/`. **Do not commit unless asked.**

**Goal:** 落地 Agent 级 Hub 覆盖（`RuntimeToolsConfig.hub_*`）+ Catalog 下拉 + 保存时 Resolve 干跑校验；运行时按 Agent 配置 `Resolve`；最小注册 `knowledge_search` / `knowledge_read`。资产列表/配装 UI 为 **P1b**（本计划可后半段或独立 PR）。

**Spec:** [2026-08-07-memory-hub-governance-knowledge-plugins-design.md](../specs/2026-08-07-memory-hub-governance-knowledge-plugins-design.md) §6 / §7 P1  
**Depends on:** P0 `framework/memory/hub` + Portal `InitLocalMemoryHub` / `ResolveForAgent`

---

## Locked choices

| Topic | Choice |
|-------|--------|
| 字段 | `optional string hub_governance` / `hub_knowledge`；`optional bool hub_fallback_to_default_on_read_error` |
| unset / 空串 | Resolve 时均视为「用 defaults」 |
| Update omit | 与 `hybrid_recall` 相同：请求 unset → **保留库中值**（勿用 GetXxx 坍缩） |
| 保存校验 | Create/Update 带非空 name 时对 Catalog `Resolve` 干跑；未注册 → 400 |
| Catalog UI | 新增只读 HTTP：列出已注册 gov/know names + defaults |
| Prefetch | 仍不改为「只召回 Loadout units」 |
| 资产 UI | **P1b**：列表/Bind；本 P1a 可不做 |

---

## File map

| File | Responsibility |
|------|----------------|
| `portal/api/agent/v1/agent.proto` | RuntimeToolsConfig +10..12 |
| `portal/api/agent/v1/agent.pb.go` 等 | `make api` 生成 |
| `portal/internal/biz/runtime_tools.go` (+test) | *string / *bool presence |
| `portal/internal/data/model/runtime_tools.go` (+test) | JSON omitempty round-trip |
| `portal/internal/data/agent_mysql.go` | biz↔model 映射 |
| `portal/internal/service/agent.go` | Update omit-preserve；Create/Update Resolve 校验 |
| `portal/internal/chat/hub_bootstrap.go` | `AgentHubConfigFromRuntime`；`ListCatalog`；`ValidateAgentHub` |
| `portal/internal/chat/hub_http.go`（或 service） | Catalog HTTP handler |
| `portal/internal/chat/runtime_tools.go`（或 knowledge_tools） | 按 Resolve 注册 knowledge_* |
| `web/src/api/client.ts` + AgentForm/Detail | 下拉 + normalize/serialize（**不**进 RUNTIME_TOOL_FIELDS） |

---

### Task 1: proto + biz + model presence

- [x] proto 加字段；`make api` / protoc
- [x] biz/model From/To 保留 presence（空串写入 = 显式空，Resolve 当 default）
- [x] 单测：unset omit JSON；非空 round-trip；fallback bool 三态

### Task 2: service 校验 + Update preserve

- [x] Update：`HubGovernance`/`HubKnowledge`/`HubFallback…` nil → 保留 stored
- [x] Create/Update：`ValidateAgentHub(cfg)` 失败 → 错误返回
- [x] 单测镜像 hybrid / Hub Validate

### Task 3: Catalog API + Resolve 接线

- [x] `GET /api/v1/memory-hub/catalog`
- [x] `AgentHubConfigFromRuntime` / `ValidateAgentHub`
- [x] 单测 ListCatalog 含 `local`

### Task 4: web Agent 表单

- [x] 拉取 Catalog；治理/知识下拉「跟随默认」+ 各 name
- [x] serialize：跟随默认 → omit key；选 local → 发 `"local"`
- [x] Detail 展示当前覆盖

### Task 5: knowledge_* 最小注册

- [x] `RegisterKnowledgeHubTools` 挂入 `RegisterAgentRuntimeTools`
- [x] 单测：默认 local；未注册覆盖装配失败

### Task 6 (P1b): 资产列表/配装

- [x] ListAccessible/Loadout + Bind/Unbind/Clear HTTP + Agent Detail UI
- [x] MySQL `agent_asset_bindings` + AutoMigrate；`WireMemoryHubFromData`
- [x] 切换 provider 提示清空 Binding（AgentForm checkbox）

---

## Test commands

```bash
cd portal && make api
cd portal && go test ./internal/biz/ ./internal/data/model/ ./internal/service/ ./internal/chat/ -count=1 -run 'Hub|RuntimeTools|Hybrid|Catalog|Knowledge'
cd web && npm test -- --run  # if hub serialize tests added
```

## Out of scope

Tencent Adapter；SkillTrustGate 物化；Wiki/CodeGraph；改 Prefetch 语义。
