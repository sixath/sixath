# `es_log_query` 索引/字段落地（普适封装）

> 状态：已实现  
> 日期：2026-09-18  
> 关联：[2026-09-02-multi-es-cluster-route-design.md](./2026-09-02-multi-es-cluster-route-design.md)、[framework/docs/elasticsearch-metadata-redesign.md](../../../framework/docs/elasticsearch-metadata-redesign.md)、[portal/docs/rca-es-log-query.md](../../../portal/docs/rca-es-log-query.md)  
> 触发：会话 `a940dc23` 对 vmid=199306 使用不存在的 `cgschedule-*` / `cginstance-*` 与未映射字段 `vmid:`，工具返回 `ok + empty`，模型把「没日志」当成现场证据。

## 0. 决策摘要

| 项 | 选择 |
|----|------|
| 问题分类 | 封装把 ES Search 原样交给模型；错索引/错字段与「真没日志」同形 |
| 原则 | 工具保证**打到真实索引、只用 mapping 里的字段**；索引长什么样、正文列叫什么，由**本 cluster 配置 + 运行时发现**，禁止写进框架常量 |
| 索引 | 通配符匹配 0 个物理索引 → `hit_status=error`，附本集群已发现的相近 pattern；不默认 `*` / `_all` |
| 字段 | Lucene `field:value` 不在 mapping → 自动改写一次（相似字段 → 配置/推断的正文列 → 无字段前缀），并标明 `query_rewritten` |
| 目录 | `_cat/indices` 按日期后缀分组（复用 `metadata.groupIndicesByPattern`）；缓存；只在缺省索引错误或 0 击时拉，不塞进每次工具描述 |
| 配置 | 集群已有 `default_index` / `purpose` / `trace_id_field`；新增可选 `body_field`。不配则从 mapping 与小名单求交 |
| 非目标 | 不把 `backend-sched-hub-*`、`M`、`vm_id` 写进框架；不改技能业务表；不做完整 ES `list_tables` 重构；不按索引猜 `cluster` |

一句话：模型只提交「搜哪套集群、搜什么值」；**索引是否存在、字段是否可查，是工具的合同，不是模型的作业。**

## 1. 背景

现网 `es_log_query`：

- `cluster` 有 enum；`index` 与 `query` 是任意字符串。
- `zj-elk` 一类过渡 RCA 工具 `default_index` 为空 → 不传 `index` 直接失败，模型按服务名发明 pattern。
- ES 对匹配不到任何索引的通配符默认 `allow_no_indices` → **HTTP 200 + 0 hits**。
- `unknownQueryFields` 在 mapping 目录为空时直接 `return nil`，空索引上连 `unknown_fields` 都不给。
- 空击改写只处理 `term`/`match`，不处理 Lucene `vmid:199306`。
- 工具描述示例是 `operation:DiscardUserArchive`，等于教模型写未校验的 `field:value`。

对照同一集群：`cgschedule-*` 物理索引 0 个；`backend-sched-hub-*` 有数亿条；`vmid:199306` 在正确索引上也是 0（无此字段），`vm_id` / 正文列 `M` 则有命中。这不是「没日志」，是封装没有把失败分类。

上述失败模式与索引前缀、业务名无关：任何滚动日志集群都会出现「模型猜错 pattern / 猜错字段」。

## 2. 目标与非目标

### 目标（一期）

1. **索引未落地不得冒充 empty**：请求的 index/pattern 在本 cluster 上匹配 0 个非系统物理索引时，返回 `hit_status=error`（`ok: false`），并给出 `suggested_index_patterns`。禁止再返回 `ok: true, hit_status: empty`。
2. **字段未落地不得冒充 empty**：Lucene / DSL 用到的字段不在该 index 的 mapping 中时：先尝试一次自动改写；改写后仍 0 击才是 `empty`。未改写成功则 `error`，并带 `unknown_fields` / `similar_fields`。文案保持现有语义：*0 hits are not evidence of missing logs*。
3. **改写规则对所有集群同一套代码路径**：顺序见 §5。任何业务字段名、任何 `backend-` 前缀都不得出现在 `framework/tool` 常量里。
4. **目录来自运行时发现**：pattern 列表 = `_cat/indices` 去掉 `.` 系统索引后，按已有日期后缀规则分组。建议列表按与用户请求的相似度排序，而不是按固定前缀过滤。
5. **缺 `index` 且 `default_index` 为空**：保持失败，但错误里附上本集群 pattern 目录（截断），让模型从真实名字里选，而不是发明。

### 非目标（一期不做）

- 把调度技能里的 `backend-sched-hub-*` 等业务索引写进框架或工具 schema enum。
- 把正文列写死为 `M`。
- 完整实施 [elasticsearch-metadata-redesign](../../../framework/docs/elasticsearch-metadata-redesign.md) 的 `list_tables` / 每模式 mapping 预热（本期只复用**分组函数**与 `_cat`）。
- 按索引 pattern 自动选择 `cluster`（多集群路由合同不变）。
- 自动加时间窗、禁止 `index=*`（模型仍可显式传 `*`；工具不把它当默认）。
- 界面中文 vs 英文 RPC（技能职责）。
- 强制迁移现网 RCA 内联 ELK；但文档要求运营把 `default_index` 填上。

### 诚实上限

- `_cat/indices` 在超大集群仍有成本：只在「缺省索引」或「0 击 / 疑似未落地」时拉，并按 cluster 短 TTL 缓存。
- 相似 pattern 是启发式（token 重叠 + 编辑距离）；`cgschedule-*` 不一定排到 `backend-sched-hub-*` 第一名，但必须能出现在「无相似则给目录切片」里，且 `default_index` 若已配必须出现在建议里。
- 无字段前缀的回退可能召回噪音（正文里碰巧出现该数字）。这比「假 empty」可接受；结果里必须标明 rewrite 类型。
- mapping 拉失败（权限/超时）时不得假装字段都合法；走 `error` + transient/permanent 既有分类。

## 3. 架构

```text
es_log_query(cluster, index?, query|trace_id)
        │
        ├─ 解析目标 index = 入参 index 或 cluster.DefaultIndex
        │     二者皆空 → error + 缓存的 index_patterns（若可发现）
        │
        ├─ Search
        │
        ├─ 0 击或 mapping 目录为空
        │     ├─ 物理索引数 = 0 → error(index_unresolved) + suggested_index_patterns
        │     ├─ Lucene/DSL 字段 ∉ mapping
        │     │     → 改写一次（§5）→ 再 Search
        │     │           命中 → hits + query_rewritten
        │     │           仍 0 → empty（真没日志）+ 改写说明
        │     └─ 无未知字段 → empty（真没日志）
        │
        └─ 命中 → hits（现有 compact / spill 不变）
```

连接、`cluster` 路由、翻页 `(cluster, index)`、ES 不进 data 三件套：全部继承 [2026-09-02](./2026-09-02-multi-es-cluster-route-design.md)。

## 4. 索引落地

### 4.1 何谓「匹配到物理索引」

对目标 `index`（可为 `foo-*`、单名、逗号分隔）：

1. 若等于 `*` / `_all`：视为已落地（模型显式选择；不在本期拦截）。
2. 否则对每个 token 调 `_cat/indices/{pattern}?h=index`（或一次 cat 全量后在内存过滤，见缓存）。
3. 过滤 `.` 前缀与非法名（与 `metadata.esIndexNameAllowed` 一致）。
4. 剩余条数为 0 → **未落地**。

不得把「Search 返回 0 hits」单独当成未落地（那可能是真没日志）。

### 4.2 发现与分组

- 复用 `framework/metadata` 已有的日期后缀分组（`base-2026.01.02` → `base-*`）。**导出**分组函数（例如 `metadata.GroupIndicesByPattern`），`es_log_query` 只拿 pattern 名列表，不在本期对每个模式拉 mapping。
- 系统索引（`.` 前缀）丢弃。
- 无法归组的索引：自身即 pattern。

### 4.3 建议列表 `suggested_index_patterns`

输入：用户请求的 index 字符串、本集群 pattern 目录、`cluster.DefaultIndex`。

排序：

1. `default_index` 非空则置顶（若它本身能在目录中或作为合法 pattern）。
2. 请求串去掉 `-*` / 日期后缀后的 base，与 pattern base 做：包含关系、`-`/`_` 分词 Jaccard、已有编辑距离。
3. 截断上限 15。若相似分为 0，退回目录字母序前 15 + 始终带上 `default_index`。

**禁止**按 `backend-` 或任何业务前缀过滤。

### 4.4 缓存

按 `cluster` ID 缓存 `{patterns, fetched_at}`，TTL 建议 5 分钟。发现失败不缓存空成功；下次调用可重试。注册工具时**不要**同步 `_cat`（避免 BuildRegistry 打挂）。

### 4.5 回包

| 情况 | `ok` | `hit_status` | 额外字段 |
|------|------|----------------|----------|
| 0 个物理索引 | false | error | `error` 人类可读；`index_error=unresolved`；`suggested_index_patterns`；`queried_index` |
| `default_index` 与 `index` 都空 | false | error | 同上（可无 `queried_index`）；尽量带目录 |
| 已落地且 0 击（字段合法或已改写） | true | empty | 现有 `evidence_refs: no hits`；若发生改写则带 `query_rewritten` |
| 命中 | true | hits | 不变 |

`error_code` 仍用既有 `permanent`（索引名写错可改参数重试，对模型是可修复的；不要标 transient 以免无意义重试同一错误名）。与「集群连不上」的 transient 区分靠 `error` 正文 / `index_error`。

## 5. 字段落地与改写

在**已确认索引落地**之后，若 Search 0 击：

1. 用现有 `collectQueryFieldNames` / `parseLuceneQueryFields` 抽出字段。
2. `ListFields` 得到 mapping 目录。拉 mapping 失败 → `error`（不要当 empty）。
3. `unknownQueryFields`：**仅当 catalog 非空**才判定未知字段（保留现逻辑）。catalog 为空且索引已落地（罕见 mapping 空）→ 不改写，返回 empty。catalog 为空且索引未落地 → 走 §4，根本不应进字段分支。
4. 未知字段改写**一次**（整条 query 最多 round-trip 一次），优先级：

| 优先级 | 条件 | 动作 |
|--------|------|------|
| A | `suggestSimilarMappedFields` 对未知名给出唯一高置信相似字段（现有归一 + 编辑距离） | 把 Lucene / term 的字段名换成建议名 |
| B | 集群 `body_field` 非空且该字段在 mapping 中 | 去掉错误字段前缀，改为 `query_string` + `default_field=<body_field>`，query 为原 value |
| C | `body_field` 未配：mapping ∩ 候选名单，取名单中**第一个**命中 | 同 B。候选名单是跨产品常见正文列，**不是**某业务 schema：`message`, `msg`, `log`, `body`, `@message`, `M`（`M` 仅作为名单中的一项，与 `message` 同等，不享有特权） |
| D | 以上皆无 | 去掉字段前缀，无 `default_field` 的 `query_string`（现有 `unknownFieldsNote` 已允许的路径） |

5. 改写成功后重查。回包带 `query_rewritten=true`、`original_query`、`rewritten_query`、`rewrite_reason`（`similar_field` / `body_field` / `unfielded`）。

已知字段但类型不匹配（text 上 term）的改写**保持现有** `rewriteEmptyHitQuery`，且仅当 `unknownFields` 为空（现合同）。先做未知字段改写，再做类型改写，仍总共最多额外 1 次 Search（合并进同一次 retry DSL）。

## 6. 配置模型

`ESLogCluster` 增加可选：

```text
BodyField string  // 日志正文列；空则走 §5.C 推断
```

装配来源（与 `default_index` 相同的 fillEmpty 规则：数据源优先，RCA 仅补空）：

| 来源 | 字段 | 必填 |
|------|------|------|
| elasticsearch 数据源 `config.datasource` | `body_field` | 否 |
| 过渡 RCA `config.rca` | `body_field` | 否 |
| framework YAML `rca.es` | `body_field` | 否 |

Portal：`DatasourceConfig` proto + `service/tool.go` 编解码 + Web 数据源表单增加「日志正文列（可选）」。保存时**不**强制填写（与 `default_index`/`purpose` 不同）。camelCase 归一与现网三字段相同。

现网 `default_index` 为空的 RCA `zj-elk`：代码不写死业务 pattern；运营应迁到 elasticsearch 数据源并填写 `default_index`（已是 9-02 合同）。本期错误回包会带发现目录，减轻空默认的伤害。

## 7. 工具描述（给模型的合同）

`RegisterESLogTool` 描述改为（语义，非逐字）：

- 必须 `cluster`；`index` 必须是本集群真实索引或已存在的 pattern，禁止按服务名发明。
- 未传 `index` 时用该 cluster 的 `default_index`；若默认也空，本次会失败并在结果里列出真实 pattern。
- `query` 可以是无字段前缀的 Lucene，或 mapping 中存在的 `field:value`。**不要假设 `vmid` / `flow_id` 一定是字段。**
- 0 击时工具会核对索引是否存在、字段是否在 mapping，并可能改写一次；`hit_status=empty` 才表示「索引和字段都合法但仍无文档」。
- 继续列出每套：`` `{id}` — {purpose}；默认索引 `{default_index}` ``；若有 `body_field` 则附上。

去掉「以 `operation:DiscardUserArchive` 为示范」这种未声明 mapping 的 field:value 示例，或改成「仅当该字段存在于 mapping」。

工具描述**不**粘贴全量 pattern（token 与过期问题）。目录只出现在错误/空击回包。

## 8. 证据闸与技能

- `hit_status=error` + `index_error=unresolved`：**不是**「日志证据不足」，是查询未落地。证据闸 / 技能应继续查，而不是写「insufficient evidence」或根据代码脑补现场。
- `hit_status=empty` 且无 `index_error`：才是「这套索引上没有匹配文档」。
- 业务技能（如 `scheduling-flow-trace`）仍可写本环境的索引表；那是技能内容，不是框架封装。本期**不改**技能正文也能受益（工具会挡错索引）。

## 9. 测试合同（框架单测即可，不打真 ES）

用 fake Reader / fake Mapper / fake Cat：

1. `index=cgschedule-*` 且 cat 返回空 → `ok=false`，`hit_status=error`，`index_error=unresolved`，建议里含 `default_index` 与一条真实 pattern（如 `app-logs-*`），**不含**写死的 `backend-`。
2. 同一查询在 cat 命中物理索引、mapping 无 `vmid`、有 `vm_id` → 改写为 `vm_id` 后第二次 Search 被调用；结果 `query_rewritten` + `rewrite_reason=similar_field`。
3. 未知字段 `foo:bar`，无相似，mapping 有 `message` 无 `M`，未配 `body_field` → 改写 `default_field=message`。
4. 未知字段、mapping 只有 `M` 与 `L` → 改写 `default_field=M`（名单命中，不是硬编码特权）。
5. 未知字段、mapping 无任何正文候选、无 `body_field` → unfielded `query_string`。
6. 索引落地、字段合法、0 击 → `ok=true`，`hit_status=empty`，不出现 `index_error`。
7. `index` 与 `default_index` 皆空 → error，且若 fake cat 有目录则出现在回包。
8. 显式 `index=*` → 不走 unresolved（视为落地）。
9. 现有：trace_id、JSON DSL、term→keyword、未知字段提示、缺 cluster、翻页 — 回归不得坏。

Portal：`body_field` 读写 round-trip；缺省仍合法；`collectESLogClusters` 把 `body_field` 填进 `ESLogCluster`。

## 10. 验收（现网，不作为单测断言）

同一 Agent、同一 `zj-elk`：

- `index=cgschedule-*`, `query=vmid:199306` → 不得 `empty`；应 error + 建议含 `backend-sched-hub-*` 或至少目录切片。
- `index=backend-sched-hub-*`, `query=vmid:199306` → 应改写后出现 199306 相关 hits，或至少 `query_rewritten` 而非静默 empty。
- 不填 `body_field` 时 zj 集群仍应能落到 `M`（因 mapping 有 `M`）。

## 11. 实施切分

| 包 | 职责 |
|----|------|
| `framework/metadata` | 导出索引分组，供 tool 复用 |
| `framework/tool` | cat 缓存、未落地判定、Lucene 改写、描述与回包 |
| `framework/config` | `RCAESConfig.BodyField` |
| `portal` 装配 + proto + Web | 可选 `body_field` 贯通 |
| `portal/docs/rca-es-log-query.md` | 运营：填 `default_index`；可选正文列；如何读 `index_error` |

一期可只合 framework 行为（假 cat/mapper 单测绿）再贯通 Portal 字段；没有 `body_field` 时 §5.C/D 已能工作。
