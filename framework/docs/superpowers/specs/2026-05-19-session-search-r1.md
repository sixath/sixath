# R1：跨会话 FTS + session_search 工具

**状态**：已落地 MVP（2026-05-19）  
**关联**：[growth-r1-r3-feasibility](./2026-05-18-growth-r1-r3-feasibility.md) §3

## 范围

| 子项 | 状态 | 说明 |
|------|------|------|
| R1a | 已落地 | `framework/sessionsearch` SQLite FTS5；消息写入后增量索引 |
| R1b | 已落地 | `session_search` 工具；portal `RegisterSessionSearchTools` |
| R1c | 未做 | 向量混合检索 |

## 数据面

- **权威**：Portal MySQL `chat_sessions` / `chat_messages`
- **索引**：每 agent 一个 sidecar `{store_dir}/{agent_id}.db`（默认 `data/session_index/`）
- **迁移**：`portal/migrations/005_chat_sessions_parent.sql` 增加 `parent_session_id`

## 工具契约

- **无 query**：最近 N 场会话元数据 + 末条消息 preview（无 LLM）
- **有 query**：FTS5 OR 关键词 → 按 `parent_session_id` 折叠根会话 → 排除当前 `session_id` → 返回 snippet 摘要（MVP 无辅助 LLM 摘要）
- **参数**：`query?`、`role_filter?`、`limit`（默认 3，最大 5）

## 配置

| 项 | 默认 | 环境变量 |
|----|------|----------|
| 启用 | true | `SATH_SESSION_SEARCH_ENABLED=false` 关闭 |
| 目录 | `data/session_index` | `SATH_SESSION_INDEX_DIR` |

## 与 memory_search 区别

| | memory_search | session_search |
|--|---------------|----------------|
| 域 | workspace 文件 + 可选 session 转录 chunk | 全 agent 历史消息 FTS |
| 存储 | `.memory_index.db` | `session_index/{agent_id}.db` |

## 后续

- R1c 向量混合；辅助 LLM 摘要（Hermes Flash）；trigram CJK 第二 FTS 表
- 流式路径持久化 user 消息并索引
