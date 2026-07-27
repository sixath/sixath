# MemoryStore P2-E：Units 向量 Sidecar

> 状态：已交付  
> 日期：2026-07-27  
> 回链：[门面 §8.4](./2026-07-25-memory-store-facade-design.md)、[P2-D2 LLM 语义冲突](./2026-07-26-memory-store-llm-conflict-design.md)、[2026-05-26 多层记忆 §4.3](../../../framework/docs/superpowers/specs/2026-05-26-multi-layer-memory-design.md)  
> 前置：P2-A/C/D1/D2（units + Facade + 语义冲突 peer LIKE）  
> 切片：**E only** — SQLite units 向量 + Portal Embedder + Recall hybrid + peer 向量 top-K；**不含** Qdrant / Neo4j

---

## 0. 目标与非目标

### 目标

1. 为 `memory_units`（session/user）提供可选 **SQLite 向量 Sidecar**：MySQL 权威，索引可重建。  
2. `Remember` 成功写入 active unit 后 **async** Upsert 向量；supersede/delete 同步删旧向量。  
3. `Recall(source=units)`：向量就绪时 semantic Search → hydrate；失败回退现有 LIKE。  
4. 语义冲突 peer 发现：向量就绪时用 `VectorIndex.Search` top-K；失败回退 LIKE。  
5. Portal Embedder：优先 `memory_vector.embedding`，否则 extraction auxiliary / Agent chat `Embed`；皆不可用则不注入。

### 非目标

| 项 | 归属 |
|----|------|
| Qdrant 生产 provider | 后续 |
| Neo4j 图记忆 | 后续 |
| Prefetch 配额/去重 | 后续 |
| agent workspace 非 nil embedder | 另开 |
| sqlite-vec ANN | 后续；本切片内存余弦 |

---

## 1. 接口

```go
// package memory（Facade 可见类型可放 memory；实现可放 memory/index）

type UnitVectorRecord struct {
    UnitID    string
    Scope     Scope
    ScopeID   string
    Embedding []float32
}

type VectorSearchQuery struct {
    Scope     Scope
    ScopeID   string
    Embedding []float32
    Limit     int
}

type ScoredUnitID struct {
    UnitID string
    Score  float64
}

type VectorIndex interface {
    Upsert(ctx context.Context, rec UnitVectorRecord) error
    Delete(ctx context.Context, memoryUnitID string) error
    Search(ctx context.Context, q VectorSearchQuery) ([]ScoredUnitID, error)
    Close() error
}

type EmbedFunc func(ctx context.Context, texts []string) ([][]float32, error)
```

SQLite 表：`unit_id PK, scope_type, scope_id, embedding BLOB`。

---

## 2. Facade

`FacadeConfig` 可选：`Vectors`、`Embed`、`VectorAsync`（默认 true）。

- 未注入 Vectors 或 Embed → 与现网一致。  
- 写路径 fail-open：索引失败不回滚 MySQL。  
- 空 query Recall：保持现有「最近 N」LIKE 行为（不强制 embed）。

---

## 3. Portal 配置

```yaml
memory_vector:
  enabled: false
  provider: sqlite   # none | sqlite
  store_dir: data/memory_units_vectors
  embedding:
    provider: openai
    model: text-embedding-3-small
```

Env：`SATH_MEMORY_VECTOR_ENABLED` 覆盖 enabled。

---

## 4. 验收

1. 默认关：无向量行为变化。  
2. 开 + embedding：add → 索引；语义近、字面不同可 Recall。  
3. peer 向量路径可命中矛盾对；向量失败回退 LIKE。  
4. supersede/delete 后旧 id 不再被 Search 返回。

---

## 5. 门面清单回链

门面 §8.4「向量 Sidecar」→ 本规格 **P2-E**（sqlite 切片）；**P2-H** Qdrant 见 [2026-07-27-memory-store-qdrant-design.md](./2026-07-27-memory-store-qdrant-design.md)。
