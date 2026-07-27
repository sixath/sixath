# MemoryStore P2-G：配置收敛与旧键清理

> 状态：已交付  
> 日期：2026-07-27  
> 回链：[门面 §5.3 / §8.7](./2026-07-25-memory-store-facade-design.md)  
> 切片：**G only** — `memory_store:` 外壳、env 硬切换、FTS 评估结论；不删 memorysearch/sessionsearch 包

---

## 0. 目标与非目标

### 目标

1. `agent_extra.yaml` 支持完整 **`memory_store:`** 嵌套块（prefetch / extraction / conflict / vector / agent_workspace）。  
2. **嵌套优先**：`memory_store.*` 与顶层旧键同时存在时，以 `memory_store` 为准。  
3. Env：**删除** `SATH_MEMORY_WRITE_ENABLED`；仅认 `SATH_AGENT_MEMORY_WRITE_ENABLED`。  
4. Agent API **保留** `runtime_tools.memory_write_enabled`（proto/JSON/DB 不迁）。  
5. 文档写明 FTS 结论：`memorysearch` / `sessionsearch` 保留为内部实现，不物理删除、不合并 units。

### 非目标

| 项 | 说明 |
|----|------|
| 删除 memorysearch/sessionsearch 包 | 不做 |
| 改 proto 字段名 | 不做 |
| Qdrant / Neo4j / MCP | 不做 |
| 强制迁移所有部署到仅嵌套键 | 顶层旧键本切片仍可读（无 memory_store 覆盖时） |

---

## 1. 配置形状

```yaml
memory_store:
  agent_workspace:
    write_enabled: false   # 进程默认；可被 Agent runtime_tools.memory_write_enabled OR 覆盖
  prefetch:                # 同 memory_orchestrator_prefetch
    enabled: true
    max_snippets: 5
    max_total: 8
  extraction:              # 同 memory_extraction
    enabled: false
  conflict:                # 同 memory_conflict
    enabled: false
  vector:                  # 同 memory_vector
    enabled: false
    provider: sqlite
```

加载后 `NormalizePortalAgentExtra`：若 `memory_store.X != nil`，覆盖顶层对应字段。

`agent_workspace.write_enabled: true` → 设置进程 `DefaultHermesP0ToolFlags.MemoryWriteEnabled`（再经 env OR）。

---

## 2. Env

| 旧 | 新 |
|----|-----|
| `SATH_MEMORY_WRITE_ENABLED` | **移除**（无效） |
| — | `SATH_AGENT_MEMORY_WRITE_ENABLED` |

---

## 3. FTS 评估

| 包 | 决策 |
|----|------|
| `memorysearch` | 保留；agent files 后端内部 |
| `sessionsearch` | 保留；transcript 后端内部 |
| 合并进 units SQLite | **否** |

---

## 4. 验收

1. 仅 `memory_store.prefetch` → Prefetch 生效。  
2. 顶层 + memory_store 同时存在 → memory_store 胜。  
3. `SATH_MEMORY_WRITE_ENABLED` 不再打开写；`SATH_AGENT_MEMORY_WRITE_ENABLED` 可打开。  
4. `runtime_tools.memory_write_enabled` 仍可按 Agent 启用写。
