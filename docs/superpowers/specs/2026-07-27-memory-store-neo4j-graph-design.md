# MemoryStore P2-I：Neo4j 图记忆

> 状态：已交付  
> 日期：2026-07-27  
> 回链：[门面 §8.5](./2026-07-25-memory-store-facade-design.md)、[P2-C Turn 提取](./2026-07-25-memory-store-turn-extract-design.md)、[P2-H Qdrant](./2026-07-27-memory-store-qdrant-design.md)、[2026-05-26 §4.4](../../../framework/docs/superpowers/specs/2026-05-26-multi-layer-memory-design.md)  
> 切片：**完整闭环** — GraphStore + Neo4j + 独立 GraphExtractor + Facade Recall RRF + supersede 失效；**不含** mysql graph fallback / Testcontainers / `source=graph` 工具

---

## 0. 目标与非目标

### 目标

1. `GraphStore` 接口 + `Neo4jGraphStore`（UpsertEntity / UpsertRelation / InvalidateByMemoryID / Expand / Close）。  
2. 节点/边按 `scope_type` + `scope_id` 分区（`user` | `session`）；Expand 不得跨分区。  
3. 独立 `GraphExtractor`（二次 LLM）+ `GraphPipeline.AddGraphFromTurn`；与 fact `AddFromTurn` 并列、可单独开关。  
4. Facade：remove/supersede/Delete → Invalidate；`Recall(source=units)` 向量命中后 Expand + RRF（无向量时关键词降级）。  
5. 配置 `memory_store.graph` + Portal 装配；失败 fail-open 不注入。

### 非目标

mysql graph fallback、Testcontainers、`source=graph`、ReindexAll、改 MCP、改现有 fact JSON schema。

---

## 1. 配置

```yaml
memory_store:
  graph:
    enabled: false
    provider: neo4j          # none | neo4j
    min_relation_confidence: 0.7
    max_hops: 1
    rrf_k: 60
    neo4j:
      uri: bolt://localhost:7687
      username: neo4j
      password: ""
      database: ""
    # auxiliary: 可选；缺省复用 extraction.auxiliary / Agent chat
```

Env：`SATH_MEMORY_GRAPH_ENABLED` 覆盖 `enabled`。

---

## 2. 数据模型

- Entity id：`sha256(scope|scopeID|normName)` 截断 hex（稳定 MERGE）。  
- 标签 `MemoryEntity`；关系类型 `REL` + 属性 `predicate`。  
- 属性含 `source_memory_id`（可选，绑定 units）。

---

## 3. 验收

1. fake：Upsert → Expand 同 scope；跨 scope 不泄漏；Invalidate 后无该 source。  
2. GraphPipeline：低置信度边丢弃；无边 → 空写。  
3. Facade Recall：向量+图 RRF（sync）；图错误回退。  
4. `provider=neo4j` 缺 uri → 不注入。  
5. 向量 sqlite/qdrant 回归仍绿。
