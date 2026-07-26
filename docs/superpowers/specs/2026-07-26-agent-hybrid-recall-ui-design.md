# Agent 面板 Hybrid Recall UI（切片 C）

> 状态：已定稿  
> 日期：2026-07-26  
> 回链：[P2-E2 Hybrid Recall](./2026-07-26-memory-store-hybrid-recall-design.md)、[P2-E2.1 Vector Backfill](./2026-07-26-memory-store-vector-backfill-design.md)、[portal `memory-integration.md`](../../../portal/docs/memory-integration.md)  
> 前置：Portal API 已交付 `runtime_tools.hybrid_recall`（`optional bool`）；gate unset=开、`false`=仅 LIKE  
> 切片：**C only** — web Agent 编辑/详情三态 UI + 序列化 + 单测/e2e；顺带将 `feat/p2e-vector-sidecar`（framework / portal / monorepo docs）Push + PR。不做 Qdrant、可配置 RRF/超时、backfill keyset

---

## 0. 目标与非目标

### 目标

1. 在 Agent 编辑页提供**三态**「混合召回」控件，完整表达 proto `optional bool`：跟随默认（omit）/ 开（`true`）/ 关（`false`）。  
2. 详情页按三态展示，避免把 unset 当成关闭。  
3. 序列化与现有 8 个 opt-in 工具开关隔离：`hybrid_recall` **不得**走 `RUNTIME_TOOL_FIELDS` 的全量 `!!` 压平。  
4. 收尾：`framework` / `portal` / monorepo docs 的 `feat/p2e-vector-sidecar` 各自 Push + PR（不与 web 同分支）。

### 非目标

| 项 | 说明 |
|----|------|
| 改后端 gate / Update omit 语义 | 已交付，本切片只消费 |
| Qdrant / RRF k / Embed 超时可配置 | E3 / E2 §7 |
| backfill UI | 运维 CLI / 启动 job 已够 |
| 本地合并进 main | 统一走 PR |

---

## 1. 仓库与分支

| 仓 | 分支 | 动作 |
|----|------|------|
| `web` | `feat/hybrid-recall-ui`（自 `main`） | 实现 UI + 测试 |
| `framework` | `feat/p2e-vector-sidecar` | Push + PR（E2 + E2.1 已在分支上） |
| `portal` | `feat/p2e-vector-sidecar` | Push + PR；**不提交**未跟踪的 `internal/service/data/` |
| monorepo docs | `feat/p2e-vector-sidecar` | Push + PR（含本规格） |

---

## 2. UI 与数据映射

### 2.1 AgentForm

在「运行时工具（Hermes P0）」面板**下方**增加独立一行（不进入 checkbox 列表）：

```html
<select data-testid="hybrid-recall-mode">
  <option value="default">跟随默认（开）</option>
  <option value="on">开</option>
  <option value="off">关（仅 LIKE）</option>
</select>
```

短说明：关闭后该 Agent 读路径不做向量混合召回；写向量索引不受此开关影响；「跟随默认」与后端 unset=开 一致。

新建 Agent：初始 `default`。编辑加载：`undefined`→`default`，`true`→`on`，`false`→`off`。

### 2.2 AgentDetail

单独一行（`data-testid="hybrid-recall-status"`），文案映射：

| 值 | 展示 |
|----|------|
| `undefined`（字段缺失） | 混合召回：跟随默认（开） |
| `true` | 混合召回：开 |
| `false` | 混合召回：关（仅 LIKE） |

**禁止**把 `hybrid_recall` 放进 `enabledRuntimeTools = RUNTIME_TOOL_FIELDS.filter(…?.[key])`——unset 会被当成未启用。

### 2.3 client.ts

```ts
// RuntimeToolsConfig 增加
hybrid_recall?: boolean

// normalize：用显式布尔检测，保留 true | false | undefined（snake + camel）
// 禁止 `cfg.hybrid_recall ?? cfg.hybridRecall` 单独依赖 ??（false 安全，但缺 key 与显式需分清）

// serializeRuntimeTools：
// 1) 对 RUNTIME_TOOL_FIELDS 仍全量 !!（防 PUT 丢 browser_enabled）
// 2) hybrid_recall 单独：
//    undefined → 不写入 key
//    true / false → 原样写入
```

**禁止**将 `hybrid_recall` 加入 `RUNTIME_TOOL_FIELDS`（语义是默认开 / 显式关，与其余 opt-in 相反）。

编码助手预设**不**设置 `hybrid_recall`（保持 omit）。

**重映射落点（§4.1）：**「跟随默认 → 有显式历史时发 `true`」发生在 **AgentForm 提交路径**（组装 `runtime_tools` / 调用 `serializeRuntimeTools` **之前**），不塞进 `serializeRuntimeTools` 内部——后者只负责「已有 `undefined|true|false` → wire JSON」。

---

## 3. 测试

### 单测 `web/tests/runtimeToolsSerialize.test.ts`

- omit → 序列化对象无 `hybrid_recall` key  
- `false` → `hybrid_recall: false`  
- `true` → `hybrid_recall: true`  
- 其余 8 字段仍显式布尔（回归）  
- normalize 接受 `hybridRecall` camelCase，且 `false` 不被当成缺失  

### e2e `e2e/agent-runtime-tools.spec.ts` + `helpers/mock-api.ts`

按 Create / Update 分场景（对齐 §4.1，禁止笼统写「跟随默认 → 永不带字段」）：

| 场景 | 期望 body |
|------|-----------|
| **Create** + 选「跟随默认」 | **不含** `hybrid_recall` |
| **Create** + 选「关」 | `hybrid_recall: false` |
| **Update**，加载时无显式值 + 选「跟随默认」 | **不含** `hybrid_recall` |
| **Update**，加载时已有显式 `false` + 改选「跟随默认」 | `hybrid_recall: true`（不能 omit，否则 portal 保留库中 `false`） |
| **Update** + 选「关」 | `hybrid_recall: false` |

详情页：unset 显示「跟随默认」，不显示为关。

### 命令

```bash
cd web && npm test && npx tsc --noEmit && npm run test:e2e
```

---

## 4. 错误处理与边界

| 场景 | 行为 |
|------|------|
| 旧 Agent 无该字段 | 表单显示「跟随默认」；保存若不改选则继续 omit |
| 用户从 `false` 改回「跟随默认」 | 发 `hybrid_recall: true`（见 §4.1；omit 无法清 unset） |

### 4.1 Update omit 与「恢复默认」

Portal 已实现：Update 请求**省略** `hybrid_recall` 时**保留库中 presence**（避免稀疏 PUT 抹掉显式值）。

因此 UI「跟随默认 → omit」在**编辑已显式设过 true/false 的 Agent** 时，**无法**仅靠 omit 清回 unset。

**本切片决议（最小可用）：**

- 「跟随默认」在**新建**时 = omit（正确 unset）。  
- 「跟随默认」在**编辑**且库中已有显式值时：表单仍提供该选项，但保存时发 **`hybrid_recall: true`**（语义上等同默认=开），并在 hint 中注明：「已显式设置过时，选跟随默认将保存为开」。  
- **不做**「清除字段回 unset」的后端 API（避免本切片扩 scope）。若日后需要真正 unset，另开切片（如 `null` clear 或专用 clear 字段）。

映射表（提交）：

| UI | Create | Update（库中无/有显式值） |
|----|--------|---------------------------|
| default | omit | omit（无显式）/ `true`（有显式，见上） |
| on | `true` | `true` |
| off | `false` | `false` |

检测「库中有显式值」：加载时 `typeof hybrid_recall === 'boolean'`。

---

## 5. 文档回写（实现完成后）

- `portal/docs/memory-integration.md`：Backlog 去掉「前端混合召回 UI」；补三态 select 说明。  
- E2 §7：前端开关 → 已交付，链到本规格。  

---

## 6. 验收清单

**Web（本切片主交付）**

1. 新建 Agent 默认「跟随默认」，创建请求无 `hybrid_recall`。  
2. 编辑改为「关」，详情显示关；有显式历史时再选「跟随默认」→ body 含 `true`（非 omit）。  
3. 单测 + e2e 绿。  

**Sidecar 收尾（可与 web 并行，独立验收闸）**

4. `framework` / `portal` / monorepo docs 的 `feat/p2e-vector-sidecar` 各自 Push + PR 已开（portal 不含 `internal/service/data/`）。  

---

## 7. 风险

| 风险 | 缓解 |
|------|------|
| omit 无法清 unset | §4.1：有显式历史时 default→`true` + hint |
| 与 opt-in checkbox 混淆 | 独立 select + 文案；不进 `RUNTIME_TOOL_FIELDS` |
| portal PR 未合导致 web 联调假绿 | e2e 用 mock；live 依赖 portal 已含字段 |
