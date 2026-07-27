# MemoryStore P2-J：Go Memory MCP Server

> 状态：已交付  
> 日期：2026-07-27  
> 回链：[门面 §8.8](./2026-07-25-memory-store-facade-design.md)、[2026-05-26 §8](../../../framework/docs/superpowers/specs/2026-05-26-multi-layer-memory-design.md)、[Portal memory-integration](../../../portal/docs/memory-integration.md)  
> 切片：**stdio + HTTP**；工具名对齐门面三工具；默认 in-memory Facade；**不含** Portal MySQL / OAuth / OpenMemory 别名

---

## 0. 目标与非目标

### 目标

1. `framework/memory/mcp`：基于 `mark3labs/mcp-go` 暴露 `memory_remember` / `memory_recall` / `memory_get`。  
2. 传输：`ServeStdio` + Streamable HTTP。  
3. `MemoryStore` 可注入；默认 in-memory `Facade`。  
4. MCP 参数补充 `user_id` / `session_id` / `agent_id` / `workspace_root`，写入 context 后复用 `tool/memory` Execute。  
5. `cmd/memory-mcp` CLI：`--transport=stdio|http`、`--addr`。

### 非目标

Portal MySQL 接线、OAuth、OpenMemory 旧名、`AddFromTurn` MCP 工具、改 Portal Agent 改走本机 MCP。

---

## 1. 验收

1. 单测：remember → recall（in-memory）。  
2. ListTools 含三工具。  
3. 缺 session_id 行为与门面工具一致。  
4. HTTP Start 可测（httptest 或 Start+client 烟雾，按实现选型）。
