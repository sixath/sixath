# 多 ES 集群绑定与 `es_log_query` 路由

> 状态：已评审  
> 日期：2026-09-02  
> 关联：[2026-08-10-es-exclude-data-tools-design.md](./2026-08-10-es-exclude-data-tools-design.md)、[2026-08-15-rca-es-log-inline-design.md](./2026-08-15-rca-es-log-inline-design.md)  
> 操作说明（落地后改）：[portal/docs/rca-es-log-query.md](../../../portal/docs/rca-es-log-query.md)

## 0. 决策摘要

| 项 | 选择 |
|----|------|
| 连接落点 | 每套 ES = Portal `type=datasource` + `elasticsearch` 工具；目录名即集群标识 |
| 查询入口 | 仍只有运行时工具 `es_log_query`；绑 ≥1 套 ES 数据源即自动注册，不必再绑 RCA「ELK 日志」 |
| 路由键 | 必填参数 `cluster` = 数据源工具目录名（如 `zj-elk` / `zj-elk_flow`）；只绑一套也必须带 |
| 模型怎么选 | catalog / 工具描述列出：名字、默认索引、用途说明；模型按用户问题选择；同一任务可来回切换 |
| Data 三件套 | ES 仍禁止（既有设计不变） |
| 翻页 / spill | 按 `(cluster, index)` 隔离；A 未翻完不挡去查 B |

## 1. 背景与问题

Agent 绑定表可挂多条 RCA「ELK 日志」（如 `zj-elk`、`zj-elk_flow`），但 `RegisterESLogTool` 写死工具名 `es_log_query`。第二次注册返回 `tool already registered`，调用方 `_ = RegisterESLogTool(...)` **丢弃错误**，后绑集群静默失效。调用参数也没有集群键，`index` 只能换同一套地址上的索引。

排障任务需要在同一轮里多套集群来回切（应用日志 ↔ 流水日志）。

## 2. 目标与非目标

### 目标

1. 一个 Agent 绑定多套 ES 地址，每次 `es_log_query` 都能打到 `cluster` 指定的那套。
2. 同一任务多次调用可切换 `cluster`；翻页回压只针对同一 `(cluster, index)`。
3. 模型能从目录看到每套的**名字、默认索引、用途**，从而选对集群。
4. 技能示例改为带 `cluster="<目录名>"`。
5. 运营在绑定表能区分两套 ES（类型 `DATASOURCE` + 默认索引 + 用途），不再两行都只显示 `RCA`。

### 非目标

- 不按索引 pattern 自动猜集群。
- 不把 ES 重新放进 `list_tables` / `describe_table` / `execute_read`。
- 不改 Jaeger / `rca_code` / `rca_symbol` 的 RCA 形态。
- 不强制一次性迁移现网已保存的 RCA 内联 ES（运行时兼容，见 §8）。
- 不把 `framework` YAML `rca.es` 改成多集群列表（仍单集群；见 §5.2）。
- 不新增第二个查询工具名（不注册 `zj-elk` 为独立 tool）。

## 3. 架构

```text
绑定：elasticsearch 数据源 × N     （目录名 = cluster id）
        │
        ▼
BuildRegistry
        ├─ data 三件套 registry：跳过 ES（SkipDataTools）
        └─ ES 专用 registry：Register 全部 elasticsearch 连接
                │
                ▼
        自动 RegisterESLogTool（仅当 N≥1）
                │
模型：es_log_query(cluster="zj-elk_flow", query=..., index?)
                │
                ▼
        查集群表 → reader.Query(datasourceID=cluster, ...)
```

连接与查询拆开：数据源只负责 DSN/认证/默认索引/trace 字段/用途；`es_log_query` 只负责 DSL 查询与路由。

## 4. 配置模型

### 4.1 Elasticsearch 数据源（正路）

Portal 工具 `type=datasource`，`config.datasource` 在既有连接字段之外增加（仅 ES 有意义，保存时校验）：

| 字段 | 必填 | 含义 |
|------|------|------|
| `type` | 是 | `elasticsearch` / `es` |
| DSN 或 Host/Port 等 | 是 | 既有连接 |
| `default_index` | 是 | 未传 `index` 时的索引/pattern |
| `trace_id_field` | 否 | 默认 `trace_id` |
| `purpose` | 是 | 给模型与绑定表的用途说明（如「应用日志」「流水/game_flow」） |

运行时数据源 ID 仍 **等于工具目录名**（`canonicalDatasourceConfig` 不变）。`cluster` 必须与此名完全一致（大小写敏感，与现网工具名一致即可）。

`purpose` / `default_index` / `trace_id_field` 只属于 Portal 工具 config，装配时读入集群表。**不要**扩 `framework/datasource.Config`（避免污染 MySQL 等类型）。连接仍走 `datasource.Config`。

现网 `DatasourceConfig` 是 proto **逐字段**编解码（`portal/api/tool/v1/tool.proto` + `portal/internal/service/tool.go`），未知键在 Create/Update/读回时会被丢掉。因此必须：

- proto `DatasourceConfig` 增加 `default_index`、`trace_id_field`、`purpose`（仅 ES 有意义；其它类型忽略）
- `service/tool.go` 编解码补这三字段
- `web/src/api/client.ts` 的 `DatasourceConfig` 同步，并对这三字段做与 RCA 相同的 camelCase 归一（`defaultIndex` → `default_index` 等），避免读回后再保存丢字段

缺这三处则绑定表和 catalog 看不到用途/默认索引。

### 4.2 RCA `func_path=es_log_query`

- **新建**：拒绝。提示改为创建 elasticsearch 数据源工具并绑定到 Agent。**不要**从 `ValidRCAFuncPath` 删除 `es_log_query`，否则与「更新仍允许」冲突。
- **已存在的更新**：仍允许（避免运营改不了旧条目）；运行时按 §8 并入集群表。
- **Web**：新建 RCA 时隐藏或禁用「ELK 日志」。**编辑已有** `func_path=es_log_query` 的工具时仍展示 endpoint / datasource_id / 默认索引 / trace 字段，否则与「更新仍允许」冲突。elasticsearch 数据源表单展示默认索引 / trace 字段 / 用途。

## 5. 运行时：集群表与 `es_log_query`

### 5.1 装配顺序

`BuildRegistry` 不要在遍历到每条 RCA 时立刻 `RegisterESLogTool`。改为：

1. 收集绑定中的 elasticsearch 数据源 → 集群表（id=工具名）。
2. 再按 §8 合并过渡期 RCA（连接：同名数据源优先；查询字段：数据源为空则用 RCA）。
3. 集群表非空则 **一次** `RegisterESLogTool`。
4. 其它 RCA（code / symbol / jaeger）保持逐条注册。

ES 连接注册到 **独立** `datasource.Registry` + `executor.NewESExecutor`，与 data 三件套用的 registry 分开。`registerDatasourceTools` 对 ES 仍 `SkipDataTools`，行为与 [2026-08-10](./2026-08-10-es-exclude-data-tools-design.md) 一致。

### 5.2 `RegisterESLogTool` 合同

由「单 `ESLogConfig`」改为接受 **集群列表**（名称可仍叫 `ESLogConfig` 切片或 `[]ESLogCluster`）：

```text
ESLogCluster{ ID, DefaultIndex, TraceIDField, Purpose }
```

- 工具名仍为 `es_log_query`（证据闸、翻页闸、toolset、spill 的 `ToolName` 不变）。
- Parameters 增加必填 `cluster`（string）。JSON Schema `enum` = 当前集群 ID 列表（减少胡编；执行期仍校验）。
- Description / catalog 列出每套：`` `{id}` — {purpose}；默认索引 `{default_index}` ``。
- `Execute`：无 `cluster` 或不在表内 → `ErrorPermanent`，文案列出全部 `id` / `default_index` / `purpose`。**禁止**回落第一套。
- `index` 缺省用该集群 `DefaultIndex`；该集群无默认索引且调用未传 `index` → 永久错误（正路保存已要求 `default_index`）。
- `reader.Query` 的 datasource id = `cluster`（与 ES registry 中的 Config.ID 一致）。
- 空击 mapping / rewrite（`ESFieldMapper` / `mapperFromReader`）必须按**本次** `cluster` 查对应 DSN，禁止注册时绑死单个 `DatasourceID` 导致打到错集群。
- 成功/空击/错误结果均带 `cluster`（及实际 `index`），供翻页闸与 spill 对齐。

`framework/templates/rca_wiring.go`：YAML 仍单集群时，集群 ID = `datasource_id`（引用模式）或 `"rca-es"`（内联 endpoint）；同样要求调用传 `cluster`。单测补上该参数。

### 5.3 Catalog / prompt

- `es_log_query` 的 catalog Bindings 或 SearchHints 含各集群 id、默认索引、用途。
- `FormatDatasourcePrompt` 的 ES 提示改为：必须用 `es_log_query(cluster=<目录名>)`，并列出已绑 ES 集群（不要把 ES id 写进 data 三件套清单）。
- 对 ES id 调用 `execute_read`：保持拒绝，文案改为带 `cluster=`。

### 5.4 Turn Tool Surface（工具族）

`es_log_query` 仍属 `FamilyRCA`（`builtinToolFamily` 不变）。现网 `BoundFamiliesFrom` 只把 `ToolTypeRCA` 计入 `FamilyRCA`；`filterToolsForSurface` 把 **所有** datasource 划到 `FamilyData`。若只绑 elasticsearch 数据源、不再绑 RCA ELK，日志轮次不会激活 RCA 表面，ES 连接也会在 RCA 表面上被滤掉，自动注册的 `es_log_query` 调不到。

必须同时改：

| 点 | 行为 |
|----|------|
| `BoundFamiliesFrom` | elasticsearch 数据源计入 **`FamilyRCA`**，**不**计入 `FamilyData`（MySQL/Hive/Mongo 仍是 Data）。此划分**不**跟 `ToolFamilySplitEnabled` 走。类型判断用现成 `isElasticsearchType`（`elasticsearch` / `es`） |
| `filterToolsForSurface` | `FamilyRCA` 激活时保留 elasticsearch 数据源；`FamilyData` 激活时不保留它们 |
| 过渡期 RCA ELK | 仍走现网 `familyForRCATool` → `FamilyRCA` |

只绑 ES 数据源的 Agent，日志类问题必须仍能激活 RCA 表面并调用 `es_log_query`。

## 6. 同一任务切换与翻页

参数 `cluster` **每次调用独立**，会话不钉死某一套。

`EvaluateTruncatedPageGate` / `lastSuccessfulESLogQuery`：

- 只根据 **最近一次成功的** `es_log_query` 的 `(cluster, index)` 是否 truncated 决定是否在「用户要查全量」时注入催页。
- 催页文案必须写出 `cluster` 名和 `from`。
- 因此：A truncated 之后成功查完 B → 最近一次是 B，不因 A 未翻完而拦结束；A truncated 之后模型直接总结 → 仍催 **A**。
- 不在「换 cluster 的新查询」上拦截（闸只在准备结束/注入时看最近一次查询，不改 tool 分发）。

Spill：`MaybeSpill` 已按 seq 分文件，不会混写同一个 jsonl。必须在 stub/payload 写入 `cluster`；文件名可带 sanitized cluster，便于运营辨认（非必须，payload 有即可）。

## 7. UI

Agent 绑定表（`AgentDetail` 绑定工具）：列至少 **名称、类型、默认索引、用途、操作**。非 ES 数据源「默认索引」空；非 ES 的用途可用工具 `description` 或空。elasticsearch 用 `config.datasource.purpose` 与 `default_index`。

工具表单：`type=elasticsearch` 时显示默认索引、trace 字段、用途；缺省不可保存。

绑定表不需要展示原始 URL。

## 8. 过渡期：旧 RCA 配置

现网常见两种旧形态：RCA + 内联 `endpoint`，或 RCA + `datasource_id`（默认索引/trace 字段写在 **RCA** 上，elasticsearch 数据源历史上只有 DSN）。目标态是数据源自带这些字段，但不能上线即失查。

先收录全部 elasticsearch 数据源为集群行（连接 + 数据源上已有的 `default_index` / `trace_id_field` / `purpose`）。再扫绑定中的 RCA `es_log_query` **合并**，不覆盖已有非空数据源字段：

| RCA 形态 | 集群 ID | 连接 | 查询字段（`default_index` / `trace_id_field`） | 用途 |
|----------|---------|------|-----------------------------------------------|------|
| 内联 `endpoint` | **RCA 工具名** | 向 ES 专用 registry `Register` `Config{ID: RCA工具名, DSN: endpoint, User, Password}`。**禁止**再用固定 `"rca-es"`（多套内联会撞 ID）。若已有同名数据源：保留数据源连接，内联 URL 丢弃并 warn | 集群行该字段为空时用 RCA 的值填上 | 空则用 RCA 工具 `description` |
| 仅 `datasource_id` | 即该 `datasource_id`（须已是数据源集群行） | 不新增连接 | **合并进该行**：数据源字段为空时用 RCA 的 `default_index` / `trace_id_field`。现网索引在 RCA 上，不合并则未传 `index` 会误走 §5.2 永久错误 | 数据源 `purpose` 为空时用 RCA `description` |
| 仅 `datasource_id` 但该数据源未绑 | — | skip + warn（与现网「找不到 datasource」一致） | — | — |

合并后仍无 `default_index` 的集群：调用必须显式传 `index`，否则永久错误。

不提供自动改写 DB 的迁移脚本。运营把连接与索引/用途写到 datasource、解绑 RCA ELK 即离开过渡期。

## 9. 技能

- 仓库内：`framework/skills_examples/skills/rca-investigation/SKILL.md` 等所有写死 `es_log_query` 且无 `cluster` 的示例，改为带 `cluster`；至少一处写明同一任务可切换集群。
- Agent 工作区技能（Portal workspace，不在本仓）：实现时能扫到的同样改；扫不到的不阻塞合并，靠运行时报错清单纠偏。

无 `cluster` 的调用 **不做** 兼容成功路径。

## 10. 报错

| 情况 | 行为 |
|------|------|
| 漏传 / 空 `cluster` | 永久错误 + 可用清单 |
| 名字不在表内 | 同上；不做前缀模糊匹配 |
| 连接/查询失败 | 与现网相同的瞬时/永久分类；结果带本次 `cluster` |
| 无 ES 数据源且无过渡 RCA 内联 | 不注册 `es_log_query` |
| 对 ES 走 data 三件套 | 现网拒绝 + 指向 `es_log_query(cluster=…)` |

## 11. 测试

装配：0 / 1 / 2 套数据源；不绑 RCA 也能注册 `es_log_query`；同名数据源保留连接、RCA 内联 URL 丢弃；RCA 仅 `datasource_id` 且数据源无 `default_index` 时合并 RCA 上的默认索引。两套内联 RCA 使用各自工具名作 registry ID，不得撞 `"rca-es"`。只绑 ES 数据源、不绑 RCA ELK 时，RCA 表面仍能注册并调用 `es_log_query`（`BoundFamiliesFrom` / `filterToolsForSurface`）。

路由：cluster 命中对应 DSN id；漏传/拼错含清单；默认索引与覆盖 `index` 不换集群。

切换：A truncated 后再调 B，翻页闸在「最近一次为 B 且 B 已完整」时允许结束；催页文案含集群名。

保存：ES 数据源缺 `default_index` 或 `purpose` 拒绝；新建 RCA `es_log_query` 拒绝。

回归：`es_log_query` 空击 mapping、spill、`result_stats`、data 三件套（MySQL）现网用例；YAML `rca.es` 单集群加 `cluster` 参数后仍注册成功。

手测验收：Agent 同时绑 `zj-elk` 与 `zj-elk_flow`；一轮对话切换两次且地址不同；漏传 `cluster` 列出两套而非默打第一套。

## 12. 实现落点

| 区域 | 变更 |
|------|------|
| `framework/tool/es_log_tool.go` | 多集群、必填 `cluster`、结果盖章 cluster |
| `framework/tool/es_log_mapping.go` | mapping 按本次 `cluster` 选 DSN |
| `framework/templates/rca_wiring.go` | 单集群亦要求 `cluster` id |
| `framework/agent/truncated_page_gate.go` | 催页按最近一次的 cluster/index；文案带名 |
| `portal/api/tool/v1/tool.proto` + 生成代码 | `DatasourceConfig` 增加三字段 |
| `portal/internal/service/tool.go` | 三字段编解码 |
| `web/src/api/client.ts` | `DatasourceConfig` 同步 + camelCase 归一 |
| `web/src/utils/toolExportFormat.ts` | datasource 三字段同样归一，避免导入导出丢失 |
| `portal/internal/chat/agent_builder.go` / `rca_builder.go` | 收集集群表并合并 §8；自动注册；RCA 不再单独 RegisterESLogTool |
| `portal/internal/chat/tool_families.go` + `filterToolsForSurface` | ES 数据源计入 FamilyRCA，不计入 FamilyData |
| `portal/internal/chat/datasource_prompt.go` | ES 清单 + `cluster=` |
| `portal/internal/biz` | ES 数据源字段校验；禁止新建 RCA es_log_query |
| `web` ToolForm / AgentDetail | ES 字段与绑定表列；编辑旧 RCA ELK 仍可改连接 |
| `portal/docs/rca-es-log-query.md` | 正路改为绑 elasticsearch 数据源；废弃「不必建数据源」 |
| 技能示例 | `cluster=` |

## 13. 成功标准

- 两套 ES 同时绑定后两次 `es_log_query` 可打到两个 DSN。
- 漏传 `cluster` 不会命中任一集群。
- 绑定表能靠默认索引和用途区分两套，而不只靠猜名字。
- 现网 RCA 内联在迁走前仍可作为同名或 RCA 工具名对应的一簇被查到。
