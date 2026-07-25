# RCA 符号导航（LSP）工具 — 设计文档

**日期**: 2026-07-25  
**状态**: 已确认（implementation plan: `docs/superpowers/plans/2026-07-25-rca-symbol-lsp.md`）  
**场景**: 多仓库代码深度阅读；在文本检索（`rca_grep`）之上补齐符号级「跳转定义 / 查找引用」，为后续静态调用链打底。  
**关联**:
- [rca-toolchain-design](./2026-07-07-rca-toolchain-design.md)（`rca_grep` / `rca_glob` / `rca_read`）
- [rca-agent-binding-design](./2026-07-08-rca-agent-binding-design.md)（portal `type=rca` + `func_path`）
- [harness-engineering-gap-design](./2026-07-11-harness-engineering-gap-design.md)（口径 C 不做完整 IDE harness；本设计只补语义导航最小集）

---

## 0. 已确认决策

| 项 | 选择 |
|----|------|
| 语言后端 | **LSP 通用适配**：抽象 `LanguageServer`；一期接 **gopls**；后续可挂 pyright / jdtls |
| 工具形态 | **单一工具** `rca_symbol`，用 `action` 分发 |
| 一期 action | 仅 **`definition`** + **`references`** |
| 注册与绑定 | **方案 A**：独立 portal `func_path: rca_symbol`；framework `RegisterRCASymbolTool`；**长驻 LSP 进程池**（按 repo root 复用） |
| 定位入参 | **两者都支持**：优先 `file`+`line`+`character`；也支持 `symbol` 名（先解析再导航） |
| 与 `rca_code` 关系 | **解耦**：不并入 `RegisterRCACodeTools`；共享 `roots` / `pathguard` / evidence 契约 |

---

## 1. 目标与非目标

### 1.1 目标

在 `sixath/framework` 新增原生工具 `rca_symbol`，并经 portal `type=rca` 绑定到 Agent，使 Agent 能在 `rca.repos.roots` 白名单内：

1. **`definition`**：给定符号位置（或符号名），返回定义处 `repo/file/line/character`（及可选 `name`）。  
2. **`references`**：给定符号位置（或符号名），返回引用列表（含定义与否可配置，默认含定义）。  
3. 底层走 **LSP**，一期实现 **gopls**；接口层不绑定 Go，便于二期加语言。  
4. 出参对齐既有 RCA evidence 契约（`ok` / `error_code` / `evidence_refs`）。

### 1.2 非目标（一期不做）

| 项 | 说明 |
|----|------|
| `call_hierarchy` / `outline` / `implementations` | 二期；接口可预留，工具 schema 一期不暴露 |
| 非 Go LSP 实现 | 只交付 gopls adapter；接口与注册点留扩展 |
| 自动安装 gopls | 依赖运行环境已安装；缺失时 `CheckFn` 隐藏工具 |
| 并入 `rca_code` 一次注册 | 避免文本检索与语义工具耦合 |
| 自动 clone/pull 仓库 | 与既有 RCA 非目标一致 |
| `rca_git` / 完整 IDE harness | 另项；本设计不覆盖 |
| 跨仓库「模块图 / 服务依赖图」 | 非符号导航范围 |

---

## 2. 背景与动机

现有 `rca_grep` / `rca_read` 只能做文本级定位。同名方法、接口实现、包内间接引用场景下，Agent 需多轮启发式 grep，易漏/易误。

**运行时调用链**已有 `jaeger_trace`；缺的是 **静态符号导航**（IDE「Go to Definition / Find References」）。本设计补齐该层，且刻意不做完整 call hierarchy，避免一期范围膨胀。

典型闭环（与现有工具配合，工具间不互调）：

```text
jaeger_trace / es_log_query
    → rca_grep（类名/错误串粗定位，拿到 1-based line）
    → rca_symbol(definition|references)（符号级确认；character 通常可省略）
    → rca_read（读实现上下文）
```

工具 Description 须写明上述推荐工作流。

---

## 3. 架构

```text
                    rca_symbol (tool.Registry)
                              │
                              ▼
                     RegisterRCASymbolTool
                              │
              ┌───────────────┴───────────────┐
              ▼                               ▼
        pathguard / selectRoots          LSP Pool（per Registry）
        (复用 rca_repos.go)          (按规范化 root 复用进程)
                                              │
                                              ▼
                                   LanguageServer 接口
                                              │
                              ┌───────────────┴───────────────┐
                              ▼                               ▼
                         GoplsServer                    (未来) Pyright / …
```

**依赖方向**：
- `tool` 包持有工具注册与路径守卫；LSP 协议客户端放在 **`tool/lsp/`**（与 `tool/browser` 并列）。
- `LanguageServer` **不**依赖 `agent` / `templates`。
- portal 仅在 `registerRCATool` 增加 `case "rca_symbol"`，调用 `RegisterRCASymbolTool`。
- 同一 Agent 可同时绑定 `rca_code` + `rca_symbol`（roots 可重复配置，允许）。

---

## 4. 行列与位置契约（钉死）

对外（工具入参/出参）与内部（LSP）分层如下：

| 层 | line | character | 编码 |
|----|------|-----------|------|
| **工具 API（Agent 可见）** | **1-based** | **0-based** | character 为 **UTF-16 code unit**（LSP 规范） |
| **`lsp.Position`（内部）** | **0-based** | **0-based** | UTF-16 code unit |
| **转换边界** | 仅在 `rca_symbol_tool.go`：入参 `line-1` → `Position`；出参 `Position.Line+1` → `Location.Line` | 同层透传 character | — |

与现有 RCA 对齐：`rca_grep` / `rca_read` 均为 **1-based line**。推荐 Agent：

1. 用 `rca_grep` 拿到 `file` + `line`；  
2. 调 `rca_symbol` 时 **优先只传 `file`+`line`，`character` 省略（默认 0）**；  
3. 仅当需同行内精确定位时再填 `character`；**不要**从字节偏移自行估算（非 ASCII 行会错）。

`Location` 类型注释与实现必须只保留上述一种对外契约，禁止「0-based 或 1-based」歧义表述。

---

## 5. 核心抽象

### 5.1 `LanguageServer` 接口（framework）

```go
// 示意；实现以最终代码为准
type Position struct {
    Line      int // 0-based，LSP
    Character int // 0-based，UTF-16 code unit
}

type Location struct {
    Repo      string
    File      string // repo-relative，slash 路径
    Line      int    // 对外 1-based
    Character int    // 对外 0-based，UTF-16
    Name      string // 可选
}

type LanguageServer interface {
    EnsureReady(ctx context.Context, root string) error
    Definition(ctx context.Context, root, relPath string, pos Position) ([]Location, error)
    References(ctx context.Context, root, relPath string, pos Position, includeDeclaration bool) ([]Location, error)
    Close(ctx context.Context) error
}
```

**工厂**：

```go
type ServerFactory func(ctx context.Context, root string, opts ServerOpts) (LanguageServer, error)
```

一期：`NewGoplsFactory(opts)`；`opts` 含 `Command`（默认 `gopls`）、`Env`、`InitTimeout`、`RequestTimeout`。

initialize 结果若缺少 `definitionProvider` / `referencesProvider` → 执行期返回 **`error_code=permanent`**（不走 CheckFn；CheckFn 只探测二进制存在）。

### 5.2 LSP Pool

| 规则 | 约定 |
|------|------|
| **作用域** | **per Registry 实例**（随 `RegisterRCASymbolTool` 创建并挂在闭包/opts 上），**不是** package 全局单例。避免多 Agent / 多 Registry 互相抢同一 gopls。 |
| **Key** | `normalizeRoot(root)`：`filepath.Clean` + 若可能则 `filepath.EvalSymlinks`；Windows 上再 `strings.ToLower` 盘符与路径（在 Eval 之后），统一分隔符语义，使 `D:\foo` 与 `D:/foo` 命中同一条目。 |
| **并发** | per-root **mutex**（一期整请求串行化到该 server）。 |
| **复用** | 已 `initialize` 的子进程 + JSON-RPC stdio。 |
| **失败恢复** | 子进程退出 / RPC 超时 → 从 pool **删除并 `Close`（Kill）** 该条目，下次按需重建；本次调用 `error_code=transient`。 |
| **超时** | 默认 ready 60s、request 30s，可配。 |
| **关闭** | `Registry` 或工具持有的 pool 提供 `Close()`：遍历 Kill 全部子进程。portal **每次 `BuildRegistry` 新建 Registry** 时，旧 Registry 若可观察则应 Close；若当前构建路径无法 hook，则依赖进程退出回收，并在实现计划中加「尽量 Close」TODO。roots 变更 = 新 Registry → 旧 pool 整体废弃。 |
| **Windows Kill** | 对 `exec.Cmd` 调 `Process.Kill()`；若留下孤儿，实现阶段可评估 process group（一期不强制 job object）。 |

### 5.3 gopls 实现要点

- 启动：`exec.Command` + stdin/stdout JSON-RPC（Content-Length framing）。  
- `initialize`：`rootUri` = 规范化仓库根的 `file://` URI；单 `workspaceFolders`。  
- 请求前对目标文件 `textDocument/didOpen`（读盘）；一期可不 `didClose`。  
- **模块**：仓内宜有可解析 `go.mod`；无法加载模块时 `EnsureReady` / 首次请求失败 → **`permanent`**（配置/工程问题，非瞬时）。  
- **跨 root**：不建多 root 联合 workspace；换 `repo` 由 Agent 负责。

---

## 6. 空 roots / CheckFn / Portal 职责（钉死）

与 `rca_code` **对称**：

| 层 | 行为 |
|----|------|
| **Portal `registerRCATool`** | `roots` 空 → **skip + warn，不调用 Register**（与 `rca_code` 相同） |
| **`RegisterRCASymbolTool`** | **不因空 roots 返回 error**；照常注册（与 `RegisterRCACodeTools` 对称）。运行时 `selectRoots` 空 → **`permanent`** |
| **`CheckFn`** | **仅**探测 `GoplsPath`（默认 `gopls`）可执行；**不**门控 empty roots |
| **Framework 直连 YAML** | 若 templates 读取 `rca.repos.roots` 为空仍调用 Register → 工具可见但执行 permanent（与上表一致） |

---

## 7. 工具契约：`rca_symbol`

### 7.1 注册

```go
func RegisterRCASymbolTool(reg *Registry, roots []string, opts RCASymbolOpts) error
```

- `opts`：`GoplsPath`、`ReadyTimeout`、`RequestTimeout`、可选 `ServerFactory`（单测 fake）、内部持有 **该次注册专属 Pool**。  
- `Toolset`: `ToolsetRCA`（`"rca"`）。  
- `CheckFn`: gopls 可执行性；不可用则不出 `ListForAPI`。  
- `RequiresSequential`: **true**。  
- Description：推荐 `rca_grep` → `rca_symbol` → `rca_read`；说明 character 常可省略；`max_results` 主要约束 `references`。

### 7.2 入参

| 字段 | 类型 | 必填 | 说明 |
|------|------|------|------|
| `action` | string | 是 | `definition` \| `references` |
| `repo` | string | 是 | 仓库逻辑名（`filepath.Base(root)`） |
| `file` | string | 条件 | 见定位规则 |
| `line` | integer | 条件 | **1-based** |
| `character` | integer | 否 | **0-based UTF-16**；默认 0 |
| `symbol` | string | 条件 | 如 `DoSomething` 或 `pkg.DoSomething` |
| `include_declaration` | boolean | 否 | 仅 `references`；默认 `true` |
| `max_results` | integer | 否 | 默认 **50**（有意小于 `rca_grep` 的 100）；对 `locations` 与 `candidates` 均生效 |

**Execute 校验（JSON Schema 无法表达 XOR，必须在代码里）**：

| 条件 | 结果 |
|------|------|
| `action` 非法或缺失 | permanent |
| `repo` 空 | permanent |
| 同时缺（`file`+`line`）与 `symbol` | permanent |
| 有 `file` 无 `line`（且无可用 symbol 路径） | permanent |
| 同时有 `file`+`line` 与 `symbol` | **以 `file`+`line` 为准**，忽略 `symbol` |

### 7.3 定位解析

1. **`file` + `line` 路径（一期必须）** → 转 `Position` 后调 LSP。  
2. **`symbol` 路径**：  
   - 优先 `workspace/symbol`（gopls）；失败或不稳则 **仓内只搜 `*.go`** 的标识符启发式（见 §7.4）。  
   - 0 命中 → `ok=false`，`permanent`。  
   - 2+ 命中（截断后仍 ≥2，或截断前 ≥2）→ 消歧响应，**不**调 definition/references。  
   - 恰 1 命中 → 用该位置继续 LSP。  
3. LSP 成功但 `locations` 为空（含过滤越权后为空）→ `ok=false`，**`permanent`**（符号无定义/无引用，或全被守卫过滤）；若属 gopls 崩溃/超时则已在 pool 层标 **transient**。

### 7.4 Symbol 启发式（可测约定）

当不走 / 不可用 `workspace/symbol` 时：

| 规则 | 约定 |
|------|------|
| 范围 | 仅当前 `repo` root 下 `*.go`（经 pathguard） |
| 输入 | `symbol` trim；若含 `.`，拆为 `pkgPart` + `namePart`（最后一段为 name）；否则 `namePart=symbol` |
| 匹配 | ripgrep/内容搜索 **整词** `namePart`；行文本匹配声明形态优先：`func … namePart`、`type namePart`、`func (.*) namePart` |
| 排序 tier | **T1** 声明行精确匹配 > **T2** 行内标识符精确匹配 > **T3** 有 `pkgPart` 且路径/package 子串匹配 > **T4** 其余整词命中 |
| multi-hit | 排序后取前 `max_results`；同 tier 多条并列 → 全部进入 candidates（仍受 max 截断） |
| 唯一 | 仅 1 条；或排序第一为 **T1** 且第二不存在或第二 tier **严格更低** → 唯一；其余 multi |

### 7.5 出参

**成功（definition / references）**：

```json
{
  "ok": true,
  "action": "definition",
  "repo": "service-a",
  "locations": [
    {
      "repo": "service-a",
      "file": "internal/biz/foo.go",
      "line": 42,
      "character": 5,
      "name": "DoSomething"
    }
  ],
  "truncated": false,
  "evidence_refs": [
    {
      "kind": "code",
      "repo": "service-a",
      "path": "internal/biz/foo.go",
      "line": 42,
      "summary": "definition DoSomething"
    }
  ]
}
```

- `definition` 多 location：同样受 `max_results` 截断，可 `truncated=true`。  
- `max_results` 主要服务于 `references`。

**消歧（不上 LSP 导航）**：

```json
{
  "ok": true,
  "action": "definition",
  "repo": "service-a",
  "needs_disambiguation": true,
  "candidates": [
    {"repo": "service-a", "file": "a.go", "line": 10, "character": 0, "name": "Foo"}
  ],
  "truncated": false
}
```

- **不得**包含 `locations`。  
- **不得**包含 `evidence_refs`（消歧不是结案证据）。  
- `action` / `repo` 必回显；`action=references` 时消歧 schema **相同**（仅 `action` 字段不同）。

**失败**：`NormalizeRCAResult` / `rcaErr`：`ok=false`，`error_code` ∈ {`transient`,`permanent`}。

**路径守卫**：所有 `file` 经 `resolveInRepos`；LSP 返回 root 外 URI → **丢弃**该条并 slog warn；不向模型暴露绝对路径。

### 7.6 Evidence 集成

`framework/tool/evidence.go` 的 `deriveEvidenceRefs` **须增加 `rca_symbol` 分支**（从 `locations` 派生 `kind=code` refs），**或**在 Execute 中通过 `EvidenceMeta.Refs` 显式注入（二选一，实现时选一种并单测）。

`evidence_refs` **不要求**带 `character`（与现有 `rca_grep`/`rca_read` 的 line 级证据对齐）；精确定位以 `locations` 为准。

消歧路径：不调用 derive / 不注入 refs。

---

## 8. Portal 绑定

### 8.1 `func_path`

| func_path | 表单字段 | config.rca | framework |
|-----------|----------|------------|-----------|
| `rca_symbol` | roots（多行）；可选 `gopls_path`；可选 timeout 字段 | `{func_path, roots, gopls_path?, ready_timeout_sec?, request_timeout_sec?}` | `RegisterRCASymbolTool` |

校验：`func_path` ∈ {`rca_code`, `jaeger_trace`, `es_log_query`, **`rca_symbol`**}。

### 8.2 `registerRCATool`

```go
case "rca_symbol":
    roots := stringSliceFromAny(cfg["roots"])
    if len(roots) == 0 {
        slog.Warn("rca: rca_symbol has no roots, skip")
        return
    }
    goplsPath, _ := cfg["gopls_path"].(string)
    _ = tool.RegisterRCASymbolTool(reg, roots, tool.RCASymbolOpts{GoplsPath: goplsPath /* + timeouts from cfg */})
```

### 8.3 Web

- `ToolForm`：子工具下拉增加「符号导航 (definition/references)」；roots 同 `rca_code`；可选 gopls 路径与超时。  
- `client.ts`：`ToolConfig.rca` 增加 `gopls_path?`、`ready_timeout_sec?`、`request_timeout_sec?`。

---

## 9. 配置

密钥不涉及。

```yaml
# framework 直连（若 templates 支持读取）；portal 以工具 Config.rca 为准，二者互不强制同步
rca:
  repos:
    roots:
      - D:/workspace/repos/service-a
  symbol:
    gopls_path: gopls
    ready_timeout_sec: 60
    request_timeout_sec: 30
```

portal 工具 JSON 示例：

```json
{
  "func_path": "rca_symbol",
  "roots": ["D:\\workspace\\cloudgame"],
  "gopls_path": "gopls"
}
```

---

## 10. 文件规划

| 路径 | 职责 |
|------|------|
| `framework/tool/lsp/server.go` | 接口与类型 |
| `framework/tool/lsp/pool.go` | per-Registry 池、normalizeRoot、Kill |
| `framework/tool/lsp/jsonrpc.go` | Content-Length + JSON-RPC |
| `framework/tool/lsp/gopls.go` | gopls Definition/References |
| `framework/tool/lsp/uri.go` | `file://` ↔ 本地路径（含 Windows） |
| `framework/tool/lsp/*_test.go` | 协议/池单测；`-tags=gopls_integration` 可选集成测（CI 默认跳过） |
| `framework/tool/rca_symbol_tool.go` | 注册、校验、定位、截断 |
| `framework/tool/rca_symbol_tool_test.go` | 守卫、消歧、fake LSP、越权过滤 |
| `framework/tool/evidence.go` | `rca_symbol` → evidence_refs |
| `portal/internal/chat/agent_builder.go` | `case "rca_symbol"` |
| `portal/internal/biz/tool.go` | 合法 `func_path` |
| `web/src/pages/ToolForm.tsx`、`web/src/api/client.ts` | UI / 类型 |

复用：`rca_repos.go`、`pathguard`、`NormalizeRCAResult`。

---

## 11. 测试与验收

### 11.1 framework

- Pool：同 normalizeRoot 复用；`D:\x`/`D:/x` 同键（Windows）；失败重建；Close 后不可用。  
- JSON-RPC framing；URI 编解码。  
- `rca_symbol`：fake server；守卫；消歧完整 schema；空 locations → permanent；empty roots 运行时 permanent；evidence 有/无。  
- gopls 集成：可选 tag，临时 module。

### 11.2 portal

- 有 roots 注册出 `rca_symbol`；无 roots skip；Create/Update 接受该 `func_path`。

### 11.3 人工验收

1. `file`+`line` → definition 正确。  
2. 同位置 references + 非空 `evidence_refs`。  
3. 多义 `symbol` → candidates，无 evidence；带行列重试成功。  
4. 无 gopls → 工具不出现在 API schema。  
5. 与 `rca_code` / jaeger / es 并存无回归。  
6. 无/坏 `go.mod` 仓 → permanent（非挂死）。

---

## 12. 二期（不在本期）

1. `call_hierarchy` / `outline` / `implementations`。  
2. 按目录探测语言 factory。  
3. 多 root 联合 workspace。  
4. RCA Skill 编排。  
5. `service_repo_map`。  
6. candidates 增加 `kind`（func/type/method）。  
7. Pool idle TTL；Windows job object 杀进程组。

---

## 13. 风险与缓解

| 风险 | 缓解 |
|------|------|
| gopls 冷启动 | 长驻 per-Registry pool；首次失败可 transient 重试 |
| 大仓内存 | 单 root 单实例；二期 idle TTL |
| Windows 路径 / URI / 重复进程 | `normalizeRoot` + `uri.go` 单测 |
| BuildRegistry 重建泄漏 | per-Registry pool；尽量 Close；implementation plan 单列跟踪项 |
| gopls 集成测拖慢 CI | 默认跳过；仅 `-tags=gopls_integration` + 本机 gopls 时跑 |
| symbol 消歧噪声 | 声明行优先 + max_results；强制 candidates 契约 |
| 非 ASCII 列偏移 | 文档引导省略 character；UTF-16 单测含中文标识符行 |
| 无 go.mod | permanent，不重试死循环 |

---

## 14. 成功标准

- 绑定 `rca_symbol` 后，无需 terminal 手敲 gopls，即可完成 Go **definition / references**。  
- `LanguageServer` 抽象可测；二期加语言不改工具 schema 主干。  
- 不破坏现有 `rca_code` / jaeger / es 与 evidence 结案流。
