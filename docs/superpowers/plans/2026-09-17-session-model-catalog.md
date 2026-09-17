# Session Model Catalog Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Web 聊天可为当前会话选择平台目录中的模型（中转站 / 直连 OpenAI·DashScope）；发消息按会话覆盖动态组客户端，未选则仍用 Agent `model_config`。

**Architecture:** `model_providers` + `model_catalog` 缓存；`chat_sessions` 存 `model_provider_id`+`model`（不绑目录行 id）。管理 HTTP 对齐 MCP 手写路由。`ChatService` 通过已有 `*data.Data` 持有 catalog store，避免新 wire 服务类型。`SendMessage`/`SendMessageStream` 共用 `ResolveTurnModelConfig`，Gateway turns 因此自动吃覆盖。不改 proto：当前选择走 `GET /agents/{id}/model-choices?session_id=`。

**Tech Stack:** Go/GORM/SQLite 单测、Kratos 原始 HTTP、Vite/React `web/src`。权威规格：[2026-09-17-session-model-catalog-design.md](../specs/2026-09-17-session-model-catalog-design.md)。

---

## File map

| Path | Responsibility |
|------|----------------|
| `portal/internal/data/model/model_provider.go` | GORM `ModelProvider`、`ModelCatalogEntry` |
| `portal/internal/data/model/chat.go` | `ChatSession` 两列 |
| `portal/internal/biz/chat.go` | `ChatSession` 两字段 |
| `portal/internal/data/data.go` | AutoMigrate 新表 |
| `portal/internal/data/model_catalog.go` | store：CRUD provider/catalog、Sync、Usable、HasUsable |
| `portal/internal/data/model_catalog_test.go` | sqlite：CRUD、sync upsert、不覆盖手工展示名/隐藏、上游删除不删行 |
| `portal/internal/data/chat_mysql.go` | `sessionModelToBiz` 映射；`SetModelOverride` |
| `portal/internal/chat/turn_model.go` | `ResolveTurnModelConfig` |
| `portal/internal/chat/turn_model_test.go` | 默认 / 覆盖 / 禁用 / 缺列 / 隐藏 |
| `portal/internal/service/chat.go` | 发消息改走 Resolve；choices + PATCH |
| `portal/internal/service/model_catalog.go` | 管理 API（Create/List/Patch/Delete/Sync） |
| `portal/internal/server/model_catalog.go` | HTTP handlers |
| `portal/internal/server/http.go` | 注册路由 |
| `portal/internal/data/fork_snapshot.go` | 子会话拷贝覆盖 |
| `web/src/api/client.ts` | `modelCatalogApi` + `patchSessionModel` |
| `web/src/pages/ModelProviderList.tsx` / `ModelProviderForm.tsx` | 管理台 |
| `web/src/App.tsx` | 导航与路由 |
| `web/src/pages/ChatPage.tsx` | 会话模型下拉 |

**不要改：** `chat.proto`、Gateway 请求体、AgentForm 模型手填、Ollama 进目录。

**ACL：** 管理写接口用 `biz.CallerUserID`（**没有**导出的 `RequireCaller`；`biz.requireCaller` 未导出）。无 caller → UNAUTHORIZED，有登录即可。`model-choices` / `PATCH session/model` 与进该 Agent 会话 / 发消息相同（`GetSession` ACL）。

**Windows：** `git commit -m "msg"`，不要 bash HEREDOC。

---

### Task 1: 表结构、session 列、catalog store CRUD

**Files:**
- Create: `portal/internal/data/model/model_provider.go`
- Create: `portal/internal/data/model_catalog.go`
- Create: `portal/internal/data/model_catalog_test.go`
- Modify: `portal/internal/data/model/chat.go`
- Modify: `portal/internal/biz/chat.go`（`ChatSession` 加 `ModelProviderID string`、`Model string`）
- Modify: `portal/internal/data/chat_mysql.go`（映射 + `SetModelOverride`）
- Modify: `portal/internal/data/data.go` AutoMigrate 加入新 struct
- Modify: 所有实现 `ChatSessionRepo` 的 test fake 都补 `SetModelOverride`（签名：`SetModelOverride(ctx, sessionID, providerID, model string) error`，可 `return nil`）。完整名单：
  - `portal/internal/service/rewind_test.go` — `rewindSessRepo`
  - `portal/internal/service/chat_session_hook_wiring_test.go` — `stubSessionRepoSucceedDelete`
  - `portal/internal/chat/session_search_notify_test.go` — `stubSessionRepo`
  - `portal/internal/biz/chat_user_isolation_test.go` — `fakeChatSessionRepo`（`chat_transcript_search_test.go` 同包共用）
  - `portal/internal/biz/channel_peer_test.go` — `fakeChatSessionRepoForPeer`

- [ ] **Step 1: Write failing tests** in `model_catalog_test.go`（sqlite helper 同 `memory_units_mysql_test.go`：`file:"+t.Name()+"?mode=memory&cache=shared"`）。AutoMigrate `model.ModelProvider`、`model.ModelCatalogEntry`、`model.ChatSession`。

```go
func TestModelCatalog_CreateProviderAndManualEntry(t *testing.T) {
	db := openModelCatalogDB(t)
	st := NewModelCatalogStore(db)
	ctx := context.Background()
	p, err := st.CreateProvider(ctx, ProviderInput{
		Name: "relay", Kind: KindOpenAICompat, BaseURL: "https://relay.example/v1", APIKey: "sk-1", Enabled: true,
	})
	if err != nil || p.ID == "" || p.HasAPIKey != true {
		t.Fatalf("provider %+v err=%v", p, err)
	}
	if p.APIKey != "" { // public DTO 不得带明文
		t.Fatal("api_key leaked on create result")
	}
	got, err := st.GetProvider(ctx, p.ID)
	if err != nil || !got.HasAPIKey || got.APIKeyPlain() == "" {
		t.Fatal("store must keep key internally; GetProvider DTO still no plain json field")
	}
	e, err := st.CreateEntry(ctx, CatalogInput{ProviderID: p.ID, Model: "deepseek-v3", DisplayName: "DS V3", Source: SourceManual})
	if err != nil || e.Model != "deepseek-v3" {
		t.Fatal(err)
	}
}

func TestChatSession_SetModelOverride(t *testing.T) {
	db := openModelCatalogDB(t)
	sessRepo := &chatSessionRepo{db: db}
	ctx := context.Background()
	sess, err := sessRepo.Create(ctx, "u1", "a1", "t", "")
	if err != nil {
		t.Fatal(err)
	}
	if sess.ModelProviderID != "" || sess.Model != "" {
		t.Fatal("new session must be agent default")
	}
	if err := sessRepo.SetModelOverride(ctx, sess.ID, "prov-1", "gpt-4o"); err != nil {
		t.Fatal(err)
	}
	got, _ := sessRepo.GetByID(ctx, sess.ID)
	if got.ModelProviderID != "prov-1" || got.Model != "gpt-4o" {
		t.Fatalf("%+v", got)
	}
	if err := sessRepo.SetModelOverride(ctx, sess.ID, "", ""); err != nil {
		t.Fatal(err)
	}
	got, _ = sessRepo.GetByID(ctx, sess.ID)
	if got.ModelProviderID != "" || got.Model != "" {
		t.Fatal("clear failed")
	}
}
```

`ProviderView` / `GetProvider`：对外 `APIKey` 空、`HasAPIKey` bool；store 内部另有 `MustAPIKey(id)` 给发消息用。测试里若不想导出 `APIKeyPlain`，可用 `MustAPIKey` 断言 key 仍在。

- [ ] **Step 2:** `cd portal && go test ./internal/data -run "TestModelCatalog_CreateProviderAndManualEntry|TestChatSession_SetModelOverride" -count=1`  
  Expected: FAIL（类型/方法不存在）

- [ ] **Step 3: Minimal implementation**

`model_provider.go`:

```go
const (
	KindOpenAICompat = "openai_compat"
	KindDashScope    = "dashscope"
	SourceSync       = "sync"
	SourceManual     = "manual"
)

type ModelProvider struct {
	ID        string `gorm:"column:id;primaryKey;size:36"`
	Name      string `gorm:"column:name;size:128;not null"`
	Kind      string `gorm:"column:kind;size:32;not null"`
	BaseURL   string `gorm:"column:base_url;size:512"`
	APIKey    string `gorm:"column:api_key;size:512"`
	Enabled   bool   `gorm:"column:enabled;not null;default:1"`
	CreatedAt time.Time
	UpdatedAt time.Time
}
func (ModelProvider) TableName() string { return "model_providers" }

type ModelCatalogEntry struct {
	ID                    string `gorm:"column:id;primaryKey;size:36"`
	ProviderID            string `gorm:"column:provider_id;size:36;uniqueIndex:uk_prov_model;index"`
	Model                 string `gorm:"column:model;size:256;uniqueIndex:uk_prov_model"`
	DisplayName           string `gorm:"column:display_name;size:256"`
	Hidden                bool   `gorm:"column:hidden;not null;default:0"`
	Source                string `gorm:"column:source;size:16;not null"`
	DisplayNameOverridden bool   `gorm:"column:display_name_overridden;not null;default:0"`
	HiddenOverridden      bool   `gorm:"column:hidden_overridden;not null;default:0"`
	CreatedAt             time.Time
	UpdatedAt             time.Time
}
func (ModelCatalogEntry) TableName() string { return "model_catalog" }
```

`ChatSession` 增加：
```go
ModelProviderID string `gorm:"column:model_provider_id;size:36"`
Model           string `gorm:"column:model;size:256"`
```
**无 FK**。

`SetModelOverride`：用 `Select("model_provider_id","model").Updates(...)` 以便清空空字符串。非法「只填一个」在 **service PATCH** 拦，repo 允许写成双空。

`CreateProvider`：`kind` 只允许两常量；`openai_compat` 要求非空 `base_url`。

AutoMigrate 在 `data.go` 列表追加 `&model.ModelProvider{}, &model.ModelCatalogEntry{}`。

- [ ] **Step 4:** 同上测试 PASS。编译闸门必须覆盖 **biz + chat + service**，否则 fake 漏补要到 Task 9 才炸：

```
cd portal && go test ./internal/data ./internal/biz ./internal/chat ./internal/service -count=1
```

- [ ] **Step 5: Commit**

```
git add portal/internal/data/model/model_provider.go portal/internal/data/model/chat.go portal/internal/data/model_catalog.go portal/internal/data/model_catalog_test.go portal/internal/data/chat_mysql.go portal/internal/data/data.go portal/internal/biz/chat.go portal/internal/biz/chat_user_isolation_test.go portal/internal/biz/channel_peer_test.go portal/internal/service/rewind_test.go portal/internal/service/chat_session_hook_wiring_test.go portal/internal/chat/session_search_notify_test.go
git commit -m "feat(data): add model provider catalog and session overlay columns"
```

---

### Task 2: Sync `/v1/models`

**Files:**
- Modify: `portal/internal/data/model_catalog.go`
- Modify: `portal/internal/data/model_catalog_test.go`

- [ ] **Step 1: Failing tests**

```go
func TestModelCatalog_SyncUpsertKeepsOverrides(t *testing.T) {
	db := openModelCatalogDB(t)
	st := NewModelCatalogStore(db)
	ctx := context.Background()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/models" && r.URL.Path != "/v1/models" {
			t.Errorf("path %s", r.URL.Path)
		}
		if got := r.Header.Get("Authorization"); !strings.Contains(got, "sk-1") {
			t.Errorf("auth %s", got)
		}
		_, _ = w.Write([]byte(`{"data":[{"id":"gpt-4o"},{"id":"gpt-4"}]}`))
	}))
	t.Cleanup(srv.Close)
	p, _ := st.CreateProvider(ctx, ProviderInput{Name: "o", Kind: KindOpenAICompat, BaseURL: srv.URL, APIKey: "sk-1", Enabled: true})
	_, _ = st.CreateEntry(ctx, CatalogInput{ProviderID: p.ID, Model: "gpt-4o", DisplayName: "Custom", Source: SourceSync})
	_ = st.PatchEntry(ctx, EntryPatch{ID: /* gpt-4o id */, DisplayName: "Custom", Hidden: true}) // 或 Create 后 Patch 设 overridden
	n, err := st.Sync(ctx, p.ID)
	if err != nil || n < 1 {
		t.Fatalf("sync n=%d err=%v", n, err)
	}
	list, _ := st.ListEntries(ctx, p.ID)
	var four, fourO *CatalogView
	for i := range list {
		if list[i].Model == "gpt-4" { four = &list[i] }
		if list[i].Model == "gpt-4o" { fourO = &list[i] }
	}
	if four == nil || fourO == nil {
		t.Fatalf("list %+v", list)
	}
	if fourO.DisplayName != "Custom" || !fourO.Hidden {
		t.Fatalf("override lost %+v", fourO)
	}
}

func TestModelCatalog_SyncDoesNotDeleteMissing(t *testing.T) {
	// 第一次 sync 两个 id；第二次只返回 gpt-4o；断言 gpt-4 行仍在
}

func TestModelCatalog_SyncDashScopeRejected(t *testing.T) {
	p, _ := st.CreateProvider(..., Kind: KindDashScope, ...)
	_, err := st.Sync(ctx, p.ID)
	if err == nil {
		t.Fatal("expected error")
	}
}
```

`PatchEntry` 设展示名/隐藏时必须把对应 `*_overridden=true`。

- [ ] **Step 2:** `go test ./internal/data -run TestModelCatalog_Sync -count=1` FAIL

- [ ] **Step 3:** `Sync`：`GET {base_url}/models`（若 base_url 已含 `/v1` 则不要再拼一层；测试 server URL 无 path，请求 `/models` **或** 规范化：`strings.TrimRight(baseURL,"/")+"/models"`）。Header `Authorization: Bearer {key}`。解析 `data[].id`。upsert：新行 `source=sync`，`display_name=id`；已存在且 `!DisplayNameOverridden` 才更新 display_name；`!HiddenOverridden` 才把 hidden 保持 false（不要把已隐藏改回显示）。上游没有的 **不 Delete**。

HTTP client timeout 10s。失败返回 error，已写入的部分可在同一函数里先读后写（不必事务，但不要半删）。

- [ ] **Step 4:** PASS

- [ ] **Step 5:** `git commit -m "feat(data): sync OpenAI-compatible model catalogs"`

---

### Task 3: ResolveTurnModelConfig

**Files:**
- Create: `portal/internal/chat/turn_model.go`
- Create: `portal/internal/chat/turn_model_test.go`

- [ ] **Step 1:**

```go
func TestResolveTurnModelConfig_AgentDefault(t *testing.T) {
	agent := &biz.AgentMeta{ModelConfig: biz.ModelConfig{Provider: "openai", Model: "gpt-4", APIKey: "ak", BaseURL: "https://api.openai.com/v1", MaxOutputTokens: 4096}}
	sess := &biz.ChatSession{}
	cfg, err := ResolveTurnModelConfig(context.Background(), nil, agent, sess)
	if err != nil || cfg.Model != "gpt-4" || cfg.APIKey != "ak" || cfg.MaxOutputTokens != 4096 {
		t.Fatalf("%+v %v", cfg, err)
	}
}

func TestResolveTurnModelConfig_Override(t *testing.T) { /* fake loader returns provider+usable true; cfg.Provider=="openai" for kind openai_compat; Model from session; MaxOutputTokens still agent */ }

func TestResolveTurnModelConfig_DisabledProvider(t *testing.T) { /* errors.Is MODEL_PROVIDER_UNAVAILABLE */ }

func TestResolveTurnModelConfig_HiddenModel(t *testing.T) { /* MODEL_CHOICE_UNAVAILABLE */ }

func TestResolveTurnModelConfig_PartialOverlay(t *testing.T) {
	sess := &biz.ChatSession{ModelProviderID: "p1"} // model empty
	_, err := ResolveTurnModelConfig(...)
	if err == nil { t.Fatal() }
}
```

定义：

```go
var (
	ErrModelProviderUnavailable = kratosErrors.BadRequest("MODEL_PROVIDER_UNAVAILABLE", "model provider unavailable")
	ErrModelChoiceUnavailable   = kratosErrors.BadRequest("MODEL_CHOICE_UNAVAILABLE", "model choice unavailable")
	ErrModelChoiceInvalid       = kratosErrors.BadRequest("MODEL_CHOICE_INVALID", "model choice invalid")
)

type TurnModelLoader interface {
	GetProviderSecret(ctx context.Context, id string) (kind, baseURL, apiKey string, enabled bool, err error)
	HasUsableEntry(ctx context.Context, providerID, model string) (bool, error)
}

func ResolveTurnModelConfig(ctx context.Context, loader TurnModelLoader, agent *biz.AgentMeta, sess *biz.ChatSession) (biz.ModelConfig, error)
```

`HasUsableEntry`：enabled provider、非空 key、!hidden、model 精确匹配。

- [ ] **Step 2:** FAIL

- [ ] **Step 3:** 实现算法与 spec §6 一致。`openai_compat`→`cfg.Provider="openai"`；`dashscope`→`dashscope`。无 loader 且无覆盖时仍返回 agent cfg。有覆盖但 loader==nil → provider unavailable。

- [ ] **Step 4:** PASS `go test ./internal/chat -run TestResolveTurnModelConfig -count=1`

- [ ] **Step 5:** `git commit -m "feat(chat): resolve per-session model overlay"`

---

### Task 4: 接入 SendMessage / SendMessageStream

**Files:**
- Modify: `portal/internal/service/chat.go`（`catalog` 字段；`ProvideChatServiceWithTurnTrace` 里 `s.catalog = data.NewModelCatalogStore(d.DB())`）
- Modify: `portal/internal/data/model_catalog.go` 让 store 实现 `TurnModelLoader`（或薄适配）
- Modify: `portal/internal/service/fork.go` 不必改
- Test: `portal/internal/chat/turn_model_test.go` 已覆盖逻辑；本任务加 `TestProvideChatService_WiresCatalog` **不要**。改为在 `model_catalog` 上测 `var _ chat.TurnModelLoader = (*ModelCatalogStore)(nil)`。

- [ ] **Step 1:** 在 `chat.go` SendMessage 与 SendMessageStream 把 `chat.BuildModel(agentMeta.ModelConfig...)` 换成：

```go
cfg, err := chat.ResolveTurnModelConfig(ctx, s.catalog, agentMeta, session)
if err != nil { return ... }
m, err := chat.BuildModel(cfg.Provider, cfg.Model, cfg.APIKey, cfg.BaseURL)
```

`s.catalog` 为 nil 时 Resolve 对无覆盖仍可用；有覆盖则 400。

`SendMessageStream` 里 timeline 用的 `modelName := agentMeta.ModelConfig.Model` **必须**改成解析后的 `cfg.Model`，否则流式 UI 仍显示 Agent 默认名。日志里的 provider/model 同样用 `cfg`。

- [ ] **Step 2:** `go test ./internal/service -count=1` 编译 PASS（现有 SendMessage 测试无覆盖）。

- [ ] **Step 3:** 若 `ModelCatalogStore` 方法名与 interface 不完全一致，加适配方法，不要改 interface 语义。

- [ ] **Step 4:** `go test ./internal/service ./internal/chat -count=1` PASS

- [ ] **Step 5:** `git commit -m "feat(chat): bind session overlay when building turn model"`

Gateway `/runtime/v1/turns` 调 `SendMessageStream`，**不要**改 runtime 请求体。

---

### Task 5: HTTP 管理 + choices + PATCH

**Files:**
- Create: `portal/internal/service/model_catalog.go`（**挂在已有 `ChatService` 上**，不要新建 `ModelCatalogService` / 不要改 `wire.go` / `NewHTTPServer` 参数）
- Create: `portal/internal/service/model_catalog_test.go`
- Create: `portal/internal/server/model_catalog.go`（handler 入参 `*service.ChatService`，`http.go` 里已有 `chat`）
- Modify: `portal/internal/server/http.go`
- Modify: `portal/internal/server/http_insights_off_test.go`（或新 `http_model_catalog_route_test.go`）锁路由字符串 `/model-providers`、`/model-choices`、`/sessions/{session_id}/model`

- [ ] **Step 1: Failing service tests**（fake store + rewindSessRepo 风格）

```go
func TestSetSessionModel_RejectsUnknown(t *testing.T) {
	// usable 为空；PATCH provider+model → ErrModelChoiceInvalid
}
func TestListModelChoices_IncludesAgentDefault(t *testing.T) {
	// 返回第一项 id=agent_default，label 含 agent provider/model
	// session 有覆盖且 usable 时 selected 指向该项
}
func TestSetSessionModel_Clear(t *testing.T) {
	// choice=agent_default 后 GetByID 两列为空
}
```

Admin：`CreateProvider` 无 caller → UNAUTHORIZED。有 caller 即可：`uid, ok := biz.CallerUserID(ctx); if !ok { return kratosErrors.Unauthorized("UNAUTHORIZED", "login required") }`。不要调用不存在的 `biz.RequireCaller`。

再加一条接线测（规格 §9）：合法 PATCH 后 `ResolveTurnModelConfig`/`SendMessage` 用新 cfg。可在 `model_catalog_test.go` 用 fake loader + 内存 session：`SetSessionModel` 成功后再 `ResolveTurnModelConfig`，断言 `cfg.Model` / `cfg.Provider`。不必真打 LLM。

- [ ] **Step 2:** FAIL

- [ ] **Step 3: Handlers** 仿 `mcp_server.go` + `runWithMiddleware`。

路由：

```
r.GET  /api/v1/model-providers
r.POST /api/v1/model-providers
r.GET  /api/v1/model-providers/{id}
r.PATCH /api/v1/model-providers/{id}
r.DELETE /api/v1/model-providers/{id}
r.POST /api/v1/model-providers/{id}/sync
r.GET  /api/v1/model-catalog
r.POST /api/v1/model-catalog
r.PATCH /api/v1/model-catalog/{id}
r.DELETE /api/v1/model-catalog/{id}
r.GET  /api/v1/agents/{agent_id}/model-choices
r.PATCH /api/v1/sessions/{session_id}/model
```

`GET model-choices?session_id=`：校验 agent 可读（`agentUC.Get` 或与 ListSessions 相同 access）；items = agent_default + `ListUsable()`；`selected`：`{provider_id, model}` 或 `null`（会话空覆盖）。**不要**改 proto GetSession。

PATCH body：`{"choice":"agent_default"}` 或 `{"model_provider_id":"...","model":"..."}`。

GET provider JSON：`has_api_key`，无 `api_key` 字段。PATCH JSON **省略** `api_key`（或 `null`）不改已有密钥；显式 `""` 才清空。用 `*string` / `json.RawMessage` 区分「没传」和「空串」，不要用普通 `string` 零值当清空。

DELETE provider：级联 catalog 行（GORM constraint 或手动删 entries）；**不**清 session 列。

- [ ] **Step 4:** `go test ./internal/service ./internal/server -count=1` PASS

- [ ] **Step 5:** `git commit -m "feat(http): model provider catalog and session model patch"`

---

### Task 6: Fork 拷贝覆盖

**Files:**
- Modify: `portal/internal/data/fork_snapshot.go`
- Modify: `portal/internal/data/fork_snapshot_test.go`

- [ ] **Step 1:** 在 `TestForkSnapshot_CopiesPrefixTracesAndMemory` 或新测试：parent `SetModelOverride` 后再 Fork，child `GetByID` 两列相同。

- [ ] **Step 2:** FAIL（child 为空）

- [ ] **Step 3:** `Create` 之后若 `in.Parent.ModelProviderID != "" || in.Parent.Model != ""`，`SetModelOverride(ctx, child.ID, parent.ModelProviderID, parent.Model)`。`seedParent` 需把 biz session 的两列填上，或 fork 前 GetByID。

注意：`ForkSnapshot` 里 `in.Parent` 来自 service 的 GetSession，必须已映射两列（Task 1 `sessionModelToBiz`）。

- [ ] **Step 4:** `go test ./internal/data -run TestForkSnapshot -count=1` PASS

- [ ] **Step 5:** `git commit -m "feat(data): copy session model overlay on fork"`

---

### Task 7: Web 管理台

**Files:**
- Modify: `web/src/api/client.ts`
- Create: `web/src/pages/ModelProviderList.tsx`
- Create: `web/src/pages/ModelProviderForm.tsx`（含同步按钮、目录表：隐藏 checkbox、展示名、手工加模型、删除）
- Modify: `web/src/App.tsx`：NavLink「模型供应商」放在 MCP 与 Agent 之间；路由 `/model-providers`、`/model-providers/new`、`/model-providers/:id`

- [ ] **Step 1:** `modelCatalogApi`：list/create/update/remove providers、sync、list/create/patch/delete catalog。类型与后端 JSON 一致。`checkRet`。

- [ ] **Step 2:** 列表页抄 `McpServerList.tsx`；表单：name、kind select（`openai_compat`/`dashscope`）、base_url、api_key password、enabled。空目录文案：「同步或手工添加模型」。

**密钥：** 编辑页密码框留空时 **不要**把 `api_key: ""` 放进 PATCH body（会清空已有 key）。仅用户输入了新密钥才带 `api_key`。新建页必须提交非空 key（或允许先建后补，但 enabled 且无 key 的供应商不算 usable）。

- [ ] **Step 3:** `cd web && npx tsc --noEmit`（若 worktree 无 node_modules，用主仓 `web/node_modules/typescript`）。无因本改动的 error。

- [ ] **Step 4:** `git commit -m "feat(web): model provider admin pages"`

---

### Task 8: ChatPage 下拉

**Files:**
- Modify: `web/src/api/client.ts`（`listModelChoices(agentId, sessionId)`、`patchSessionModel`）
- Modify: `web/src/pages/ChatPage.tsx`

- [ ] **Step 1:** `sessionId`/`agentId` 变化时拉 `listModelChoices`。`<select>` 放在输入框上方，`<optgroup>` 按 `provider_name`；第一项 Agent 默认。`value`：默认用 `agent_default`，否则 `` `${provider_id}::${model}` ``。

- [ ] **Step 2:** onChange：立即 PATCH；失败 `setError` 且 select 回到 PATCH 前的值（用 state `modelChoice` 只在成功后更新）。`disabled={streaming}`。

- [ ] **Step 3:** tsc；无新错误。

- [ ] **Step 4:** `git commit -m "feat(web): session model picker on ChatPage"`

---

### Task 9: 回归

- [ ] **Step 1:**

```
cd framework && go test ./model -count=1
cd portal && go test ./internal/data ./internal/chat ./internal/service ./internal/server -count=1
```

Expected: 本功能相关 PASS。已知无关失败（如 `TestSearchSessionsWithAgentFilterRequiresAgentUse`）不要改测试去绿。

- [ ] **Step 2:** 若本地 Portal+Web 可用：建中转站、sync、聊天选模型、发一句；新会话回到默认。不可用则 MANUAL_SKIP。

- [ ] **Step 3:** 仅当有修复才再 commit。

---

## 实现时注意

- 不改 proto；choices 的 `selected` + `session_id` query 还原下拉。
- `max_output_tokens` 始终来自 Agent。
- 覆盖非法时发消息 400，**不清**会话列。
- `ListBySession` 默认 100 条与本功能无关。
- wire：`ProvideChatServiceWithTurnTrace` 注入 store；HTTP handler 一律吃已有 `ChatService`。**禁止**为本功能改 `wire.go` / `NewHTTPServer` 签名。
- 规格 §5.2 曾写「扩展 GetSession」；本计划用 `model-choices?session_id=` 的 `selected` 还原下拉，与「不改 proto」一致。
- 删除供应商不更新 session 行。
