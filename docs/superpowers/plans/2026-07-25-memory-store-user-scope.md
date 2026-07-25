# MemoryStore P2-A User Scope Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** 鍚敤 `MemoryStore` 鐨?`scope=user`锛堝悓琛?units 宸ュ叿璇诲啓 + Prefetch user 璺級锛岀己 `user_id` 鏃堕潤榛樿烦杩囷紱涓嶅惈 LLM 鎻愬彇 / 鍐茬獊 / 鍚戦噺銆?

**Architecture:** Units backend锛堝唴瀛?+ MySQL锛夊悓鏃舵湇鍔?`session` 涓?`user`锛汧acade 鍘绘帀 user stub锛岀┖ ScopeID 闈欓粯锛汸ortal 浠?`chat_sessions.user_id`锛堜紭鍏堬級鎴?`CallerUserID` 瑙ｆ瀽韬唤锛屾敞鍏?`tool.ContextKeyUserID` 涓?PrefetchQuery.UserID锛沗StorePrefetchBackend` 椤哄簭 `user 鈫?session 鈫?agent`銆?

**Tech Stack:** Go銆丮ySQL/GORM锛圥ortal锛夈€乫ramework `memory` + `tool`銆佹棦鏈?Auth/`CallerUserID`銆?

**Spec:** `docs/superpowers/specs/2026-07-25-memory-store-user-scope-design.md`

**Repos璇存槑:** `framework/`銆乣portal/` 涓哄祵濂?git锛涙敼鍔ㄥ垎鍒湪瀵瑰簲浠撳簱 commit锛涜鏍?鏈鍒掑湪 monorepo 鏍逛粨搴撱€?

---

## File Structure

| 鏂囦欢 | 璐ｄ换 |
|------|------|
| `framework/tool/tool.go` | 鏂板 `ContextKeyUserID` |
| `framework/memory/orchestrator.go` | `PrefetchQuery.UserID` |
| `framework/memory/session_memory.go` | 鍐呭瓨 units 鏀寔 `ScopeUser`锛坰copeType + scopeID锛?|
| `framework/memory/facade.go` | user 鈫?units锛涚┖ ScopeID 闈欓粯 |
| `framework/memory/facade_test.go` | 鏇挎崲銆寀ser not enabled銆嶏紱鍔犻潤榛樹笌璺?scope 娴嬭瘯 |
| `framework/memory/store_prefetch_backend.go` | user 璺紭鍏?|
| `framework/memory/store_prefetch_backend_test.go` | 涓夎矾 / 鏃?UserID 璺宠繃 |
| `framework/agent/react_agent.go` | `prefetchQueryFrom` 璇?UserID锛坢etadata + context锛?|
| `framework/tool/memory/store_tools.go` | 鍚敤 user锛沗skipped` |
| `framework/tool/memory/store_tools_test.go` | 宸ュ叿濂戠害 |
| `portal/migrations/010_memory_units_user_id.sql` | 鍙┖ `user_id` + 绱㈠紩 |
| `portal/internal/data/memory_units_mysql.go` | GORM 鍔?`UserID` |
| `portal/internal/data/memory_units_backend.go` | session+user锛涘懡涓?Scope 鍙栬嚜琛?|
| `portal/internal/data/memory_units_mysql_test.go` | user CRUD |
| `portal/internal/chat/memory_user.go`锛堟柊寤猴級 | `ResolveMemoryUserID` |
| `portal/internal/service/chat.go` | 娉ㄥ叆 ContextKeyUserID锛沵etadata `user_id` |
| `portal/internal/chat/confirm_response.go` | 鍚屼笂锛堣嫢鏈夌嫭绔?runCtx锛?|
| `portal/docs/memory-integration.md` | 鍚敤 user + 闈欓粯璇箟 |

**绂佹鏈凯浠?** `AddFromTurn`銆丆onflictResolver銆丵drant銆佹敼 `USER.md` 璇箟銆佸垹闄?`memory.Manager`銆?

---

### Task 1: ContextKeyUserID + PrefetchQuery.UserID

**Files:**
- Modify: `framework/tool/tool.go`
- Modify: `framework/memory/orchestrator.go`
- Modify: `framework/agent/react_agent.go`
- Test: `framework/agent/react_memory_orchestrator_test.go`锛堟墿灞曟垨鍚屾枃浠舵柊娴嬶級

- [x] **Step 1: 鍐欏け璐ユ祴璇曪紙PrefetchQuery 鎼哄甫 user_id锛?*

鍦?`framework/agent/react_memory_orchestrator_test.go` 澧炲姞锛堟垨鎵╁睍鐜版湁 `TestReActAgent_PrefetchQueryCarriesRecentIdentityAndContextKeys`锛夛細

```go
func TestPrefetchQueryFrom_ReadsUserIDFromContextAndMetadata(t *testing.T) {
	ctx := context.WithValue(context.Background(), tool.ContextKeyUserID, "user-ctx")
	q := prefetchQueryFrom(ctx, &Request{
		Messages: []model.Message{{Role: "user", Content: "hi"}},
		Metadata: map[string]any{"user_id": "user-meta"},
	}, nil)
	if q.UserID != "user-meta" {
		t.Fatalf("UserID = %q, want metadata override user-meta", q.UserID)
	}
	q2 := prefetchQueryFrom(ctx, &Request{
		Messages: []model.Message{{Role: "user", Content: "hi"}},
	}, nil)
	if q2.UserID != "user-ctx" {
		t.Fatalf("UserID = %q, want context user-ctx", q2.UserID)
	}
}
```

锛堣嫢 `prefetchQueryFrom` 鏈鍑猴紝娴嬭瘯鏀惧悓鍖?`agent`銆傦級

- [x] **Step 2: 杩愯纭澶辫触**

```bash
cd framework
go test ./agent/ -run TestPrefetchQueryFrom_ReadsUserIDFromContextAndMetadata -count=1
```

Expected: FAIL锛坄UserID` 瀛楁涓嶅瓨鍦ㄦ垨鎭掍负绌猴級

- [x] **Step 3: 鏈€灏忓疄鐜?*

`tool.go` 鍦?`ContextKeySessionID` 鏃侊細

```go
// ContextKeyUserID 涓哄綋鍓嶇敤鎴?id锛屼緵 memory_remember/recall(scope=user) 涓?Prefetch 浣跨敤銆?
const ContextKeyUserID = "user_id"
```

`orchestrator.go`锛?

```go
type PrefetchQuery struct {
	SessionID, AgentID, WorkspaceRoot string
	UserID                            string // 绌哄垯璺宠繃 user Prefetch 璺?
	UserMessage                       string
	Recent                            []model.Message
	Identity                          string
	Locale                            string
}
```

`prefetchQueryFrom`锛歮etadata 閿?`user_id` 浼樺厛锛屽惁鍒?`ctx.Value(tool.ContextKeyUserID)`銆?

- [x] **Step 4: 杩愯纭閫氳繃**

```bash
cd framework
go test ./agent/ -run TestPrefetchQueryFrom_ReadsUserIDFromContextAndMetadata -count=1
```

Expected: PASS

- [x] **Step 5: Commit锛坒ramework 浠撳簱锛?*

```bash
cd framework
git add tool/tool.go memory/orchestrator.go agent/react_agent.go agent/react_memory_orchestrator_test.go
git commit -m "feat(memory): add user_id to tool context and PrefetchQuery"
```

---

### Task 2: 鍐呭瓨 SessionMemory 鏀寔 ScopeUser

**Files:**
- Modify: `framework/memory/session_memory.go`
- Test: `framework/memory/session_memory_user_test.go`锛堟柊寤猴級

- [x] **Step 1: 鍐欏け璐ユ祴璇?*

```go
package memory

import (
	"context"
	"testing"
)

func TestSessionMemory_UserScopeIsolatedFromSession(t *testing.T) {
	m := NewSessionMemory()
	ctx := context.Background()
	if _, err := m.Remember(ctx, RememberInput{
		Scope: ScopeUser, ScopeID: "u1", Action: ActionAdd, Content: "prefers dark mode",
	}); err != nil {
		t.Fatalf("Remember user: %v", err)
	}
	if _, err := m.Remember(ctx, RememberInput{
		Scope: ScopeSession, ScopeID: "s1", Action: ActionAdd, Content: "session only",
	}); err != nil {
		t.Fatalf("Remember session: %v", err)
	}
	hits, err := m.Recall(ctx, RecallQuery{Scope: ScopeUser, ScopeID: "u1", Query: "dark", Source: SourceUnits})
	if err != nil || len(hits) != 1 || hits[0].Scope != ScopeUser {
		t.Fatalf("user Recall = %+v err=%v", hits, err)
	}
	sess, err := m.Recall(ctx, RecallQuery{Scope: ScopeSession, ScopeID: "s1", Query: "session", Source: SourceUnits})
	if err != nil || len(sess) != 1 || sess[0].Scope != ScopeSession {
		t.Fatalf("session Recall = %+v err=%v", sess, err)
	}
}
```

- [x] **Step 2: 杩愯纭澶辫触**

```bash
cd framework
go test ./memory/ -run TestSessionMemory_UserScopeIsolatedFromSession -count=1
```

Expected: FAIL锛堝綋鍓嶅疄鐜版妸涓€鍒囧綋 session锛屾垨鎷掔粷 user锛?

- [x] **Step 3: 鏈€灏忓疄鐜?*

鍦?`sessionUnit` 澧炲姞 `scope Scope` 涓?`scopeID string`锛坰ession 鏃?`scopeID`/`sourceSessionID` 鍧?浼氳瘽 id锛泆ser 鏃?`scopeID`=user_id锛宍sourceSessionID` 鍙┖鎴栨潵鑷?Metadata锛夈€?

`Remember`锛?

- 鍏佽 `in.Scope` 鈭?{`ScopeSession`, `ScopeUser`}锛涘叾瀹冭繑鍥?`ErrNotSupported`銆?
- 瑕佹眰 `ScopeID` 闈炵┖锛坆ackend 灞傦紱Facade 浼氬厛闈欓粯绌?ID锛夈€?
- `hit().Scope` = 璇?unit 鐨?scope銆?

`Recall`/`Get`/`List`/`Delete`锛氭寜 `q.Scope`锛堥粯璁?`ScopeSession` 浠ュ吋瀹规棫璋冪敤锛? `ScopeID` 杩囨护锛?*涓嶈**鎶?user unit 娣疯繘 session 鏌ヨ銆?

- [x] **Step 4: 杩愯纭閫氳繃**

```bash
cd framework
go test ./memory/ -run 'TestSessionMemory_|TestFacadeSession' -count=1
```

Expected: PASS锛堟棦鏈?session 娴嬭瘯涓嶅緱鐮村潖锛?

- [x] **Step 5: Commit**

```bash
cd framework
git add memory/session_memory.go memory/session_memory_user_test.go
git commit -m "feat(memory): SessionMemory supports scope=user units"
```

---

### Task 3: Facade 鍚敤 user + 闈欓粯绌?ScopeID

**Files:**
- Modify: `framework/memory/facade.go`
- Modify: `framework/memory/facade_test.go`

- [x] **Step 1: 鏀瑰啓澶辫触娴嬭瘯**

鍒犻櫎/鏇挎崲 `TestFacadeUserScopeIsNotEnabled`锛?

```go
func TestFacadeUserScopeRoutesToUnits(t *testing.T) {
	facade := NewFacade(FacadeConfig{Session: NewSessionMemory()})
	ctx := context.Background()
	hit, err := facade.Remember(ctx, RememberInput{
		Scope: ScopeUser, ScopeID: "u1", Action: ActionAdd, Content: "timezone=UTC",
	})
	if err != nil || hit.ID == "" {
		t.Fatalf("Remember user: hit=%+v err=%v", hit, err)
	}
	hits, err := facade.Recall(ctx, RecallQuery{Scope: ScopeUser, ScopeID: "u1", Query: "timezone"})
	if err != nil || len(hits) != 1 {
		t.Fatalf("Recall user: %+v err=%v", hits, err)
	}
}

func TestFacadeUserScopeSilentWhenScopeIDEmpty(t *testing.T) {
	facade := NewFacade(FacadeConfig{Session: NewSessionMemory()})
	ctx := context.Background()
	hit, err := facade.Remember(ctx, RememberInput{Scope: ScopeUser, Action: ActionAdd, Content: "x"})
	if err != nil || hit.ID != "" {
		t.Fatalf("silent Remember: hit=%+v err=%v", hit, err)
	}
	hits, err := facade.Recall(ctx, RecallQuery{Scope: ScopeUser, Query: "x"})
	if err != nil || len(hits) != 0 {
		t.Fatalf("silent Recall: %+v err=%v", hits, err)
	}
	list, err := facade.List(ctx, ListFilter{Scope: ScopeUser})
	if err != nil || len(list) != 0 {
		t.Fatalf("silent List: %+v err=%v", list, err)
	}
	if err := facade.Delete(ctx, GetRef{Scope: ScopeUser, ID: "any"}); err != nil {
		t.Fatalf("silent Delete: %v", err)
	}
}
```

- [x] **Step 2: 杩愯纭澶辫触**

```bash
cd framework
go test ./memory/ -run 'TestFacadeUserScope' -count=1
```

Expected: FAIL锛堜粛 `ErrScopeNotEnabled`锛?

- [x] **Step 3: 瀹炵幇 facade 璺敱**

```go
func userScopeIDEmpty(id string) bool { return strings.TrimSpace(id) == "" }

// Remember: ScopeUser + empty ScopeID 鈫?(MemoryHit{}, nil)
//          ScopeUser + backend 鈫?f.session.Remember
// Recall SourceUnits: 鑻?q.Scope==ScopeUser && empty ScopeID 鈫?([], nil)锛涘惁鍒欎氦缁?session backend
// Get/List/Delete: 鍚岄潤榛樿鍒欙紱Get 绌?ScopeID 鈫?not found 椋庢牸閿欒浜﹀彲锛屼絾涓嶅緱 ErrScopeNotEnabled
```

`Recall` 鍦?`source==""` 涓?`ScopeUser` 鏃堕粯璁?`SourceUnits`锛堜笌 session 鐩稿悓锛夈€?

- [x] **Step 4: 杩愯纭閫氳繃**

```bash
cd framework
go test ./memory/ -count=1
```

Expected: PASS

- [x] **Step 5: Commit**

```bash
cd framework
git add memory/facade.go memory/facade_test.go
git commit -m "feat(memory): enable scope=user on Facade with silent empty id"
```

---

### Task 4: StorePrefetchBackend 涓夎矾

**Files:**
- Modify: `framework/memory/store_prefetch_backend.go`
- Modify: `framework/memory/store_prefetch_backend_test.go`

- [x] **Step 1: 鍐?鏀规祴璇?*

鎵╁睍 `fakePrefetchStore.Recall` 鏀寔 `ScopeUser` 鈫?`userHits`銆?

```go
func TestStorePrefetchBackend_Prefetch_UserThenSessionThenAgent(t *testing.T) {
	store := &fakePrefetchStore{
		userHits:    []MemoryHit{{Content: "user pref"}},
		sessionHits: []MemoryHit{{Content: "session fact"}},
		agentHits:   []MemoryHit{{Content: "agent note"}},
	}
	b := &StorePrefetchBackend{Store: store, MaxSnippets: 3}
	parts, err := b.Prefetch(context.Background(), PrefetchQuery{
		UserID: "u1", SessionID: "s1", AgentID: "a1", WorkspaceRoot: "/ws", UserMessage: "q",
	})
	// want labels: user, session, agent in that order; 3 Recall calls
}

func TestStorePrefetchBackend_Prefetch_SkipsUserWhenNoUserID(t *testing.T) {
	// UserID ""; only session+agent calls; no ScopeUser in store.calls
}
```

鏇存柊 `TestStorePrefetchBackend_Prefetch_MergesSessionAndAgent`锛氭棤 UserID 鏃朵粛 2 璺紙鍏煎锛夈€?

- [x] **Step 2: 杩愯纭澶辫触**

```bash
cd framework
go test ./memory/ -run TestStorePrefetchBackend_Prefetch_User -count=1
```

Expected: FAIL

- [x] **Step 3: 瀹炵幇**

鍦?session 璺箣鍓嶏細

```go
if uid := strings.TrimSpace(q.UserID); uid != "" {
	userHits, err := b.Store.Recall(ctx, RecallQuery{
		Query: qText, Scope: ScopeUser, ScopeID: uid, Source: SourceUnits, Limit: limit,
	})
	// append PrefetchPart{Label: "user", ...}; firstErr 閫昏緫涓庣幇缃戜竴鑷?
}
```

- [x] **Step 4: 杩愯纭閫氳繃**

```bash
cd framework
go test ./memory/ -run TestStorePrefetchBackend -count=1
```

Expected: PASS

- [x] **Step 5: Commit**

```bash
cd framework
git add memory/store_prefetch_backend.go memory/store_prefetch_backend_test.go
git commit -m "feat(memory): prefetch user units before session and agent"
```

---

### Task 5: 宸ュ叿鍚敤 scope=user

**Files:**
- Modify: `framework/tool/memory/store_tools.go`
- Modify: `framework/tool/memory/store_tools_test.go`锛堣嫢鏃犲垯鏂板缓锛?

- [x] **Step 1: 鍐欏け璐ユ祴璇?*

```go
func TestMemoryRemember_UserScopeUsesContextUserID(t *testing.T) {
	store := memory.NewFacade(memory.FacadeConfig{Session: memory.NewSessionMemory()})
	reg := tool.NewRegistry()
	if err := RegisterMemoryStoreTools(reg, store, StoreToolsOptions{}); err != nil {
		t.Fatal(err)
	}
	ctx := context.WithValue(context.Background(), tool.ContextKeyUserID, "user-1")
	out, err := reg.Get("memory_remember").Execute(ctx, map[string]any{
		"scope": "user", "action": "add", "content": "likes concise answers",
	})
	// assert no "error" key; has id; then recall hits
}

func TestMemoryRemember_UserScopeSilentWithoutUserID(t *testing.T) {
	// Execute scope=user without ContextKeyUserID
	// want map with skipped=true, reason=user_id_missing; no error key
}
```

- [x] **Step 2: 杩愯纭澶辫触**

```bash
cd framework
go test ./tool/memory/ -run 'TestMemoryRemember_UserScope' -count=1
```

Expected: FAIL锛堜粛 `scope_not_enabled`锛?

- [x] **Step 3: 瀹炵幇宸ュ叿鍒嗘敮**

- 鍘绘帀涓夊 `scope == ScopeUser 鈫?scopeNotEnabledResult()`銆?
- `remember`/`recall`/`get`锛歚scope=user` 鏃?`ScopeID = contextString(ctx, tool.ContextKeyUserID)`锛涚┖鍒?remember 杩斿洖 `skipped`锛況ecall 杩斿洖 `{"hits":[]}`锛沢et 杩斿洖 `not_found` 鎴?`skipped`锛堜簩閫変竴锛屾帹鑽?get 鈫?`errorResult("not_found")` 涓旀棤 `scope_not_enabled`锛夈€?
- `unit_id` 鐢ㄤ簬 user replace/remove锛堜笌 session 鐩稿悓锛夈€?
- 鏇存柊 Description 鏂囨銆?

杈呭姪锛?

```go
func skippedUserIDResult() map[string]any {
	return map[string]any{"skipped": true, "reason": "user_id_missing"}
}
```

- [x] **Step 4: 杩愯纭閫氳繃**

```bash
cd framework
go test ./tool/memory/ -count=1
go test ./memory/ ./agent/ ./tool/memory/ -count=1
```

Expected: PASS

- [x] **Step 5: Commit**

```bash
cd framework
git add tool/memory/store_tools.go tool/memory/store_tools_test.go
git commit -m "feat(memory): enable memory_* tools for scope=user"
```

---

### Task 6: Portal 杩佺Щ + MySQL units 鏀寔 user

**Files:**
- Create: `portal/migrations/010_memory_units_user_id.sql`
- Modify: `portal/internal/data/memory_units_mysql.go`
- Modify: `portal/internal/data/memory_units_backend.go`
- Modify: `portal/internal/data/memory_units_mysql_test.go`

- [x] **Step 1: 鍐欏け璐ユ祴璇曪紙user CRUD锛?*

鍦?`memory_units_mysql_test.go`锛堟部鐢ㄦ棦鏈?test DB / sqlite 鑻ラ」鐩湁 skip锛夛細

```go
func TestSessionUnitsBackend_UserScope(t *testing.T) {
	// skip if no DB
	b := NewSessionUnitsBackend(db)
	hit, err := b.Remember(ctx, memory.RememberInput{
		Scope: memory.ScopeUser, ScopeID: "user-1", AgentID: "agent-1",
		Action: memory.ActionAdd, Content: "prefers UTC",
		Metadata: map[string]any{"source_session_id": "sess-9"}, // 鑻ュ悗绔粠 Metadata 鍙栧垯鐢紱鍚﹀垯 RememberInput 鏃犲崟鐙瓧娈垫椂鍐?Source锛氳 Step 3
	})
	// Recall ScopeUser ScopeID user-1 Query UTC 鈫?1 hit, Scope=user
	// Recall ScopeSession ScopeID sess-x 鈫?涓嶅惈璇ヨ
}
```

- [x] **Step 2: 杩愯纭澶辫触**

```bash
cd portal
go test ./internal/data/ -run TestSessionUnitsBackend_UserScope -count=1
```

Expected: FAIL锛坄session units only support session scope`锛?

- [x] **Step 3: 杩佺Щ + 瀹炵幇**

`010_memory_units_user_id.sql`:

```sql
ALTER TABLE memory_units
  ADD COLUMN user_id VARCHAR(36) NULL AFTER agent_id,
  ADD INDEX idx_mu_user (user_id, status);
```

GORM锛?

```go
UserID *string `gorm:"column:user_id;size:36;index:idx_mu_user,priority:1"`
```

Backend锛?

- `Remember`锛歚Scope` 鈭?{session, user}锛泆ser 鏃?`ScopeType=user`锛宍ScopeID=user_id`锛宍UserID=&user_id`锛沗SourceSessionID`锛氳嫢 input Metadata 鍚?`source_session_id` 鍒欏啓鍏ワ紝**鎴?*鏇村共鍑€锛氬湪 `RememberInput` **涓?*鎵╁瓧娈碘€斺€擯ortal 宸ュ叿灞傚彲鎶婂綋鍓?session 鏀捐繘 `Metadata["source_session_id"]`锛堝伐鍏峰湪 Task 5/7 鍐欏叆锛夈€係ession 璺緞淇濇寔鐜伴€昏緫锛坄SourceSessionID=ScopeID`锛夈€?
- 杩囨护锛歴ession 缁х画 `scope_type=session` + `source_session_id`/`scope_id`锛堜笌鐜扮綉涓€鑷达紝鍕跨牬鍧忥級锛泆ser 鐢?`scope_type=user AND scope_id=?`銆?
- `memoryUnitHit`锛歚Scope: memory.Scope(unit.ScopeType)`銆?

- [x] **Step 4: 杩愯纭閫氳繃**

```bash
cd portal
go test ./internal/data/ -run 'MemoryUnit|SessionUnits' -count=1
```

Expected: PASS 鎴栨棤 DB 鏃?Skip锛堝嬁绾級

- [x] **Step 5: Commit锛坧ortal 浠撳簱锛?*

```bash
cd portal
git add migrations/010_memory_units_user_id.sql internal/data/memory_units_mysql.go internal/data/memory_units_backend.go internal/data/memory_units_mysql_test.go
git commit -m "feat(memory): persist scope=user units with user_id column"
```

---

### Task 7: Portal 娉ㄥ叆 user_id + 鏂囨。

**Files:**
- Create: `portal/internal/chat/memory_user.go`
- Create: `portal/internal/chat/memory_user_test.go`
- Modify: `portal/internal/service/chat.go`锛堜袱澶?runCtx + `prefetchRequestMetadata`锛?
- Modify: `portal/internal/chat/confirm_response.go`锛堣嫢鏋勯€?runCtx锛?
- Modify: `portal/docs/memory-integration.md`
- Modify: `docs/superpowers/specs/2026-07-25-memory-store-user-scope-design.md` 鐘舵€?鈫?宸叉壒鍑?瀹炵幇涓紙鍙€夛級

- [x] **Step 1: 鍐?ResolveMemoryUserID 娴嬭瘯**

```go
func TestResolveMemoryUserID_PrefersSession(t *testing.T) {
	ctx := biz.WithCallerUserID(context.Background(), "caller")
	got := ResolveMemoryUserID(ctx, &biz.ChatSession{UserID: "owner"})
	if got != "owner" {
		t.Fatalf("got %q", got)
	}
}

func TestResolveMemoryUserID_FallsBackToCaller(t *testing.T) {
	ctx := biz.WithCallerUserID(context.Background(), "caller")
	got := ResolveMemoryUserID(ctx, &biz.ChatSession{UserID: ""})
	if got != "caller" {
		t.Fatalf("got %q", got)
	}
}
```

- [x] **Step 2: 杩愯纭澶辫触**

```bash
cd portal
go test ./internal/chat/ -run TestResolveMemoryUserID -count=1
```

Expected: FAIL

- [x] **Step 3: 瀹炵幇鎺ョ嚎**

```go
// memory_user.go
func ResolveMemoryUserID(ctx context.Context, session *biz.ChatSession) string {
	if session != nil {
		if id := strings.TrimSpace(session.UserID); id != "" {
			return id
		}
	}
	if id, ok := biz.CallerUserID(ctx); ok {
		return strings.TrimSpace(id)
	}
	return ""
}
```

`chat.go`锛氬湪宸叉湁 `ContextKeySessionID` 鏃侊細

```go
userID := chat.ResolveMemoryUserID(ctx, session)
if userID != "" {
	runCtx = context.WithValue(runCtx, tool.ContextKeyUserID, userID)
}
```

`prefetchRequestMetadata` 澧炲姞鍙€?`userID` 鍙傛暟锛屽啓鍏?`"user_id": userID`锛堢┖鍒欑渷鐣ワ級銆?

宸ュ叿 remember user 鏃跺湪 `store_tools` 宸茬敤 ContextKey锛涘彲閫夊湪 Metadata 鍐欏叆 `source_session_id`锛堜粠 ContextKeySessionID锛夆€斺€斿湪 Task 5 琛ヤ竴琛屽嵆鍙€?

鏂囨。鏇存柊瑕佺偣锛?

- 琛ㄦ牸锛歚user` | MySQL `memory_units` | 宸ュ叿璇诲啓 + Prefetch  
- 缂?user_id 鈫?闈欓粯 / `skipped`  
- Prefetch 椤哄簭 user鈫抯ession鈫抋gent  
- `USER.md` 瀵圭収涓嶅彉  

- [x] **Step 4: 杩愯纭閫氳繃**

```bash
cd portal
go test ./internal/chat/ -run TestResolveMemoryUserID -count=1
go test ./internal/chat/ ./internal/service/ -count=1
```

Expected: PASS锛堣€楁椂娴嬭瘯鍙?`-short` 鑻ヤ粨搴撴敮鎸侊級

- [x] **Step 5: Commit**

```bash
cd portal
git add internal/chat/memory_user.go internal/chat/memory_user_test.go internal/service/chat.go internal/chat/confirm_response.go docs/memory-integration.md
git commit -m "feat(memory): wire user_id into chat tools and prefetch"

# monorepo docs锛堣嫢鏈凯浠ｆ敼浜?spec 鐘舵€侊級
cd ..
git add docs/superpowers/specs/2026-07-25-memory-store-user-scope-design.md docs/superpowers/plans/2026-07-25-memory-store-user-scope.md
git commit -m "docs(memory): P2-A user scope plan and spec status"
```

---

### Task 8: 鍐掔儫楠屾敹娓呭崟锛堟墜宸ワ級

- [ ] **Step 1: 搴旂敤杩佺Щ** `010_memory_units_user_id.sql` 鍒版湰鍦?MySQL銆?

- [ ] **Step 2: 鐧诲綍鐢ㄦ埛寮€鑱?*锛岃皟鐢ㄦ垨璇卞妯″瀷锛?

  - `memory_remember(scope=user, action=add, content="鈥?)` 鈫?鎴愬姛鏈?id  
  - 鏂板紑**鍚岀敤鎴?*鍙︿竴 session 鈫?`memory_recall(scope=user, query=鈥?` 鍛戒腑  
  - Prefetch 鍥存爮鍑虹幇鏃跺彲瑙?user 鐗囨锛堟湁鍛戒腑鏃讹級

- [ ] **Step 3: 鏃?user 涓婁笅鏂囪矾寰?*锛堣嫢鍙瀯閫狅級鈫?remember 杩斿洖 `skipped`锛屼笉鎶ラ敊銆?

- [ ] **Step 4: 鍥炲綊** `scope=session` / `scope=agent` / `USER.md` 鍐欏叆浠嶅彲鐢ㄣ€?

---

## 椋庨櫓涓庡洖婊?

| 椋庨櫓 | 缂撹В |
|------|------|
| 鏃ф祴璇曚緷璧?`ErrScopeNotEnabled` | Task 3 鍚屾鏀规祴璇曚笌浠讳綍鏂囨。鏂█ |
| session 杩囨护鏀瑰潖 | user 鍒嗘敮鐙珛 `scope_type`锛泂ession 璺緞灏戝姩 |
| TEXT DEFAULT 绫?MySQL 閿欒 | `user_id` 鐢?`VARCHAR(36) NULL`锛屾棤 TEXT DEFAULT |
| Prefetch 鍙樻參 | user LIKE 涓?session 鍚岄檺 `MaxSnippets`锛沠ail-open |

鍥炴粴锛氳繕鍘?Facade stub + 宸ュ叿鐭矾锛涜縼绉?`user_id` 鍒楀彲淇濈暀锛堝彲绌烘棤瀹筹級銆?
