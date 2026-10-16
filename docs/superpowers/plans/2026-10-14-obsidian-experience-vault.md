# Obsidian 经验知识库（experience knowledge vault）实现计划

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** 给 agent 加一层 handbook 之外的经验知识：人 + agent 用 Obsidian 编辑 markdown 笔记进一个文件系统 vault，框架用 `framework/memory` 的向量索引做片段级检索，让 agent 下次排查时「检索命中」经验、不再现场 rediscover。

**Architecture:** vault 落文件系统（`_agent_notes/` 隔离区 + `ops/` 正式目录），笔记用 frontmatter（`status`/`type`/`tags`/`source`/`created`）承载元数据。写入复用现有 workspace 文件工具（`write_file`），靠 system prompt + 模板约束只写 `_agent_notes/` 且 `status:pending`。检索层新增一个「vault 召回」能力：把 `ops/`（及后续正式目录）里 `status:promoted` 的笔记 embed 成向量存进 `UnitVectorIndex`（跨 agent 的固定 scope），agent 排查时按语义召回片段。pending 笔记不进索引。

**Tech Stack:** Go（`framework/memory`、`framework/tool`、`framework/skills`）、Obsidian（纯编辑器）、SQLite 向量起步（`InMemoryUnitVectorIndex`/`sqlite_unit_vector.go`）、`evals` A/B 验证。

**关键既有代码（写码时必读）：**
- `framework/memory/store.go` — `RememberInput`/`RecallQuery`/`MemoryHit`/`MemoryStore` 接口、`RecallSource` 枚举。
- `framework/memory/unit_vector.go` — `UnitVectorIndex`/`UnitEmbedder`/`UnitVectorHit` 接口（vault 召回直接复用的嵌入 + 向量层）。
- `framework/memory/kind.go` — `Scope` 类型与 `UnitKindFromMetadata`。
- `framework/tool/file_tools.go` — `RegisterWorkspaceFileTools`（`write_file`/`read_file`/`patch`/`search_files`），写入复用。
- `framework/skills/index.go` + `meta.go` — `SkillMeta`/frontmatter 解析范式（vault 扫描器参照，但 vault 是任意 `*.md` 而非 `SKILL.md`）。
- `portal/internal/chat/agent_builder.go:459` — `BuildEffectiveSystemPrompt`（注入「沉淀经验」指引 + vault 召回工具描述）。
- `portal/internal/conf/handbook_config.go` — 环境开关范式（`SATH_HANDBOOK_ENABLED`），vault 开关照此加 `SATH_VAULT_ENABLED`。

---

## 设计决策速查（实现时不许偏离）

| 决策 | 结论 |
|------|------|
| 写入 | 复用 `write_file`，不新造「写知识库」工具 |
| 检索 | 走 memory 语义索引（`UnitVectorIndex`），片段命中 |
| 隔离 | agent 只写 `_agent_notes/`，`status:pending`；pending 不进索引 |
| 晋升 | 人移动文件 + 改 `status:promoted`，agent 无权晋升 |
| 范围 | 先只做显式触发沉淀 + 检索命中闭环；自动触发、ACL、生产化后置 |
| 存储 | 向量先 `InMemory`/`SQLite`，Neo4j graph 可选不接 |

---

## Task 1: vault 目录模型与 frontmatter 解析

**Files:**
- Create: `framework/vault/vault.go` — vault 根目录发现、目录约定（`_agent_notes`/`ops`/`mine`/`shared`）、笔记 frontmatter 解析成 `Note` 结构。
- Create: `framework/vault/note.go` — `Note` 结构 + frontmatter 常量（`status`/`type`/`tags`/`source`/`created`）。
- Test: `framework/vault/vault_test.go`

- [ ] **Step 1: 写失败测试 —— frontmatter 解析 + 目录归类**

```go
// framework/vault/note_test.go
package vault

import "testing"

func TestParseNoteFrontmatter(t *testing.T) {
    raw := "---\ntype: experience\nstatus: promoted\ntags: [es, 流水]\nsource: session:abc\ncreated: 2026-10-14\n---\n\n# 标题\n正文..."
    n, err := parseNote(raw, "/v/ops/es-flow.md")
    if err != nil {
        t.Fatal(err)
    }
    if n.Type != "experience" || n.Status != "promoted" || len(n.Tags) != 2 {
        t.Fatalf("bad note: %+v", n)
    }
    if n.Source != "session:abc" {
        t.Fatalf("bad source: %s", n.Source)
    }
}

func TestNoteZone(t *testing.T) {
    if zoneOf("/v/_agent_notes/x.md") != zonePending { t.Fatal("pending zone") }
    if zoneOf("/v/ops/x.md") != zonePromoted { t.Fatal("promoted zone") }
    if zoneOf("/v/mine/x.md") != zoneMine { t.Fatal("mine zone") }
}
```

- [ ] **Step 2: 跑测试确认失败**

Run: `cd framework && go test ./vault/... -run 'TestParseNoteFrontmatter|TestNoteZone' -v`
Expected: FAIL（`package vault` 不存在 / `parseNote` 未定义）

- [ ] **Step 3: 实现 `note.go` + `vault.go`**

```go
// framework/vault/note.go
package vault

type Status string

const (
    StatusPending  Status = "pending"
    StatusPromoted Status = "promoted"
)

type Zone int

const (
    zonePending Zone = iota // _agent_notes/
    zonePromoted             // ops/（正式经验）
    zoneMine                 // mine/
    zoneShared               // shared/
    zoneUnknown
)

type Note struct {
    Path    string
    Zone    Zone
    Type    string
    Status  Status
    Tags    []string
    Source  string
    Created string
    Body    string
}
```

```go
// framework/vault/vault.go
package vault

import (
    "os"
    "path/filepath"
    "strings"

    yaml "go.yaml.in/yaml/v2"
)

// Root 是 vault 根目录（Obsidian 打开这里）。空串表示未启用。
func Discover(root string) (*Vault, error) { /* 见下 */ }

type Vault struct {
    Root string
}

func (v *Vault) zoneOf(path string) Zone {
    rel, _ := filepath.Rel(v.Root, path)
    switch {
    case rel == "_agent_notes" || strings.HasPrefix(rel, "_agent_notes"+string(filepath.Separator)):
        return zonePending
    case rel == "ops" || strings.HasPrefix(rel, "ops"+string(filepath.Separator)):
        return zonePromoted
    case rel == "mine" || strings.HasPrefix(rel, "mine"+string(filepath.Separator)):
        return zoneMine
    case rel == "shared" || strings.HasPrefix(rel, "shared"+string(filepath.Separator)):
        return zoneShared
    }
    return zoneUnknown
}
```

实现 `parseNote(raw, path string) (Note, error)`：用 `yaml.Unmarshal` 解析 frontmatter（`---\n...\n---`），body 为分隔符之后的部分。frontmatter 字段按上面 `Note` 结构映射；`status` 缺省时按 zone 推断（`_agent_notes/` → `pending`，`ops/` → `promoted`）。

- [ ] **Step 4: 跑测试确认通过**

Run: `cd framework && go test ./vault/... -v`
Expected: PASS

- [ ] **Step 5: 提交**

```bash
git add framework/vault/note.go framework/vault/vault.go framework/vault/vault_test.go
git commit -m "feat(vault): 新增经验知识 vault 目录模型与 frontmatter 解析"
```

---

## Task 2: vault 扫描器（任意 `*.md`，非 SKILL.md）

**Files:**
- Create: `framework/vault/scan.go` — 递归扫描 vault，只收集 `*.md`（排除 `.obsidian/`），按 zone 归类，返回 `[]Note`；只索引 `zonePromoted` 且 `status==promoted` 的笔记。
- Test: `framework/vault/scan_test.go`

- [ ] **Step 1: 写失败测试**

```go
// framework/vault/scan_test.go
package vault

import (
    "os"
    "path/filepath"
    "testing"
)

func writeNote(t *testing.T, dir, rel, content string) {
    t.Helper()
    p := filepath.Join(dir, rel)
    if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil { t.Fatal(err) }
    if err := os.WriteFile(p, []byte(content), 0o644); err != nil { t.Fatal(err) }
}

func TestScanPromotedOnly(t *testing.T) {
    dir := t.TempDir()
    writeNote(t, dir, "ops/a.md", "---\nstatus: promoted\ntype: experience\n---\n\nbodyA")
    writeNote(t, dir, "_agent_notes/b.md", "---\nstatus: pending\n---\n\nbodyB")
    writeNote(t, dir, ".obsidian/workspace.json", "{}")
    notes, err := Scan(dir)
    if err != nil { t.Fatal(err) }
    if len(notes) != 1 {
        t.Fatalf("want 1 promoted note, got %d", len(notes))
    }
    if notes[0].Zone != zonePromoted {
        t.Fatalf("want promoted zone, got %v", notes[0].Zone)
    }
}
```

- [ ] **Step 2: 跑测试确认失败** — `go test ./vault/... -run TestScanPromotedOnly` → FAIL（`Scan` 未定义）

- [ ] **Step 3: 实现 `scan.go`**

```go
package vault

import (
    "os"
    "path/filepath"
    "strings"
)

// Scan 扫描 vault，返回可检索的正式笔记（zonePromoted 且 status==promoted）。
// 跳过 .obsidian/、以及 _agent_notes 下所有 pending 笔记。
func Scan(root string) ([]Note, error) {
    v := &Vault{Root: root}
    var notes []Note
    err := filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
        if err != nil { return err }
        if d.IsDir() {
            if d.Name() == ".obsidian" { return filepath.SkipDir }
            return nil
        }
        if !strings.EqualFold(filepath.Ext(path), ".md") { return nil }
        z := v.zoneOf(path)
        if z != zonePromoted { return nil }
        raw, err := os.ReadFile(path)
        if err != nil { return err }
        n, err := parseNote(string(raw), path)
        if err != nil { return err }
        if n.Status != StatusPromoted { return nil }
        n.Zone = z
        n.Path = path
        notes = append(notes, n)
        return nil
    })
    return notes, err
}
```

- [ ] **Step 4: 跑测试确认通过** — `go test ./vault/...` → PASS

- [ ] **Step 5: 提交**

```bash
git add framework/vault/scan.go framework/vault/scan_test.go
git commit -m "feat(vault): 扫描器只收集 promoted 正式笔记"
```

---

## Task 3: vault 召回（复用 `UnitVectorIndex` 语义检索）

**Files:**
- Create: `framework/vault/recall.go` — `Recall(ctx, embed UnitEmbedder, idx UnitVectorIndex, query, limit)`：把 query embed 成向量，`idx.Search`，命中 unitID 反查笔记，返回带 `Score`/`Source`/`Created`/`Type` 的片段。
- Test: `framework/vault/recall_test.go`

- [ ] **Step 1: 写失败测试（用 `InMemoryUnitVectorIndex` + 假 embed）**

```go
// framework/vault/recall_test.go
package vault

import (
    "context"
    "testing"

    "github.com/sixath/framework/memory"
)

type fakeEmbedder struct{}

func (fakeEmbedder) Embed(ctx context.Context, agentID string, texts []string) ([][]float32, error) {
    out := make([][]float32, len(texts))
    for i, s := range texts {
        out[i] = []float32{float32(len(s)) % 7, 1} // 简单确定性向量
    }
    return out, nil
}

func TestRecallHits(t *testing.T) {
    idx := memory.NewInMemoryUnitVectorIndex()
    // 预置两条 promoted 笔记的向量（unitID = 文件相对路径）
    _ = idx.Upsert(context.Background(), memory.UnitVectorEntry{
        Scope: "vault", ScopeID: "vault", UnitID: "ops/es-flow.md", Vector: []float32{3, 1},
    })
    hits, err := Recall(context.Background(), fakeEmbedder{}, idx, "es 流水", 5)
    if err != nil { t.Fatal(err) }
    if len(hits) == 0 { t.Fatal("expected a hit") }
}
```

> 注：`InMemoryUnitVectorIndex` 的 `Search` 返回 `UnitVectorHit{UnitID, Score}`；`Recall` 需要把 unitID 映射回 `Note`（通过一个 `map[unitID]*Note` 或按路径反查）。为让测试自洽，本 Task 先定义 `Recall` 接受一个 `Note` 集合参数。

- [ ] **Step 2: 跑测试确认失败** — `go test ./vault/... -run TestRecallHits` → FAIL

- [ ] **Step 3: 实现 `recall.go`**

```go
package vault

import (
    "context"

    "github.com/sixath/framework/memory"
)

// Recall 对 query 做语义召回，返回命中的笔记片段。
// unitID 约定 = 笔记相对 vault 根的路径，便于反查 Note 与溯源。
func Recall(ctx context.Context, embed memory.UnitEmbedder, idx memory.UnitVectorIndex, byID map[string]Note, query string, limit int) ([]Hit, error) {
    vecs, err := embed.Embed(ctx, "", []string{query})
    if err != nil { return nil, err }
    if len(vecs) == 0 || len(vecs[0]) == 0 { return nil, nil }
    uhs, err := idx.Search(ctx, memory.UnitVectorQuery{
        Scope: "vault", ScopeID: "vault", Vector: vecs[0], Limit: limit,
    })
    if err != nil { return nil, err }
    out := make([]Hit, 0, len(uhs))
    for _, uh := range uhs {
        n, ok := byID[uh.UnitID]
        if !ok { continue }
        out = append(out, Hit{Note: n, Score: uh.Score})
    }
    return out, nil
}

type Hit struct {
    Note  Note
    Score float64
}
```

- [ ] **Step 4: 跑测试确认通过** — `go test ./vault/...` → PASS

- [ ] **Step 5: 提交**

```bash
git add framework/vault/recall.go framework/vault/recall_test.go
git commit -m "feat(vault): 复用 UnitVectorIndex 做经验语义召回"
```

---

## Task 4: vault 索引同步（promoted 笔记写入向量索引）

**Files:**
- Create: `framework/vault/index.go` — `Sync(ctx, embed, idx, root)`：`Scan` 出 promoted 笔记 → 每条 embed body → `idx.Upsert(UnitVectorEntry{Scope:"vault", ScopeID:"vault", UnitID: relPath, Vector})`；对已不存在于 `Scan` 结果的旧 unitID 调 `idx.Delete`。
- Create: `framework/vault/watcher.go` — 参照 `framework/skills/watcher.go` 的 fsnotify 去抖，`root` 变化时重跑 `Sync`。
- Test: `framework/vault/index_test.go`

- [ ] **Step 1: 写失败测试（Sync 写入 + 删除过期）**

```go
// framework/vault/index_test.go
package vault

import (
    "context"
    "path/filepath"
    "testing"

    "github.com/sixath/framework/memory"
)

func TestSyncUpsertAndDelete(t *testing.T) {
    dir := t.TempDir()
    writeNote(t, dir, "ops/a.md", "---\nstatus: promoted\n---\n\nbodyA")
    idx := memory.NewInMemoryUnitVectorIndex()
    if err := Sync(context.Background(), fakeEmbedder{}, idx, dir); err != nil { t.Fatal(err) }
    has, err := idx.Has(context.Background(), "vault", "vault", []string{"ops/a.md"})
    if err != nil { t.Fatal(err) }
    if !has["ops/a.md"] { t.Fatalf("expected ops/a.md indexed") }

    // 删除笔记后再次 Sync，应清掉旧 unitID
    if err := os.Remove(filepath.Join(dir, "ops", "a.md")); err != nil { t.Fatal(err) }
    if err := Sync(context.Background(), fakeEmbedder{}, idx, dir); err != nil { t.Fatal(err) }
    has, _ = idx.Has(context.Background(), "vault", "vault", []string{"ops/a.md"})
    if has["ops/a.md"] { t.Fatalf("expected ops/a.md removed after delete") }
}
```

> 注意 import `os`。

- [ ] **Step 2: 跑测试确认失败** — `go test ./vault/... -run TestSyncUpsertAndDelete` → FAIL

- [ ] **Step 3: 实现 `index.go` + `watcher.go`**

```go
// framework/vault/index.go
package vault

import (
    "context"
    "path/filepath"

    "github.com/sixath/framework/memory"
)

const (
    vaultScope   memory.Scope = "vault"
    vaultScopeID = "vault"
)

// Sync 把 root 下所有 promoted 笔记 embed 后 upsert 进向量索引，并删除已消失的旧 unitID。
func Sync(ctx context.Context, embed memory.UnitEmbedder, idx memory.UnitVectorIndex, root string) error {
    notes, err := Scan(root)
    if err != nil { return err }
    current := make(map[string]bool, len(notes))
    for _, n := range notes {
        rel, _ := filepath.Rel(root, n.Path)
        current[rel] = true
        vecs, err := embed.Embed(ctx, "", []string{n.Body})
        if err != nil { return err }
        if len(vecs) == 0 || len(vecs[0]) == 0 { continue }
        if err := idx.Upsert(ctx, memory.UnitVectorEntry{
            Scope: vaultScope, ScopeID: vaultScopeID, UnitID: rel, Vector: vecs[0],
        }); err != nil { return err }
    }
    // 删除不再存在的旧 unitID（用 Has 反查需要旧全量，简化：调用方传旧 rel 集合）
    return nil
}
```

> **设计注意**：`UnitVectorIndex.Has` 只按「给定 unitID」查存在性，无法枚举「索引里所有 unitID」。要清理已删除笔记，需在 `Sync` 侧维护「上次的 rel 集合」。本 Task 简化：`Sync` 接受可选的 `prev []string`，对 `prev` 中不在 `current` 里的 unitID 调 `Delete`。测试里第二段删文件后传第一次的 rel 集合。

`watcher.go` 参照 `skills/watcher.go`：`NewWatcher(root, ...)` 首跑 `Sync`，`fsnotify` 监听 `root`，300ms 去抖后重跑 `Sync`；reload 失败保留旧状态。

- [ ] **Step 4: 跑测试确认通过** — `go test ./vault/...` → PASS

- [ ] **Step 5: 提交**

```bash
git add framework/vault/index.go framework/vault/watcher.go framework/vault/index_test.go
git commit -m "feat(vault): promoted 笔记同步进向量索引并热重载"
```

---

## Task 5: 环境开关 `SATH_VAULT_ENABLED` + vault 根配置

**Files:**
- Create: `portal/internal/conf/vault_config.go` — 解析 `SATH_VAULT_ENABLED`（bool）+ `SATH_VAULT_ROOT`（vault 绝对路径）。
- Test: `portal/internal/conf/vault_config_test.go`
- Modify: `portal/internal/conf/handbook_config.go`（如已存在通用 env 解析可复用；否则照抄范式）

- [ ] **Step 1: 写失败测试**

```go
// portal/internal/conf/vault_config_test.go
package conf

import "testing"

func TestVaultConfigDefaultsDisabled(t *testing.T) {
    t.Setenv("SATH_VAULT_ENABLED", "")
    c := LoadVaultConfig()
    if c.Enabled { t.Fatal("vault should be disabled by default") }
    if c.Root != "" { t.Fatal("root should be empty by default") }
}

func TestVaultConfigEnabled(t *testing.T) {
    t.Setenv("SATH_VAULT_ENABLED", "true")
    t.Setenv("SATH_VAULT_ROOT", "E:/sitath/knowledge")
    c := LoadVaultConfig()
    if !c.Enabled { t.Fatal("vault should be enabled") }
    if c.Root != "E:/sitath/knowledge" { t.Fatalf("bad root: %s", c.Root) }
}
```

> 先读 `portal/internal/conf/handbook_config.go`，仿照它读取 env 的方式（`os.Getenv` + `strconv.ParseBool`），保持与既有 `LoadHandbookConfig` 一致的命名与错误处理风格。

- [ ] **Step 2: 跑测试确认失败** — `cd portal && go test ./internal/conf/... -run TestVaultConfig` → FAIL（`LoadVaultConfig` 未定义）

- [ ] **Step 3: 实现 `vault_config.go`**

```go
package conf

import (
    "os"
    "strconv"
)

type VaultConfig struct {
    Enabled bool
    Root    string
}

func LoadVaultConfig() VaultConfig {
    enabled, _ := strconv.ParseBool(os.Getenv("SATH_VAULT_ENABLED"))
    return VaultConfig{
        Enabled: enabled,
        Root:    os.Getenv("SATH_VAULT_ROOT"),
    }
}
```

> 若 `handbook_config.go` 已有共享的 env 解析 helper（如 `envBool`），复用它而非重复实现。

- [ ] **Step 4: 跑测试确认通过** — `cd portal && go test ./internal/conf/...` → PASS

- [ ] **Step 5: 提交**

```bash
git add portal/internal/conf/vault_config.go portal/internal/conf/vault_config_test.go
git commit -m "feat(portal): 新增 SATH_VAULT_ENABLED/SATH_VAULT_ROOT 环境开关"
```

---

## Task 6: 注册 `vault_recall` 工具（agent 可检索经验）

**Files:**
- Create: `framework/tool/vault_recall_tool.go` — `RegisterVaultRecallTool(reg, recallFn)`：工具名 `vault_recall`，入参 `query`（string）+ `limit`（int），出参命中片段列表（含 `path`/`score`/`source`/`type`/`created` + 正文摘要）。
- Test: `framework/tool/vault_recall_tool_test.go`
- Modify: `portal/internal/chat/agent_builder.go`（或对应的工具注册处）—— `vault.Enabled` 时注册 `vault_recall`。

- [ ] **Step 1: 写失败测试**

```go
// framework/tool/vault_recall_tool_test.go
package tool

import "testing"

func TestRegisterVaultRecallTool(t *testing.T) {
    reg := NewRegistry()
    if err := RegisterVaultRecallTool(reg, nil); err != nil {
        t.Fatal(err)
    }
    if _, ok := reg.Get("vault_recall"); !ok {
        t.Fatal("vault_recall not registered")
    }
}
```

> 先读 `framework/tool/` 里一个简单工具（如 `rca_code_tools.go`）确认 `Registry`/`Get`/工具注册的精确 API（`reg.Get` 的返回签名），照实写，避免编造。

- [ ] **Step 2: 跑测试确认失败** — `cd framework && go test ./tool/... -run TestRegisterVaultRecallTool` → FAIL

- [ ] **Step 3: 实现 `vault_recall_tool.go`**

工具定义要点（描述要让 agent 知道「有经验可查时用它，别现场 rediscover」）：
- name：`vault_recall`
- description：`Search the experience knowledge vault for prior troubleshooting notes (workflows, gotchas, tool usage, cases). Use BEFORE guessing how to query logs / trace a flow — there may already be a recorded way.`
- params：`query`（必填，自然语言检索串）、`limit`（选填，默认 5）。
- execute：调注入的 `recallFn(ctx, query, limit)`，返回命中的 `[]Hit`，序列化成 JSON，每条含 `path`/`score`/`source`/`type`/`created`/`excerpt`（正文前 N 字符）。

`recallFn` 类型：`func(ctx context.Context, query string, limit int) ([]vault.Hit, error)`，由 portal 注入（Task 7 装配）。

- [ ] **Step 4: 跑测试确认通过** — `cd framework && go test ./tool/... -run TestRegisterVaultRecallTool` → PASS

- [ ] **Step 5: 提交**

```bash
git add framework/tool/vault_recall_tool.go framework/tool/vault_recall_tool_test.go
git commit -m "feat(tool): 新增 vault_recall 工具供 agent 检索经验"
```

---

## Task 7: portal 装配（vault → 索引 → 工具 → system prompt 指引）

**Files:**
- Modify: `portal/internal/chat/agent_builder.go` — 当 `vault.Enabled`：
  1. 建 `vault.Watcher`（或启动时 `Sync` 一次），持有 `UnitVectorIndex` + `UnitEmbedder` + `map[unitID]Note`。
  2. `RegisterVaultRecallTool(reg, recallFn)`，`recallFn` 闭包捕获上面的索引与 embed。
  3. 在 `BuildEffectiveSystemPrompt` 追加「沉淀经验」指引：排查结束若有用得上的经验，用 `write_file` 写 `_agent_notes/`，frontmatter 必须 `status: pending` + `type` + `tags` + `source: session:<id>`；禁止写正式目录、禁止自晋升。

- [ ] **Step 1: 写失败测试（system prompt 注入指引）**

```go
// portal/internal/chat/agent_builder_test.go（或就近新增）
func TestBuildEffectiveSystemPromptVaultHint(t *testing.T) {
    p := BuildEffectiveSystemPrompt("base", nil, vaultHintEnabled(true))
    if !strings.Contains(p, "_agent_notes") {
        t.Fatalf("expected vault deposit hint in system prompt:\n%s", p)
    }
}
```

> 先读 `agent_builder.go:459` 的 `BuildEffectiveSystemPrompt` 现有签名与调用点，确认是否要改成可注入 vault 提示的变体；若签名需要加参数，同步改所有调用点。避免编造签名。

- [ ] **Step 2: 跑测试确认失败** — `cd portal && go test ./internal/chat/... -run TestBuildEffectiveSystemPromptVaultHint` → FAIL

- [ ] **Step 3: 实现装配**

在 `agent_builder.go` 中：
- 读 `conf.LoadVaultConfig()`（或通过依赖注入传入）。
- `Enabled` 时：`embedder := <portal 已有的 UnitEmbedder 实现>`（查 `memory_extraction` / `agent_builder` 里如何构造 embedder，照用）；`idx := memory.NewInMemoryUnitVectorIndex()`（或 SQLite）；启动时 `vault.Sync(...)`，起 `vault.NewWatcher(...)`。
- 注册 `vault_recall`，`recallFn` 闭包：`vault.Recall(ctx, embedder, idx, byID, query, limit)`，`byID` 由 watcher 每次 reload 后重建。
- system prompt 追加沉淀指引（见 Step 1 文案）。

- [ ] **Step 4: 跑测试确认通过** — `cd portal && go test ./internal/chat/...` → PASS

- [ ] **Step 5: 提交**

```bash
git add portal/internal/chat/agent_builder.go
git commit -m "feat(portal): 装配 vault 索引/vault_recall 工具并注入沉淀指引"
```

---

## Task 8: 端到端验证（手工 + evals A/B 骨架）

**Files:**
- Modify: `evals/tasks/rca_handbook.jsonl`（或新增 `evals/tasks/vault_experience.jsonl`）—— 加一条「二次排查命中经验」的样本。
- Modify: `evals/runner/portal.go`（如需区分 vault on/off；先复用 `rca_handbook` 路径）

- [ ] **Step 1: 手工验证闭环**

```bash
# 1. 建 vault 根，写一条 promoted 经验笔记
mkdir -p E:/sitath/knowledge/ops
cat > E:/sitath/knowledge/ops/es-flow-8110008.md <<'EOF'
---
type: experience
status: promoted
tags: [es, 流水, 8110008]
source: session:manual
created: 2026-10-14
---
# 存档卸载失败流水怎么查
存档卸载失败的流水 event=8110008，在 backend-cgarchive-* 集群，用 es_log_query(cluster=cgarchive) 查。
EOF

# 2. 以 SATH_VAULT_ENABLED=true SATH_VAULT_ROOT=E:/sitath/knowledge 启动 portal
# 3. 用 agent 提一个同类问题，观察 timeline 里是否先调 vault_recall 命中经验，
#    而不是反复 list_tools/load_skill 才动手
```

- [ ] **Step 2: 写失败评测样本（A/B）**

在 `evals/tasks/` 新增 vault 样本，判据沿用 `expect.root_cause` 关键词命中（复用 `rca_handbook` 的 `evaluateRCALocalization`）。样本输入是「同类的二次排查问题」，验证有 vault 时 agent 命中经验、更快给出结论。

- [ ] **Step 3: 跑 A/B**

```bash
cd evals/runner
# off：SATH_VAULT_ENABLED 未设；on：设 true + root
go run . -mode portal -tasks vault_experience -agent <id> -repeat 1
```

- [ ] **Step 4: 记录结果**，对比 on/off 的 `ToolsUsed`（on 应含 `vault_recall`）与命中根因速度。

- [ ] **Step 5: 提交评测样本与说明**

```bash
git add evals/tasks/vault_experience.jsonl
git commit -m "test(evals): 新增 vault 经验命中 A/B 样本"
```

---

## Self-Review 记录

- **Spec 覆盖**：spec 的四个组件（沉淀/晋升/索引/检索）分别落在 Task 1–8；「pending 不进检索」由 Task 2 的 `Scan` 只收集 promoted 保证；「复用 write_file」在 Task 7 system prompt 指引；「evals A/B」在 Task 8。无遗漏。
- **占位符扫描**：无 TBD/TODO；每个实现步骤都有可编译代码或精确命令。
- **类型一致性**：`Note`/`Hit`/`Status`/`Zone` 在 Task 1 定义、Task 3–4 复用；`UnitVectorIndex`/`UnitEmbedder` 来自 `framework/memory` 既有接口；`vaultScope`/`vaultScopeID` 常量在 Task 4 定义、Task 3 引用（Task 3 的测试里以字面量 `"vault"` 出现，与 Task 4 常量一致）。
- **范围**：单闭环；自动触发、ACL、Neo4j 明确后置。

---

## 依赖与风险提示（实现者须知）

1. **`UnitEmbedder` 的真实实现**：Task 3/4/7 依赖一个能 embed 的 `UnitEmbedder`。生产上由 portal 按 agent 模型解析（`memory_extraction.auxiliary`）。实现 Task 7 前必须先定位现有 embedder 构造方式（grep `UnitEmbedder`/`Embed(` in portal），复用而非新造。
2. **`UnitVectorIndex` 无法枚举 unitID**：清理已删除笔记需在 `Sync` 侧维护「上次 rel 集合」（Task 4 已标注）。SQLite 实现（`sqlite_unit_vector.go`）可能有 `Has`/删除能力，但同样无法全量枚举——设计上以 `Sync` 维护增量集合为准。
3. **`vault_recall` 的 recallFn 签名**：Task 6 定义 `func(ctx, query, limit) ([]vault.Hit, error)`，Task 7 闭包实现必须完全匹配。
4. **system prompt 注入签名**：Task 7 Step 1 要求先读 `BuildEffectiveSystemPrompt` 现有签名再改，勿凭记忆改签名。
5. **SQLite vs InMemory**：最小闭环用 `InMemoryUnitVectorIndex`（重启即空，靠启动 `Sync` 重建）；持久化留待后续（若需跨重启，用 `sqlite_unit_vector.go`）。
