# MemoryStore P2-H：Qdrant 向量 Provider

> 状态：已交付  
> 日期：2026-07-27  
> 回链：[P2-E 向量 Sidecar](./2026-07-27-memory-store-vector-sidecar-design.md)、[门面 §8.4](./2026-07-25-memory-store-facade-design.md)  
> 切片：**H only** — Qdrant 实现现有 `VectorIndex`；配置 `provider: qdrant`；**不含** ReindexAll / Testcontainers / Neo4j

---

## 0. 目标与非目标

### 目标

1. `QdrantVectorIndex` 实现 `memory.VectorIndex`（Upsert / Delete / Search / Close）。  
2. Point id = `unit_id`（UUID string）；payload：`scope_type`、`scope_id`。  
3. Search 带 scope filter；集合不存在时按首次 embedding 维度 Cosine 创建。  
4. 配置：`memory_vector.provider: qdrant` + `qdrant.url/collection/api_key`。  
5. Portal 按 provider 装配；失败不注入 Vectors（fail-open）。

### 非目标

ReindexAll、reindex_on_start、compose 集成测、改 Facade 接口。

---

## 1. 配置

```yaml
memory_store:
  vector:
    enabled: true
    provider: qdrant
    qdrant:
      url: http://localhost:6333   # REST 口；gRPC 默认同 host:6334
      collection: sixath_memory_units
      api_key: ""
    embedding: { ... }
```

`url` 解析 host；若端口为 6333 则 gRPC 用 6334；显式 `grpc_port` 可覆盖。

---

## 2. 验收

1. fake/API stub：Upsert → Search 同 scope 命中；跨 scope 过滤。  
2. Delete 后不再命中。  
3. `provider: qdrant` 缺 url → 不注入。  
4. `provider: sqlite` 回归仍绿。
