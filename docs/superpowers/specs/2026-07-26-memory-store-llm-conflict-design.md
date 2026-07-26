# MemoryStore P2-D2：LLM 语义冲突消解

> 状态：已确认（待实现计划）  

> 日期：2026-07-26  
> 回链：[门面 §8.3](./2026-07-25-memory-store-facade-design.md)、[P2-D1 ConflictResolver](./2026-07-25-memory-store-conflict-resolver-design.md)、[P2-C turn extract](./2026-07-25-memory-store-turn-extract-design.md)  
> 前置：P2-D1（结构化 supersede + `ConflictResolver`）；P2-C（`AddFromTurn` + hash 去重）  
> 切片：**D2 only** — `Remember(add)` 语义冲突；不改 D1 `replace` Structural 门闩

---

## 0. 目标与非目标

### 目标

1. 对 **所有经 Facade 的 `Remember(add)`**（工具 + Turn 提取）在写入前做语义冲突检测（可开关）。  
2. 候选路径：**content_hash 精确去重 →（开启时）Recall top-K → 一次 LLM 裁决**。  
3. 决策：`Ignore` / `Supersede` / `KeepBoth`；LLM 失败 **fail-closed（不写）**。  
4. `Supersede` 复用 D1 链（新 active + 旧 superseded）；`KeepBoth` 正常 `add`。  
5. 与 D1 解耦：新增 `SemanticConflictResolver`，不扩宽单 existing 的 `ConflictResolver`。

### 非目标

| 项 | 归属 |
|----|------|
| `replace` 语义裁决（仍 Structural） | P2-D1 |
| 向量预筛 / Neo4j / Prefetch 配额 | 后续清单 |
| agent 文件语义冲突 | 不做 |
| 多 peer 同时 supersede | 不做 |
| 新 DB 迁移 | 不需要 |

---

## 1. 背景

P2-D1 仅覆盖工具显式 `replace` → 恒 supersede。  
P2-C `AddFromTurn` 与工具 `add` 仅有 hash 去重，无法处理「文本不同但事实矛盾 / 可并存」。  
D1 规格 §6 预留本切片。

---

## 2. 架构

```mermaid
flowchart TD
  A[Facade Remember add session/user] --> H{active content_hash 命中?}
  H -->|是| Skip[不写 skipped=hash_dedupe]
  H -->|否| On{turn_extract 或 ToolSemanticConflict?}
  On -->|否| Add[backend add]
  On -->|是| R[Recall units top-K LIKE]
  R --> Empty{peers 空?}
  Empty -->|是| Add
  Empty -->|否| L[SemanticConflictResolver.ResolveAdd]
  L -->|err / Ignore| Skip2[不写]
  L -->|KeepBoth| Add
  L -->|Supersede + TargetID| S[backend supersede 写]
```

### 2.1 接口

保留 D1：

```go
type ConflictResolver interface {
    Resolve(ctx context.Context, existing MemoryHit, candidate RememberInput) (ConflictDecision, error)
}
```

新增（D2）：

```go
type SemanticConflictVerdict struct {
    Decision     ConflictDecision // Ignore | Supersede | KeepBoth
    TargetUnitID string           // Supersede 时必填；否则空
}

// SemanticConflictResolver judges an add candidate against peer active units.
type SemanticConflictResolver interface {
    ResolveAdd(ctx context.Context, candidate RememberInput, peers []MemoryHit) (SemanticConflictVerdict, error)
}
```

`LLMSemanticConflictResolver`：单次 LLM completion，严格 JSON 输出。

### 2.2 Facade 编排

`FacadeConfig` 增加：

| 字段 | 含义 |
|------|------|
| `SemanticConflicts SemanticConflictResolver` | nil = 永不跑语义步骤（直 add，仍做 hash） |
| `SemanticConflictK int` | Recall limit，默认 **8** |
| `ToolSemanticConflict bool` | **工具** `add` 是否启用语义冲突；默认 **false** |
| （既有）`Conflicts ConflictResolver` | 仅 `replace`；nil → Structural |

提取路径**不**用单独 Facade 布尔：由调用方在 `RememberInput.Metadata["source"]=="turn_extract"` 标明（P2-C Pipeline 已写该字段）。Portal 仅在 `memory_extraction.enabled` 时跑 Pipeline；故「提取开 ⇒ 提取 add 可走语义」由 **Pipeline 是否调用 + Facade 见 `source=turn_extract`** 共同保证。

`rememberUnits(add)` 语义分支伪代码：

```text
if hash hit → skip (MemoryHit{}, nil)
enabled := false
if f.SemanticConflicts != nil {
  if meta source == "turn_extract" {
    enabled = true          // 提取路径：见 source 即启用语义（上游已受 extraction 开关约束）
  } else if f.ToolSemanticConflict {
    enabled = true          // 工具路径：独立开关
  }
}
if !enabled → backend add
peers := Recall(... K ...)
if len(peers)==0 → backend add
verdict, err := SemanticConflicts.ResolveAdd(...)
...
```

| Action | 行为 |
|--------|------|
| `add` | 上表编排 |
| `replace` | **仅** D1 `Conflicts` 门闩 → backend supersede（不变） |
| `remove` | 直 backend（不变） |

**语义裁决后的 Supersede：** 将 `add` 改写为对 `TargetUnitID` 的 supersede 写（新 content），**直接调用 units backend 的 supersede/replace 实现**，不再次进入 D1 `ConflictResolver`（避免双重裁决）。工具显式 `replace` 仍走 D1 Structural。

**Target 校验：** `TargetUnitID` 必须 ∈ 本次 `peers`，且 Get 后为 `active`；否则视为无效裁决 → fail-closed 不写。

### 2.3 语义启用条件（Portal ↔ Facade）

| 调用路径 | Portal | Facade |
|----------|--------|--------|
| Turn 提取 `add` | 仅当 `memory_extraction.enabled`（+ env）为 true 时调用 Pipeline；Pipeline `Metadata source=turn_extract` | `SemanticConflicts != nil` 且 `source=turn_extract` → 跑语义 |
| 工具 `memory_remember(add)` | 将 `memory_conflict.enabled` / `SATH_MEMORY_CONFLICT_ENABLED`（**默认 false**）映射为 `FacadeConfig.ToolSemanticConflict` | `SemanticConflicts != nil` 且 `ToolSemanticConflict` → 跑语义 |

未装配 resolver（nil）：两路径都跳过语义并 **直 add**（未配置 ≠ 调用失败）。  
有 resolver 但工具开关 false：工具 `add` 直 add；提取 `add` 仍可按 `source=turn_extract` 跑语义。

### 2.4 候选集与 hash

1. Facade 对 session/user `add` **统一**做 active `content_hash` 检查（与 P2-C List 扫描语义一致）；命中 → 不写，返回 `(MemoryHit{}, nil)`。  
2. Pipeline 可保留自身 hash 预过滤（优化，非唯一关卡）。  
3. Recall：`source=units`，`query=candidate.Content`，`limit=K`。  
   - **Peer 发现依赖现有关键词/LIKE 子串匹配**（非向量）。措辞完全不同、无共享子串的「矛盾事实」可能 peers 为空 → **直 add**（本切片接受该限制；向量预筛另开迭代）。  
   - 验收用例构造须保证矛盾对在现有 Recall 下能互相命中（共享关键词/子串），或显式断言「peers 空 → 直 add」。  
4. peers 为空 → 直 add，不调 LLM。  
5. 仅 `scope=session|user`。

### 2.5 决策与失败

| Verdict | 行为 |
|---------|------|
| Ignore | 不写；可选 skipped reason `conflict_ignore` |
| KeepBoth | `backend.Remember(add)` |
| Supersede | backend supersede 链（§2.2） |
| LLM/超时/非法 JSON/非法 target | **fail-closed 不写**；Facade 返回 `(MemoryHit{}, nil)`（与 D1 `ConflictIgnore` 同形）；reason `conflict_llm_error` / `conflict_invalid_target` |

对外：不因冲突检测向对话抛硬错误（勿向工具/Pipeline 返回 error；工具可返回 skipped 观测字段）。

### 2.6 LLM 约束

- 输入：candidate + peers（id、content；单条 content 截断对齐 P2-C `maxTurnFactBytes`=2048）。  
- 输出：`{"decision":"supersede"|"ignore"|"keep_both","target_unit_id":"..."}`。  
- `supersede` 时 `target_unit_id` 必填且 ∈ peers。  
- 模型：复用提取 auxiliary 或 Agent chat model（与 P2-C JSON 解析 / fail 策略一致）。  
- 单次 completion；短超时（建议与提取 auxiliary 同级或更短）。  
- 日志：decision、target、耗时；不记录全文 prompt。

---

## 3. Portal 接线

- 有可用 LLM client 时注入 `LLMSemanticConflictResolver`；否则 `SemanticConflicts=nil`。  
- `agent_extra.yaml`：`memory_conflict.enabled`（默认 false）+ `SATH_MEMORY_CONFLICT_ENABLED` → `FacadeConfig.ToolSemanticConflict`。  
- 提取路径不另设 Facade 布尔：跟随 `memory_extraction.enabled`；Pipeline 继续写 `source=turn_extract`。  
- 更新 `docs/memory-integration.md`（P2-D2 小节）；Backlog 标已规格化 / 实现中。

---

## 4. 测试与验收

1. hash 命中 → 不调 LLM、不写。  
2. `ToolSemanticConflict=false` 且非 `turn_extract` → 直 add；`SemanticConflicts=nil` → 任意 add 直 add。  
3. peers 空 → 直 add。  
4. KeepBoth → 两条 active。  
5. Supersede（peers 经 LIKE 可命中）→ 新 id、旧 superseded；Recall 只见新。  
6. LLM error / 非法 JSON / 非法 target → 不写。  
7. 工具 `replace` 仍仅 Structural。  
8. 提取开 + resolver 已注入：构造 **Recall 可命中** 的矛盾对 → supersede；P2-C hash 回归仍绿。  
9. `ToolSemanticConflict=false` + 注入 stub resolver：工具 add **不**调用 stub；`source=turn_extract` 的 add **会**调用 stub。

验收：两开关默认关时与现网一致；无新迁移。

---

## 5. 实现落点（文件级）

| 位置 | 变更 |
|------|------|
| `framework/memory/semantic_conflict.go`（新建，或扩 `conflict.go`） | `SemanticConflictResolver`、Verdict、LLM 实现 |
| `framework/memory/facade.go` | add 编排；hash；K；`ToolSemanticConflict` |
| `framework/memory/turn_extract.go` | `Remember` 后仅当返回 hit 含非空 `ID` 时 `written++`（Ignore/hash skip 不计） |
| `framework/memory/*_test.go` | §4 用例 |
| Portal `memory_store` / agent_extra / extract 接线 | 双开关映射 + 注入 |
| `portal/docs/memory-integration.md` | 文档 |

---

## 6. 门面清单回链

门面 §8.3「冲突消解」：D1 结构化 → 已交付；**LLM 语义 → 本规格 P2-D2**。
