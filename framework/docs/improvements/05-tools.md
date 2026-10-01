# 工具层改进(参数契约、空结果诊断、执行前校验)

> 记录日期: 2026-10-01
> 范围: `framework/tool`(es_log_query、jaeger_trace、rca_*、file tools、memory)、`framework/tool/data`(execute_read、list_tables、describe_table)、外部 MCP(vm_manager)
> 来源: Portal 线上数据统计(8 个 agent、209 个会话、10459 次工具调用)+ 源码评审
> 关联: [04-harness.md](./04-harness.md) 的 B2(ES 聚合字段)、C1(引用失败提示)在本文中展开

## 背景

### 数据概况

| 指标 | 数值 |
|------|------|
| 工具调用总数 | 10459 |
| 报错失败 | 1343(约 13%) |
| 成功但返回空结果 | 922(其中 `es_log_query` 620 次) |

报错失败按原因粗分(每个工具最多抽样 200 条,共 1218 条):

| 类别 | 占比 | 说明 |
|------|------|------|
| 参数 / 契约错误 | 约 63% | 模型传错参数,但多数源于工具没告诉模型合法取值、没在本地挡住明显误用 |
| 配置 / 部署缺口 | 约 17% | 模型怎么改参数都没用,只会反复重试 |
| 环境 / 网络 / 权限 | 约 20% | 超时、EOF、Unauthorized 等 |

### 失败率较高的工具

| 工具 | 失败 / 调用 | 失败率 | 主要原因 |
|------|-------------|--------|----------|
| `execute_read` | 275 / 576 | 47% | 把 ES 查询发给 MySQL;数据源 id 是编的 |
| `ssh_exec` | 14 / 43 | 32% | 只返回 `ok=false`,没有原因 |
| `jaeger_trace` | 117 / 374 | 31% | 把 flow_id、vmid 当成 trace_id |
| `investigation` | 71 / 333 | 21% | 引用原文找不到 |
| `list_tables` | 65 / 317 | 20% | 数据源 id 是编的 |
| `es_log_query` | 250 / 1744 | 14% | 未传 index 而集群未配默认索引 |
| `vm_run_cmd` | 58 / 545 | 10% | 绑定了多个 MySQL 数据源 |
| `rca_read` / `rca_grep` | 86 / 1589 | 5% | 把服务名当仓库名;文件路径靠猜 |

### 空结果率较高的工具

| 工具 | 空结果 / 成功调用 | 空结果率 |
|------|-------------------|----------|
| `es_log_query` | 620 / 约 1500 | 约 41% |
| `rca_glob` | 100 / 242 | 41% |
| `search_files` | 74 / 216 | 34% |
| `memory_recall` | 19 / 69 | 27% |
| `execute_read` | 40 / 301 | 13% |

`es_log_query` 的 620 次空结果中,至少 151 次在之后 3 次调用内用同一标识符换参数即查到,说明是参数错误而非数据不存在。空结果的危害大于报错:模型会把 0 条当作"日志里没有",写进结论(会话列表中可见"ES 中完全没有 archive/UnLoad/DiscardUserArchive…"这类结论)。

### MySQL 与 ES 的差异

- MySQL 字段 / 表名写错会**直接报错**(Unknown column 仅 4 次、表不存在约 25 次),模型可借 `describe_table` 自行纠正。
- ES 字段写错**不报错,只返回 0 条**(`vmid:225781` 中 `vmid` 不是字段),模型分不清"没数据"和"写错了"。
- 因此执行前校验的重点在 ES;MySQL 补齐预检即可。

## 优先级图例

沿用 [README.md](./README.md):🔴 A 严重 / 🟠 B 中等 / 🟡 C 较小 / 🔵 D 方向。

## 一、参数契约(报错类)

### 🔴 A1 ES 集群未配默认索引,`index` 却标为可选

**现状**: `zj-elk`、`zj-elk_flow`、`mg-rca-es` 三个集群 `default_index` 为空,schema 中 `index` 描述为 "Override the default log index/pattern",看起来可选。`index is required when cluster default_index is empty` 共 182 次,单项最大失败来源。另有 37 次 `index pattern matched no physical indices`(如 `vm-manager-*`、`backend-cgvmagent-*`)。

**建议**:
- 为三个集群配置 `default_index`;或在 `default_index` 为空时,把该集群可用索引模式(来自 `IndexCatalog`)直接写进工具描述,并在描述中标明该集群必须传 `index`。
- 工具描述分两层:前几行写硬约束,详细用法放 `tool_describe`(当前描述约 2000 字符,关键约束被淹没)。

**涉及**: `framework/tool/es_log_tool.go`、`es_log_index.go`、集群配置

### 🔴 A2 `execute_read` schema 诱导把 ES 查询发给 MySQL

**现状**: `framework/tool/data/execute_read.go:64` 声明了 `index` 参数(描述为 "Elasticsearch: target index…"),`dsl` / `query` 互为别名,而运行时 `RejectElasticsearchDatasource` 拒绝 ES 数据源。模型据此发出 `{"index":"cgsession*","query":"flow_id:4103_... AND (startGame OR endGame)"}` 或 `SELECT * FROM "cgsession*" ... ORDER BY @timestamp`,MySQL 报 `You have an error in your SQL syntax` 共 124 次。报错只说语法错,模型反复改语法,意识不到选错了工具。

**建议**:
- 绑定的不是 ES 数据源时,不声明 `index`;参数统一为 `sql`,去掉别名。
- 执行前本地检查:不以 `SELECT` / `WITH` / `SHOW` / `DESC` / `EXPLAIN` 开头,或形似 Lucene / ES DSL(`field:value`、`{"query"`、`@timestamp`)时直接拒绝,提示"这是 SQL 工具,查日志请用 es_log_query"。

**涉及**: `framework/tool/data/execute_read.go`

### 🔴 A3 枚举型参数用自由字符串,合法值不告诉模型

**现状**:
- 数据源 id 约 100 次 `datasource: not found`:`cges`、`d_cgarchive`、`cgsession`、`d_cg`,甚至把 ES 集群 `zj-elk` 传给 `list_tables`。schema 只写 "Datasource ID; if omitted, the session default is used.",报错不列合法值。
- `rca_*` 的 `repo` 约 50 次 unknown repo:传入 `vm-manager`、`cgsession`、`sched-hub` 等服务名,实际只配置了 `cloudgame` / `migu`。参数名 `repo` 诱导填服务名。
- 技能名约 25 次 not found:`migu-rca`、`migu_rca`、`vm-prelaunch` 等。

**建议**:
- 合法值较少时写进 schema `enum` 或描述中列出;报错附合法值列表 + 编辑距离最近的候选(复用 `tool_not_found.go` 逻辑)。
- `repo` 描述改为"代码根目录名,不是服务名,可选值:cloudgame",服务名请写进 `glob` / 路径。

**涉及**: `framework/tool/data/datasource_id.go`、`list_tables.go`、`describe_table.go`、`rca_code_tools.go`、`rca_repos.go`、`skillops/skill_tools.go`

### 🟠 B1 标识符类型混淆,工具不做格式校验

**现状**:
- `jaeger_trace` 约 45 次收到 `4100_oxid8yvhdz6k`、`0_5q4jd565xgwq`、`102707` 作为 trace_id,等服务端报 `invalid syntax` / `trace not found`。
- `rca_read` 22 次 `file not found`,路径靠猜(如 `rock-stack/cgschedule/internal/execute/executor.go`,真实为 `rock-stack/apps/cgschedule/internal/biz/planner/execute/executor.go`)。
- `read_file` 24 次 pathguard 拒绝,读 `D:/workspace/cloudgame/...`,实际想读代码。

**建议**:
- `jaeger_trace` 本地校验 trace_id 为 16 / 32 位十六进制;不符合时提示"这看起来是 flow_id 或 vmid,请先用 es_log_query 找到 TRACE_ID"。
- `rca_read` 找不到文件时按文件名模糊匹配,返回 3 个候选路径。
- `read_file` 被 pathguard 拒绝时说明工作区根目录;路径形似代码路径时提示改用 `rca_read`。

**涉及**: `framework/tool/jaeger_tool.go`、`rca_code_tools.go`、`file_tools.go`

### 🟠 B2 必填参数未声明,全靠运行时报错

**现状**: `execute_read` `required: []`;`jaeger_trace` 未声明必填;缺必填参数约 50 次(`http_request` 缺 `method` / `url`、`search_files` 缺 `pattern`、`jaeger_trace` 空参数、`memory_recall` 传 `query: ""` 被拒 7 次)。

**建议**: 能声明 `required` 的都声明(`es_log_query` 的 `cluster` 例外,保留运行时报错以带出集群列表);能给默认值的给默认值(`method` 默认 GET);`memory_recall` 空查询退化为列出最近记忆。

**涉及**: `framework/tool/data/execute_read.go`、`jaeger_tool.go`、`http_tool.go`、`file_tools.go`、`memory/store_tools.go`

### 🟠 B3 配置缺口被包装成模型错误

**现状**(约 200 次,模型无法自行修正):
- A1 中的 `default_index` 为空(182 次)。
- `vm_run_cmd`(外部 vm_manager MCP)报 `multiple MySQL datasources bound (pro_mysql_vm_tool, cgarchive); pass host or set preferred` 31 次,模型无法设置 preferred。
- 技能包缺文件、路径拼错(`E:\sitath\workspace`)、脚本不存在。

**建议**: agent 加载时做配置自检——集群无默认索引、多数据源无法区分、技能引用文件不存在、代码根目录不存在或为空——在启动或健康检查时告警并在 Portal 展示,而不是每次调用时失败。

**涉及**: `framework/templates/from_config.go`、`rca_wiring.go`、`framework/skills/loader.go`、vm_manager MCP 配置

### 🟡 C1 失败信息不可操作

**现状**: `ssh_exec` 失败只返回 `ok=false`(13 次);`pods_exec` 7 次参数不是合法 JSON;模型编造工具名 `web_fetch`、`runCmd`。

**建议**: `ssh_exec` 返回退出码与 stderr 摘要;MCP 工具参数 JSON 解析失败时回显 schema 中的必填字段。

**涉及**: `framework/tool/ssh_exec.go`、`mcp.go`

### 🟡 C2 凭据明文出现在工具参数与执行链路中

**现状**: 咪咕 agent 的 `terminal` 调用中,FTP 账号密码以明文写在 curl 命令里,并随执行链路(timeline metadata)落库。

**建议**: 提供凭据引用机制(如 `${secret:ftp_lightplay}`,执行时替换);执行链路落库前对 `-u user:pass`、`password=`、`Authorization:` 等模式脱敏。

**涉及**: `framework/tool/terminal_tool.go`、`portal/internal/chatsse/sse.go`(timeline 落库)

## 二、空结果诊断(查不出结果类)

### 🔴 A4 区分"可疑的空"与"正常的空"

**现状**: 只要结果为 0,工具统一返回 `ok: true` + 空结果。`hit_status=empty` 原意是"索引和字段都合法,只是没有数据",实际把"查询不可能命中"的情况也装了进去:
- 识别出未知字段(`unknown_fields` 58 次)仍返回 `ok: true` 空结果。
- 原力 agent 的 `rca_glob` 75 次空结果,连 `**/go.mod` 都为空,返回 `{"matches": [], "ok": true}`——代码根目录未配置或为空,却表现为"代码里没有"。
- `search_files` 在不含源码的工作区里搜代码中的中文报错文案,结果为空但不说明搜了哪里。

**建议**:
- 新增 `hit_status: "suspect"` 并附原因(未知字段、根目录不存在、扫描 0 个文件、请求字段整列为 null)。
- stop 规则与结案审查拒绝把 `suspect` 当作"不存在"的证据(与 [04-harness.md](./04-harness.md) 联动)。
- 代码 / 文件类工具空结果时返回搜索根目录与扫描文件数;根目录不存在或扫描 0 个文件时直接报错。

**涉及**: `framework/tool/evidence.go`(HitStatus)、`es_log_tool.go`、`rca_code_tools.go`、`file_tools.go`、`framework/harness/harness_stop_rules.go`、`critic_hook.go`

### 🟠 B4 结果为 0 时自动做低成本探测

**现状**: `es_log_query` 空结果中的典型参数错误:

| 类型 | 次数 | 例子 |
|------|------|------|
| 精确值猜错 | 82 | `term operation.keyword = "/schedule.hub.v2.ScheduleService/Release"` 为 0;`operation:*Release*` 有结果 |
| 分词不匹配 | 约 150 | 疑似只搜 `oxid8yvhdz6k` 未带 `4100_` 前缀(待用 `_analyze` 验证);对分词文本做 `*x*` 通配;在 JSON 文本 `args` 上 `match_phrase` 子串 |
| 索引选错 | — | `archive-*` 为 0,`*cg*` 有;`backend-access-proxy-*` 为 0,`backend-api-gateway-*` 有 |
| 时间窗口 | 约 46 | 22 次 `now-30m` / `now-1h` 等短窗口;24 次时间写在 query 里且自带时区 |

**建议**: 结果为 0 时按顺序探测,每项一次 count 查询:
1. 字段不存在 → 去掉字段名,用值本身重查。
2. 精确匹配为 0 → 该字段按前缀 / 模糊取 top 值,返回"是否要找:…"。
3. 时间窗 → 返回"窗口内 0 条,最近 7 天 N 条,最新一条在 …"。
4. 索引 → 在集群索引目录中按标识符查 count,返回"某索引中有 N 条"。
5. 分词 → 对标识符调 `_analyze`,提示分词结果与改用完整值 / keyword 字段。

探测结果放入 `diagnosis` 字段,不替模型改写查询(避免掩盖问题),仅给出可执行建议。

**涉及**: `framework/tool/es_log_tool.go`、`es_log_index.go`、`es_log_mapping.go`

### 🟡 C3 修正相似字段推荐

**现状**: `flow_id` 推荐 `['L']`;`vmid` 推荐 `['LPID','M','sid','pid','vmIds']`。

**建议**: 按编辑距离 + 下划线 / 驼峰互转(`vm_id` ↔ `vmId`)+ 子串打分,过滤单字母字段;字段不存在但该集群有 `BodyField` 时,提示"该标识符可能在正文 `M` 中"。

**涉及**: `framework/tool/es_log_mapping.go`(`suggestSimilarMappedFields`)

### 🟡 C4 请求字段整列为 null 时告警

**现状**: `backend-cgsession-*` 请求 `fields: ["@timestamp","L","M"]` 返回 `"M": null`,`columns` 中无 `M`,模型在没看到正文的情况下继续推理。

**建议**: 请求字段在全部命中中均缺失时返回 `warnings`,并提示该索引实际正文字段(集群配置已有 `BodyField`,需支持按索引覆盖)。

**涉及**: `framework/tool/es_log_tool.go`、`es_log_compact.go`

## 三、执行前用 mapping / 表结构校验字段

### 🔴 A5 字段校验从"空结果后补救"改为"执行前统一校验"

**现状**: `es_log_tool.go:279` 仅在 `totalFromResult(res) == 0 && from == 0` 时取 mapping,且只检查 query 中的字段:
1. 先查一次、空了再查 mapping、再重查,多一轮往返。
2. `agg_field`、`fields`、`sort`、`time_field` 不检查——`agg_field: vmId` 静默返回空桶(见 [04-harness.md](./04-harness.md) 复盘),`fields: ["M"]` 返回整列 null。
3. 有命中就完全不检查(`vmid:225781 OR error` 前半段字段错误不会被发现)。

**建议**:
- mapping 按"集群 + 索引模式"缓存(TTL 约 10 分钟)。
- query、`agg_field`、`fields`、`sort`、`time_field` 中引用的全部字段在**执行前**校验;不存在则不执行,返回错误 + 候选字段 + 正文提示(如"`vmid` 不是字段;vmid 位于正文 `M` 中,格式为 `vmId[数字]`")。
- 不要求模型每次先 describe:校验在工具内完成,代价为一次缓存查找。
- MySQL 同理:用缓存的表结构预检 SQL 中的表名与列名,报错附 `describe_table` 结果摘要。

**涉及**: `framework/tool/es_log_tool.go`、`es_log_mapping.go`、`es_log_query_opts.go`、`framework/tool/data/execute_read.go`

### 🟠 B5 提前提供字段速查,替代让模型读整份 mapping

**现状**: ES 日志索引字段多、按索引模式不同、存在动态 mapping;且 mapping 只能回答"字段是否存在",回答不了"标识符在哪"(flow_id、vmid 多在正文中)和"值长什么样 / 如何分词"。

**建议**:
- 为每个集群与常用索引生成十几行的字段速查:常用字段、类型、是否有 `.keyword`、正文字段、常见标识符在正文中的格式(如 `vmId[...]`、`flowId[prelaunch_4103_...]`)。
- 由 mapping + 抽样数据半自动生成,人工补充业务知识;放入工具描述或对应技能。

**涉及**: `framework/tool/es_log_mapping.go`、`es_log_index.go`、工作区技能(如 `new-prelaunch-troubleshoot`)

### 🟠 B6 新增 ES 版 describe 工具

**现状**: MySQL 有 `describe_table` / `list_tables`;RCA 侧的 ES 没有给模型查看字段的工具。

**建议**: 新增 `es_describe_index`:返回字段、类型、keyword 子字段、每字段 3 个样例值、正文字段名。仅用于新索引探索,不作为每次查询的必经步骤。

**涉及**: `framework/tool/` 新增 `es_describe_tool.go`,注册到 `ToolsetRCA`

### 🟠 B7 `es_log_query` 支持从正文提取后计数

**现状**: vmid 等标识符只在正文 `M` 中,对 `M.keyword` 聚合返回整条日志,无法按 vmid 统计;模型只能分页拉 500 行人工数或逐个单查(复盘会话中逐台查了 13 次)。

**建议**: 新增 `extract: {field: "M", pattern: "vmId\\[(\\d+)\\]"}`,对全量命中(或落盘结果)做 group-by 计数,配合 `result_stats`。即 [04-harness.md](./04-harness.md) B2 的后半部分。

**涉及**: `framework/tool/es_log_tool.go`、`es_log_query_opts.go`、`result_stats.go`

## 实施节奏

| 阶段 | 内容 | 预计效果 |
|------|------|----------|
| Phase 1(配置 + 小改) | A1(配置 default_index / 描述列索引)、A2(execute_read 去 index + SQL 预检)、B3(启动配置自检) | 消除约 310 次报错 |
| Phase 2(契约) | A3(枚举 + 候选)、B1(格式校验 + 候选路径)、B2(必填 + 默认值)、C1 | 消除约 270 次报错 |
| Phase 3(校验与诊断) | A5(执行前字段校验 + mapping 缓存)、A4(suspect 状态)、C3、C4 | 消除大部分"字段写错导致空结果" |
| Phase 4(能力) | B4(空结果探测)、B5(字段速查)、B6(es_describe_index)、B7(正文提取计数) | 减少值层面与分词导致的空结果 |
| 随时 | C2(凭据脱敏) | 安全项,建议尽快 |

前三项(A1、A2、A3)合计约可消除现有报错失败的一半。

## 验收用例

- `es_log_query` 对 `zj-elk` 不传 index:描述中已列出可用索引,或返回含索引列表的错误;不再出现无提示失败。
- `execute_read` 传入 Lucene 查询或 `@timestamp` 排序的 SQL:本地拒绝并提示改用 `es_log_query`,不再到达 MySQL。
- `jaeger_trace` 传 `4100_xxx`:本地拒绝并给出改用 es_log_query 的提示。
- `es_log_query` 使用 `vmid:225781` 或 `agg_field: vmId`:执行前拒绝,提示 vmid 位于正文及格式。
- `rca_glob` 在代码根目录不存在的 agent 上调用:返回错误而非 `ok: true` 空结果。
- 结果为 0 的查询:返回 `diagnosis`(时间窗外数量 / 其他索引数量 / 字段 top 值之一)。
- 用同一批 Portal 数据重跑统计脚本:报错失败率与 `es_log_query` 空结果率较基线(13% / 约 41%)显著下降。
- 建议将上述用例加入 `evals/tasks/`。
