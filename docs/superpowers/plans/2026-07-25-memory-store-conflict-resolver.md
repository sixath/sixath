# MemoryStore P2-D1 ConflictResolver / Supersede Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** 为 session/user units 启用结构化 supersede：`replace` 建链、`remove`/`Delete` 级联软删，并挂上 `ConflictResolver`（本迭代 `StructuralReplaceResolver`）。

**Architecture:** `ConflictResolver` 决策接口 + Facade 在 session/user `replace` 前 Get→Resolve；units 后端（内存 + MySQL）执行 supersede 写与级联删除。agent 文件路径不变。LLM 语义冲突属 P2-D2，本计划禁止实现。

**Tech Stack:** Go；MySQL/GORM（Portal）；既有 `memory_units.status` / `supersedes_id`（迁移 `009`，无新 SQL）。

**Spec:** `docs/superpowers/specs/2026-07-25-memory-store-conflict-resolver-design.md`

**Repos说明:** `framework/`、`portal/` 为嵌套 git；改动分别 commit。规格/本计划在 monorepo 根仓库。

---

## File Structure

| 文件 | 责任 |
|------|------|
| `framework/memory/conflict.go` | `ConflictDecision`、`ConflictResolver`、`StructuralReplaceResolver` |
| `framework/memory/conflict_test.go` | Resolver 单测 |
| `framework/memory/facade.go` | `FacadeConfig.Conflicts`；session/user replace 编排 |
| `framework/memory/facade_test.go` | supersede / 级联 / Get superseded |
| `framework/memory/session_memory.go` | supersede 写、级联删除、Get 允许 superseded、hit 带 status |
| `framework/memory/session_memory_supersede_test.go` | 内存后端契约 |
| `portal/internal/data/memory_units_backend.go` | MySQL supersede + 级联 + Get 放宽 |
| `portal/internal/data/memory_units_mysql.go` | 确认 `SupersedesID` / Status 字段（若缺则补） |
| `portal/internal/data/memory_units_mysql_test.go` | session+user 各测 replace/remove/Delete |
| `framework/tool/memory/store_tools.go` | Description：replace 更换 id |
| `portal/docs/memory-integration.md` | supersede / breaking 说明 |
| monorepo specs | 状态 → 实现中（可选） |

**禁止本迭代:** LLM ConflictResolver、agent 文件 supersede、新迁移、改 Turn 提取 hash 逻辑。

---

### Task 1: ConflictResolver + StructuralReplaceResolver

**Files:**
- Create: `framework/memory/conflict.go`
- Create: `framework/memory/conflict_test.go`

- [ ] **Step 1: 写失败测试**

```go
func TestStructuralReplaceResolver_ReturnsSupersede(t *testing.T) {
	var r StructuralReplaceResolver
	d, err := r.Resolve(context.Background(), MemoryHit{ID: "old", Content: "a"}, RememberInput{
		Action: ActionReplace, Content: "b", UnitID: "old",
	})
	if err != nil || d != ConflictSupersede {
		t.Fatalf("decision=%v err=%v", d, err)
	}
}
```

- [ ] **Step 2: 运行确认失败**

```bash
cd framework
go test ./memory/ -run TestStructuralReplaceResolver -count=1
```

Expected: FAIL（类型未定义）

- [ ] **Step 3: 最小实现**

```go
type ConflictDecision int

const (
	ConflictIgnore ConflictDecision = iota
	ConflictSupersede
	ConflictKeepBoth
)

type ConflictResolver interface {
	Resolve(ctx context.Context, existing MemoryHit, candidate RememberInput) (ConflictDecision, error)
}

type StructuralReplaceResolver struct{}

func (StructuralReplaceResolver) Resolve(ctx context.Context, existing MemoryHit, candidate RememberInput) (ConflictDecision, error) {
	return ConflictSupersede, nil
}
```

- [ ] **Step 4: 测试通过**

```bash
go test ./memory/ -run TestStructuralReplaceResolver -count=1
```

- [ ] **Step 5: Commit（framework）**

```bash
git add memory/conflict.go memory/conflict_test.go
git commit -m "feat(memory): add ConflictResolver and StructuralReplaceResolver"
```

---

### Task 2: SessionMemory supersede + 级联删除

**Files:**
- Modify: `framework/memory/session_memory.go`
- Modify: `framework/memory/facade_test.go`（旧 replace 同 id 断言 → supersede）
- Create: `framework/memory/session_memory_supersede_test.go`

- [ ] **Step 1: 写失败测试**

覆盖：
1. `replace` 后旧 id status=superseded、新 id active、Recall 只见新；旧 hit metadata 含 `supersedes_id` 在新行上  
2. 对 superseded id 再 replace → not found  
3. 链 A←B←C，`remove(C)` 后 A/B/C deleted；`Delete` 同行为  
4. `Get(superseded)` OK；`Get(deleted)` not found  
5. **session 与 user 各跑 replace + 级联 remove**

示例骨架：

```go
func TestSessionMemory_ReplaceSupersedesActiveUnit(t *testing.T) {
	m := NewSessionMemory()
	ctx := context.Background()
	old, err := m.Remember(ctx, RememberInput{Scope: ScopeSession, ScopeID: "s1", Action: ActionAdd, Content: "v1"})
	// replace with UnitID=old.ID Content=v2
	// assert new.ID != old.ID; Recall query v2 len=1; Get(old.ID) ok metadata status=superseded
}
```

- [ ] **Step 2: 运行确认失败**

```bash
go test ./memory/ -run TestSessionMemory_ReplaceSupersedes -count=1
```

Expected: FAIL（仍原地 replace / 同 id）

- [ ] **Step 3: 实现**

- 常量：`unitStatusSuperseded = "superseded"`  
- `sessionUnit` 增加 `supersedesID string`  
- `ActionReplace`：旧行必须 `active`（非 deleted/superseded）；INSERT 新行 `supersedesID=旧id`；旧行 `status=superseded`；返回新 hit  
- `softDelete` → `cascadeSoftDelete(id, scope, scopeID)`：BFS 祖先（沿 supersedesID）+ 子孙（扫描同 scope 中 supersedesID==当前）；全部标 deleted；目标不存在或已 deleted → not found  
- `ActionRemove` 与 `Delete` 均调用 `cascadeSoftDelete`  
- `Get`：允许 `active` 与 `superseded`；拒绝 `deleted`  
- `hit()`：metadata 写 `status`；若 `supersedesID!=""` 写 `supersedes_id`

- [ ] **Step 4: 同步改既有 Facade 测试（避免 Task 2 全量绿卡住）**

`facade_test.go` 中 `TestFacadeSessionUnitReplaceRemoveAndGetDeleted`（或同名）当前断言 `replaced.ID == hit.ID`。改为：
- `replaced.ID != hit.ID`
- `Get(旧 id)` 成功且 `metadata["status"]=="superseded"`
- `Recall` 只见新内容
- `remove`/`Delete` 后旧链不可 Get（deleted）

本步只改断言以匹配 SessionMemory 新语义；Facade 的 ConflictResolver 门闩仍在 Task 3。

- [ ] **Step 5: 测试通过**

```bash
go test ./memory/ -run 'TestSessionMemory_|TestFacadeSessionUnit' -count=1
go test ./memory/ -count=1
```

Expected: PASS（含已改写的 facade replace 断言）

- [ ] **Step 6: Commit**

```bash
git add memory/session_memory.go memory/session_memory_supersede_test.go memory/facade_test.go
git commit -m "feat(memory): SessionMemory supersede replace and cascade delete"
```

---

### Task 3: Facade 挂载 ConflictResolver

**Files:**
- Modify: `framework/memory/facade.go`
- Modify: `framework/memory/facade_test.go`

- [ ] **Step 1: 写/改测试**

```go
func TestFacade_ReplaceUsesSupersedeChain(t *testing.T) {
	facade := NewFacade(FacadeConfig{Session: NewSessionMemory()})
	// add → replace → assert new id; Get old superseded; Conflicts nil still works
}

func TestFacade_ReplaceKeepBothFailsClosed(t *testing.T) {
	// inject stub resolver returning ConflictKeepBoth → error
}
```

- [ ] **Step 2: 运行确认失败**

```bash
go test ./memory/ -run 'TestFacade_Replace' -count=1
```

- [ ] **Step 3: 实现**

`FacadeConfig` 增加 `Conflicts ConflictResolver`；`NewFacade`：`if cfg.Conflicts == nil { cfg.Conflicts = StructuralReplaceResolver{} }` 存入 `f.conflicts`。

session/user `Remember` 在委托 backend 前：

```go
if in.Action == ActionReplace {
    existing, err := f.session.Get(ctx, GetRef{Scope: in.Scope, ID: in.UnitID, ScopeID: in.ScopeID})
    if err != nil { return MemoryHit{}, err }
    // Get 可读 superseded；此处要求 active：
    if st, _ := existing.Metadata["status"].(string); st != "" && st != "active" {
        return MemoryHit{}, fmt.Errorf("memory: unit %q not found", in.UnitID)
    }
    d, err := f.conflicts.Resolve(ctx, existing, in)
    if err != nil { return MemoryHit{}, err }
    switch d {
    case ConflictSupersede:
        return f.session.Remember(ctx, in)
    case ConflictIgnore:
        return MemoryHit{}, nil
    default: // KeepBoth or unknown
        return MemoryHit{}, fmt.Errorf("memory: conflict decision %v not allowed for replace", d)
    }
}
```

`Delete` 仍直接 `f.session.Delete`（backend 已级联）。agent 分支不动。

- [ ] **Step 4: 全量 memory 测试**

```bash
go test ./memory/ -count=1
go test ./tool/memory/ -count=1
```

- [ ] **Step 5: Commit**

```bash
git add memory/facade.go memory/facade_test.go
git commit -m "feat(memory): Facade gates replace via ConflictResolver"
```

---

### Task 4: Portal MySQL units supersede

**Files:**
- Modify: `portal/internal/data/memory_units_mysql.go`（确认 `SupersedesID *string` / Status）
- Modify: `portal/internal/data/memory_units_backend.go`
- Modify: `portal/internal/data/memory_units_mysql_test.go`

- [ ] **Step 1: 写失败测试 + 改写旧断言**

新增：`TestSessionUnitsBackend_ReplaceSupersedes`（session）与 `TestSessionUnitsBackend_UserReplaceSupersedes`（user）：  
replace 后旧行 status superseded、新行 supersedes_id、Recall 只见新；`Delete`/`remove` 级联。

同时改写既有 `TestMemoryUnitReplaceRequiresExistingUnitAndDeleteHidesIt`（或同名）：去掉 `replaced.ID == created.ID`，改为新 id + 旧行 superseded；Delete 用**新 active id**（或链上任一 id）后整链不可 Get。

（沿用 sqlite AutoMigrate / skip-if-no-DB 既有模式。）

- [ ] **Step 2: 运行确认失败**

```bash
cd portal
go test ./internal/data/ -run 'ReplaceSupersedes|UserReplace|MemoryUnitReplace' -count=1
```

- [ ] **Step 3: 实现**

- 常量 `memoryUnitStatusSuperseded = "superseded"`  
- `ActionReplace`：仅 `status=active` 的旧行；事务内 Create 新行（`SupersedesID=&oldID`）+ Update 旧行 superseded；字段规则同 add  
- `Get`：查询 `status IN ('active','superseded')`（不要用 `activeUnits`）  
- `cascadeSoftDelete`：事务内收集链（SQL：反复查 id / `WHERE supersedes_id=?`）后批量 Update deleted；`Delete` 与 `ActionRemove` 共用  
- `memoryUnitHit`：metadata 带 `status`、`supersedes_id`

- [ ] **Step 4: 测试通过**

```bash
go test ./internal/data/ -run 'MemoryUnit|SessionUnits' -count=1
```

Expected: PASS（含改写后的旧 replace/delete 测试）
- [ ] **Step 5: Commit（portal）**

```bash
git add internal/data/memory_units_mysql.go internal/data/memory_units_backend.go internal/data/memory_units_mysql_test.go
git commit -m "feat(memory): MySQL units supersede replace and cascade delete"
```

---

### Task 5: 工具文案 + 集成文档

**Files:**
- Modify: `framework/tool/memory/store_tools.go`（Description）
- Modify: `portal/docs/memory-integration.md`
- Optional: monorepo spec 状态 → 实现中

- [ ] **Step 1: 更新文案**

`memory_remember` Description 注明：`scope=session|user` 的 `replace` 会创建新 unit id，旧 unit 变为 superseded。  
文档增加「Supersede 语义」小节：链、级联 remove/Delete、Get 可读历史、breaking。

- [ ] **Step 2: 回归测试**

```bash
cd framework && go test ./memory/ ./tool/memory/ -count=1
cd portal && go test ./internal/chat/ ./internal/data/ -count=1
```

- [ ] **Step 3: Commit**

```bash
# framework
git add tool/memory/store_tools.go
git commit -m "docs(memory): note replace changes unit id for supersede"

# portal
git add docs/memory-integration.md
git commit -m "docs(memory): document supersede chain and cascade delete"

# monorepo（若改 spec 状态）
git add docs/superpowers/specs/2026-07-25-memory-store-conflict-resolver-design.md
git commit -m "docs(memory): P2-D1 status implementing"
```

---

### Task 6: 冒烟清单（手工）

- [ ] `memory_remember(scope=session, replace)` → 新 id；DB 旧行 `superseded`  
- [ ] `memory_get` 旧 id 仍可读；`memory_recall` 只见新内容  
- [ ] `remove` / 若有 Delete API：整链 `deleted`  
- [ ] `scope=user` 同上  
- [ ] agent `replace` 仍改 MEMORY.md 正文  
- [ ] 提取开启时 hash 去重仍有效  

---

## 风险与回滚

| 风险 | 缓解 |
|------|------|
| 客户端缓存 unit_id | 文档 + Description；属预期 breaking |
| MySQL Get 仍限 active | Task 4 必须改 Get |
| 级联漏删子孙 | BFS + 单测 A←B←C |
| Facade 与 backend 双重 supersede | replace 写逻辑只在 backend；Facade 只做 Resolve 门闩 |
