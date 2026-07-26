# MemoryStore P2-D2 LLM Semantic Conflict Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** 为 Facade `Remember(add)` 增加可开关的语义冲突检测：hash → Recall top-K → `SemanticConflictResolver`，决策 Ignore / KeepBoth / Supersede（复用 D1 链）。

**Architecture:** 新增 `SemanticConflictResolver`（与 D1 `ConflictResolver` 并列）。Facade 对 session/user `add` 做 hash 与双路径启用（`source=turn_extract` 或 `ToolSemanticConflict`）；LLM 实现一次 JSON 裁决。Portal 注入 resolver + 映射 `memory_conflict.enabled`。

**Tech Stack:** Go；framework `model.Model` Chat；Portal `agent_extra` / env；无新 SQL。

**Spec:** `docs/superpowers/specs/2026-07-26-memory-store-llm-conflict-design.md`

**Repos说明:** `framework/`、`portal/` 嵌套 git，分别 commit；本计划与规格在 monorepo。Windows：`git commit -m "..."`；Go 测试分包跑避免 OOM。

---

## File Structure

| 文件 | 责任 |
|------|------|
| `framework/memory/semantic_conflict.go` | `SemanticConflictVerdict`、`SemanticConflictResolver`、stub 可用类型（可选） |
| `framework/memory/llm_semantic_conflict.go` | `LLMSemanticConflictResolver` |
| `framework/memory/llm_semantic_conflict_test.go` | JSON 解析 / 非法 target / 模型错误 |
| `framework/memory/facade.go` | `SemanticConflicts`、`SemanticConflictK`、`ToolSemanticConflict`；add 编排 |
| `framework/memory/facade_semantic_test.go` | §4 验收 1–7、9（stub resolver） |
| `framework/memory/turn_extract.go` | `written++` 仅当 `hit.ID != ""` |
| `framework/memory/turn_extract_test.go` | skip 不计 written |
| `framework/config/...`（若 MemoryExtraction 同文件） | 增加 `MemoryConflict` 配置结构 |
| `portal/internal/chat/memory_store.go` | 注入 SemanticConflicts + ToolSemanticConflict |
| `portal/internal/chat/memory_conflict.go`（新建） | 开关解析（YAML + env） |
| `portal/internal/chat/portal_agent_extra.go` | 装载 memory_conflict |
| `portal/configs/agent_extra.yaml` | 注释示例 |
| `portal/docs/memory-integration.md` | P2-D2 小节 |
| monorepo spec | 状态 → 实现中 |

**禁止本迭代:** 向量预筛、改 D1 replace 门闩、agent 文件冲突、新迁移、多 target supersede。

---

### Task 1: SemanticConflictResolver 接口 + stub

**Files:**
- Create: `framework/memory/semantic_conflict.go`
- Create: `framework/memory/semantic_conflict_test.go`

- [ ] **Step 1: 写失败测试**

```go
func TestStubSemanticConflictResolver_KeepBoth(t *testing.T) {
	r := StubSemanticConflictResolver{Decision: ConflictKeepBoth}
	v, err := r.ResolveAdd(context.Background(), RememberInput{Content: "x"}, nil)
	if err != nil || v.Decision != ConflictKeepBoth {
		t.Fatalf("got %+v err=%v", v, err)
	}
}
```

- [ ] **Step 2: 运行确认失败**

```bash
cd framework
go test ./memory/ -run TestStubSemanticConflictResolver -count=1
```

Expected: FAIL（类型未定义）

- [ ] **Step 3: 最小实现**

```go
type SemanticConflictVerdict struct {
	Decision     ConflictDecision
	TargetUnitID string
}

type SemanticConflictResolver interface {
	ResolveAdd(ctx context.Context, candidate RememberInput, peers []MemoryHit) (SemanticConflictVerdict, error)
}

// StubSemanticConflictResolver for tests; Decision/TargetUnitID/Err control output.
type StubSemanticConflictResolver struct {
	Decision     ConflictDecision
	TargetUnitID string
	Err          error
	Calls        int
}

func (s *StubSemanticConflictResolver) ResolveAdd(ctx context.Context, candidate RememberInput, peers []MemoryHit) (SemanticConflictVerdict, error) {
	s.Calls++
	if s.Err != nil {
		return SemanticConflictVerdict{}, s.Err
	}
	return SemanticConflictVerdict{Decision: s.Decision, TargetUnitID: s.TargetUnitID}, nil
}
```

- [ ] **Step 4: 测试通过**

```bash
go test ./memory/ -run TestStubSemanticConflictResolver -count=1
```

- [ ] **Step 5: Commit（framework）**

```bash
git add memory/semantic_conflict.go memory/semantic_conflict_test.go
git commit -m "feat(memory): add SemanticConflictResolver interface"
```

---

### Task 2: Facade add 编排（hash + 双开关 + stub 决策）

**Files:**
- Modify: `framework/memory/facade.go`
- Create: `framework/memory/facade_semantic_test.go`

- [ ] **Step 1: 写失败测试**（覆盖规格 §4 的核心）

必测（用 `NewSessionMemory` + `StubSemanticConflictResolver`）：

1. `TestFacade_AddHashDedupeSkipsWrite` — 先 add 同 content，再 add → 第二次 ID 空、stub.Calls==0  
2. `TestFacade_ToolSemanticConflictOff_DoesNotCallResolver` — ToolSemanticConflict=false，resolver 非 nil → Calls==0，写入成功  
3. `TestFacade_SemanticConflictsNil_DirectAdd` — SemanticConflicts=nil → 直 add，无 panic  
4. `TestFacade_EmptyPeers_DirectAddNoResolverCall` — Tool=true + stub，但 content 与已有无 LIKE 重叠 → Calls==0，仍写入  
5. `TestFacade_TurnExtractSource_CallsResolver` — Tool=false，Metadata source=turn_extract → Calls>=1  
6. `TestFacade_KeepBothAddsSecondActive` — stub KeepBoth；两条 Recall 可见（内容需 LIKE 可互命中，如共享词 `color`）  
7. `TestFacade_SupersedeViaSemanticAdd` — stub Supersede+TargetID；旧 superseded，新 id；**必须** `f.session.Remember(replace)` 绕过 D1  
8. `TestFacade_SemanticLLMErrorSkipsWrite` — stub Err → `(hit, nil)` 且无新行  
9. `TestFacade_InvalidSupersedeTargetSkipsWrite` — TargetID 不在 peers → 不写  
10. `TestFacade_ReplaceStillStructural` — 显式 replace 仍 supersede，不受 Semantic stub 影响

- [ ] **Step 2: 运行确认失败**

```bash
go test ./memory/ -run 'TestFacade_AddHash|TestFacade_ToolSemantic|TestFacade_TurnExtract|TestFacade_KeepBoth|TestFacade_SupersedeVia|TestFacade_SemanticLLM|TestFacade_InvalidSupersede|TestFacade_ReplaceStill' -count=1
```

- [ ] **Step 3: 实现 Facade**

`FacadeConfig` / `Facade` 字段：

```go
SemanticConflicts     SemanticConflictResolver
SemanticConflictK     int  // default 8 in NewFacade if <=0
ToolSemanticConflict  bool
```

`NewFacade`：**不要**为 SemanticConflicts 填默认 LLM；nil 保持 nil。`Conflicts` 仍默认 Structural。

`rememberUnits` 重写分支：

```go
if in.Action == ActionRemove {
    return f.session.Remember(ctx, in)
}
if in.Action == ActionReplace {
    // existing D1 gate unchanged
    ...
}
// ActionAdd (and default treat as add):
if hit, ok := f.skipIfActiveContentHash(ctx, in); ok {
    return hit, nil // empty hit
}
enabled := f.semanticEnabled(in)
if !enabled || f.semanticConflicts == nil {
    return f.session.Remember(ctx, in)
}
k := f.semanticConflictK
peers, err := f.session.Recall(ctx, RecallQuery{
    Scope: in.Scope, ScopeID: in.ScopeID, Source: SourceUnits,
    Query: in.Content, Limit: k,
})
if err != nil {
    return MemoryHit{}, nil // fail-closed: treat recall error as skip write? Spec: LLM fail-closed; Recall 失败建议同样 (MemoryHit{}, nil)
}
if len(peers) == 0 {
    return f.session.Remember(ctx, in)
}
verdict, err := f.semanticConflicts.ResolveAdd(ctx, in, peers)
if err != nil || verdict.Decision == ConflictIgnore {
    return MemoryHit{}, nil
}
switch verdict.Decision {
case ConflictKeepBoth:
    return f.session.Remember(ctx, in)
case ConflictSupersede:
    if !peerIDActive(peers, verdict.TargetUnitID) {
        return MemoryHit{}, nil
    }
    // Direct backend replace — bypass D1 ConflictResolver:
    return f.session.Remember(ctx, RememberInput{
        Scope: in.Scope, ScopeID: in.ScopeID, AgentID: in.AgentID,
        Action: ActionReplace, UnitID: verdict.TargetUnitID,
        Content: in.Content, Metadata: in.Metadata,
    })
default:
    return MemoryHit{}, nil
}
```

Helpers：`semanticEnabled`（turn_extract vs ToolSemanticConflict）、`skipIfActiveContentHash`（List active 比 metadata content_hash；无 hash 则用 `ContentHash(in.Content)` 与 hit 比）、`peerIDActive`。

**重要：** 语义 Supersede 必须调用 `f.session.Remember(..., ActionReplace)`，**不要**调用 `f.Remember` / `rememberUnits` 的 replace 分支（否则再进 D1 Structural，虽结果都是 supersede，但规格要求绕过双重裁决；且 KeepBoth 路径清晰）。

- [ ] **Step 4: 全量 memory 测试**

```bash
go test ./memory/ -count=1
```

Expected: PASS（含既有 D1 / P2-C）

- [ ] **Step 5: Commit**

```bash
git add memory/facade.go memory/facade_semantic_test.go
git commit -m "feat(memory): Facade semantic conflict gate on Remember add"
```

---

### Task 3: LLMSemanticConflictResolver

**Files:**
- Create: `framework/memory/llm_semantic_conflict.go`
- Create: `framework/memory/llm_semantic_conflict_test.go`

- [ ] **Step 1: 写失败测试**（fake Model，仿 `llm_extractor_test.go`）

- 合法 JSON `keep_both` / `ignore` / `supersede`+target  
- 非法 JSON → error  
- supersede 缺 target → error（或 Facade 侧再校验；LLM 层应 error）  
- Model.Chat error → propagate error（Facade fail-closed）

- [ ] **Step 2: 运行确认失败**

```bash
go test ./memory/ -run TestLLMSemanticConflict -count=1
```

- [ ] **Step 3: 实现**

镜像 `LLMExtractor`：

```go
type LLMSemanticConflictResolver struct {
	Model model.Model
}

const llmSemanticConflictSystem = `You judge if a new memory fact conflicts with existing facts.
Reply with ONLY valid JSON (no markdown fences):
{"decision":"ignore"|"supersede"|"keep_both","target_unit_id":""}
Rules:
- ignore: new fact is noise or already covered
- supersede: new fact updates/replaces one existing fact; set target_unit_id to that peer id
- keep_both: related but both can remain
- target_unit_id required for supersede and must be one of the peer ids
- if unsure whether they conflict but both useful, keep_both`
```

截断 peer/candidate content 至 `maxTurnFactBytes`（2048）。解析后 normalize decision 字符串；supersede 无 target → `fmt.Errorf(...)`。

- [ ] **Step 4: 测试通过**

```bash
go test ./memory/ -run TestLLMSemanticConflict -count=1
go test ./memory/ -count=1
```

- [ ] **Step 5: Commit**

```bash
git add memory/llm_semantic_conflict.go memory/llm_semantic_conflict_test.go
git commit -m "feat(memory): LLMSemanticConflictResolver for add conflicts"
```

---

### Task 4: turn_extract written 计数

**Files:**
- Modify: `framework/memory/turn_extract.go`
- Modify: `framework/memory/turn_extract_test.go`

- [ ] **Step 1: 写/改测试**

Facade + stub Ignore：`AddFromTurn` 对将触发 Ignore 的候选 → `written==0`。  
（可在 Pipeline 测：Store 为带 Semantic 的 Facade，Extractor 返回固定 fact。）

- [ ] **Step 2: 确认失败**（若当前仍 `written++` 无条件）

- [ ] **Step 3: 实现**

```go
hit, err := p.Store.Remember(...)
if err != nil { return written, ... }
if hit.ID != "" {
    written++
}
```

- [ ] **Step 4: `go test ./memory/ -run AddFromTurn -count=1`**

- [ ] **Step 5: Commit**

```bash
git add memory/turn_extract.go memory/turn_extract_test.go
git commit -m "fix(memory): count turn-extract writes only when unit id returned"
```

---

### Task 5: Framework config `MemoryConflict`（必做）

**Files:**
- Modify: `framework/config/tool_guardrails.go`（`PortalAgentExtra` / `MemoryExtraction` 旁）
- Modify: `framework/config/config_test.go`

- [ ] **Step 1: 写失败测试** `TestLoadPortalAgentExtra_MemoryConflictOnly`（yaml `memory_conflict.enabled: true` + `k: 5`）

- [ ] **Step 2: 运行确认失败**

- [ ] **Step 3: 实现**

```go
type MemoryConflict struct {
	Enabled bool `yaml:"enabled" json:"enabled"`
	K       int  `yaml:"k" json:"k"` // 0 → Facade default 8
}
// PortalAgentExtra 增加:
MemoryConflict *MemoryConflict `json:"memory_conflict" yaml:"memory_conflict"`
```

更新 `LoadPortalAgentExtra` 空配置判定（约 L94–96）纳入 `MemoryConflict == nil`。

- [ ] **Step 4: 测试通过**

```bash
go test ./config/ -run MemoryConflict -count=1
```

- [ ] **Step 5: Commit**

```bash
git add config/tool_guardrails.go config/config_test.go
git commit -m "feat(config): MemoryConflict on PortalAgentExtra"
```

---

### Task 6: Portal 接线 + 文档

**Files:**
- Create: `portal/internal/chat/memory_conflict.go` + `_test.go`
- Modify: `portal/internal/chat/memory_store.go` — **注意：** 今日 `BuildMemoryStore` 无 model；需扩展签名或旁路 setter：

**接线锁定：**

1. 扩展 `BuildMemoryStore` 接受 `MemoryStoreOptions`：

```go
type MemoryStoreOptions struct {
	SemanticConflicts    memory.SemanticConflictResolver
	ToolSemanticConflict bool
	SemanticConflictK    int
}
```

2. **模型注入（必做）：** `BuildMemoryStore` 启动时往往无 per-agent chat model。实现 Portal 侧 `dynamicSemanticConflictResolver`（或同名）：
   - `ResolveAdd` 内按 `candidate.AgentID`（及 extraction auxiliary）解析 `model.Model`，复用 `memory_extract.go` 的 `buildExtractionModel` 逻辑；
   - 解析失败 → 返回 error（Facade fail-closed 不写）；
   - **禁止**仅在 bootstrap 绑死单一模型却声称支持「回退 Agent chat model」。

3. 同步修改调用点（至少）：`internal/service/chat.go`、`internal/chat/runtime_tools.go`、`internal/chat/memory_prefetch_bootstrap.go`、`internal/chat/memory_extract_pipeline_test.go`（及 `grep BuildMemoryStore` 全量）。

- Modify: `portal_agent_extra.go` — `SetMemoryConflictConfig`  
- Modify: `configs/agent_extra.yaml` — 注释块  
- Modify: `docs/memory-integration.md` — P2-D2 节；Backlog 更新  
- Env: `SATH_MEMORY_CONFLICT_ENABLED` 解析同 extraction 模式  

- [ ] **Step 1: 开关单测** default off；env true；yaml true  

- [ ] **Step 2–3: 实现接线 + dynamic resolver**；无任何可用模型工厂时 `SemanticConflicts=nil`  

- [ ] **Step 4:**

```bash
cd portal
go test ./internal/chat/ -count=1
go test ./internal/data/ -count=1
```

- [ ] **Step 5: Commit（portal）**

```bash
git add internal/chat/memory_conflict.go internal/chat/memory_conflict_test.go internal/chat/memory_store.go internal/chat/portal_agent_extra.go configs/agent_extra.yaml docs/memory-integration.md
# plus any BuildMemoryStore call-site files
git commit -m "feat(memory): wire semantic conflict resolver and tool switch"
```

---

### Task 7: Monorepo 状态 + 回归

**Files:**
- Modify: `docs/superpowers/specs/2026-07-26-memory-store-llm-conflict-design.md` 状态 → 实现中（实现完成后可再改已交付）

- [ ] **Step 1:**

```bash
cd framework && go test ./memory/ ./tool/memory/ -count=1   # 若 OOM 则分包
cd portal && go test ./internal/chat/ ./internal/data/ -count=1
```

- [ ] **Step 2: Commit monorepo**

```bash
git add docs/superpowers/specs/2026-07-26-memory-store-llm-conflict-design.md docs/superpowers/plans/2026-07-26-memory-store-llm-conflict.md
git commit -m "docs(memory): P2-D2 implementation plan and status"
```

---

### Task 8: 冒烟清单（手工）

- [ ] 默认两关：工具 add / 提取关 → 与现网一致  
- [ ] 仅 `memory_conflict.enabled=true` + 有模型：工具 add 矛盾对（共享关键词）→ supersede  
- [ ] 仅提取开：turn_extract add 走语义；工具 add 不走  
- [ ] LLM 不可用时不炸对话  
- [ ] replace 工具路径仍 D1  

---

## 风险

| 风险 | 缓解 |
|------|------|
| LIKE 漏检矛盾 | 文档 + 测例用共享子串；向量另迭代 |
| BuildMemoryStore 签名变更 | Task 6 更新全部调用点 |
| 语义 supersede 误走 D1 门闩 | 强制 `f.session.Remember(replace)` |
| 双开关弄反 | 验收 #9 stub Calls |

---

## 执行注意

- Framework 分支建议：`feat/memory-store-p2-d2`（自 `main`）  
- Portal 同名分支  
- Monorepo 已在 `feat/memory-store-p2-d2`（本计划与规格）
