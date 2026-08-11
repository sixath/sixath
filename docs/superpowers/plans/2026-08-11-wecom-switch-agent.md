# 企微 `/switch` 两步 Agent 绑定 Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** 企微用户可通过 `/switch` 查看带「当前」标记的白名单 Agent，并在 2 分钟内回复序号完成 `force_new` 精准绑定（本条不 Turn）。

**Architecture:** Gateway 进程内 `PendingSwitchStore` 保存 channel+peer 的选项快照与 TTL；入站在 auto-route 之前拦截 pending / `/switch`。当前绑定经 Portal **只读** `GET /runtime/v1/sessions/binding` 查询（禁止用会建 session 的 Resolve 探活）。改绑复用现有 `switchChannelAgent` 语义。

**Tech Stack:** Go、Gateway adapter/command、Portal runtime HTTP、现有 `ListChannelAgents` / Resolve force_new。

**Spec:** `docs/superpowers/specs/2026-08-11-wecom-switch-agent-design.md`

**前置：** 建议在已合并或 cherry-pick 消息级自动路由相关改动的分支上实现；本计划自包含 Portal `GetBinding`（若目标分支已有 `ChannelPeerUsecase.GetBinding`，Task 1 仅补 HTTP）。

---

## File map

| File | Responsibility |
|------|----------------|
| `portal/internal/biz/channel_peer.go` | `GetBinding`（只读，不创建） |
| `portal/internal/runtime/http.go` + `service.go` | `GET /runtime/v1/sessions/binding` |
| `gateway/internal/runtimeclient/client.go` | `GetBinding` 客户端 |
| `gateway/internal/pendingswitch/store.go` | 内存 pending map |
| `gateway/internal/command/parse.go` | `KindSwitch` / `/switch` |
| `gateway/internal/adapter/commands.go` | 名单文案、`startSwitch`、改绑清 pending |
| `gateway/internal/adapter/wecom_bot.go` | pending 拦截接线 |
| `gateway/README.md` | 文档 |

Webhook 本轮不修改 handler。

---

### Task 1: Portal 只读 GetBinding + Runtime GET

**Files:**
- Modify: `portal/internal/biz/channel_peer.go`
- Modify: `portal/internal/biz/channel_peer_test.go`（或新建小测）
- Modify: `portal/internal/runtime/service.go`
- Modify: `portal/internal/runtime/http.go`
- Modify: `portal/internal/runtime/sessions_test.go`

- [ ] **Step 1: 写失败测试（usecase）**

若尚无 `GetBinding`：

```go
func TestGetBinding_ReturnsRow(t *testing.T) { /* peerRepo 有 row → 返回 AgentID */ }
func TestGetBinding_NotFound(t *testing.T) { /* ErrNotFound */ }
```

- [ ] **Step 2: 实现 `GetBinding`**

```go
func (uc *ChannelPeerUsecase) GetBinding(ctx context.Context, channelID, peerID string) (*ChannelPeerSession, error) {
	channelID = strings.TrimSpace(channelID)
	peerID = strings.TrimSpace(peerID)
	if channelID == "" || peerID == "" {
		return nil, kratosErrors.BadRequest("INVALID_ARGUMENT", "channel_id and peer_id are required")
	}
	row, err := uc.peerRepo.Get(ctx, channelID, peerID)
	if err != nil {
		if errors.Is(err, pkgErrors.ErrNotFound) {
			return nil, pkgErrors.ErrNotFound
		}
		return nil, err
	}
	return row, nil
}
```

- [ ] **Step 3: Runtime HTTP**

注册：`r.GET("/runtime/v1/sessions/binding", svc.wrap(svc.handleGetBinding))`  
Query：`channel_id`、`peer_id`。  
200 JSON：`{"channel_id","peer_id","session_id","agent_id"}`；无映射 → **404**（与 delete 幂等不同；Gateway 将 404 视为未绑定）。

- [ ] **Step 4: 跑测**

```bash
cd D:\workspace\github\sixath\portal
go test ./internal/biz/ ./internal/runtime/ -count=1 -run "GetBinding|Binding"
```

Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add portal/internal/biz/channel_peer.go portal/internal/biz/channel_peer_test.go portal/internal/runtime
git commit -m "feat(portal): GET session binding for current agent lookup"
```

---

### Task 2: Gateway `runtimeclient.GetBinding`

**Files:**
- Modify: `gateway/internal/runtimeclient/client.go`
- Modify: `gateway/internal/runtimeclient/client_test.go`

- [ ] **Step 1: 类型 + 方法**

```go
type BindingReply struct {
	ChannelID string `json:"channel_id"`
	PeerID    string `json:"peer_id"`
	SessionID string `json:"session_id"`
	AgentID   string `json:"agent_id"`
}

func (c *Client) GetBinding(ctx context.Context, channelID, peerID string) (*BindingReply, error)
```

路径：`GET /runtime/v1/sessions/binding?channel_id=&peer_id=`  
404 → 返回 `(nil, err)`；调用方用 `errors.As`/`HTTPError.StatusCode==404` 视为未绑定。

- [ ] **Step 2: httptest 测试** — 200 与 404

- [ ] **Step 3: Commit**

```bash
git add gateway/internal/runtimeclient
git commit -m "feat(gateway): runtimeclient GetBinding"
```

---

### Task 3: `PendingSwitchStore`

**Files:**
- Create: `gateway/internal/pendingswitch/store.go`
- Create: `gateway/internal/pendingswitch/store_test.go`

- [ ] **Step 1: 失败测试**

```go
func TestStore_PutGetDelete(t *testing.T) { ... }
func TestStore_ExpiredIsMiss(t *testing.T) { /* Put expires_at past; Get → miss + deleted */ }
```

- [ ] **Step 2: 实现**

```go
type Agent struct{ ID, Name string }
type Entry struct {
	Agents    []Agent
	ExpiresAt time.Time
}
type Store struct { /* mu + map[string]Entry */ }
func (s *Store) Put(channelID, peerID string, e Entry)
func (s *Store) Get(channelID, peerID string, now time.Time) (Entry, bool)
func (s *Store) Delete(channelID, peerID string)
```

Key：`channelID + "\x00" + peerID`。`Get` 若过期则删并返回 false。

- [ ] **Step 3: 跑测 PASS → Commit**

```bash
git add gateway/internal/pendingswitch
git commit -m "feat(gateway): in-memory pending switch store"
```

---

### Task 4: `command.Parse` 支持 `/switch`

**Files:**
- Modify: `gateway/internal/command/parse.go`
- Modify: `gateway/internal/command/parse_test.go`

- [ ] **Step 1: 测试**

```go
func TestParse_Switch(t *testing.T) {
	c, ok := Parse("/switch")
	// ok && KindSwitch && Target==""
	c, ok = Parse("/SWITCH")
	// same
}
```

- [ ] **Step 2: 实现** — 增加 `KindSwitch`；`case "switch":` 忽略 rest（有 rest 仍 KindSwitch，Target 可忽略或保留 YAGNI：忽略）。

- [ ] **Step 3: Commit**

```bash
git add gateway/internal/command
git commit -m "feat(gateway): parse /switch slash command"
```

---

### Task 5: `startSwitch` 名单文案 + 命令分发

**Files:**
- Modify: `gateway/internal/adapter/commands.go`
- Create: `gateway/internal/adapter/switch_test.go`（package adapter 测文案纯函数）

- [ ] **Step 1: 纯函数测 `formatSwitchPrompt`**

输入：agents 列表、currentAgentID（可空）、now 不需要。  
断言：含「当前：」、含 `← 当前`、含「2 分钟」。

- [ ] **Step 2: 实现 `startSwitch`**

```text
list := ListChannelAgents
若空/失败 → 返回错误文案，不 Put
current := GetBinding；404 → current=""
build agents snapshot from list.Agents
Put pending TTL=2*time.Minute
return formatSwitchPrompt(...)
```

- [ ] **Step 3: `runSlashCommand`**

- `KindSwitch` → `startSwitch`（需要注入 `*pendingswitch.Store`：扩展 `runSlashCommand` 签名或经 deps 闭包；**推荐**给 `WecomBotDeps` 加 `PendingSwitch *pendingswitch.Store`，`runSlashCommand` 增加 store 参数）。
- `KindAgentSwitch` / `KindNew` / `KindUnbind` / `KindAgentList`：在执行前 `store.Delete`（list 也可删，保持干净）。

- [ ] **Step 4: Commit**

```bash
git add gateway/internal/adapter/commands.go gateway/internal/adapter/switch_test.go
git commit -m "feat(gateway): /switch lists agents and arms pending store"
```

---

### Task 6: 企微 pending 拦截 + 序号消费

**Files:**
- Modify: `gateway/internal/adapter/wecom_bot.go`
- Modify: `gateway/internal/adapter/wecom_bot_test.go`
- Modify: 组装 deps 处（`cmd/gateway` 或 StartWecomBots 调用方）——确保 `PendingSwitch: pendingswitch.New()` 非 nil

- [ ] **Step 1: 集成测（假 Portal）**

1. `/switch` → 卡片含序号；turns=0；随后 resolve 改绑仅在回 `2` 时发生  
2. 回 `2` → force_new + 确认 + turns=0  
3. 回 `hello`（窗口内）→ 提示序号；turns=0  
4. Put 过期 entry 后发业务句 → 走 Turn（可 404 agents/route）

- [ ] **Step 2: 在 `HandleWecomMsgCallback` 中，slash 之前：**

```go
if deps.PendingSwitch != nil {
  if ent, ok := deps.PendingSwitch.Get(ch.ID, n.PeerID, time.Now()); ok {
    // 若是 / 开头 → 清 pending 后 fallthrough 到 runSlashCommand（或先 Parse）
    // 若纯数字 → switchChannelAgent by ID from ent.Agents[n-1]；成功 Delete；确认卡；return
    // 否则 → 提示范围；return
  }
}
// 然后现有 runSlashCommand（含 /switch）
// 然后 prepareAutoRoute ...
```

纯数字正则：`^\s*(\d+)\s*$`（规格：`/1` 不当序号）。

改绑失败（非超时）：`Delete` pending（规格 5xx/403 清 pending）。

- [ ] **Step 3: 跑**

```bash
cd D:\workspace\github\sixath\gateway
go test ./internal/adapter/ ./internal/command/ ./internal/pendingswitch/ ./internal/runtimeclient/ -count=1
```

Expected: PASS

- [ ] **Step 4: Commit**

```bash
git add gateway/internal/adapter gateway/cmd
git commit -m "feat(gateway): wecom pending intercept for /switch digit bind"
```

---

### Task 7: README + 烟雾清单

**Files:**
- Modify: `gateway/README.md`（Slash 指令表增加 `/switch`；注明与自动路由优先级）

- [ ] **Step 1: 文档**

| `/switch` | 列出白名单（标当前）；2 分钟内回序号改绑；本条不 Turn |

优先级：pending → slash → auto-route → Resolve。

- [ ] **Step 2: Commit**

```bash
git add gateway/README.md
git commit -m "docs(gateway): document /switch two-step agent bind"
```

---

## 手动烟雾（实现后）

1. 企微渠道 ≥2 Agent；发 `/switch` 见当前标记  
2. 回正确序号 → 确认切换；再问业务走新 Agent  
3. `/switch` 后回废话 → 提示；再回序号仍有效  
4. `/switch` 后等 >2min 发业务 → 正常 Turn  
5. `/agent` / 自动 `@` 回归仍可用  

---

## Execution note

Prefer branch off `main` (or merge auto-route first). Workdir: `D:\workspace\github\sixath`（勿用嵌套 `E:\...\sixath\sixath`）。
