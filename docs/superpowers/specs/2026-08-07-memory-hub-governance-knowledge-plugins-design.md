# 治理与知识面插件化 — 本地实现 + 外部 Memory Hub

**日期**: 2026-08-07  
**状态**: **框架/后端已对齐（2026-08-07）**——3 个接口决策点获确认（§12）；错误/权威边界已定案（§3.5）；**可进入 P0 实现规划**  
**目标**: 将「治理」与「知识面」解耦为可插拔 Provider；**默认提供本地实现**；通过同一接口对接多家外部 memory-hub，无需改内核。

**关联**:
- [MemoryStore 门面](./2026-07-25-memory-store-facade-design.md)
- [Prefetch 配额](./2026-07-27-memory-store-prefetch-quota-design.md)
- [P0 实现计划](../plans/2026-08-07-memory-hub-governance-knowledge-p0.md)
- [评审对齐](./2026-08-07-memory-hub-review-alignment.md)

---

## 0. 决策摘要

| 项 | 选择 |
|----|------|
| 内核保留 | `MemoryStore` + 压缩 L0–L2 + Prefetch/Orchestrator + procedural/轨迹 |
| 插件面 | **GovernanceProvider** + **KnowledgeProvider**（可独立启停） |
| **默认实现** | **全局默认各一份**：默认 Governance + 默认 Knowledge（通常均为 `local`） |
| **Agent 覆盖** | **已确认**：Agent 可配置自己的治理面 / 知识面；**已配置则优先用 Agent 自己的**，未配置的面回落到全局默认 |
| 外部对接 | Hub **Adapter** 实现同一接口；挂到「默认」或「某 Agent」的 provider 槽位 |
| **解析规则** | 每个面（治理 / 知识）独立：`Agent 配置 ?? 全局默认`；**同一面不同时合并多个 Provider**（避免混权） |
| **外部资产读写** | **已确认**：**可读可写**；写 fail-closed；按 `AssetRef.Hub` 路由 |
| **Skill 优先级** | **已确认**：Loadout 绑定 > 本地 `skills_dirs`；Hub 未绑定不进运行时（§9）；Loadout 来自**该 Agent 解析到的治理面** |
| 权威边界 | 会话/轨迹/压缩：**始终本地**；资产/知识：当前 Agent **解析后的** Provider（判定见 §3.5） |
| 失败策略 | 外部读 fail-open；写 fail-closed；**未注册 name = 装配期硬失败**；**已注册但运行时不可达 = 运行时错误**（读可配回落、写不可）——两者区分见 §3.5.1 |
| Agent 覆盖落点 | **`RuntimeToolsConfig`（proto/DB）新增两个 `optional string`**，与 `hybrid_recall` / `memory_write_enabled` 同路径；**不**另开 YAML 面（§6） |
| 非目标（一期） | 本地↔外部双向 sync；完整 Wiki/CodeGraph；单 Agent 同时挂多个治理 Provider；`PrefetchHints`（§3.3）；`Confidence` 参与过滤（§10.3） |

---

## 1. 问题

治理与知识若焊死在产品路径里：

- 无法渐进增强（先本地资产治理，再接外部 Hub）；
- 换一家 Hub 要改 Prefetch / 工具 / 面板；
- 「只有外挂、没有本地」会让未部署 Hub 的环境永久缺治理/知识面。

因此：**同一套 Provider 契约下，先有本地实现，再有外部 Adapter。**

---

## 2. 架构

```text
┌──────────────────────────────────────────────────────────────┐
│ Portal / ReAct                                               │
│  按 AgentID 解析 → 治理面 + 知识面                           │
└──────────────┬─────────────────────────────┬─────────────────┘
               │                             │
               ▼                             ▼
      MemoryStore（运行时）          Provider 目录（可注册多个）
      units / transcript /           local | tencent | ...
      agent files / 压缩 / 轨迹              │
               │                    ┌────────┴────────┐
               │                    ▼                 ▼
               │            全局默认槽位          Agent 覆盖槽位
               │         gov=local               gov=tencent?
               │         know=local              know=tencent?
               │                    │                 │
               │                    └────────┬────────┘
               │                             ▼
               │                    Resolve(agent):
               │                      gov  = agent.gov  ?? default.gov
               │                      know = agent.know ?? default.know
```

**原则**:

1. Provider 只暴露 sixath 语义；厂商字段留在 Adapter。  
2. **`local` 作为默认实现一等公民**；也可被设为某 Agent 的覆盖。  
3. **全局默认各一个**治理面 / 知识面；Agent 可分别覆盖。  
4. **Agent 已配置 → 用自己的；未配置 → 用默认**（两面独立）。  
5. ~~全局 local+外部合并 Loadout~~：**由本决策取代**——同一 Agent、同一面只使用解析后的那一个 Provider。

---

## 3. 插件接口

建议包：`framework/memory/hub`（不依赖具体 Hub SDK；本地实现可放 `hub/local`，Portal 接线放 portal）。

### 3.1 公共类型（合并 §10.3 补强字段，单处定义）

```go
package hub

type Identity struct {
    UserID, OrgID, TeamID, AgentID, SessionID, TaskID string
    ExternalIDs map[string]string // 仅外部 Adapter 使用
}

type AssetKind string // chat_memory | skill | wiki | code_graph | procedural | unit | custom

// AssetStatus：一期枚举，与 memory_units.status 显式对齐（映射见 §4.1）。
type AssetStatus string

const (
    AssetDraft      AssetStatus = "draft"      // 自动沉淀默认（敏感类型），不进 Loadout/Prefetch
    AssetActive     AssetStatus = "active"     // 可进 Loadout/Prefetch
    AssetStale      AssetStatus = "stale"      // 过旧，召回降权/不注入
    AssetSuperseded AssetStatus = "superseded" // 被新版取代（对齐 units.superseded）
    AssetArchived   AssetStatus = "archived"   // 归档/软删（本地映射 units.deleted，见 §4.1）
)

type AssetRef struct {
    Kind AssetKind
    ID, Hub, Name, Version, Visibility, OwnerID string
    Status     AssetStatus
    Confidence float64 // 0..1；一期本地 extract 恒 0.5，且【不参与任何过滤/排序】（§10.3）
    SourceRef  string  // turn_id / message_id / file uri，可回点原文
    Meta       map[string]any
}
```

`Hub` 字段：本地填 `"local"`；外部填 provider `name`。**`Hub` 是写路由与权威判定的唯一依据（见 §3.5.2）。**

### 3.2 GovernanceProvider / Writer

```go
type GovernanceProvider interface {
    Name() string
    Capabilities() Capabilities
    ResolveLoadout(ctx context.Context, id Identity) ([]AssetRef, error)
    CheckAccess(ctx context.Context, id Identity, asset AssetRef, action string) (bool, error)
    ListAccessible(ctx context.Context, id Identity, filter AssetFilter) (Page[AssetRef], error)
}

type GovernanceWriter interface {
    BindAssets(ctx context.Context, agentID string, refs []AssetRef) error
    UnbindAssets(ctx context.Context, agentID string, refs []AssetRef) error
    SetVisibility(ctx context.Context, asset AssetRef, v Visibility) error
    SetStatus(ctx context.Context, asset AssetRef, status AssetStatus) error // 含人工「确认/废弃」
    // 可选：Share / RevokeShare —— Adapter 按 Hub 能力实现；不支持则返回 ErrNotSupported
}

// GovernanceProvider 若 Capabilities().Write==true，则必须同时实现 GovernanceWriter。
// 本地与外部均适用（已确认：外部资产可读可写）。
```

写路径路由：按 `AssetRef.Hub` 派发到 **Resolve 得到的** Writer；一致性校验见 §3.5.2。**禁止**把外部资产写进 local DB 冒充权威。

### 3.3 KnowledgeProvider

```go
type KnowledgeProvider interface {
    Name() string
    Capabilities() Capabilities
    DescribeTools() []ToolDesc // 可含只读与变更类工具；按 Capabilities 过滤
    Call(ctx context.Context, id Identity, tool string, args map[string]any) (any, error)
}
```

- Knowledge 写（ingest / sync / 更新页等）走 `Call` 中的变更工具；`Capabilities().Write==false` 时不得注册变更工具。写失败 **fail-closed** 返回给工具/UI。
- **`PrefetchHints` 一期不进接口**（YAGNI，见 §0）。理由：一期 LocalKnowledge 默认不注入（深知识靠工具调用），保留空方法违反 MemoryStore 门面「不放空方法」的既有纪律；二期确需 Prefetch 补强时再加。

### 3.4 解析：默认槽位 + Agent 覆盖

```go
// Catalog: 进程内已注册的 Provider 实例（按 name）
type Catalog struct {
    Gov  map[string]GovernanceProvider
    Know map[string]KnowledgeProvider
}

// Defaults: 全局默认各引用一个 name（通常 "local"）
type Defaults struct {
    Governance string // e.g. "local"
    Knowledge  string // e.g. "local"
}

// AgentHubConfig: 来自 RuntimeToolsConfig（proto/DB，见 §6）
type AgentHubConfig struct {
    Governance *string // nil = 用默认；非空 = 覆盖为该 provider name
    Knowledge  *string
    FallbackToDefaultOnReadError bool // 运行时读失败是否回落默认（§3.5.1）
}

// Resolve 只做「name → 实例」映射；name 缺失 = 硬失败（装配期）。
// 运行时可达性不在此判定——见 §3.5.1。
func Resolve(cat Catalog, def Defaults, agent AgentHubConfig) (gov GovernanceProvider, know KnowledgeProvider, err error) {
    gName := def.Governance
    if agent.Governance != nil && *agent.Governance != "" {
        gName = *agent.Governance // Agent 优先
    }
    kName := def.Knowledge
    if agent.Knowledge != nil && *agent.Knowledge != "" {
        kName = *agent.Knowledge
    }
    gov = cat.Gov[gName]
    if gov == nil {
        return nil, nil, fmt.Errorf("hub: governance provider %q not registered", gName)
    }
    know = cat.Know[kName]
    if know == nil {
        return nil, nil, fmt.Errorf("hub: knowledge provider %q not registered", kName)
    }
    return gov, know, nil
}
```

**已确认语义**：

| 场景 | 治理面 | 知识面 |
|------|--------|--------|
| Agent 两面都未配 | 默认 | 默认 |
| 只配了治理 | Agent 的治理 | 默认知识 |
| 只配了知识 | 默认治理 | Agent 的知识 |
| 两面都配了 | Agent 的 | Agent 的 |

同一面**不**再与默认合并。若既要本地 units 治理又要远程 Hub，应：默认仍 `local`，或把「需要的资产」在所选 Hub 侧维护 / 由产品显式切换 Agent 配置。

### 3.5 错误与权威边界（阻塞问题解决方案）

> 本节钉死三条「不写清楚实现者一定各写各的」的接缝。前评审的三个阻塞项在此逐一解决。

#### 3.5.1 「未注册」vs「运行时不可达」——两类失败的判定

**问题**：§0 曾把「Agent 覆盖的 Provider 不可达时读可回落默认」与「缺失则装配期失败」混在一起，导致「腾讯 Hub 宕机」可能被误当成「配置错误」而拒绝启动整个 Agent。

**解决**：明确分成两个阶段、两种失败：

| 阶段 | 判定对象 | 失败即 | 处理 |
|------|----------|--------|------|
| **A. Resolve（装配期）** | provider **name** 是否在 Catalog | name 不在 Catalog | **硬失败**：`Resolve` 返回 error；Agent 保存配置/启动时即拒绝。**绝不静默回落**——避免「以为用了 tencent，实际跑 local」。 |
| **B. 运行时调用** | 已解析的 provider 实例 RPC 是否可达 | RPC 超时 / 连接失败 / 5xx | **运行时错误**：按操作类型分流（见下） |

阶段 B 运行时错误分流：

- **读**（`ResolveLoadout` / `ListAccessible` / `CheckAccess` / Knowledge 只读 `Call`）：
  - `fail_open_external_read: true`（全局）→ 该次读降级为空结果，记 `PrefetchSkipReason` / 工具返回可空。
  - 若 `AgentHubConfig.FallbackToDefaultOnReadError: true` → **仅读** 回落到 `Defaults` 的对应 Provider 再试一次；仍失败则 fail-open 空。
  - `CheckAccess` 特例：读不可达且无回落时，**按拒绝处理**（`false`）——权限判定不 fail-open 放行。
- **写**（`BindAssets` / `SetVisibility` / `SetStatus` / Knowledge 变更 `Call`）：
  - **一律 fail-closed**：返回错误给工具/UI；**不回落默认**（回落默认会把资产写错 Hub，违反 §3.5.2）。

配置校验时机：Agent 保存 `governance`/`knowledge` 覆盖 → 立即对 Catalog 做一次 `Resolve` 干跑，name 缺失当场报错（对应 §8 风险「Agent 覆盖指到未注册 name」）。

#### 3.5.2 写路由的 Hub 一致性 invariant

**问题**：`BindAssets(agentID, refs)` 里每个 `AssetRef.Hub` 是调用方（UI/API）填的。若 UI 传来 `Hub="local"` 的 asset 却 bind 到 `governance=tencent` 的 Agent，无机制拦截——就会出现方案自己点名禁止的「LocalGovernance 把外部资产写进 local DB 冒充权威」。

**解决**：把一致性做成**接口层强制 invariant**，不依赖 UI 自觉。所有写方法入口统一走一个守卫：

```go
var ErrHubMismatch = errors.New("hub: asset.Hub does not match resolved provider")

// enforceHub 在 BindAssets/UnbindAssets/SetVisibility/SetStatus 入口调用。
// provider 是 Resolve(agent) 得到的那个治理面。
func enforceHub(provider GovernanceProvider, refs ...AssetRef) error {
    want := provider.Name()
    for _, r := range refs {
        if r.Hub != want {
            return fmt.Errorf("%w: asset %s hub=%q, provider=%q", ErrHubMismatch, r.ID, r.Hub, want)
        }
    }
    return nil
}
```

规则：

1. 写入口**第一件事**就是 `enforceHub`，不一致直接 `ErrHubMismatch`，绝不落库/发远程。
2. `AssetRef.Hub` 是权威归属的唯一标识：`Hub=="local"` 的资产**只能**由 LocalGovernance 落 local DB；`Hub=="tencent"` 的**只能**经 Tencent Writer。
3. Knowledge 变更 `Call` 同理：Portal 在派发前校验目标 Provider `Name()` 与工具声明的 Hub 归属一致。
4. UI/API 层可做前置校验改善体验，但**不作为安全边界**——边界在接口层的 `enforceHub`。

对应 §8 风险「跨 Hub 误写」「local 与 external 资产 ID 冲突」：invariant + Catalog 内 name 唯一，双重收缩冲突面。

#### 3.5.3 远程 Skill 物化的信任边界与门控（安全红线）

**问题**：绑定外部 Hub 的 skill 到 Agent = 让外部内容进入本地执行路径（远程代码执行风险）。§9.2 原来只有一句「需先物化，不得直接 curl 执行远程脚本」，撑不住。`auto_inject_unbound_hub: false` 只挡住「未绑定自动注入」，但**已绑定的外部 skill 内容变更**这条路径原本敞开。

**解决**：明确物化流程、信任校验、与门控复用。**此条不落地，P2 外部 Adapter 不得上线。**

物化（materialize）流程——外部 Skill 进入运行时的唯一通路：

1. **拉取到只读正文 + 落盘缓存**：Adapter 提供 skill 正文（SKILL.md + 附件），物化到本地缓存目录（如 `~/.sixath/hub-skills/<hub>/<skill_id>@<version>/`）；ReAct **只执行本地缓存副本**，禁止运行时 `curl` 远程脚本。
2. **内容指纹与信任校验**：缓存时记录内容 hash（`content_hash`）。
   - Adapter 若提供签名/校验和 → 校验通过才落盘。
   - 无签名能力的 Hub → 物化默认落 `AssetStatus=draft`，**需人工确认**（`SetStatus(active)`）才能进 Loadout；对应配置 `require_manual_approve_unsigned_hub_skill: true`（默认 true）。
3. **变更即重新过闸**：外部 skill `version`/`content_hash` 变化被视为**新内容**——重新拉取、重新校验，并重新走 **SkillTrustGate**（签名/校验和/人工确认；与 `CommitProceduralRepair` **分离**——后者仅用于 `kind=procedural` 绑定路径）。旧缓存标 `superseded`。
4. **缓存失效**：`version` 不变但远端已删/不可达 → 保留最后可用缓存并标 `stale`；不静默用陈旧内容顶替确认过的版本。

执行与内容权威（原 §9.2 保留并强化）：

- **执行**：仍由 sixath Skills 运行时（`load_skill` / script 策略）负责，作用于**物化后的本地副本**。
- **内容权威**：绑定来自哪个 `AssetRef.Hub`，更新/卸载走哪个 Writer；本地 `skills_dirs` 文件权威仍是工作区/安装目录。
- **procedural 绑定的 `skill_id`**：先查 Loadout，再查 `skills_dirs`；都没有则门控失败（与现网「技能必须存在」一致）。

配置：

```yaml
memory_hub:
  skills:
    loadout_overrides_dirs: true             # 默认 true
    auto_inject_unbound_hub: false           # 默认 false：未绑定 Hub Skill 不进运行时
    require_manual_approve_unsigned_hub_skill: true  # 无签名外部 skill 落 draft，需人工确认
    rematerialize_on_version_change: true    # 版本/hash 变更重拉+重过闸
```

---

## 4. 本地实现（默认）

### 4.1 LocalGovernance

| 能力 | 本地落地 |
|------|----------|
| 资产来源 | `memory_units`（kind=fact/procedural）、本地 Skill 索引、可选「会话 ChatMemory」合成资产 |
| visibility | 新字段或映射：`scope=user`→偏 private；`session`→session/agent；`agent` 文件→agent；显式列 `visibility`（一期可先：owner + org 成员） |
| Loadout | 表 `agent_asset_bindings(agent_id, asset_kind, asset_id, priority)`；**无绑定默认**：仅本机 skills（可列表），**不**自动灌入全部可见 session/user units（units 召回仍走 MemoryStore Prefetch）。可选 `loadout_include_visible_units` 默认 **false**（P0 锁定，见实现计划）。 |
| CheckAccess | Owner **或** Org 成员（复用 Portal ACL）**或** Binding 命中；`private` 仅 Owner |
| ListAccessible | 按 Identity 过滤后的资产页，供 Portal 资产/配装 UI |
| Writer | 一期实现 Bind + SetVisibility + SetStatus（本地 DB） |

**AssetStatus ↔ `memory_units.status` 映射（解决两套状态机不一致）**：

| `AssetStatus`（Provider 面） | `memory_units.status`（DB） | 说明 |
|------------------------------|------------------------------|------|
| `draft` | `active` + `metadata.hub_status="draft"` | DB 无 draft 枚举；用 metadata 标记，**Loadout/Prefetch 过滤 `hub_status!=draft`** |
| `active` | `active`（无 hub_status 或 =active） | 正常可召回 |
| `stale` | `active` + `metadata.hub_status="stale"` | 召回降权，不硬改 DB 枚举 |
| `superseded` | `superseded` | 一一对应 |
| `archived` | `deleted` | 软删；`archived` 是 Provider 面对 DB `deleted` 的对外叫法 |

原则：不新增 DB 枚举值（避免迁移 `memory_units` 破坏既有 P2-D 语义），draft/stale 用 `metadata.hub_status` 承载；召回侧统一按「`status=active` 且 `hub_status ∈ {∅, active}`」过滤。

与 Portal Org ACL 关系：**能否调用该 Agent** 仍由 Org/Resource ACL 决定；**能否读某条记忆资产** 由 LocalGovernance 决定；运行时两层 AND。

### 4.2 LocalKnowledge

| 能力 | 本地落地（一期务实） |
|------|----------------------|
| `knowledge_search` | 合并：transcript FTS / workspace memorysearch / Neo4j（按 args.source）；**默认不含 units**（避免与 `memory_recall` 重复；显式 `source=units` 时可含） |
| `knowledge_read` | 按 hit id 读 unit / 消息锚点窗 / 工作区文件片段 |
| `graph_*`（可选） | 封装现有 Neo4j Expand / MatchSeeds |
| Wiki / CodeGraph | **一期可不做完整引擎**；有后端时才声明 `wiki` / `code_graph`。P3b：本地目录索引（`SATH_HUB_WIKI_ROOT` / `SATH_HUB_CODEGRAPH_ROOT`）；默认 search 仍不含二者 |

原则：**深知识靠工具调用**；Prefetch 仍以 MemoryStore 为主，Knowledge 不整库注入（故一期无 `PrefetchHints`，见 §3.3）。

### 4.3 与 MemoryStore 的分工

| | MemoryStore | LocalGovernance / LocalKnowledge |
|--|-------------|----------------------------------|
| 记住一条 fact | `Remember` / `AddFromTurn` | 可选：写入后登记/更新 Asset 元数据 |
| 每轮召回 | `StorePrefetchBackend` | Loadout 决定「召回范围」；或只提供资产元数据给 UI |
| 搜文档/图 | 不负责 | `Knowledge.Call` |
| 分享给另一 Agent | 无 | `BindAssets` + visibility |

---

## 5. 外部 Adapter（同一接口）

| Hub 能力 | Governance | Knowledge |
|----------|------------|-----------|
| 列出 Agent 可用资产 | 必须 | — |
| 读权限判定 | 必须（或 List 已过滤） | — |
| search + read | — | 必须（最低） |
| 分享 / 绑定 / 改可见性 | **必须**（外部可读可写；实现 Writer） | — |
| Wiki ingest / CodeGraph sync 等 | — | **应支持**（Capabilities.Write；经 Call） |
| MCP | — | 可选：映射到 `Call` |

Tencent 映射：Meta fixed-asset + ACL（含写）；KS `/v3/tools/list|call`（含变更类）。其它 Hub：OpenAPI/MCP Adapter 翻译到同一接口。

所有外部 Adapter 必须遵守 §3.5：写 fail-closed、`enforceHub` 一致性、Skill 物化门控。

---

## 6. 配置草图

**全局默认**（进程级 YAML）：

```yaml
memory_hub:
  enabled: true
  defaults:
    governance: local        # name 须在 providers 中注册（否则装配期硬失败，§3.5.1）
    knowledge: local
  fail_open_external_read: true

  providers:
    - name: local
      type: local
      governance: true
      knowledge: true
      knowledge_sources: [units, transcript, workspace, graph]

    - name: tencent
      type: tencent_agent_memory
      endpoint: http://127.0.0.1:8420
      knowledge_endpoint: http://127.0.0.1:8421
      api_key_env: TDAI_MEMORY_API_KEY
      governance: true
      knowledge: true

  skills:                    # §3.5.3
    loadout_overrides_dirs: true
    auto_inject_unbound_hub: false
    require_manual_approve_unsigned_hub_skill: true
    rematerialize_on_version_change: true
```

**Agent 级覆盖 —— 落 `RuntimeToolsConfig`（proto/DB），不另开 YAML 面**：

现有 `portal/api/agent/v1/agent.proto` 的 `RuntimeToolsConfig` 已承载 `hybrid_recall`（`optional bool`）等 Agent 级开关。本方案新增两个字段，走同一存储/编辑路径：

```proto
message RuntimeToolsConfig {
  bool memory_write_enabled = 1;
  // ... 现有字段 ...
  optional bool   hybrid_recall = 9;
  optional string hub_governance = 10;  // 空/unset = 用 defaults；非空 = 覆盖为该 provider name
  optional string hub_knowledge  = 11;  // 同上
  optional bool   hub_fallback_to_default_on_read_error = 12; // §3.5.1 阶段 B 读回落
}
```

- 未配 Agent 覆盖 → 全员走 `defaults`（通常 local）。  
- Agent 配了 `tencent` → 该 Agent 的 Loadout / 知识工具只走 tencent。  
- 保存时对 Catalog 做 `Resolve` 干跑校验 name（§3.5.1）。  
- `enabled: false` → 与今日一致（无资产 UI / 无动态 knowledge_*）。

理由：Agent 级开关统一在 `RuntimeToolsConfig`，避免 YAML 文件面与 proto/DB 面分裂（与既有 `hybrid_recall` / `memory_write_enabled` 一致）。

---

## 7. 切片

### P0 — 接口 + Local + Resolve + 边界

1. `hub` 类型（含 §3.1 合并后的 `AssetRef`）、Catalog、Defaults、`Resolve`  
2. **§3.5.1 失败分流** + **§3.5.2 `enforceHub` 守卫**（P0 即落，作为接口不变量）  
3. **LocalGovernance / LocalKnowledge**（含 §4.1 status 映射）  
4. Prefetch / 工具按 **Resolve 结果** 取 Provider  
5. 单测：默认 / 只覆治理 / 只覆知识 / 全覆；name 未注册硬失败；`enforceHub` 拒绝 Hub 不符  

### P1 — Portal 读写 UI + Agent 配置项

1. 资产列表 / 配装（打到 Resolve 的治理面）  
2. `RuntimeToolsConfig` 新增 `hub_governance`/`hub_knowledge`/`hub_fallback_*`；Agent 编辑页下拉 = Catalog；保存即校验  
3. `knowledge_*` 按 Resolve 的知识面注册  

### P2 — 外部 Adapter

1. Tencent 等注册进 Catalog；可被设为 default 或某 Agent 覆盖  
2. 读 fail-open / 写 fail-closed；`enforceHub` 路由  
3. **§3.5.3 Skill 物化门控（前置条件，未完成不上线）**  

### P3 — 本地知识增强

1. 可选 Wiki/CodeGraph；禁止隐式双向 sync  

---

## 8. 风险与约束

| 风险 | 缓解 |
|------|------|
| local 与 external 资产 ID 冲突 | 同 Agent 同面只绑一个 Provider；`enforceHub`（§3.5.2）；Catalog 内 name 唯一 |
| Agent 覆盖指到未注册 name | 保存配置时 `Resolve` 干跑校验；运行硬失败不静默（§3.5.1 阶段 A） |
| 外部 Hub 运行时宕机被误当配置错误 | §3.5.1 明确区分：装配期 name 缺失 vs 运行时不可达；后者按读/写分流 |
| 改用覆盖后旧 Loadout 失效 | UI 提示；允许一键清除 Binding 或迁移工具（二期） |
| 本地 visibility 与 Org ACL 重复 | 文档化两层 AND；私有资产不绕过 Owner |
| draft/stale 状态与 DB 枚举不一致 | §4.1 metadata 映射，不新增 DB 枚举，不破坏 P2-D supersede |
| 「本地 Wiki」范围膨胀 | 一期明确不做完整 Wiki；Capabilities 诚实降级 |
| 厂商 API 泄漏 | Adapter 独立包；framework 核心只依赖 `hub` 接口 |
| 外部写失败被静默吞掉 | **写 fail-closed**；与 Prefetch 读 fail-open 分离（§3.5.1） |
| 跨 Hub 误写 | `enforceHub` invariant（§3.5.2）；UI 前置校验仅改善体验、非边界 |
| 外部 Skill = 远程代码执行 | §3.5.3 物化+信任校验+变更重过闸；未签名默认 draft 需人工确认 |
| CheckAccess 读不可达误放行 | §3.5.1：CheckAccess 不 fail-open，不可达且无回落按拒绝 |

---

## 9. Skill 优先级（Hub vs 本地 `skills_dirs`）

**已确认**运行时解析顺序：

| 优先级 | 来源 | 规则 |
|--------|------|------|
| 1（最高） | **Loadout 显式绑定** | 无论资产来自 `local` 还是外部 Hub，只要 Bind 到当前 Agent 且 `status=active`，即作为该 Agent 的 Skill 来源。外部来源须先经 §3.5.3 物化 |
| 2 | **本地 `skills_dirs`** | 未在 Loadout 中声明同名/同 id 时，继续走现有 framework Skills 扫描（`load_skill` / `skills_list`） |
| — | **Hub 未绑定 Skill** | **不**自动进入运行时工具面；只出现在资产库 / `ListAccessible` |

### 9.1 同名冲突

- Loadout 已绑定名称 `N` → **忽略** `skills_dirs` 中同名 Skill（绑定胜出）。  
- Loadout 未绑定、仅 `skills_dirs` 有 `N` → 用本地。  
- 同一 Agent 的 Loadout 内禁止重复技能逻辑名。  
- Loadout 一律来自 **`Resolve(agent).Governance`**（Agent 覆盖优先于默认）。

### 9.2 执行与内容权威

见 §3.5.3（物化流程已把原「执行/内容权威」条目吸收并强化）。

---

## 10. 知识正确性（如何保证「沉淀的是对的」）

**立场**：无法用单次 LLM 抽取保证正确；正确性是 **多层门控 + 可纠错 + 可降权** 的工程属性，不是一次写入的真理标签。

### 10.1 分层策略

| 层 | 机制 | sixath / Hub 落点 |
|----|------|-------------------|
| **写入前过滤** | 宁缺毋滥：空/过长/低置信丢弃；turn extract max_facts；hash 去重 | `AddFromTurn` drops；L1 priority 阈值 |
| **冲突裁决** | 与已有知识比对：Ignore / Supersede / KeepBoth；LLM 失败 **不写** | D2 `LLMSemanticConflictResolver`（fail-closed） |
| **门控写入** | 高风险类型不得裸写 | `procedural` 五门闸（`CommitProceduralRepair`）；Wiki `locked` 页不可瞎覆盖 |
| **来源与溯源** | 每条资产带 `SourceRef` / turn / unit_id，可回点原文 | `AssetRef.SourceRef`；Knowledge 页 frontmatter；TurnTrace |
| **人审 / 分享门** | 默认私有；进团队或配装前需显式动作 | Governance visibility；Hub 审核流；本地 Bind 即「采用」 |
| **状态机** | draft → active → stale / superseded / archived | `AssetStatus`（§4.1 映射）；Curator 式过期（可选） |
| **使用反馈** | 命中后是否被采用、是否被用户纠正 | usage_count；可选 thumbs / 「遗忘」工具 |
| **召回侧降权** | 未验证、低置信、过旧的少注入或不注入 | Prefetch 过滤 `status=active` 且 `hub_status ∈ {∅,active}`；MinScore / 配额 |

### 10.2 按知识类型的「正确」含义不同

| 类型 | 「对」的判据 | 推荐保证 |
|------|-------------|---------|
| fact units | 与用户陈述一致、不互相矛盾 | extract 保守 + D2 冲突 + 可 supersede |
| procedural | 绑定触发码/技能合法、有失败证据 | 已有五门闸；禁止裸 Remember |
| Skill | 可执行、边界清晰、不把一次性噪音当技能 | 物化后 draft（§3.5.3）；Bind/分享前人工或复盘 Agent |
| Wiki | 与源文档一致、locked 不被误并 | ingest 后 draft；merge 策略；人工 publish |
| CodeGraph | 索引与仓库 revision 一致 | sync 校验 commit；status=ready 才可召回 |

### 10.3 状态与置信度约定

- `AssetRef.Status` 定义与 DB 映射见 §3.1 / §4.1。
- **自动沉淀默认 `draft` 或低置信 `active`**（推荐敏感类型 draft）。  
- **进 Loadout / Prefetch 的默认仅 `active`（且非 draft/stale）**。  
- `AssetRef.Confidence`：**一期本地 extract 恒 0.5，且不参与任何过滤/排序**（占位；避免用无信息量字段诱导召回逻辑）。二期接入真实置信来源后再启用。
- **外部 Hub 写**：创建/更新同样带 status；本地不把未确认外部草稿当真相。

### 10.4 不保证什么

- 不保证抽取内容事实永真（世界会变 → 靠 supersede / 过期）。  
- 不保证 Wiki/Code 与源站实时一致（靠 sync + ready）。  
- 不在 Prefetch fail-open 路径上「猜对」——读可降级为空，写不可静默成功。

### 10.5 与现网的衔接

一期优先 **复用** 已有 D1/D2、procedural 五门闸、hash 去重，在 LocalGovernance 上补 **status + Bind=采用**；完整人工审核台与使用反馈打点放在 P1/P2 UI。

---

## 11. 验收

1. **仅默认 local**：行为符合 §4；Agent 未配覆盖。  
2. **Agent 覆盖 tencent**：该 Agent Loadout/知识走 tencent；其它 Agent 仍默认。  
3. **只覆一面**：另一面仍默认。  
4. **关掉 memory_hub**：与现网一致。  
5. **§3.5.1**：覆盖到未注册 name → 保存/装配硬失败；已注册但 RPC 不可达 → 读 fail-open（或按配置回落默认），写 fail-closed；CheckAccess 不可达不放行。  
6. **§3.5.2**：`BindAssets` 传入 `Hub` 与 Resolve 的 provider 不符 → `ErrHubMismatch`，不落库/不发远程。  
7. **§3.5.3**：外部 skill 未物化不得执行；未签名外部 skill 落 draft 需人工确认；`version`/`hash` 变更触发重拉 + **SkillTrustGate**（非 `CommitProceduralRepair`）。  
8. 接口单测不依赖真实远程 Hub。  

---

## 12. 已确认 / 开放

| 项 | 状态 |
|----|------|
| 治理与知识面本地实现一份 | **已确认**（可作为全局默认） |
| 全局默认各一个；Agent 可覆盖；自己的优先 | **已确认**（`Resolve = agent ?? default`；两面独立；同面不合并多 Provider） |
| 外部资产可读可写 | **已确认**（写 fail-closed；`enforceHub` 路由；不双向 sync） |
| Hub Skill vs 本地 `skills_dirs` | **已确认**（§9；Loadout 来自 Resolve 后的治理面） |
| 三个阻塞项（失败分流 / Hub 一致性 / Skill 物化） | **已在 §3.5 定案** |
| Agent 覆盖落 `RuntimeToolsConfig` 而非 YAML | **已确认**（§6；框架/后端 2026-08-07 对齐） |
| framework 核心只依赖 `hub` 接口，厂商 SDK 留 Adapter | **已确认**（框架/后端 2026-08-07 对齐） |
| 状态机不动 DB 枚举，draft/stale 走 `metadata.hub_status` | **已确认**（§4.1；框架/后端 2026-08-07 对齐） |
| `PrefetchHints` 一期不进接口；`Confidence` 一期不参与过滤 | **已确认**（§3.3 / §10.3） |
| ~~local + 外部合并 Loadout~~ | **由 Agent 覆盖模型取代** |
