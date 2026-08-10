# ES 不进入 Data 三件套（保留 datasource 连接 + es_log_query / http_request）

**日期**: 2026-08-10  
**状态**: 设计已确认；待实现  
**目标**: Elasticsearch 不再通过 `list_tables` / `describe_table` / `execute_read` 查询；ES 日志正路保留 `es_log_query` 与 `http_request`；`framework/datasource` 仍注册 elasticsearch，供 RCA 等按工具 id 引用 DSN（禁止把集群 URL 写死在技能或框架逻辑里）。

**背景**:
- Agent（如 zone-4100）把 ES DSL + 虚构 `datasource_id`（如 `cges`）塞进 `execute_read`，落到 MySQL 后反复 `1064` / `not found`，拖长 Turn 直至超时。
- 技能已声明「ES 不用 execute_read」，但仅靠 prompt 不够。
- 曾讨论「绑 ES 让 execute_read 查 ES」；产品决定改为 **硬禁止** data 三件套碰 ES，并 **保留** datasource 包内 ES 类型作为连接配置。

---

## 0. 决策摘要

| 项 | 选择 |
|----|------|
| ES 查询正路 | **`es_log_query` + `http_request` 都保留** |
| Data 三件套 | **ES 一律不走**（list / describe / execute_read） |
| `datasource` 注册 elasticsearch | **保留**（连接工厂；供 `es_log_query.datasource_id` → 数据源工具） |
| URL 位置 | 仅 Portal **数据源工具配置**；技能/框架不写死集群地址 |
| 误用防御 | 注册阶段跳过 + 执行期若类型为 ES 则明确报错 |

---

## 1. 分层

```text
Portal Tool(type=datasource, elasticsearch)  →  DSN / 只读等环境配置
        ↑ datasource_id（工具名）
es_log_query (RCA)
http_request（通用 HTTP；地址来自配置或会话上下文，非框架写死）

list_tables / describe_table / execute_read
        → 仅 mysql / hive / mongodb 等非 ES 类型
```

**不要**从 `framework/datasource` 删除 `elasticsearch.go`：删掉会逼 RCA 把 URL 塞进自身配置，泛化更差（现网 `zj-elk` 已出现 raw URL 误配，属配置问题，本设计用「ES 工具 id」纠偏，不靠删类型）。

---

## 2. Portal：`registerDatasourceTools`

文件：`portal/internal/chat/agent_builder.go`

对每个绑定的 datasource config：

1. 若 `normalizeDSType(cfg.Type)` ∈ `{elasticsearch, es}`：
   - **不** `dsReg.Register` 进 data 用的 MultiExecutor 路径（或 register 到独立连接表仅供 RCA——若当前 RCA 已从 tools 列表自行解析 DSN，则此处只需 skip data 三件套）。
   - **不**因「仅有 ES 绑定」而注册 list/describe/execute_read。
   - 在返回的 bindings / prompt 中标注：该 id **不用于** data 三件套；查 ES 用 `es_log_query` 或 `http_request`。
2. 非 ES：保持现有 Register + MultiExecutor（MySQL / ES executor 仍可挂在 Multi 上，但 **registry 内无 ES 实例** 则 Execute 到 ES id 会 not found——与「不注册」一致）。

更干净的实现约定：

- `dataConfigs` = filter out ES  
- 仅对 `dataConfigs` 调用现有 register + 注册三件套  
- `esBindings` 单独进入 prompt 段「ES 连接（非 data 查询）」或仅一句总提示  

若过滤后 `dataConfigs` 为空：

- **不**注册 list/describe/execute_read  
- **不**返回「所有数据源均注册失败」（今日逻辑会对「全部 Register 失败」报错）；ES-only Agent 应成功构建 registry，仅无 SQL 三件套  

---

## 3. 执行期防御（framework `tool/data`）

在 `execute_read` / `list_tables` / `describe_table` 解析到 `datasource_id` 后，若能从 Registry 取到类型且为 ES（或未来误注册）：

```text
error: <tool> 不支持 Elasticsearch；请使用 es_log_query 或 http_request
```

若 id 不存在：保持现有 `not found`（模型瞎编 `cges` 仍失败，但文案可附「可用 datasource_id: …」若易取）。

优先保证：**正常路径上 MultiExecutor 根本没有 ES 条目**，执行期检查为第二道闸。

---

## 4. Prompt / 描述符

- `FormatDatasourcePrompt`：只列出 **可用于 data 三件套** 的绑定；文案写明仅 SQL/文档库等。  
- 若 Agent 绑了 ES 数据源工具或 RCA `es_log_query`：追加固定句——日志/检索引擎请用 `es_log_query` 或 `http_request`，禁止 `execute_read`。  
- `framework/templates` 中 elasticsearch 的 dataquery 描述符可保留给其它入口；**Portal Agent 路径不再按 ES 描述符注册三件套**。不强制本迭代删除 `DefaultToolCapabilitiesByType["elasticsearch"]`（避免误伤独立 dataquery demo）；若改动成本低可同步从 Portal 能力说明中弱化。

---

## 5. 技能（zijian workspace）

- `scheduling-flow-trace`：保持「ES ≠ execute_read」；正路写 `es_log_query` / `http_request`，**示例 URL 改为占位或「使用已绑定 ES / 环境配置」**，避免技能正文写死单一集群为唯一真源。  
- `vm-xagent-log-search`：已改为禁止 execute_read 查 ES；复查一遍。  

技能改动可与代码同 PR 或紧随其后的 docs commit。

---

## 6. 配置纠偏（建议，可另单）

| 项 | 现状 | 建议 |
|----|------|------|
| `zj-elk` `rca.datasourceId` | raw `http://10.137.211.84:29200` | 改为指向已创建的 ES **datasource 工具名**（先建工具再改引用） |

本设计实现 **不阻塞** 于该项；在 README/故障说明中提一句即可。

---

## 7. 测试

1. 仅绑 MySQL：三件套可用；行为回归。  
2. MySQL + ES 数据源工具：只对 MySQL 注册三件套；prompt 含 ES 勿用 execute_read。  
3. 仅绑 ES 数据源：BuildRegistry **成功**；registry **无** list/describe/execute_read（或有但调用必失败——推荐无）。  
4. `execute_read` + 若仍传入 ES id：明确错误文案。  
5. RCA `es_log_query` + 合法 datasource 工具 id：不受影响（既有验收测试仍过）。  

---

## 8. 成功标准

- 模型再对 `cges` / ES DSL 调 `execute_read` 时，不再误打 MySQL 烧步数；或立即得到「不支持 ES」类错误。  
- 绑了 `zj-elk` 的 Agent 仍可用 `es_log_query`；`http_request` 仍可用。  
- datasource 包仍可 `RegisterElasticsearch`。  

---

## 9. 非目标

- 删除 `framework/datasource/elasticsearch.go`  
- 强制所有 Agent 绑定 ES RCA  
- 实现新的统一「search_logs」工具  
- 自动迁移现网 `zj-elk` raw URL（仅文档建议）  

---

## 10. 实现落点

| 区域 | 变更 |
|------|------|
| `portal/internal/chat/agent_builder.go` | 过滤 ES；ES-only 不报全失败 |
| `portal/internal/chat/datasource_prompt.go` | data-only 列表 + ES 路由提示 |
| `portal/internal/chat/*_test.go` | 上述用例 |
| `framework/tool/data/*.go`（可选） | ES 类型拒绝 |
| `E:/sixath/workspace/zijian/skills/...` | 与设计对齐的措辞 |
| 本 spec | 实现后改状态为已落地 |
