# MemoryStore P2-F：Prefetch 配额与去重

> 状态：已交付  
> 日期：2026-07-27  
> 回链：[门面 §8.6](./2026-07-25-memory-store-facade-design.md)、[P2-A Prefetch user 车道](./2026-07-25-memory-store-user-scope-design.md)  
> 切片：**F only** — 全局 `max_total` + 跨 scope 精确去重；无 rune 预算、无分车道配额、无 Qdrant/Neo4j

---

## 0. 目标与非目标

### 目标

1. Prefetch 三路（user → session → agent）合并后做**精确去重**与**全局条数顶**。  
2. 去重键：`ContentHash(TrimSpace(content))`；先到先得（user 优先）。  
3. `max_total`：省略（YAML 未写）→ 默认 **8**；显式 `<=0` → 不截断（仍去重）。实现上用 `*int` 区分「省略」与「显式 0」。  
4. 每路 Recall `Limit` 仍由 `max_snippets`（默认 5）控制。  
5. fail-open / 围栏格式 / Prefetch 顺序不变。

### 非目标

| 项 | 说明 |
|----|------|
| 总 rune / 单条 rune 预算 | 明确不做 |
| 分车道配额（user:3 等） | 不做 |
| 语义近似去重 | 不做（仅 hash） |
| 改 Orchestrator 多 Backend | 仍单 Backend |

---

## 1. 行为

在 `StorePrefetchBackend.Prefetch`：

1. 按现逻辑三路 Recall → 收集 `[]PrefetchPart`（跳过空正文）。  
2. 按序扫描：hash 已见则跳过；否则加入（`ContentHash(TrimSpace)`）。  
3. 应用 `applyPrefetchQuota` 的 `MaxTotal *int` 规则（**默认值只在此处解析，config/Portal 不做二次默认**）：  
   - `MaxTotal == nil`（YAML 省略）→ 截断到 **8**  
   - `MaxTotal != nil && *MaxTotal <= 0` → **不截断**（仍去重）  
   - `MaxTotal != nil && *MaxTotal > 0` → 截断到 `*MaxTotal`  
4. 返回裁切后列表。

---

## 2. 配置

```yaml
memory_orchestrator_prefetch:
  enabled: true
  max_snippets: 5   # 每路 Recall limit；省略或 <=0 → backend 用 5
  max_total: 8      # 可选；省略则字段为 nil
```

类型：

```go
type MemoryOrchestratorPrefetch struct {
    // ...
    MaxSnippets int  `yaml:"max_snippets"`
    MaxTotal    *int `yaml:"max_total"` // nil=省略→backend 默认 8；非 nil 且 *v<=0=不截断
}

type StorePrefetchBackend struct {
    MaxSnippets int
    MaxTotal    *int // 同上三态；nil 时 backend 应用默认 8
}
```

Portal `BuildPrefetchMemoryOrchestrator`：**仅透传** YAML 解析结果（省略保持 `nil`），**禁止**把未配置写成 `0`（否则会误变成「不截断」）。

---

## 3. 验收

1. 跨 scope 相同正文 → 只保留 user（或先出现者）。  
2. 去重后 > max_total → 截断，保留前 N。  
3. max_total<=0 → 不截断。  
4. 默认未配置时：snippets=5、total=8。  
5. 既有 Prefetch 单测仍绿（条数未超顶时行为不变）。

---

## 4. 门面回链

门面 §8.6 Prefetch 策略增强 → **P2-F**（本规格）。
