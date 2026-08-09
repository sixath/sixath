# WeCom 智能机器人 Gateway Adapter Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** 在已落地的入站 Gateway 上新增 `type=wecom_bot` Adapter，用企业微信「智能机器人 AI+」**WebSocket 长连接**完成群/单聊入站与 `aibot_respond_msg` 出站（含发起人+问题回显）；Portal `type=wecom` Webhook 不动。

**Architecture:** Gateway 主动连 `wss://openws.work.weixin.qq.com` → `aibot_subscribe` → 收 `aibot_msg_callback` → Normalize → Runtime `resolve` + `turns(final)` → `aibot_respond_msg`（stream：先 `finish=false` 处理中，再 `finish=true` 卡片）。**无公网 HTTP 回调、无加解密。** 权威规格：[`2026-08-09-wecom-bot-gateway-design.md`](../specs/2026-08-09-wecom-bot-gateway-design.md)。官方：[长连接 101463](https://developer.work.weixin.qq.com/document/path/101463)。

**Tech Stack:** Go（`gateway/`，`go 1.26`）、WebSocket 客户端（新增依赖 `github.com/coder/websocket` 或 `github.com/gorilla/websocket`，实现时二选一并写进 go.mod）、`gopkg.in/yaml.v3`。

**Repos:** 改动在编排仓 `gateway/` + `docs/`；**不改** Portal `type=wecom`。**Do not commit unless asked**（文档提交除外若用户已要求）。

---

## Locked decisions

| 项 | 锁定 |
|----|------|
| 接入模式 | **长连接**；控制台选「使用长连接」 |
| 端点 | 默认 `wss://openws.work.weixin.qq.com`；可配 `ws_url` |
| 凭证 | `bot_id` + `secret`（长连接专用；≠ Token/AESKey） |
| 一 bot 一连接 | 新 subscribe 踢旧连接；**多 Gateway 副本勿对同一 bot 重复 enabled** |
| 入站一期 | 仅 `aibot_msg_callback` + `msgtype=text`；事件回调可忽略 |
| 出站一期 | `aibot_respond_msg` + `msgtype=stream`：先 `finish=false`「处理中…」，turns 后同 `stream.id` `finish=true` 卡片；透传 `headers.req_id` |
| 失败出站 | turns 失败仍 `finish=true` + 失败卡片（发起人/问题 + 错误提示）；禁止静默 |
| peer | `group` → `chat:{chatid}`；`single` → `user:{from.userid}` |
| `asker_name` | 配置 `corp_id`+`corp_secret` 时解析通讯录显示名；否则回退 userid |
| `question_text` | 去 `@机器人`；`quote` 可附 Runtime content，不进卡片「问题」 |
| Runtime content | 规格 §4.3 字面模板 |
| 超长 | stream content 按 **20480** 字节截断 |
| 心跳 | `cmd=ping`，间隔 **30s** |
| 重连 | 指数退避；无限重试（进程存活期间） |
| Portal | **不改** `type=wecom` / `send_to_wecom` |
| 非目标 | URL 回调路由、crypt、`response_url`、模板卡片 HITL、`aibot_send_msg` 无触发推送 |

---

## File map

| Path | Responsibility |
|------|----------------|
| `gateway/internal/channel/registry.go` | `wecom_bot`：`bot_id`/`secret`/`bot_names`/`ws_url` |
| `gateway/internal/wecom/frame.go` | 帧 JSON 结构（subscribe / ping / msg_callback / respond_msg） |
| `gateway/internal/wecom/normalize.go` | body → peer / asker / question / RuntimeContent |
| `gateway/internal/wecom/card.go` | `FormatReplyCard` / `FormatFailureCard` |
| `gateway/internal/wecom/wsclient.go` | Dial、subscribe、读循环、ping、SendRespond |
| `gateway/internal/wecom/*_test.go` | normalize/card/frame；wsclient 用 `httptest`+WS 或假连接 |
| `gateway/internal/adapter/wecom_bot.go` | Runner：启停 client；回调 → Runtime → respond |
| `gateway/internal/adapter/wecom_bot_test.go` | 幂等、peer、失败卡片、req_id 透传（mock WS + fake Runtime） |
| `gateway/cmd/gateway/main.go` | 启动 wecom_bot runners（与 webhook HTTP 并存） |
| `gateway/configs/channels.yaml` | 占位 `enabled: false` |
| `gateway/README.md` | 长连接接入说明 + 单副本约束 |

---

### Task 1: Channel 配置扩展

**Files:**
- Modify: `gateway/internal/channel/registry.go`
- Modify: `gateway/internal/channel/registry_test.go`
- Modify: `gateway/configs/channels.yaml`

- [ ] **Step 1: Failing test**

```go
func TestLoad_WecomBotLongConnFields(t *testing.T) {
	const yaml = `
channels:
  - id: xiaotiancai
    type: wecom_bot
    default_agent: "00000000-0000-0000-0000-000000000001"
    enabled: true
    bot_id: "BOTID"
    secret: "SECRET"
    bot_names: ["小天才"]
    ws_url: "wss://openws.work.weixin.qq.com"
`
	// Load → assert BotID, Secret, BotNames, WSURL
}
```

- [ ] **Step 2: Run — expect fail**

```bash
cd gateway && go test ./internal/channel/ -run TestLoad_WecomBotLongConnFields -count=1
```

- [ ] **Step 3: Extend Channel**

```go
	BotID    string   `yaml:"bot_id"`
	Secret   string   `yaml:"secret"`
	BotNames []string `yaml:"bot_names"`
	WSURL    string   `yaml:"ws_url"`
```

`type==wecom_bot && enabled` → `bot_id`/`secret` 非空。

- [ ] **Step 4: Tests pass + Commit**

```bash
cd gateway && go test ./internal/channel/ -count=1
git add gateway/internal/channel/ gateway/configs/channels.yaml
git commit -m "feat(gateway): wecom_bot long-conn channel config"
```

---

### Task 2: Normalize + 回复卡片

**Files:**
- Create: `gateway/internal/wecom/normalize.go`
- Create: `gateway/internal/wecom/card.go`
- Create: `gateway/internal/wecom/normalize_test.go`

- [ ] **Step 1: Failing tests**

```go
func TestStripBotMention(t *testing.T) { /* "@小天才 帮我查" → "帮我查"；空 bot_names 用 ^@\S+\s* */ }
func TestPeerID(t *testing.T) {
	if PeerID("group", "C1", "U1") != "chat:C1" { t.Fatal() }
	if PeerID("single", "", "U1") != "user:U1" { t.Fatal() }
}
func TestNormalizeMsgCallbackText(t *testing.T) {
	body := []byte(`{"msgid":"M1","aibotid":"BOT","chatid":"C1","chattype":"group","from":{"userid":"alice"},"msgtype":"text","text":{"content":"@小天才 今天天气如何"}}`)
	n, err := NormalizeMsgBody(body, NormalizeOpts{BotNames: []string{"小天才"}, BotID: "BOT"})
	// QuestionText=="今天天气如何"
	// RuntimeContent == "[企微] 发起人=alice(alice)\n问题：今天天气如何"
}
func TestFormatReplyCard(t *testing.T) {
	s := FormatReplyCard("alice", "今天天气如何", "晴")
	// 含「发起人：alice」「问题：今天天气如何」「晴」
}
```

- [ ] **Step 2–4: Implement, pass, commit**

```bash
cd gateway && go test ./internal/wecom/ -count=1
git add gateway/internal/wecom/
git commit -m "feat(gateway): normalize WeCom long-conn text and reply card"
```

---

### Task 3: WS 客户端（subscribe / ping / respond）

**Files:**
- Create: `gateway/internal/wecom/frame.go`
- Create: `gateway/internal/wecom/wsclient.go`
- Create: `gateway/internal/wecom/wsclient_test.go`
- Modify: `gateway/go.mod`（加 websocket 依赖）

- [ ] **Step 1: Failing tests with mock WS server**

用 `httptest` + 升级 WebSocket：

```go
func TestWSClient_SubscribeAndPing(t *testing.T) {
	// server expects first client msg cmd=aibot_subscribe with bot_id/secret
	// replies errcode=0; later receives ping
}
func TestWSClient_RespondStream(t *testing.T) {
	// SendRespond(reqID, streamID, content, finish) → server sees aibot_respond_msg
}
func TestWSClient_SubscribeRejected(t *testing.T) {
	// server returns errcode!=0 → Connect returns error
}
```

- [ ] **Step 2: Implement**

```go
type Client struct { /* conn, botID, secret, onMessage func(Frame) */ }

func (c *Client) Run(ctx context.Context) error {
	// dial → subscribe → loop: read frames; dispatch aibot_msg_callback to handler
	// ticker 30s ping; on disconnect return err for outer reconnect
}

func (c *Client) RespondStream(ctx context.Context, reqID, streamID, content string, finish bool) error
```

帧形状：

```json
{"cmd":"aibot_subscribe","headers":{"req_id":"..."},"body":{"bot_id":"...","secret":"..."}}
{"cmd":"ping","headers":{"req_id":"..."}}
{"cmd":"aibot_respond_msg","headers":{"req_id":"<callback req_id>"},"body":{"msgtype":"stream","stream":{"id":"...","finish":true,"content":"..."}}}
```

- [ ] **Step 3: Tests pass + Commit**

```bash
cd gateway && go test ./internal/wecom/ -count=1
git add gateway/internal/wecom/ gateway/go.mod gateway/go.sum
git commit -m "feat(gateway): WeCom aibot WebSocket client"
```

---

### Task 4: Adapter Runner（Runtime 编排）

**Files:**
- Create: `gateway/internal/adapter/wecom_bot.go`
- Create: `gateway/internal/adapter/wecom_bot_test.go`
- Modify: `gateway/cmd/gateway/main.go`

流程（每个 enabled `wecom_bot` channel 一个 goroutine）：

1. 外层：`for { Run(ctx); sleep backoff }` 直到 ctx cancel  
2. 收到 text 回调：  
   - Normalize；幂等 `msgid`（重复则忽略，勿二次 respond）  
   - 立即 `RespondStream(req_id, streamID, "处理中…", false)`  
   - Resolve + `TurnsFinal`  
   - 成功：`RespondStream(..., FormatReplyCard(...), true)`  
   - 失败：`RespondStream(..., FormatFailureCard(...), true)`  
3. `streamID`：可用 `msgid` 的稳定 hash/hex，同一次回调复用  

与 webhook **共用**同一 `session.Router` + `idempotency.Store` 实例。

- [ ] **Step 1: Failing tests**

```go
func TestWecomBot_TextTurn_ReplyCard(t *testing.T) { /* mock client + fake runtime */ }
func TestWecomBot_IdempotentMsgID(t *testing.T) { /* TurnsFinal once */ }
func TestWecomBot_GroupPeer(t *testing.T) { /* Resolve peer chat:C1 */ }
func TestWecomBot_TurnFailure_FailureCard(t *testing.T) {}
func TestWecomBot_ReqIDPassthrough(t *testing.T) {}
```

可用 interface：

```go
type WecomConn interface {
	RespondStream(ctx context.Context, reqID, streamID, content string, finish bool) error
}
```

- [ ] **Step 2: Implement + mount in main**

```go
// main.go after loading reg:
sessions := session.NewRouter(rt, 30*time.Second)
idem := idempotency.NewStore(10 * time.Minute)
// webhook uses sessions+idem
adapter.StartWecomBots(ctx, adapter.WecomBotDeps{Registry: reg, Runtime: rt, Sessions: sessions, Idempotency: idem, TurnTimeout: turnTimeout})
```

**不要**注册 `/hooks/wecom_bot` HTTP。

- [ ] **Step 3: `go test ./...` + Commit**

```bash
cd gateway && go test ./... -count=1
git add gateway/internal/adapter/ gateway/cmd/gateway/main.go
git commit -m "feat(gateway): WeCom smart-bot long-conn adapter runner"
```

---

### Task 5: 文档 + 示例配置

**Files:**
- Create/Modify: `gateway/README.md`
- Modify: `gateway/configs/channels.yaml`（`enabled: false` 占位）
- Modify: `gateway/configs/config.example.yaml`（注释）

README 必写：

1. 控制台选长连接 → 复制 BotID / 获取 Secret  
2. `channels.yaml` 字段  
3. **单副本约束**（同 bot 勿多实例）  
4. 与 Portal Webhook 凭证分离  
5. 验收：日志 subscribe ok → 单聊/群 @ 卡片

- [ ] **Step 1–2: Write docs + commit**

```bash
git add gateway/README.md gateway/configs/
git commit -m "docs(gateway): WeCom long-conn setup notes"
```

---

### Task 6: 手工验收

- [ ] 控制台「使用长连接」；Gateway subscribe 成功  
- [ ] 单聊 → 发起人/问题卡片  
- [ ] 群 @ 续聊 + 问题正文正确  
- [ ] Portal `send_to_wecom` 不变  
- [ ] 危险确认不自动放行  

---

## 风险

1. **多副本踢连接**：生产需 sticky 单实例跑该 channel，或外部选主。  
2. **Secret 只显示一次**：丢了要控制台重生成并改配置。  
3. **读循环阻塞**：turns 必须在独立 goroutine，避免耽误 ping。  
4. **stream 10 分钟上限**：turn timeout 保持 ≤120s。
