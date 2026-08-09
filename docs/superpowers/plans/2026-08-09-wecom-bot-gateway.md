# WeCom 智能机器人 Gateway Adapter Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** 在已落地的入站 Gateway 上新增 `type=wecom_bot` Adapter，对接企业微信「智能机器人 AI+」HTTP 回调，完成群/单聊入站对话与 `response_url` 出站回复（含发起人+问题回显）；Portal `type=wecom` Webhook 出站不动。

**Architecture:** 企微加密 JSON 回调 → Gateway `GET|POST /hooks/wecom_bot/{channel_id}` → 验签/解密/normalize → Runtime `resolve` + `turns(final)` → 向回调中的 `response_url` POST markdown。复用 Registry / SessionRouter / RuntimeClient / Idempotency；回复卡片上下文字段留在 Adapter 本地，不扩展通用 `InboundEvent`。权威规格：[`2026-08-09-wecom-bot-gateway-design.md`](../specs/2026-08-09-wecom-bot-gateway-design.md)。官方文档：接收消息 [100719](https://developer.work.weixin.qq.com/document/path/100719)、被动回复格式 [101031](https://developer.work.weixin.qq.com/document/path/101031)、加解密 [101033](https://developer.work.weixin.qq.com/document/path/101033)、主动回复 [101138](https://developer.work.weixin.qq.com/document/path/101138)。

**Tech Stack:** Go（`gateway/`，`go 1.26`）、标准库 `crypto`（AES-CBC + PKCS#7，企微 JSON 信封）、`net/http`、现有 `gopkg.in/yaml.v3`。无新第三方依赖。

**Repos:** 改动仅在编排仓 `gateway/` + `docs/` + 可选 `_neo4j_q/` 验收脚本；**不改** `portal/` 的 `type=wecom`。**Do not commit unless asked**（文档仓提交除外若用户已要求）。

---

## Locked decisions（规格 §8 收敛）

| 项 | 锁定 |
|----|------|
| 接入模式 | **HTTP 回调 URL**（非 WebSocket 长连接）；控制台回调填 `https://<gateway>/hooks/wecom_bot/{channel_id}` |
| 加解密 | JSON 信封 `{"encrypt":"..."}`；`ReceiveId=""`（企业内部智能机器人）；Token + EncodingAESKey |
| URL 验证 | `GET ?msg_signature&timestamp&nonce&echostr` → 解密 echostr → **1s 内**返回明文（无引号/BOM/换行） |
| 入站一期 | 仅处理 `msgtype=text`；`stream` 刷新回调忽略（空包）；其它类型空包 ack（可选日志） |
| 出站一期 | **一律 async**：POST 文本回调先空包 ack → 后台 `turns(final)` → `POST response_url` markdown（**无需再加密**） |
| `response_url` | 官方：有效期 1h、**仅可调用一次**；host 形如 `qyapi.weixin.qq.com`；走现有 SSRF 校验（公网 host 可通过） |
| peer | `group` → `chat:{chatid}`；`single` → `user:{from.userid}` |
| `asker_name` | 一期 = `from.userid`（不调通讯录） |
| `question_text` | `text.content` 去掉 `@机器人名` / 纯提及壳；`quote` 可附在 Runtime content，**不替代**问题回显 |
| 配置字段 | `callback_token`、`callback_aes_key`（EncodingAESKey）、可选 `bot_id`（校验 `aibotid`）；**不需要** corp_id/secret（本期无 access_token） |
| HITL | 不实现企微确认按钮；Runtime 返回的失败/需确认文案原样进卡片；危险操作依赖 Portal fail-closed |
| 失败出站 | turns/`response_url` 失败时仍尝试向 `response_url` 发一条含发起人/问题的失败提示（若 url 仍可用）；静默禁止 |
| Portal | **不改** `type=wecom` / `send_to_wecom` |

---

## File map

| Path | Responsibility |
|------|----------------|
| `gateway/internal/channel/registry.go` | Channel 增加 `wecom_bot` 凭证字段；加载校验 |
| `gateway/internal/wecom/crypt.go` | VerifyURL / DecryptMsg / EncryptMsg（JSON；ReceiveId=""） |
| `gateway/internal/wecom/crypt_test.go` | 签名与加解密单测（自生成向量） |
| `gateway/internal/wecom/normalize.go` | 明文 JSON → peer / asker / question_text / Runtime content |
| `gateway/internal/wecom/normalize_test.go` | 剥离 @、peer、卡片正文 |
| `gateway/internal/wecom/outbound.go` | `response_url` POST markdown；超长按 20480 字节截断（url 仅一次，不可多分片） |
| `gateway/internal/wecom/outbound_test.go` | 卡片格式 + HTTP mock |
| `gateway/internal/wecom/card.go` | `FormatReplyCard(asker, question, answer)` |
| `gateway/internal/adapter/wecom_bot.go` | `GET\|POST /hooks/wecom_bot/{channel_id}` 编排 |
| `gateway/internal/adapter/wecom_bot_test.go` | 验签失败、幂等、peer 续聊、失败出站 |
| `gateway/cmd/gateway/main.go` | 注册路由 |
| `gateway/configs/channels.yaml` | 示例 `wecom_bot`（disabled 或占位） |
| `gateway/configs/config.example.yaml` | 注释说明 |
| `gateway/README.md` | 新建：智能机器人接入说明 |
| `_neo4j_q/verify_wecom_bot_gateway.ps1` | 可选：本地 mock 回调烟雾（无真实企微） |

---

### Task 1: Channel 配置扩展（`wecom_bot`）

**Files:**
- Modify: `gateway/internal/channel/registry.go`
- Modify: `gateway/internal/channel/registry_test.go`
- Modify: `gateway/configs/channels.yaml`（示例条目，`enabled: false`）

- [ ] **Step 1: Write failing test for wecom_bot fields**

```go
func TestLoad_WecomBotFields(t *testing.T) {
	const yaml = `
channels:
  - id: xiaotiancai
    type: wecom_bot
    default_agent: "00000000-0000-0000-0000-000000000001"
    enabled: true
    callback_token: "tok"
    callback_aes_key: "abcdefghijklmnopqrstuvwxyz0123456789ABCDEFG" # 43 chars
    bot_id: "AIBOTID"
`
	// write temp file, Load, Get → assert Type, CallbackToken, CallbackAESKey, BotID
}
```

- [ ] **Step 2: Run test — expect fail**

```bash
cd gateway && go test ./internal/channel/ -run TestLoad_WecomBotFields -count=1
```

Expected: unknown field / zero values.

- [ ] **Step 3: Extend Channel struct**

```go
type Channel struct {
	ID               string   `yaml:"id"`
	Type             string   `yaml:"type"`
	DefaultAgent     string   `yaml:"default_agent"`
	WebhookSecret    string   `yaml:"webhook_secret"`
	IPWhitelist      []string `yaml:"ip_whitelist"`
	Enabled          bool     `yaml:"enabled"`
	DefaultReplyMode string   `yaml:"default_reply_mode"`
	// wecom_bot
	CallbackToken  string   `yaml:"callback_token"`
	CallbackAESKey string   `yaml:"callback_aes_key"`
	BotID          string   `yaml:"bot_id"`
	BotNames       []string `yaml:"bot_names"` // 可选；空则 Strip 用 ^@\S+\s*
}
```

可选校验：`type==wecom_bot` 且 `enabled` 时 `callback_token`/`callback_aes_key` 非空、AESKey 长度 43。

- [ ] **Step 4: Tests pass**

```bash
cd gateway && go test ./internal/channel/ -count=1
```

- [ ] **Step 5: Commit**

```bash
git add gateway/internal/channel/ gateway/configs/channels.yaml
git commit -m "feat(gateway): add wecom_bot channel config fields"
```

---

### Task 2: 企微 JSON 加解密

**Files:**
- Create: `gateway/internal/wecom/crypt.go`
- Create: `gateway/internal/wecom/crypt_test.go`

参考官方 [101033](https://developer.work.weixin.qq.com/document/path/101033)：VerifyURL / DecryptMsg / EncryptMsg；**ReceiveId 传空字符串**。

- [ ] **Step 1: Write failing round-trip tests**

```go
func TestCrypt_VerifyURLDecryptEcho(t *testing.T) {
	token := "QDG6eK"
	aesKey := "abcdefghijklmnopqrstuvwxyz0123456789ABCDEFG" // 43
	c, err := NewCrypt(token, aesKey, "") // receiveID empty
	// Encrypt a known plain "hello_echo" as echostr payload using EncryptMsg helpers,
	// then VerifyURL(sig, ts, nonce, echostr) → "hello_echo"
}

func TestCrypt_DecryptMsg_JSONEnvelope(t *testing.T) {
	// EncryptMsg plaintext JSON → envelope; DecryptMsg(sig, ts, nonce, body) → plaintext
}

func TestCrypt_RejectBadSignature(t *testing.T) {
	// wrong msg_signature → error
}
```

- [ ] **Step 2: Run — expect fail**

```bash
cd gateway && go test ./internal/wecom/ -run TestCrypt_ -count=1
```

- [ ] **Step 3: Implement crypt**

实现要点（与官方 XML 版同算法，信封为 JSON）：

1. `EncodingAESKey` base64 解码得 32-byte AES key；IV = key[:16]
2. 签名：`sha1(sort(token, timestamp, nonce, encrypt))`
3. 明文缓冲：`random(16) + uint32_be(len(msg)) + msg + receiveId`（receiveId=""）
4. AES-256-CBC + PKCS#7
5. POST body：`{"encrypt":"..."}`；被动回复加密信封字段名按文档：`encrypt` / `msgsignature` / `timestamp` / `nonce`（本期主动回复走 `response_url`，EncryptMsg 主要用于单测与未来被动 stream；VerifyURL/DecryptMsg 为必需）

- [ ] **Step 4: Tests pass**

```bash
cd gateway && go test ./internal/wecom/ -count=1
```

- [ ] **Step 5: Commit**

```bash
git add gateway/internal/wecom/crypt.go gateway/internal/wecom/crypt_test.go
git commit -m "feat(gateway): WeCom JSON callback crypto for smart bot"
```

---

### Task 3: Normalize + 回复卡片

**Files:**
- Create: `gateway/internal/wecom/normalize.go`
- Create: `gateway/internal/wecom/card.go`
- Create: `gateway/internal/wecom/normalize_test.go`

- [ ] **Step 1: Failing tests**

```go
func TestStripAtMention(t *testing.T) {
	got := StripBotMention("@小天才 帮我查一下订单", []string{"小天才", "RobotA"})
	if got != "帮我查一下订单" { t.Fatalf(...) }
	// also: "@小天才" alone → empty; "hello" unchanged
}

func TestPeerID(t *testing.T) {
	if PeerID("group", "C1", "U1") != "chat:C1" { ... }
	if PeerID("single", "", "U1") != "user:U1" { ... }
}

func TestNormalizeTextMessage(t *testing.T) {
	raw := []byte(`{
	  "msgid":"M1","aibotid":"AIBOTID","chatid":"C1","chattype":"group",
	  "from":{"userid":"alice"},"response_url":"https://qyapi.weixin.qq.com/cgi-bin/aibot/response?response_code=x",
	  "msgtype":"text","text":{"content":"@小天才 今天天气如何"},
	  "quote":{"msgtype":"text","text":{"content":"昨日纪要"}}
	}`)
	ev, err := NormalizeInbound(raw, NormalizeOpts{BotNames: []string{"小天才"}, BotID: "AIBOTID"})
	// PeerID chat:C1, AskerID alice, QuestionText "今天天气如何"
	// RuntimeContent MUST be exactly (spec §4.2):
	//   [企微] 发起人=alice(alice)\n问题：今天天气如何
	// quote 可另起一行附在 RuntimeContent，但不进 QuestionText / 卡片「问题」
	// IdempotencyKey=M1, ReplyURL=response_url
}

func TestFormatReplyCard(t *testing.T) {
	s := FormatReplyCard("alice", "今天天气如何", "晴")
	// must contain:
	// 发起人：alice
	// 问题：今天天气如何
	// 晴
	// must NOT put msgid/task id into 问题 line
}
```

- [ ] **Step 2: Run — expect fail**

```bash
cd gateway && go test ./internal/wecom/ -run "TestStrip|TestPeer|TestNormalize|TestFormat" -count=1
```

- [ ] **Step 3: Implement**

```go
type Normalized struct {
	MsgID        string
	AibotID      string
	ChatType     string
	ChatID       string
	AskerID      string
	AskerName    string // == AskerID phase 1
	QuestionText string
	QuoteText    string
	ResponseURL  string
	PeerID       string
	RuntimeContent string // injected to turns
}

func FormatReplyCard(askerName, question, answer string) string {
	return fmt.Sprintf("发起人：%s\n问题：%s\n\n%s", askerName, question, answer)
}
```

`StripBotMention`：去掉前缀 `@Name`（空白分隔）；`bot_names` 来自 Channel（Task 1）；若空则用正则 `^@\S+\s*` 剥一层。

若 `BotID` 配置非空且 `aibotid` 不匹配 → error。

- [ ] **Step 4: Tests pass + Commit**

```bash
cd gateway && go test ./internal/wecom/ -count=1
git add gateway/internal/wecom/
git commit -m "feat(gateway): normalize WeCom smart-bot text and reply card"
```


---

### Task 4: `response_url` 出站

**Files:**
- Create: `gateway/internal/wecom/outbound.go`
- Create: `gateway/internal/wecom/outbound_test.go`
- Reuse: `gateway/internal/reply/ssrf.go`（`ValidateReplyURL`）

- [ ] **Step 1: Failing test with httptest**

```go
func TestPostMarkdownReply(t *testing.T) {
	os.Setenv("GATEWAY_ALLOW_LOOPBACK_REPLY", "1")
	defer os.Unsetenv("GATEWAY_ALLOW_LOOPBACK_REPLY")
	var got map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		json.NewDecoder(r.Body).Decode(&got)
		w.WriteHeader(200)
	}))
	defer srv.Close()
	err := PostMarkdown(context.Background(), srv.URL, "发起人：a\n问题：q\n\nok")
	// assert got["msgtype"]=="markdown" && nested content
}

func TestPostMarkdown_RejectSSRF(t *testing.T) {
	err := PostMarkdown(context.Background(), "http://127.0.0.1/x", "x")
	// expect error unless allow env
}
```

- [ ] **Step 2: Implement**

```go
func PostMarkdown(ctx context.Context, responseURL, content string) error {
	if err := reply.ValidateReplyURL(responseURL); err != nil { return err }
	// if utf8 byte length > 20480: truncate (keep prefix) + "…" — response_url is one-shot
	body := map[string]any{
		"msgtype": "markdown",
		"markdown": map[string]string{"content": content},
	}
	// POST JSON; non-2xx → error
}
```

失败文案辅助：

```go
func FormatFailureCard(asker, question, errMsg string) string {
	return FormatReplyCard(asker, question, "处理失败："+errMsg+"\n如需确认危险操作，请到 Web 继续。")
}
```

- [ ] **Step 3: Tests pass + Commit**

```bash
cd gateway && go test ./internal/wecom/ -run TestPostMarkdown -count=1
git add gateway/internal/wecom/
git commit -m "feat(gateway): WeCom response_url markdown outbound"
```

---

### Task 5: HTTP Adapter 编排

**Files:**
- Create: `gateway/internal/adapter/wecom_bot.go`
- Create: `gateway/internal/adapter/wecom_bot_test.go`
- Modify: `gateway/cmd/gateway/main.go`

流程（对齐 webhook async，但不复用 webhook secret 头）：

1. 查 channel；`type` 必须为 `wecom_bot`；`enabled`
2. **GET**：VerifyURL → 写明文 echo
3. **POST**：读 body → DecryptMsg → 若 `msgtype!="text"` → 200 空包 return  
4. Normalize → 幂等 `msgid`  
5. Resolve peer → Begin idempotency  
6. **立即 200 空包**（企微要求快速响应）  
7. `go`：`turns(final)` → `FormatReplyCard` / `FormatFailureCard` → `PostMarkdown(response_url)` → Complete idempotency  
8. 重复 `msgid`：**仅空包 ack，不再 POST `response_url`**（官方一次性）

GET 参数用 `r.URL.Query()`（已 Urldecode），勿手拆 `RawQuery`。

- [ ] **Step 1: Handler unit tests（httptest + fake Runtime）**

仿 `webhook_test.go`：fake Runtime Resolve/TurnsFinal；crypt 用 Task 2 的密钥加密明文回调。

```go
func TestWecomBot_GET_VerifyURL(t *testing.T) { ... }
func TestWecomBot_POST_BadSignature(t *testing.T) { ... expect 401/403 }
func TestWecomBot_POST_Text_AsyncReply(t *testing.T) {
	// encrypt text callback; assert handler returns 200 quickly;
	// wait until mock response_url received card with 发起人/问题
}
func TestWecomBot_IdempotentMsgID(t *testing.T) {
	// two POSTs same msgid → TurnsFinal called once
}
func TestWecomBot_GroupPeerContinuity(t *testing.T) {
	// two different msgid same chatid → Resolve same peer chat:C1
}
func TestWecomBot_TurnFailure_PostsFailureCard(t *testing.T) {
	// TurnsFinal error → response_url gets 处理失败
}
```

- [ ] **Step 2: Run — expect fail**

```bash
cd gateway && go test ./internal/adapter/ -run TestWecomBot_ -count=1
```

- [ ] **Step 3: Implement handler + mount**

```go
// main.go — share the SAME Sessions Router + Idempotency Store instances as webhook
h := adapter.NewWecomBotHandler(adapter.WecomBotDeps{ /* Registry, Runtime, Sessions, Idempotency, TurnTimeout */ })
mux.Handle("GET /hooks/wecom_bot/{channel_id}", h)
mux.Handle("POST /hooks/wecom_bot/{channel_id}", h)
```

Deps 与 WebhookDeps 同形：Registry, Runtime, Sessions, Idempotency, TurnTimeout；出站用 `wecom.PostMarkdown`（不必经 `reply.Dispatcher` 的 FinalPayload 形状）。

Runtime user content = `Normalized.RuntimeContent`。

- [ ] **Step 4: Tests pass**

```bash
cd gateway && go test ./... -count=1
```

- [ ] **Step 5: Commit**

```bash
git add gateway/internal/adapter/wecom_bot.go gateway/internal/adapter/wecom_bot_test.go gateway/cmd/gateway/main.go
git commit -m "feat(gateway): WeCom smart-bot inbound adapter with response_url reply"
```

---

### Task 6: 文档 + 示例配置 + 本地烟雾

**Files:**
- Create: `gateway/README.md`（若已有则 Modify）
- Modify: `gateway/configs/config.example.yaml`（注释）
- Modify: `gateway/configs/channels.yaml`（占位 `wecom_bot`，`enabled: false`）
- Create: `_neo4j_q/verify_wecom_bot_gateway.ps1`（可选）
- Modify: `docs/superpowers/specs/2026-08-09-wecom-bot-gateway-design.md` — §8 标注「已在计划收敛」

README 必写：

1. 控制台「智能机器人」→ API 模式 → 回调 URL / Token / EncodingAESKey  
2. Gateway 配置字段与 `enabled: true`  
3. 验收：URL 验证通过；单聊/群 @ 收到「发起人/问题」卡片  
4. 与 Portal 群 Webhook **凭证不可混用**

烟雾脚本（无真实企微）：

- 启动 gateway（mock portal 或跳过 turns）  
- 用测试密钥构造 GET echo + POST encrypt text  
- 断言 mock `response_url` 收到 markdown  

- [ ] **Step 1: Write README + example channel**

- [ ] **Step 2: Optional verify script**

```powershell
# _neo4j_q/verify_wecom_bot_gateway.ps1
# Uses gateway test helpers or curl against local server with fixture ciphertext
```

- [ ] **Step 3: Commit**

```bash
git add gateway/README.md gateway/configs/ docs/superpowers/specs/2026-08-09-wecom-bot-gateway-design.md _neo4j_q/verify_wecom_bot_gateway.ps1
git commit -m "docs(gateway): WeCom smart-bot setup and verify notes"
```

---

### Task 7: 手工验收清单（真人 + 企微控制台）

不写代码；实现完成后勾选：

- [ ] 控制台回调 URL 验证通过  
- [ ] 单聊文本 → 回复含「发起人」「问题」正文（非 `@机器人 msgid`）  
- [ ] 群 @ → 同群两轮同一会话续聊  
- [ ] Portal `send_to_wecom` 群 Webhook 行为不变  
- [ ] 危险确认类不在企微自动放行（Web 提示或 Runtime 失败文案）

---

## 非目标（再次确认）

- WebSocket 长连接 `aibot_subscribe`  
- 被动 stream 边生成边推（可用 `response_url` 最终一条代替）  
- 图片/文件/语音入站  
- 迁 Portal Webhook  
- 自建应用 XML 回调  

---

## 风险与注意

1. **`response_url` 一次性**：幂等重复回调不得二次 POST。  
2. **turns > 1h**：url 过期则只能打日志（极端）；默认 120s 足够。  
3. **加密 userid**：非超管创建的机器人可能拿到密文 userid——peer 仍可用该字符串保持连续性；展示名一期即该 id。  
4. **与 webhook 路由并存**：`POST /hooks/{id}` 仍是通用 webhook；`wecom_bot` 必须走 `/hooks/wecom_bot/{id}`，避免 type 混淆。
