# 工作区记忆功能架构、设计与详细处理流程

本文档描述**工作区记忆（Memory Search / RAG）**功能的架构设计、详细设计与处理流程，供 framework 实现与 Portal 对接参考。实现语言为 **Go**。

---

## 实现状态

| 组件 | 状态 | 实现位置 |
|------|------|----------|
| 配置模型 | ✅ | `config/config.go`：MemoryConfig、MemorySearchConfig、MemorySessionsConfig 等 |
| MemorySearchManager 接口 | ✅ | `memorysearch/types.go` |
| MemoryIndexManager（builtin） | ✅ | `memorysearch/builtin.go` |
| ResolveMemorySearch | ✅ | `memorysearch/types.go` |
| ChunkMarkdown | ✅ | `memorysearch/chunk.go`（字符近似 token） |
| SQLite 存储（meta、files、chunks、chunks_fts） | ✅ | `memorysearch/builtin.go` |
| FTS 检索 | ✅ | `memorysearch/builtin.go`（FTS5，未用 bm25 排序） |
| 向量检索（可选） | ✅ | `memorysearch/builtin.go`（需传入 Embedder） |
| 混合检索 | ✅ | `memorysearch/builtin.go` |
| GetMemorySearchManager | ✅ | `memorysearch/manager.go` |
| RegisterMemorySearchTools | ✅ | `tool/memory_search.go` |
| ContextKeyWorkspaceRoot / ContextKeyAgentID | ✅ | `tool/tool.go` |
| memorysearchembed（model.Model → Embedder） | ✅ | `memorysearchembed/embedder.go` |
| **Phase 2** | | |
| MemorySessionsConfig（deltaBytes、deltaMessages） | ✅ | `config/config.go` |
| SessionTranscriptProvider 接口 | ✅ | `memorysearch/types.go` |
| SessionDirtyNotifier 接口 | ✅ | `memorysearch/types.go` |
| syncSessionFiles、NotifySessionDirty | ✅ | `memorysearch/builtin.go` |
| watch（fsnotify） | ✅ | `memorysearch/builtin.go` |
| interval（time.Ticker） | ✅ | `memorysearch/builtin.go` |
| **Phase 3** | | |
| QmdMemoryManager | ✅ | `memorysearch/qmd.go` |
| FallbackMemoryManager | ✅ | `memorysearch/fallback.go` |
| backend 选择（builtin / qmd） | ✅ | `memorysearch/manager.go` |

---

## 〇、术语与现有 memory 包的关系

| 概念 | 说明 |
|------|------|
| **工作区记忆（Memory Search）** | 本文档所述功能：对工作区记忆文件（MEMORY.md、memory/*.md）及可选的会话转录做索引与检索，供 Agent 通过 `memory_search` / `memory_get` 工具调用。 |
| **会话记忆（memory.Memory）** | framework 现有 `memory` 包：`BufferMemory` 存储最近 N 条对话，供 Agent 上下文窗口使用。 |
| **向量记忆（memory.VectorStore）** | framework 现有接口：`Add` / `Search` / `Clear`，RAG Handler 用于文档检索。 |

**关系**：工作区记忆与会话记忆、向量记忆**互补**。`MemorySearchManager` 是独立接口，不实现 `memory.VectorStore`；通过 `memory_search` / `memory_get` 工具暴露给 ReActAgent。若需与 `memory.Manager` 协同（如会话消息同时写入工作区索引），由上层（Portal/Handler）在 `AddMessage` 后按需触发 sync，本文档不强制耦合。

---

## 一、架构设计

### 1.1 总体定位

- **工作区记忆功能**：对工作区记忆文件（MEMORY.md、memory/*.md）及可选的会话转录做**索引**与**检索**，在 Agent 回答「过往工作、决策、人物、偏好、待办」等问题前先召回相关片段，实现检索增强生成（RAG）。
- **边界**：不包含通用网页检索、实时 API、非记忆文档库；仅聚焦「个人/工作区记忆」的持久化索引与语义/混合检索。

### 1.2 分层架构

```
┌─────────────────────────────────────────────────────────────────────────┐
│  Agent 工具层（tool.Registry）                                            │
│  memory_search / memory_get（RegisterMemorySearchTools 注册）             │
│  - 依赖 workspace_root（tool.ContextKeyWorkspaceRoot）                     │
└─────────────────────────────────────────────────────────────────────────┘
                                    │
                                    ▼
┌─────────────────────────────────────────────────────────────────────────┐
│  记忆服务入口层                                                            │
│  GetMemorySearchManager(cfg, agentID, workspaceRoot) (Manager, error)    │
│  - 后端选择：memory.backend = builtin | qmd                               │
│  - 若 qmd：优先 QmdMemoryManager，失败时回退到 builtin                     │
└─────────────────────────────────────────────────────────────────────────┘
                                    │
              ┌─────────────────────┼─────────────────────┐
              ▼                     ▼                     ▼
┌──────────────────────┐ ┌──────────────────────┐ ┌──────────────────────┐
│  Builtin 实现         │ │  QMD 实现（Phase 3）  │ │  配置与解析           │
│  MemoryIndexManager  │ │  QmdMemoryManager    │ │  config.MemoryConfig  │
│  - 索引/同步/检索     │ │  - 调用外部 qmd 命令  │ │  ResolveMemorySearch  │
│  - 向量 + FTS + 混合  │ │  - MCP/mcporter 可选 │ │  ResolveMemoryBackend │
└──────────────────────┘ └──────────────────────┘ └──────────────────────┘
              │
              ▼
┌─────────────────────────────────────────────────────────────────────────┐
│  Embedding 与存储层                                                       │
│  - model.Model.Embed 或 EmbeddingProvider（OpenAI/Ollama/本地）           │
│  - SQLite：meta / files / chunks / chunks_vec / chunks_fts /             │
│            embedding_cache                                                │
└─────────────────────────────────────────────────────────────────────────┘
```

### 1.3 核心组件职责

| 组件 | 职责 |
|------|------|
| **配置解析** | 将 `config.Memory`（或 `config.MemorySearch`）解析为 `ResolvedMemorySearchConfig`、`ResolvedMemoryBackendConfig`；与 `SkillsConfig`、`DataSources` 同级。 |
| **管理器入口** | 按 backend 创建或复用 `MemorySearchManager`（builtin 或 QMD）；QMD 不可用时回退到 builtin，封装为 `FallbackMemoryManager`；使用 `sync.Once` 或缓存 key 做实例复用。 |
| **Builtin 管理器** | `MemoryIndexManager`：维护 SQLite 索引、执行 sync（索引/增量）、提供 Search/ReadFile/Status、管理 fsnotify 与 cron 定时同步。 |
| **Embedding** | 复用 `model.Model.Embed` 或新增 `EmbeddingProvider` 接口；单次与批量 embedding（含缓存、重试）；维度与向量表一致。 |
| **存储** | 元数据（meta）、文件清单（files）、块与向量（chunks + chunks_vec）、全文（chunks_fts）、embedding 缓存（embedding_cache）。 |

### 1.4 数据流概览

- **写入路径**：记忆文件/会话文件 → 列表与 hash → 分块 → Embedding（含缓存）→ 写入 chunks / chunks_vec / chunks_fts；文件删除或过期 → 删除对应行。
- **读取路径**：用户或 Agent 查询 → 查询文本 Embedding（或仅 FTS）→ 向量检索和/或关键词检索 → 混合与重排（MMR/时间衰减可选）→ 返回片段列表；memory_get 按 path/from/lines 读文件片段。

---

## 二、详细设计

### 2.1 配置模型

**与 config.Config 对齐**：在 `config.Config` 中增加 `Memory` 或 `MemorySearch` 配置块，与 `Skills`、`DataSources` 同级。

```go
// config/config.go 扩展
type MemoryConfig struct {
    Backend   string              `json:"backend" yaml:"backend"`     // "builtin" | "qmd"
    Defaults  MemorySearchConfig  `json:"defaults" yaml:"defaults"`
    Qmd       *QmdConfig          `json:"qmd" yaml:"qmd"`              // backend 为 qmd 时
}

type MemorySearchConfig struct {
    Enabled   bool     `json:"enabled" yaml:"enabled"`
    Sources   []string `json:"sources" yaml:"sources"`   // ["memory"] 或 ["memory","sessions"]
    ExtraPaths []string `json:"extra_paths" yaml:"extra_paths"`
    Provider  string   `json:"provider" yaml:"provider"`  // "openai" | "ollama" | ...
    Model     string   `json:"model" yaml:"model"`
    Store    MemoryStoreConfig       `json:"store" yaml:"store"`
    Chunking MemoryChunkingConfig    `json:"chunking" yaml:"chunking"`
    Sync     MemorySyncConfig        `json:"sync" yaml:"sync"`
    Query    MemoryQueryConfig      `json:"query" yaml:"query"`
}
```

- **ResolvedMemorySearchConfig**（每 Agent，由配置解析得到）  
  - enabled, sources（Phase 1 仅支持 `["memory"]`；sessions 为 Phase 2）, extraPaths  
  - provider, model, fallback；remote（baseUrl, apiKey, headers, batch）；local（modelPath, modelCacheDir）  
  - store.path, store.vector.enabled, store.vector.extensionPath  
  - chunking.tokens, chunking.overlap  
  - sync：onSessionStart, onSearch, watch, watchDebounceMs, intervalMinutes；sessions.deltaBytes, deltaMessages（Phase 2）  
  - query：maxResults, minScore；hybrid（enabled, vectorWeight, textWeight, candidateMultiplier, mmr, temporalDecay）  
  - cache.enabled, cache.maxEntries  

- **ResolvedMemoryBackendConfig**  
  - backend: "builtin" | "qmd"  
  - citations: "on" | "off" | "auto"  
  - qmd：command, collections, searchMode, sessions, update, limits, scope 等（当 backend 为 qmd 时）  

### 2.2 存储模型（Builtin）

- **meta**：key-value，存 memory_index_meta_v1（model, provider, providerKey, sources, chunkTokens, chunkOverlap, vectorDims）。  
- **files**：path（主键）, source, hash, mtime, size；用于增量判断（hash 未变则跳过）。  
- **chunks**：id, path, source, start_line, end_line, hash, model, text, embedding, updated_at。  
- **chunks_vec**：vec0 虚拟表，id + embedding FLOAT[N]；用于向量相似度检索。  
- **chunks_fts**：fts5 虚拟表，text + 若干 UNINDEXED 列；用于全文/BM25。  
- **embedding_cache**：(provider, model, provider_key, hash) 为主键，embedding, dims, updated_at；用于避免重复调用 Embedding API。  

### 2.3 接口契约（MemorySearchManager）

Go 接口定义：

```go
type MemorySearchManager interface {
    Search(ctx context.Context, query string, opts *SearchOpts) ([]MemorySearchResult, error)
    ReadFile(ctx context.Context, params *ReadFileParams) (*ReadFileResult, error)
    Status(ctx context.Context) (*MemoryProviderStatus, error)
    Sync(ctx context.Context, params *SyncParams) error
}
```

- **Search(ctx, query, opts)**  
  - opts: MaxResults, MinScore, SessionKey（可选，Phase 2）  
  - 返回：`([]MemorySearchResult, error)`  
  - 单条：Path, StartLine, EndLine, Score, Snippet, Source, Citation  

- **ReadFile(ctx, params)**  
  - params: RelPath, From, Lines  
  - 返回：`(*ReadFileResult{Text, Path}, error)`  
  - 路径必须在记忆路径或 extraPaths 下，且为 .md  

- **Status(ctx)**  
  - 返回：`*MemoryProviderStatus`（backend, provider, model, files, chunks, vector, fts, cache 等）  

- **Sync(ctx, params)**  
  - params: Reason, Force, Progress 回调  
  - 触发索引或增量更新；内部用 `sync.Mutex` 保证同一实例仅一个 sync 进行中  

- **ProbeEmbeddingAvailability** / **ProbeVectorAvailability**  
  - 用于健康检查与诊断  

### 2.4 工具注册与 Agent 集成

在 `tool` 包中新增 `RegisterMemorySearchTools`：

```go
func RegisterMemorySearchTools(reg *Registry, getManager func(context.Context) (MemorySearchManager, error)) error
```

- `getManager` 从 context 读取 `workspace_root`、`agent_id`（或从 Request.Metadata），调用 `GetMemorySearchManager(cfg, agentID, workspaceRoot)` 得到 manager。
- 若 `workspace_root` 为空或 manager 不可用：工具执行时返回 `{ disabled: true, warning, action }`，不 panic。
- 注册时机：Handler 构建时，根据 Agent 是否启用 memory search 决定是否调用；与 `load_skill`、`read_workspace_file` 等工具共存。

### 2.5 索引与同步逻辑（Builtin）

- **是否需要全量重索引（needsFullReindex）**  
  - 条件：force 为 true、无 meta、model/provider/providerKey 变化、sources 变化、chunk 参数变化、或启用向量但 meta 无 vectorDims。  
  - 若为 true：执行 runSafeReindex（写临时库 → 原子替换）或测试用 runUnsafeReindex。  

- **增量同步**  
  - **memory 源**：listMemoryFiles（MEMORY.md、memory.md、memory/**/*.md + extraPaths）→ buildFileEntry（hash、mtime、size）→ 对每个文件，若 hash 与 files 表一致且非 force 则跳过，否则 indexFile（分块 → embed → 写 chunks/files/vec/fts）；最后删除已不在 activePaths 的 files/chunks/vec/fts。  
  - **sessions 源**（Phase 2）：listSessionFilesForAgent → 按 sessionsDirtyFiles 或全量决定是否 indexFile；同样维护 activePaths 并删除过期 path。依赖 Portal 提供会话存储、转录格式与事件通知。  

- **indexFile（单文件）**  
  - 读取内容（或使用已传入的 content）；chunkMarkdown（按 token 数 + overlap）得到 MemoryChunk[]；  
  - 先查 embedding_cache，缺失的块再调用 embedChunksInBatches / embedChunksWithBatch（OpenAI/Gemini/Voyage 等可走批量 API）；  
  - 写入 chunks（含 embedding 序列化）、chunks_vec（向量）、chunks_fts（全文）；更新 files 表。  

### 2.6 检索逻辑（Builtin）

- **无 Embedding 仅 FTS**  
  - 若未配置 provider：仅当 FTS 可用时，对查询做 extractKeywords → 多词 FTS 检索 → 合并去重、按 score 排序、minScore 过滤、截断 maxResults。  

- **有 Embedding**  
  - 若启用 hybrid 且 FTS 可用：并行执行 searchKeyword（FTS + BM25→score）与 searchVector（查询向量与 chunks_vec 余弦相似度）；  
  - mergeHybridResults（vectorWeight/textWeight、可选 MMR、可选 temporalDecay）得到统一排序；  
  - 按 minScore 过滤；若过滤后为空且有关键词结果，则用放宽的 minScore 再过滤一次以保留纯关键词高匹配；  
  - 最终截断为 maxResults 条。  

- **向量检索实现**  
  - 若 sqlite-vec 可用：SQL 中 `vec_distance_cosine(embedding, ?) ORDER BY dist ASC LIMIT N`；  
  - 若不可用（Phase 1 默认）：从 chunks 读出 embedding 列，内存中计算余弦相似度（与 `memory.InMemoryVectorStore` 类似）后排序取前 N。  

### 2.7 触发同步的入口

- **onSearch**：Search() 被调用时若 dirty 或 sessionsDirty，则启动 goroutine 异步调用 Sync(ctx, "search")，不阻塞 Search 返回。  
- **onSessionStart**：WarmSession(sessionKey) 被调用时若配置了 onSessionStart，则异步 Sync(ctx, "session-start")。  
- **watch**：使用 `github.com/fsnotify/fsnotify` 监听记忆路径下 .md 变更，防抖（watchDebounceMs）后异步 Sync(ctx, "watch")。  
- **interval**：使用 `github.com/robfig/cron` 或 `time.Ticker` 按 intervalMinutes 异步 Sync(ctx, "interval")。  
- **session-delta**（Phase 2）：会话转录更新且满足 deltaBytes/deltaMessages 时异步 Sync(ctx, "session-delta")。  
- **手动**：CLI 或 API 调用 Sync(ctx, &SyncParams{Reason: "manual", Force: true})。  

### 2.8 并发与错误处理

- **sync 互斥**：`MemoryIndexManager` 内部用 `sync.Mutex` 保护 sync 状态；若已有 sync 进行中，新 Sync 调用可返回「sync in progress」或排队等待（由实现决定）。  
- **search 与 sync 并发**：Search 允许在 sync 进行时执行，读到的是 sync 开始前的数据；不阻塞。  
- **只读错误恢复**：`runSyncWithReadonlyRecovery`：若发生 SQLite 只读错误，关闭 db、重新 Open、重试 runSync 一次；仍失败则返回错误。  
- **Embedding 失败降级**：若 provider 不可用且未配置 fallback，可降级为仅 FTS 检索；或写入 chunks 但不写 embedding，向量检索时跳过。  

### 2.9 边界情况

| 场景 | 处理 |
|------|------|
| workspace_root 为空 | 工具返回 `{ disabled: true, warning: "workspace_root not set" }` |
| Embedding 失败 | 降级为仅 FTS；或 chunks 不写 embedding，向量检索跳过 |
| 大文件分块 | 单文件 token 上限可配置；超限时分批处理，避免 OOM |
| embedding_cache 淘汰 | pruneEmbeddingCacheIfNeeded：按 LRU 或 updated_at，保留 maxEntries 条 |
| 多 Agent 共享索引 | 按 agentID 分库（不同 SQLite 文件）或分表；由 cacheKey 决定 |

---

## 三、详细处理流程

### 3.1 管理器获取流程（GetMemorySearchManager）

1. 解析 ResolvedMemoryBackendConfig（backend、qmd、citations）。  
2. 若 backend == "qmd" 且 qmd 配置有效：  
   - 尝试创建 QmdMemoryManager（mode: full 或 status）；  
   - 成功则可选地包装为 FallbackMemoryManager（primary=QMD，fallbackFactory=builtin），并缓存；  
   - 失败则打日志，继续步骤 3。  
3. 调用 MemoryIndexManager.Get(cfg, agentID, workspaceRoot)：  
   - 解析 ResolvedMemorySearchConfig；  
   - 若已存在相同 cacheKey 的实例则直接返回；  
   - 否则 createEmbeddingProvider（按 provider/remote/local/fallback）→ 打开 SQLite、EnsureSchema、读 meta、EnsureWatcher、EnsureSessionListener（Phase 2）、EnsureIntervalSync；  
   - 新建 MemoryIndexManager 实例并放入 INDEX_CACHE，返回。  
4. 返回 `(manager, nil)` 或 `(nil, error)`。  

### 3.2 索引/同步流程（RunSync）

1. 若已 closed，直接返回。  
2. 若已有 syncing 进行中，返回「sync in progress」或等待（由实现决定）。  
3. 执行 runSyncWithReadonlyRecovery（内部调用 runSync；若发生只读库错误则关闭 db、重新 Open、重试 runSync 一次）。  
4. **runSync 内部**：  
   - 创建 progress 状态（若有 progress 回调）；  
   - ensureVectorReady（加载 sqlite-vec、创建/校验 vector 表）；  
   - 读 meta；计算 needsFullReindex（force、meta 缺失或与当前 provider/model/sources/chunk 不一致等）。  
   - **若 needsFullReindex**：  
     - runSafeReindex：在临时路径建新库、ensureSchema、syncMemoryFiles + syncSessionFiles 写入新库、写 meta、原子替换目标库文件（swapIndexFiles）、删除临时与备份；  
     - 或 runUnsafeReindex（测试用，直接在原库上清空后重建）。  
   - **否则**：  
     - syncMemoryFiles：listMemoryFiles → buildFileEntry；对每个 entry 若 hash 未变则跳过，否则 indexFile(entry, "memory")；删除不在 activePaths 的 files/chunks/vec/fts；  
     - 若应同步会话：syncSessionFiles：listSessionFilesForAgent → 对每个文件（或仅 sessionsDirtyFiles）buildSessionEntry → 同上 indexFile 与清理。  
5. 写 meta（model, provider, providerKey, sources, chunkTokens, chunkOverlap, vectorDims）；置 dirty=false、sessionsDirty=false；pruneEmbeddingCacheIfNeeded。  

### 3.3 单文件索引流程（indexFile）

1. 取得文件内容（内存中或从磁盘读取）。  
2. chunkMarkdown（按 settings.chunking.tokens、overlap）→ MemoryChunk[]（startLine, endLine, text, hash）。  
3. embedChunksWithBatch：  
   - 按 chunk.hash 查 embedding_cache，得到已有 embedding；  
   - 对缺失的块：buildEmbeddingBatches（按 token 估算分批）→ 每批 embedBatchWithRetry（或 provider 的 batch API）→ upsertEmbeddingCache；  
4. 删除该 path+source 在 chunks、chunks_vec、chunks_fts 中的旧行。  
5. 对每个块：生成 id；写入 chunks（id, path, source, start_line, end_line, hash, model, text, embedding, updated_at）；写入 chunks_vec（id, embedding）；若 FTS 可用则写入 chunks_fts。  
6. 更新 files 表（path, source, hash, mtime, size）。  

### 3.4 检索流程（Search）

1. 可选：WarmSession(opts.SessionKey)（若配置 onSessionStart）。  
2. 若配置 onSearch 且 (dirty || sessionsDirty)，启动 goroutine 异步调用 Sync(ctx, "search")（不等待）。  
3. 清洗 query（trim）；解析 maxResults、minScore；计算 candidates = min(200, max(maxResults * candidateMultiplier, 1))。  
4. **无 provider（仅 FTS）**：  
   - extractKeywords(query) → 多词 FTS 检索 → 合并去重、按 score 排序、minScore 过滤、取前 maxResults；返回。  
5. **有 provider**：  
   - 若 hybrid 且 FTS 可用：并行 searchKeyword(query, candidates) 与 embedQueryWithTimeout(query) → searchVector(queryVec, candidates)。  
   - 若未启用 hybrid 或 FTS 不可用：仅 searchVector；按 minScore 过滤、取前 maxResults 返回。  
6. **混合**：mergeHybridResults（vector 与 keyword 结果、vectorWeight、textWeight、mmr、temporalDecay）→ 按 minScore 过滤；若结果为空且有关键词结果，用放宽的 minScore 再滤一次 → 取前 maxResults 返回。  

### 3.5 memory_search 工具调用流程

1. 解析 agentID（从 context 或 Request.Metadata；resolveSessionAgentID(sessionKey, config)）。  
2. ResolveMemorySearchConfig(cfg, agentID)；若为 nil，工具不注册或返回 disabled。  
3. 用户/Agent 调用 memory_search(query, maxResults?, minScore?)。  
4. GetMemorySearchManager(cfg, agentID, workspaceRoot) → 得到 manager 或 error。  
5. 若 manager == nil：返回 `map[string]any{"disabled": true, "warning": ..., "action": ...}`（JSON 序列化给模型）。  
6. manager.Search(ctx, query, opts) → rawResults。  
7. 按 memory.citations 与 sessionKey 决定是否 decorateCitations（在 snippet 后追加 "Source: path#Lstart-end"）。  
8. 若 backend 为 qmd 且配置了 maxInjectedChars，对结果做 clampResultsByInjectedChars。  
9. 返回 `map[string]any{"results": ..., "provider": ..., "model": ..., ...}`（工具 Execute 返回 any，由框架序列化）。  

### 3.6 memory_get 工具调用流程

1. 解析 path、from、lines。  
2. GetMemorySearchManager(cfg, agentID, workspaceRoot)；若无 manager 则返回 `{ path, text: "", disabled: true, error }`。  
3. manager.ReadFile(ctx, params)：  
   - 解析为绝对路径；校验在 workspace 记忆路径或 extraPaths 下且为 .md；  
   - 若未传 from/lines 则读全文件；否则按行切片；  
4. 返回 `map[string]any{"text": ..., "path": ...}`。  

### 3.7 会话源增量触发（session-delta，Phase 2）

1. 订阅会话转录更新事件（onSessionTranscriptUpdate，由 Portal 提供）。  
2. 收到更新时，按 sessionKey 累积 pendingBytes、pendingMessages。  
3. 若某会话的 pending 超过 sync.sessions.deltaBytes 或 deltaMessages，将该会话文件加入 sessionsDirtyFiles，置 sessionsDirty=true。  
4. 防抖（SESSION_DIRTY_DEBOUNCE_MS）后触发 Sync(ctx, "session-delta")。  
5. syncSessionFiles 中只对 sessionsDirtyFiles 内的文件或全量（indexAll）执行 indexFile。  

---

## 四、实现阶段

| 阶段 | 范围 | 说明 |
|------|------|------|
| **Phase 1** | memory 源 + builtin | ✅ 已实现：MEMORY.md、memory/*.md、extraPaths |
| **Phase 2** | sessions 源 | ✅ 已实现：SessionTranscriptProvider、syncSessionFiles、watch、interval、NotifySessionDirty；Portal 需实现 SessionTranscriptProvider 并注入 |
| **Phase 3** | QMD 后端 | ✅ 已实现：QmdMemoryManager、FallbackMemoryManager；需安装 qmd 命令 |

**Phase 1 技术选型（Go）**：

| 组件 | 选型 |
|------|------|
| SQLite | `modernc.org/sqlite` 或 `github.com/mattn/go-sqlite3` |
| sqlite-vec | 需确认 Go 绑定；若无，则 chunks 表存 embedding，内存计算余弦相似度 |
| FTS5 | SQLite 内置，Go 直接使用 |
| 文件监听 | `github.com/fsnotify/fsnotify` |
| Embedding | 复用 `model.Model.Embed` 或新增 `EmbeddingProvider` 接口 |

---

## 五、Phase 2 / 3 对接说明

### 5.1 Phase 2：Portal 接入 sessions 源

1. **实现 SessionTranscriptProvider**：新建 `portal/internal/memory/chat_transcript.go`，实现 `ListSessionsForAgent`（调用 `ChatUsecase.ListSessions`）与 `GetTranscript`（调用 `ListMessages` 并按 `role: content` 格式拼接为 Markdown）。
2. **注入 GetMemorySearchManager**：在构建 `getManager` 时传入 `sessionProvider`，例如 `GetMemorySearchManager(cfg, agentID, workspaceRoot, embedder, chatTranscriptProvider)`。
3. **会话更新时调用 NotifySessionDirty**：在 `CreateMessage` 或流式回复完成后，若 manager 为 `SessionDirtyNotifier`，则调用 `NotifySessionDirty(sessionID, len(content), 1)`。

### 5.2 Phase 3：启用 QMD 后端

1. **安装 qmd**：`npm install -g @tobilu/qmd` 或按 [qmd](https://github.com/tobi/qmd) 文档安装。
2. **配置**：`memory.backend: "qmd"`，`memory.qmd.command: "qmd"`，`memory.defaults.enabled: true`。
3. **行为**：QMD 可用时返回 `FallbackMemoryManager`（QMD 为主、builtin 为 fallback）；QMD 不可用时回退到 builtin。

---

## 六、与 self-improving-agent 的衔接

- **.learnings/**：可纳入 `extraPaths`，使 `.learnings/LEARNINGS.md`、`ERRORS.md` 等被索引，Agent 可通过 memory_search 检索学习记录。  
- **memory_get 与 read_workspace_file**：`memory_get` 限定在记忆路径（含 extraPaths）且仅 .md；`read_workspace_file`（见 self-improving-agent-support.md）覆盖更广。两者互补，不合并。  
- **学习记录写入后**：当 Agent 通过 `append_file` 写入 `.learnings/` 后，若配置了 watch，fsnotify 会触发 Sync；或由上层在写入后显式调用 Sync。  

---

## 七、遗漏与改进建议

### 6.1 当前遗漏

| 遗漏项 | 说明 | 建议 |
|--------|------|------|
| **Token 估算** | chunkMarkdown 按 token 数分块，但未说明如何估算 token | 采用字符数近似（如 1 token ≈ 4 字符），或引入 `github.com/pkoukk/tiktoken-go`；在 ChunkingConfig 中增加 `tokenEstimator: "chars" \| "tiktoken"` |
| **记忆路径约定** | MEMORY.md、memory/*.md 相对于 workspace_root 的规范未明确 | 约定：`{workspace_root}/MEMORY.md`、`{workspace_root}/memory.md`、`{workspace_root}/memory/**/*.md`；extraPaths 为相对 workspace_root 的路径 |
| **cacheKey 组成** | 多 Agent 实例复用时 cacheKey 未定义 | 约定：`cacheKey = hash(agentID + workspaceRoot + storePath)` 或 `agentID + ":" + workspaceRoot` |
| **store.path 语义** | SQLite 索引文件路径是否相对、多 Agent 如何隔离未说明 | 约定：可为绝对路径或相对 workspace_root；多 Agent 时按 cacheKey 分文件，如 `{storePath}/memory_{cacheKey}.db` |
| **可观测性** | 与 events.Bus、OpenTelemetry 的集成未定义 | 在 Sync/Search 关键点发布事件（如 `MemorySyncStarted`、`MemorySearchExecuted`）；工具执行沿用 `ToolExecuted` |
| **路径安全** | memory_get 的路径校验细则未写 | 与 `read_workspace_file` 一致：`filepath.Clean` 后禁止 `..`，最终路径必须在记忆路径或 extraPaths 下 |
| **查询超时** | Embedding、Search 的总体超时未定义 | 在 QueryConfig 中增加 `timeoutSeconds`；`embedQueryWithTimeout`、Search 用 `context.WithTimeout` |
| **Chunk 分隔策略** | 仅按 token 切分，未考虑 Markdown 结构 | 可选：按段落/标题边界切分（`##` 前、空行处），再按 token 微调；在 ChunkingConfig 中增加 `respectStructure: bool` |
| **Embedding 模型与 Chat 模型分离** | 记忆检索的 Embedding 可能与对话模型不同 | 配置中 `provider`、`model` 专指 Embedding；与 `config.ModelName`（Chat 模型）分离，已在 MemorySearchConfig 中体现 |
| **Ollama Embed 不可用** | framework 的 Ollama 当前 `Embed` 返回未实现 | Phase 1 若 provider=ollama，降级为仅 FTS；或要求配置独立的 Embedding 服务（如 OpenAI） |

### 6.2 功能提升建议

| 提升项 | 说明 | 优先级 |
|--------|------|--------|
| **预加载 / 预热** | 首次 Search 前若从未 Sync，可自动触发一次 Sync；或提供 `Warmup()` 显式预热 | P1 |
| **分页检索** | Search 支持 `offset`、`limit`，便于大量结果时分页返回 | P2 |
| **按 source 过滤** | Search opts 增加 `Sources []string`，仅检索 memory 或 sessions | P2 |
| **按时间范围过滤** | Search opts 增加 `From`、`To`（文件 mtime 或 chunk updated_at），便于「最近一周」类查询 | P2 |
| **结构化摘要** | 对检索结果做 LLM 摘要（如「与 X 相关的记忆有：…」），减少注入 token | P3 |
| **记忆写入工具** | 除检索外，提供 `memory_append` 工具，供 Agent 主动追加到 MEMORY.md，并触发增量 Sync | P3 |
| **跨 workspace 检索** | 多 workspace 场景下，可选检索多个 workspace 的记忆（需明确权限与隔离） | P3 |
| **检索结果去重与合并** | 同一文件相邻 chunk 在结果中合并为连续片段，减少碎片 | P2 |
| **健康检查 API** | 暴露 `ProbeEmbeddingAvailability`、`ProbeVectorAvailability` 为 HTTP 或 gRPC，供 Portal 诊断页使用 | P2 |

### 6.3 非功能提升

| 提升项 | 说明 |
|--------|------|
| **测试策略** | 单元测试：chunkMarkdown、路径校验、embedding_cache 命中、Phase 2/3 逻辑；集成测试（`go test -tags=integration ./tool/... -run MemoryIntegration`）：端到端 Sync → memory_search → memory_get；Mock Embedding 避免调用真实 API |
| **迁移与升级** | meta 表存 schema 版本；若 chunk 格式、向量维度变更，需定义迁移脚本或全量重索引策略 |
| **文档** | 用户侧文档：如何组织 MEMORY.md、memory/*.md；如何配置 extraPaths、.learnings；最佳实践（chunk 大小、overlap 建议） |

---

## 八、小结

- **实现状态**：Phase 1 已完成，见文档开头的实现状态表。  
- **架构**：Agent 工具（memory_search/memory_get）→ 记忆服务入口（后端选择 + 回退）→ Builtin（MemoryIndexManager）/ QMD → Embedding 与 SQLite（或外部命令）。  
- **与现有 memory 包**：工作区记忆与会话记忆、向量记忆互补；通过工具暴露，不实现 VectorStore。  
- **详细设计**：配置模型（与 config.Config 对齐）、存储表结构、MemorySearchManager 接口、工具注册、索引/同步规则、检索分支（仅 FTS、仅向量、混合）、同步触发源、并发与边界情况。  
- **流程**：管理器获取、RunSync（全量/增量）、indexFile（分块→embed→写表）、Search（FTS/向量/混合）、memory_search/memory_get 的端到端调用，形成完整工作区记忆功能链路。  
- **遗漏与改进**：见第七节，涵盖 token 估算、路径约定、cacheKey、可观测性、超时、Chunk 策略等遗漏项，以及预热、分页、过滤、记忆写入工具等功能提升建议。  
