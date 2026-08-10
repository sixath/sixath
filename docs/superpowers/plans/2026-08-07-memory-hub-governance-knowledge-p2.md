# Memory Hub — P2 Implementation Plan

> Nested repos: `framework/`, `portal/`. **Do not commit unless asked.**

**Goal:** 落地 §3.5.3 **SkillTrustGate 物化门控** + 可注册的外部 Adapter 骨架（Fake，可被 Catalog / Agent 覆盖）；读 fail-open / 写 fail-closed 已有 runtime 辅助，本切片接上 Adapter 运输错误。真实腾讯 HTTP 客户端 **不**在本切片（独立 follow-up，未过 SkillTrustGate 不得上线）。

**Spec:** [governance-knowledge-plugins-design](../specs/2026-08-07-memory-hub-governance-knowledge-plugins-design.md) §3.5.3 / §7 P2  
**Depends on:** P0 hub + P1 Resolve/Catalog/bindings

---

## Locked choices

| Topic | Choice |
|-------|--------|
| 真实 Tencent API | **Out of P2a**；本切片交付 `fake` Adapter + 接口，Catalog 可注册 `fake` |
| SkillTrustGate vs procedural | **禁止**调用 `CommitProceduralRepair`；独立包路径 `hub.SkillTrustGate` |
| 未签名外部 skill | 物化后 `AssetStatus=draft`；`require_manual_approve_unsigned_hub_skill` 默认 true |
| 物化路径 | `{cacheRoot}/hub-skills/<hub>/<skill_id>@<version>/` 含 `SKILL.md` + `manifest.json`（hash/version） |
| Bind 外部 skill | 必须先 `Materialize`；失败 fail-closed；draft 可 Bind 但不进 LoadoutEligible |
| Catalog | `RegisterGovernance` / `RegisterKnowledge`；Portal `InitLocalMemoryHub` 后可选注册 fake |

---

## File map

| File | Responsibility |
|------|----------------|
| `framework/memory/hub/skill_trust.go` | SkillContent / MaterializeResult / SkillTrustGate / SkillSource |
| `framework/memory/hub/materialize_fs.go` | FS 落盘物化 + hash |
| `framework/memory/hub/skill_trust_test.go` | 未签名→draft；hash 变更重过闸；已签名→active |
| `framework/memory/hub/fake/` | Fake Governance+Knowledge；可注入 transport err |
| `portal/internal/chat/hub_bootstrap.go` | RegisterExternal / 可选启用 fake |
| `portal/internal/chat/hub_materialize.go` | Bind 前对非 local skill 走 TrustGate |
| `portal/configs/agent_extra.yaml` | `memory_hub.adapters` 注释 |

---

### Task 1: SkillTrustGate + FS materialize（framework）

- [x] 接口 + FS 实现 + 单测

### Task 2: Fake Adapter + Catalog 注册

- [x] `fake.New(...)` Name=`fake`；ResolveLoadout/ListAccessible；Writer enforceHub
- [x] 运输错误返回 `fmt.Errorf("%w: ...", hub.ErrTransport)`
- [x] Portal `RegisterHubProvider` / `SATH_HUB_FAKE_ADAPTER`

### Task 3: Bind 路径物化门控（portal）

- [x] `BindAgentAssets`：`Hub!="local" && Kind==skill` → Materialize；draft 写入 Binding.Status
- [x] 单测：fake skill 未签名 → draft；draft 不进 Loadout

### Task 4: 文档

- [x] P2 plan checklist；memory-integration P2a 边界；agent_extra 注释

---

## Out of scope

真实 Tencent HTTP；Portal 完整「人工确认」审核台（可用 SetStatus API/现有 bindings）；Wiki/CodeGraph（P3）。

## Test commands

```bash
cd framework && go test ./memory/hub/... -count=1
cd portal && go test ./internal/chat/ -count=1 -run 'Hub|Trust|Fake|Material'
```
