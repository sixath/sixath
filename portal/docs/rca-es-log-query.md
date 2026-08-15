# RCA `es_log_query`（ELK 日志）

## 推荐配置（内联 ES）

在工具类型 **RCA**、子工具 **ELK 日志 / `es_log_query`** 上直接填写：

| 字段 | 说明 |
|------|------|
| **ES 地址** `endpoint` | 如 `http://10.x.x.x:29200` |
| 用户 / 密码 | 可选 Basic 认证 |
| 默认索引 / `trace_id` 字段 | 与原先相同 |

绑定到 Agent 后即可调用 `es_log_query`，**不必**再创建 elasticsearch datasource 工具。

## 兼容：引用 datasource

也可只填 **datasource 工具名**（`datasource_id`），且该 datasource 工具必须绑定到同一 Agent。

## 互斥（保存时强制）

`endpoint` 与 `datasource_id` **二选一**：

- 两者都填 → Create/Update 拒绝  
- 两者都空 → Create/Update 拒绝  

不要把 ES URL 填进 `datasource_id`（那是工具名，不是地址）。

## 设计规格

见 [docs/superpowers/specs/2026-08-15-rca-es-log-inline-design.md](../../docs/superpowers/specs/2026-08-15-rca-es-log-inline-design.md)。
