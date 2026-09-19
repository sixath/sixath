# es_log_query 索引/字段落地 Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [x]`) syntax for tracking.

**Goal:** `es_log_query` 把「索引不存在 / 字段不在 mapping」从假 empty 改成可修复的 error 或自动改写，且不把任何业务索引名、正文列名写进框架。

**Architecture:** 在现有 `cluster` 路由上增加：物理索引探测（`_cat/indices` + 已有日期分组）与未知 Lucene 字段一次改写（相似字段 → 配置/推断 `body_field` → 无前缀 query_string）。发现结果按 cluster 短 TTL 缓存；Portal 只增加可选 `body_field`。

**Tech Stack:** Go（`framework/tool`、`framework/metadata`）、Portal proto/装配、现有 fake Reader/Mapper 单测。

**Spec:** [docs/superpowers/specs/2026-09-18-es-log-query-grounding-design.md](../specs/2026-09-18-es-log-query-grounding-design.md)

**不要写死：** `backend-sched-hub-*`、`M` 作为唯一正文列、`vm_id`。`M` 只允许出现在跨产品候选名单的一项。

提交：仅在用户明确要求时 commit。

---

## File map

| File | 职责 |
|------|------|
| `framework/metadata/elasticsearch.go` | 导出 `GroupIndicesByPattern`（或返回 pattern 名的薄封装） |
| `framework/metadata/elasticsearch_test.go`（若无则创建/补测） | 分组回归：日期后缀、单索引、系统索引不在此函数输入里 |
| `framework/tool/es_log_index.go`（新建） | cat 缓存、物理索引计数、pattern 建议 |
| `framework/tool/es_log_index_test.go`（新建） | 未落地 / 建议排序 / `*` 视为落地 |
| `framework/tool/es_log_mapping.go` | Lucene 未知字段改写；`unknownQueryFields` 与未落地分支的边界保持 |
| `framework/tool/es_log_tool.go` | 接入探测与改写；描述文案；`ESLogCluster.BodyField` |
| `framework/tool/es_log_tool_test.go` | §9 合同 |
| `framework/config/config.go` | `RCAESConfig.BodyField` |
| `portal/api/tool/v1/tool.proto` + 生成物 | `DatasourceConfig.body_field`、RCA 可选 `body_field` |
| `portal/internal/service/tool.go` | 编解码 |
| `portal/internal/chat/es_log_clusters.go` | fillEmpty `BodyField` |
| `web` 数据源表单 / `client.ts` / export format | 可选正文列 |
| `portal/docs/rca-es-log-query.md` | 运营说明 |

---

### Task 1: 导出索引分组

**Files:**
- Modify: `framework/metadata/elasticsearch.go`
- Test: `framework/metadata` 现有测试或 `elasticsearch_test.go`

- [x] **Step 1: 写失败测试** — 对 `[]string{"app-2026.01.02", "app-2026.01.03", "lonely"}` 调用导出函数，期望 patterns 含 `app-*` 与 `lonely`。

- [x] **Step 2: 导出** 现有 `groupIndicesByPattern`（可改名为导出符号，内部仍复用）。不要在此拉 mapping。

- [x] **Step 3:** `cd framework && go test ./metadata/ -count=1`

---

### Task 2: 物理索引探测与建议（纯函数 + fake cat）

**Files:**
- Create: `framework/tool/es_log_index.go`
- Create: `framework/tool/es_log_index_test.go`

需要注入的能力（接口，避免 tool → 真 HTTP）：

```go
type ESIndexCatalog interface {
    ListIndexNames(ctx context.Context, clusterID, indexPattern string) ([]string, error)
}
```

`ListIndexNames` 对 `*` / `_all` / 空 pattern 的语义在测试里钉死：空 pattern 表示「列全量非系统索引」（供建议目录）；具体 pattern 表示 cat 过滤。

- [x] **Step 1: 失败测试**

```go
func TestIndexUnresolvedWhenCatEmpty(t *testing.T) {
    // ResolveIndex("cgschedule-*", cat=empty) → unresolved
    // suggestions include DefaultIndex "app-logs-*" and catalog pattern "svc-logs-*"
    // suggestions must not require a "backend-" prefix
}

func TestIndexResolvedStar(t *testing.T) {
    // index="*" → resolved even if we never cat
}

func TestSuggestPrefersDefaultIndex(t *testing.T) {
    // request "cgschedule-*", catalog has "app-logs-*","other-*", default "app-logs-*"
    // first suggestion is default
}
```

- [x] **Step 2: 实现** `indexPhysicalHits`、`suggestIndexPatterns`（default 置顶；token/编辑距离；上限 15）。TTL 缓存可先放结构体，Task 3 再接到 mapper/reader。

- [x] **Step 3:** `go test ./tool/ -count=1 -run 'TestIndex|TestSuggest'`

---

### Task 3: 把探测接到 `es_log_query` 空击路径

**Files:**
- Modify: `framework/tool/es_log_tool.go`
- Modify: `framework/tool/es_log_tool_test.go`
- 可能：扩展 fakeReader，使测试能返回 0 hits 并提供 cat。

合同：

- cat 空 + 查询 0 击 → 不要 `hit_status=empty`。
- 若希望少打一次 Search：索引未落地可在 Search **之前** 判定（推荐，测「未调用 Search」）。

- [x] **Step 1: 失败测试** `TestESLogQuery_UnmatchedIndexIsErrorNotEmpty`  
  cluster default_index=`app-logs-*`；params.index=`cgschedule-*`；fake cat 对该 pattern 返回空、对全量返回 `app-logs-2026.01.02`。  
  期望：`ok==false`，`hit_status==error`，payload `index_error=="unresolved"`，`suggested_index_patterns` 含 `app-logs-*`。

- [x] **Step 2: 失败测试** `TestESLogQuery_MissingIndexListsPatterns`  
  default_index 空、无 index 参数、fake cat 全量有 `svc-*`。期望仍失败（现有「index is required」），且建议/目录非空。

- [x] **Step 3: 实现接入**；catalog 空时 `unknownQueryFields` 不得把未落地伪装成 empty（未落地已在字段检查前 return）。

- [x] **Step 4:** `go test ./tool/ -count=1 -run ESLogQuery`

---

### Task 4: Lucene 未知字段改写

**Files:**
- Modify: `framework/tool/es_log_mapping.go`（改写 DSL）
- Modify: `framework/tool/es_log_mapping_test.go`
- Modify: `framework/tool/es_log_tool.go`（0 击且索引已落地时 retry）
- Modify: `framework/tool/es_log_tool_test.go`

- [x] **Step 1: 纯函数测试** `rewriteUnknownLuceneFields`

  - `vmid:199306` + similar `vm_id` → query 字段变为 `vm_id`
  - `foo:bar` + mapping `{message}` + 空 BodyField → `query_string` `default_field=message` query=`bar`（或保留 bar 原值）
  - `foo:bar` + mapping `{M,L}` → `default_field=M`
  - `foo:bar` + mapping `{L}` 无正文候选 → unfielded `query_string` query 含原 value
  - 已映射字段不改

- [x] **Step 2: 工具级测试** fake 第一次 0 击、mapping 无 `vmid` 有 `vm_id`、第二次 Search 返回一行 → `query_rewritten==true`，`rewrite_reason==similar_field`，`hit_status==hits`。

- [x] **Step 3: 工具级测试** 字段合法、0 击 → 仍 empty，无 `index_error`。

- [x] **Step 4: 实现**；与现有 `rewriteEmptyHitQuery` 合并为**一次** extra Search（未知字段改写优先，再类型改写）。

- [x] **Step 5:** `go test ./tool/ -count=1`

正文候选名单（仅此项允许出现 `M`）：

```go
var esBodyFieldCandidates = []string{"message", "msg", "log", "body", "@message", "M"}
```

---

### Task 5: `BodyField` 贯通

**Files:**
- Modify: `framework/tool/es_log_tool.go` `ESLogCluster`
- Modify: `framework/config/config.go` `RCAESConfig`
- Modify: `portal/internal/chat/es_log_clusters.go` fillEmpty
- Modify: `portal/internal/chat/es_log_clusters_test.go`
- Modify: `portal/api/tool/v1/tool.proto`（`DatasourceConfig.body_field = 13`，`RCAConfig.body_field` 新号）
- Modify: `portal/internal/service/tool.go` + web `DatasourceConfig` / 表单 / `toolExportFormat.ts`
- 生成 pb（沿用仓库现有 protoc 命令）

- [x] **Step 1:** Portal 单测：数据源 config 带 `body_field: "message"` → cluster.BodyField == `message`；缺省为空。

- [x] **Step 2:** 工具单测：BodyField=`text` 且 mapping 含 `text` → 未知字段走 `body_field` 而不是名单里的 `message`。

- [x] **Step 3:** 实现装配与表单；`body_field` 选填，不改 `ValidateElasticsearchDatasource` 的必填集合。

- [x] **Step 4:** `go test ./internal/chat/ ./internal/service/ ./internal/biz/ -count=1`（在 `portal/`）以及 `framework` `./tool/`。

---

### Task 6: 描述与文档

**Files:**
- Modify: `framework/tool/es_log_tool.go` 工具 Description
- Test: `framework/tool/es_log_tool_test.go` 已有 `TestESLogQueryDescriptionMentionsRunResultScript` 旁增加：描述含 “do not invent” / 真实 pattern、**不含**未加限定的 `operation:DiscardUserArchive` 作为唯一示例。
- Modify: `portal/docs/rca-es-log-query.md`
- Modify: `framework/skills_examples/skills/rca-investigation/SKILL.md` 仅补一句：`empty` 与 `index_error=unresolved` 的区别（不要写业务索引名）。

- [x] **Step 1:** 描述断言测试先红。
- [x] **Step 2:** 改描述与运营文档（填 `default_index`；可选 `body_field`；如何读回包）。
- [x] **Step 3:** `go test ./tool/ -count=1 -run Description`

---

### Task 7: 回归

- [x] `cd framework && go test ./tool/ ./metadata/ ./config/ -count=1`
- [x] `cd portal && go test ./internal/chat/ ./internal/service/ ./internal/biz/ -count=1`
- [x] 现网手验见 spec §10（Portal 重启后）。过渡 RCA `zj-elk` 若仍无 `default_index`，未传 index 应能列出真实 pattern，而不是只报 `index is required`。

---

## 明确不做

- 技能里的 `backend-sched-hub-*` 表（可后续单独改技能，不是本期框架合同）
- ES `list_tables` 全量元数据方案
- 默认 `index=*`
- 自动选 cluster
