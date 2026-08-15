# RCA `es_log_query` 内联 ES 配置

> 状态：设计已确认（待实现）  
> 日期：2026-08-15  
> 分支：`feature/mea-minimal-subset`（或后续独立 feature 分支）  
> 动机：运营误把 ES URL 填进「数据源工具 ID」，导致 `es_log_query` 未注册、模型只能「建议查日志」无法实查。与 `jaeger_trace` 的 `query_url` 体验对齐。

## 1. 目标与非目标

### 目标

1. 在 RCA 工具（`func_path=es_log_query`）上支持**直接配置** ES 连接信息（endpoint + 可选认证）。
2. **保留**既有 `datasource_id` 引用路径（同绑 Agent 的 datasource 工具名）。
3. **互斥**：内联与 `datasource_id` 不可同时配置；冲突在 **Create/Update 保存时拒绝**（前端同步校验）。
4. 缺任一侧时亦拒绝保存；运行时对脏配置仍 skip + warn（防御）。

### 非目标

- 不改变 `es_log_query` 调用参数语义（`trace_id` / `query` / `index` / `limit`）。
- 不强制迁移现有只配 `datasource_id` 的工具。
- 不删除 datasource 工具类型，不把内联 ES 暴露给 `list_tables` / `describe_table` / `execute_read`。
- 不做「从已绑 datasource 下拉选择」UI（可 follow-up）。
- 不在本设计中改 Turn Tool Surface / 意图门逻辑。

## 2. 配置模型

扩展 `rca` / protobuf `RCAConfig`（仅当 `func_path=es_log_query` 有意义）：

| 字段 | 新增/保留 | 含义 |
|------|-----------|------|
| `endpoint` | **新增** | ES 完整 URL，如 `http://10.137.211.84:29200`（映射到 `datasource.Config.DSN`） |
| `user` | **新增** | 可选 Basic 用户名 |
| `password` | **新增** | 可选 Basic 密码 |
| `datasource_id` | 保留 | 同绑 Agent 的 elasticsearch datasource **工具名** |
| `default_index` | 保留 | 默认索引/pattern |
| `trace_id_field` | 保留 | 默认 `trace_id` |

定义：

- **内联模式**：`strings.TrimSpace(endpoint) != ""`
- **引用模式**：`strings.TrimSpace(datasource_id) != ""`

### 互斥规则（保存时强制）

| endpoint | datasource_id | Create/Update |
|----------|---------------|---------------|
| 有 | 有 | **拒绝** 400：二者互斥，请只保留其一 |
| 无 | 无 | **拒绝** 400：须配置 endpoint 或 datasource_id |
| 有 | 无 | 通过（建议校验 URL 形态：可接受无 scheme，运行时按现有 ES 工厂补 `http://`） |
| 无 | 有 | 通过（不在保存时验证 Agent 是否已绑定该 datasource） |

Web `ToolForm` 在 `es_log_query` 下：

- 主路径展示：ES 地址 / 用户 / 密码 / 默认索引 / trace 字段。
- 兼容路径：`datasource_id` 标注「或：引用已绑定 datasource 工具名（与上方地址二选一）」。
- 前端禁用保存条件与上表一致；错误文案与 API 对齐。

## 3. 运行时注册

主路径：`portal/internal/chat/rca_builder.go` → `case "es_log_query"`。

```
若 endpoint 与 datasource_id 皆非空或皆空 → skip + warn（防御）
否则若仅 endpoint:
  构建 datasource.Config{
    ID:       合成 id（固定 "rca-es" 或 RCA 工具名）,
    Type:     elasticsearch,
    DSN:      endpoint,
    User/Password: 可选,
  }
  RegisterElasticsearch → NewESExecutor → RegisterESLogTool
否则若仅 datasource_id:
  现有 buildESReaderFromAgentTools（不变）
```

- `framework/tool.ESLogConfig.DatasourceID` 仍要求非空：内联路径填合成 id，仅供内部 `reader.Query` 使用，**不**要求 Agent 再绑 datasource。
- `framework/templates/rca_wiring.go`（若存在进程级 RCA ES 接线）同步支持内联字段，行为与 Portal 一致。

## 4. API / Proto / 编解码

- `portal/api/tool/v1/tool.proto`：`RCAConfig` 增加 `endpoint`、`user`、`password`。
- 生成 pb；`portal/internal/service/tool.go` 的 RCA map ↔ proto 编解码补字段。
- OpenAPI / `web/src/api/client.ts` 类型同步（若由手写维护）。
- CreateTool / UpdateTool：在写入前调用共享校验函数（如 `ValidateRCAESLogConfig`），失败返回业务错误。

## 5. 测试

- **Portal `rca_builder_test`**：内联注册出现 `es_log_query`；仅 `datasource_id` 旧路径仍过；双填/双空 → 不注册。
- **Tool service/biz 校验单测**：双填/双空 → error；单模式 → ok。
- **现有** `es_log_tool` / RCA 相关测试不回归。
- 可选：ToolForm 无强测要求；以手工或轻量单测覆盖互斥提示即可。

## 6. 文档

- 更新操作说明（如 `portal/docs` 中 RCA/ES 相关段落，或本规格落地后的简短 README 指针）：优先填 `endpoint`；`datasource_id` 为兼容二选一。
- 明确：填 URL 到 `datasource_id` 为错误用法，保存期应被互斥/形态引导纠正。

## 7. 验收标准

1. 仅配 `endpoint`（+ 可选认证）并绑定 RCA 工具到 Agent 后，对话时间线可出现 `es_log_query` 调用。
2. 仅配合法 `datasource_id` + 同绑 ES datasource 的旧配置仍可用。
3. 同时填写 endpoint 与 datasource_id 时，Create/Update 失败且文案可读。
4. 两者皆空时 Create/Update 失败。
5. 无 Go 行为变更触及 MEA / ReAct 入口分流（与本设计无关）。
