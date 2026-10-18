# 微信 iLink 渠道 P1：Portal 基础 Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Portal 支持 `type=weixin_ilink` 渠道：数据模型、扫码登录、名册（白名单）管理、Gateway 用的 Runtime API、运行状态派生，以及 Web 渠道页的扫码与名册 UI。完成后可以在 Portal 扫码登录并看到 `login_required` → 等待 Gateway（P2）上报的状态；名册由 P2 的 Gateway 写入。

**Architecture:** 新增叶子包 `internal/ilink`（iLink HTTP 客户端 + 二维码 PNG）；biz 层 `WeixinIlinkUsecase` 编排扫码 / 名册 / Runtime 操作，`WeixinIlinkRepo` 在 data 层用 GORM 实现（含 generation CAS）；Runtime API 挂在既有 `/runtime/v1/gateway/channels/{channel_id}` 下，通过 `runtime.Service.SetIlink` 注入；Admin API 为自定义路由 `/api/v1/channels/{id}/ilink/*`（经 `runWithMiddleware` 走登录鉴权）。`wire_gen.go` 手工接线（该文件含手写调用，**不要**重新运行 wire）。

**Tech Stack:** Go 1.26、Kratos v2.9.2、GORM（MySQL / 测试用 SQLite）、`github.com/skip2/go-qrcode`、protoc 34；React 19 + TypeScript（无 UI 库）、`node --test`。

**Spec:** `docs/superpowers/specs/2026-10-18-weixin-ilink-channel-design.md`（§3 协议、§5 数据模型、§5.5 状态派生、§5.6 Runtime API、§6.1 扫码、§6.4 名册）

**不在本计划内：** Gateway 长轮询（P2）；Cron 投递、`send_to_weixin`、推回发起人、`Push`（P3）。

---

## 约定

- 除特别说明，Go 命令在 `portal/` 目录执行；每个 shell 会话先执行 `Remove-Item Env:GOROOT -ErrorAction SilentlyContinue`（本机残留旧 GOROOT，否则编译器版本不匹配）。
- Web 命令在 `web/` 目录执行。
- 工作区 `main` 上已有与本计划无关的未提交改动（`evals/runner/runner.exe`、`sixath.exe`、`web/tsconfig.tsbuildinfo`、`evals/results/*.log` 等）。**只 `git add` 每个任务列出的文件**，禁止 `git add -A` / `git add .` / `git commit -a`。
- `gofmt -w` 只作用于本任务改动的文件（避免顺带改动无关文件）；Task 12 再用 `gofmt -l` 检查本计划涉及的文件。
- 新增 Go 注释只写代码本身表达不了的约束。

## 文件结构

| 文件 | 职责 |
|------|------|
| `gateway/internal/adapter/webhook.go` | `/hooks` 拒绝非 webhook 类型渠道（安全前置，Task 0A） |
| `portal/internal/ilink/client.go`（新） | iLink HTTP 客户端：`GetBotQRCode`、`GetQRCodeStatus`、`SendText`、`APIError` / `IsSessionExpired` |
| `portal/internal/ilink/qrcode.go`（新） | 二维码内容 → PNG data URL |
| `portal/internal/ilink/*_test.go`（新） | 客户端与二维码测试 |
| `portal/internal/data/model/channel.go` | `Channel` 新增 `Ilink*` 列 |
| `portal/internal/data/model/channel_ilink.go`（新） | `ChannelIlinkPeer`、`ChannelIlinkState` |
| `portal/internal/data/data.go` | AutoMigrate 注册新模型；ProviderSet 注册 `NewWeixinIlinkRepo` |
| `portal/migrations/023_weixin_ilink_channel.sql`（新） | 手工 DDL 记录 |
| `portal/internal/biz/channel.go` | `ChannelMeta` 新增 `Ilink*` 字段 |
| `portal/internal/data/channel_mysql.go` | `channelModelToBiz` 映射、`ListGatewayChannels` 类型过滤、`Delete` 级联 |
| `portal/internal/biz/identity.go` + `data/auth_ephemeral_mysql.go` | `AuthEphemeralRepo.Get`（非消费读） |
| `portal/internal/biz/channel_runtime.go` | `DeriveRuntimeStatus` 支持 `weixin_ilink` |
| `portal/internal/biz/weixin_ilink.go`（新） | 类型、错误、`WeixinIlinkRepo` 接口 |
| `portal/internal/data/channel_ilink_mysql.go`（新） | `WeixinIlinkRepo` GORM 实现 |
| `portal/internal/biz/weixin_ilink_usecase.go`（新） | `WeixinIlinkUsecase` |
| `portal/internal/biz/biz.go` | ProviderSet 注册 `NewWeixinIlinkUsecase` |
| `portal/internal/runtime/ilink.go`（新） | Runtime API 处理器、网关下发字段填充 |
| `portal/internal/runtime/service.go` / `http.go` | `Service.ilink` 字段、`gatewayChannelItem` 新字段、路由注册 |
| `portal/internal/server/weixin_ilink.go`（新） | Admin 扫码 / 名册处理器 |
| `portal/internal/server/http.go` | `NewHTTPServer` 新参数与路由 |
| `portal/cmd/backend/wire_gen.go` | 手工接线 |
| `portal/api/channel/v1/channel.proto` + 生成文件 + `portal/openapi.yaml` | `ChannelReply` 新字段 |
| `portal/internal/service/channel.go` | `channelMetaToReply` 映射新字段 |
| `web/src/api/client.ts` | 类型、`normalizeChannel`、`ilinkApi` |
| `web/src/utils/ilink.ts`（新） + `web/tests/ilink.test.ts`（新） | 纯函数与测试 |
| `web/src/components/IlinkLoginDialog.tsx`（新） | 扫码弹窗 |
| `web/src/components/IlinkRosterPanel.tsx`（新） | 名册面板 |
| `web/src/pages/ChannelForm.tsx` / `ChannelList.tsx` / `web/src/App.css` | 渠道页接入 |

---

### Task 0: 基线

**Files:** 无

- [ ] **Step 1: 记录基线测试结果**

```powershell
Set-Location portal
Remove-Item Env:GOROOT -ErrorAction SilentlyContinue
go build ./...
go test ./internal/... -count=1 2>&1 | Select-String -Pattern "^(ok|FAIL|---)"
Set-Location ../gateway
go test ./... -count=1 2>&1 | Select-String -Pattern "^(ok|FAIL|---)"
Set-Location ../portal
```

Expected: 记下每个包 `ok` / `FAIL`（已知 `internal/chat` 的 `TestToolDiscoveryIntegration_AskUserBlockedForWecomWebhook` 在改动前就失败）。后续任务只要求**不新增**失败；若基线已有失败，在最终报告中注明。

---

### Task 0A: Gateway `/hooks` 只接受 `type=webhook`（安全前置）

`weixin_ilink`（以及现有 `wecom_bot`）渠道没有 webhook secret，`webhookSecretEqual("", "")` 为真，导致任何人都能 `POST /hooks/{channel_id}` 匿名跑 Turn、绕过白名单。本计划会把 `weixin_ilink` 下发给 Gateway，必须先堵上。

**Files:**
- Modify: `gateway/internal/adapter/webhook.go`
- Test: `gateway/internal/adapter/webhook_test.go`

- [ ] **Step 1: 写失败测试**

在 `gateway/internal/adapter/webhook_test.go` 的 `TestWebhook_DisabledChannel_410` 之后追加：

```go
func TestWebhook_NonWebhookChannel_404(t *testing.T) {
	yaml := `channels:
  - id: wx-1
    type: weixin_ilink
    enabled: true
  - id: wb-1
    type: wecom_bot
    enabled: true
    bot_id: b1
    secret: s1
`
	h, _, cleanup := newWebhookFixture(t, yaml)
	defer cleanup()

	for _, id := range []string{"wx-1", "wb-1"} {
		rr := postHook(t, h, id, "", "127.0.0.1:1", map[string]any{"content": "hi", "peer_id": "p1"})
		if rr.Code != http.StatusNotFound {
			t.Fatalf("%s: status=%d body=%s", id, rr.Code, rr.Body.String())
		}
	}
}
```

- [ ] **Step 2: 运行确认失败**

```powershell
Set-Location ../gateway
go test ./internal/adapter/ -run TestWebhook_NonWebhookChannel_404 -count=1
```

Expected: FAIL（返回 202 / 200 而不是 404）。

- [ ] **Step 3: 实现**

`gateway/internal/adapter/webhook.go` 的 `ServeHTTP` 中，在

```go
	ch, err := h.deps.Registry.Get(channelID)
	if err != nil {
		http.Error(w, "unknown channel", http.StatusNotFound)
		return
	}
```

之后插入：

```go
	// Other channel types carry no webhook secret, so they must never be reachable through /hooks.
	if ch.Type != "webhook" {
		http.Error(w, "unknown channel", http.StatusNotFound)
		return
	}
```

- [ ] **Step 4: 运行确认通过**

```powershell
gofmt -w internal/adapter/webhook.go internal/adapter/webhook_test.go
go test ./internal/adapter/ -count=1
```

Expected: `ok  github.com/sixath/gateway/internal/adapter`（含既有 webhook 测试）。

- [ ] **Step 5: Commit**

```powershell
git add internal/adapter/webhook.go internal/adapter/webhook_test.go
git commit -m "fix(gateway): /hooks 只接受 webhook 类型渠道，堵住无 secret 渠道的匿名入站"
Set-Location ../portal
```

---

### Task 1: `internal/ilink` 客户端与二维码

**Files:**
- Create: `portal/internal/ilink/client.go`、`portal/internal/ilink/qrcode.go`
- Test: `portal/internal/ilink/client_test.go`、`portal/internal/ilink/qrcode_test.go`
- Modify: `portal/go.mod`、`portal/go.sum`

- [ ] **Step 1: 引入二维码依赖**

```powershell
go get github.com/skip2/go-qrcode@v0.0.0-20200617195104-da1b6568686e
```

Expected: `go.mod` 出现 `github.com/skip2/go-qrcode`。

- [ ] **Step 2: 写失败测试**

`portal/internal/ilink/client_test.go`：

```go
package ilink

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
	"time"
)

func TestSendTextHeadersBodyAndMessageID(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/ilink/bot/sendmessage" {
			t.Errorf("path = %s", r.URL.Path)
		}
		if r.Header.Get("AuthorizationType") != "ilink_bot_token" || r.Header.Get("Authorization") != "Bearer tok" {
			t.Errorf("auth headers = %q / %q", r.Header.Get("AuthorizationType"), r.Header.Get("Authorization"))
		}
		uin, err := base64.StdEncoding.DecodeString(r.Header.Get("X-WECHAT-UIN"))
		if err != nil {
			t.Errorf("X-WECHAT-UIN not base64: %v", err)
		} else if _, err := strconv.ParseUint(string(uin), 10, 32); err != nil {
			t.Errorf("X-WECHAT-UIN not uint32 decimal: %q", uin)
		}
		raw, _ := io.ReadAll(r.Body)
		var body struct {
			Msg      map[string]any    `json:"msg"`
			BaseInfo map[string]string `json:"base_info"`
		}
		if err := json.Unmarshal(raw, &body); err != nil {
			t.Errorf("decode: %v", err)
		}
		if body.BaseInfo["channel_version"] != channelVersion {
			t.Errorf("base_info = %v", body.BaseInfo)
		}
		m := body.Msg
		if v, ok := m["from_user_id"]; !ok || v != "" {
			t.Errorf("from_user_id = %v (present=%v)", v, ok)
		}
		if m["to_user_id"] != "u@im.wechat" || m["context_token"] != "ctx-1" {
			t.Errorf("to/context_token = %v / %v", m["to_user_id"], m["context_token"])
		}
		if m["message_type"] != float64(2) || m["message_state"] != float64(2) {
			t.Errorf("message_type/state = %v / %v", m["message_type"], m["message_state"])
		}
		if id, _ := m["client_id"].(string); !strings.HasPrefix(id, "sixath:") {
			t.Errorf("client_id = %v", m["client_id"])
		}
		if !strings.Contains(string(raw), `"text_item":{"text":"你好"}`) {
			t.Errorf("item_list missing text: %s", raw)
		}
		_, _ = w.Write([]byte(`{"message_id":7517413341670998920}`))
	}))
	defer srv.Close()

	id, err := NewClient(srv.URL, "tok").SendText(context.Background(), "u@im.wechat", "ctx-1", "你好")
	if err != nil {
		t.Fatalf("SendText: %v", err)
	}
	if id != "7517413341670998920" {
		t.Fatalf("message id = %q", id)
	}
}

func TestSendTextEmptyResponse(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{}`))
	}))
	defer srv.Close()

	id, err := NewClient(srv.URL, "tok").SendText(context.Background(), "u", "c", "x")
	if err != nil || id != "" {
		t.Fatalf("id=%q err=%v", id, err)
	}
}

func TestSessionExpiredErrcodeOnly(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"errcode":-14,"errmsg":"session timeout"}`))
	}))
	defer srv.Close()

	_, err := NewClient(srv.URL, "tok").SendText(context.Background(), "u", "c", "x")
	if !IsSessionExpired(err) {
		t.Fatalf("err = %v, want session expired", err)
	}
}

func TestSessionExpiredRet(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"ret":-14}`))
	}))
	defer srv.Close()

	_, err := NewClient(srv.URL, "tok").SendText(context.Background(), "u", "c", "x")
	if !IsSessionExpired(err) {
		t.Fatalf("err = %v, want session expired", err)
	}
}

func TestHTTPErrorMasksSecrets(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadGateway)
		_, _ = w.Write([]byte(`{"bot_token":"leak-bt","context_token":"leak-ctx"}`))
	}))
	defer srv.Close()

	_, err := NewClient(srv.URL, "tok").SendText(context.Background(), "u", "c", "x")
	if err == nil {
		t.Fatal("want error for 502")
	}
	if IsSessionExpired(err) {
		t.Fatalf("502 must not be session expired: %v", err)
	}
	if strings.Contains(err.Error(), "leak-") {
		t.Fatalf("error leaks secrets: %v", err)
	}
}

func TestGetBotQRCode(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/ilink/bot/get_bot_qrcode" || r.URL.Query().Get("bot_type") != "3" {
			t.Errorf("url = %s", r.URL.String())
		}
		if r.Header.Get("iLink-App-ClientVersion") != "1" {
			t.Errorf("missing iLink-App-ClientVersion")
		}
		_, _ = w.Write([]byte(`{"qrcode":"qrc-1","qrcode_img_content":"https://liteapp.weixin.qq.com/q/x","ret":0}`))
	}))
	defer srv.Close()

	qr, err := NewClient(srv.URL, "").GetBotQRCode(context.Background())
	if err != nil {
		t.Fatalf("GetBotQRCode: %v", err)
	}
	if qr.QRCode != "qrc-1" || qr.ImgContent != "https://liteapp.weixin.qq.com/q/x" {
		t.Fatalf("got %+v", qr)
	}
}

func TestGetQRCodeStatusConfirmed(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("qrcode") != "qrc 1" {
			t.Errorf("qrcode = %q", r.URL.Query().Get("qrcode"))
		}
		_, _ = w.Write([]byte(`{"status":"confirmed","bot_token":"bt","ilink_bot_id":"b@im.bot","ilink_user_id":"u@im.wechat","baseurl":"https://x","ret":0}`))
	}))
	defer srv.Close()

	s, err := NewClient(srv.URL, "").GetQRCodeStatus(context.Background(), "qrc 1")
	if err != nil {
		t.Fatalf("GetQRCodeStatus: %v", err)
	}
	if s.Status != "confirmed" || s.BotToken != "bt" || s.BotID != "b@im.bot" || s.UserID != "u@im.wechat" || s.BaseURL != "https://x" {
		t.Fatalf("got %+v", s)
	}
}

func TestGetQRCodeStatusClientTimeoutIsWait(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		select {
		case <-time.After(500 * time.Millisecond):
		case <-r.Context().Done():
		}
	}))
	defer srv.Close()

	c := NewClient(srv.URL, "")
	c.PollTimeout = 50 * time.Millisecond
	s, err := c.GetQRCodeStatus(context.Background(), "qrc-1")
	if err != nil {
		t.Fatalf("GetQRCodeStatus: %v", err)
	}
	if s.Status != "wait" || !s.ClientTimeout {
		t.Fatalf("got %+v", s)
	}
}

func TestNewClientDefaults(t *testing.T) {
	c := NewClient("", "")
	if c.BaseURL != DefaultBaseURL || c.PollTimeout != DefaultLoginPollTimeout {
		t.Fatalf("got base=%q poll=%s", c.BaseURL, c.PollTimeout)
	}
	if NewClient("https://x/", "").BaseURL != "https://x" {
		t.Fatal("trailing slash not trimmed")
	}
}
```

`portal/internal/ilink/qrcode_test.go`：

```go
package ilink

import (
	"encoding/base64"
	"strings"
	"testing"
)

func TestQRCodePNGDataURL(t *testing.T) {
	got, err := QRCodePNGDataURL("https://liteapp.weixin.qq.com/q/7GiQu1?qrcode=abc&bot_type=3")
	if err != nil {
		t.Fatalf("QRCodePNGDataURL: %v", err)
	}
	const prefix = "data:image/png;base64,"
	if !strings.HasPrefix(got, prefix) {
		t.Fatalf("prefix = %q", got[:30])
	}
	png, err := base64.StdEncoding.DecodeString(strings.TrimPrefix(got, prefix))
	if err != nil {
		t.Fatalf("base64: %v", err)
	}
	if !strings.HasPrefix(string(png), "\x89PNG") {
		t.Fatal("not a PNG")
	}
}

func TestQRCodePNGDataURLEmpty(t *testing.T) {
	if _, err := QRCodePNGDataURL("  "); err == nil {
		t.Fatal("want error for empty content")
	}
}
```

- [ ] **Step 3: 运行确认失败**

Run: `go test ./internal/ilink/ -count=1`  
Expected: FAIL，`undefined: NewClient` 等。

- [ ] **Step 4: 实现**

`portal/internal/ilink/client.go`：

```go
// Package ilink is a minimal HTTP client for the WeChat iLink bot gateway (QR login and outbound text).
package ilink

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/base64"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"time"
)

const (
	DefaultBaseURL = "https://ilinkai.weixin.qq.com"
	// DefaultLoginPollTimeout must exceed the ~30s the server holds get_qrcode_status open.
	DefaultLoginPollTimeout = 35 * time.Second

	channelVersion     = "1.0.0"
	requestTimeout     = 15 * time.Second
	itemTypeText       = 1
	messageTypeBot     = 2
	messageStateFinish = 2
)

// APIError is a business error returned with HTTP 200 (ret / errcode non-zero).
type APIError struct {
	Ret     int
	ErrCode int
	ErrMsg  string
}

func (e *APIError) Error() string {
	return fmt.Sprintf("ilink ret=%d errcode=%d errmsg=%s", e.Ret, e.ErrCode, e.ErrMsg)
}

// SessionExpired reports -14; the server may set only one of ret / errcode.
func (e *APIError) SessionExpired() bool { return e.Ret == -14 || e.ErrCode == -14 }

func IsSessionExpired(err error) bool {
	var apiErr *APIError
	return errors.As(err, &apiErr) && apiErr.SessionExpired()
}

type Client struct {
	BaseURL     string
	Token       string
	HTTP        *http.Client
	PollTimeout time.Duration
}

func NewClient(baseURL, token string) *Client {
	if strings.TrimSpace(baseURL) == "" {
		baseURL = DefaultBaseURL
	}
	return &Client{
		BaseURL:     strings.TrimRight(baseURL, "/"),
		Token:       token,
		HTTP:        &http.Client{},
		PollTimeout: DefaultLoginPollTimeout,
	}
}

type QRCode struct {
	QRCode     string `json:"qrcode"`
	ImgContent string `json:"qrcode_img_content"`
}

type QRStatus struct {
	Status        string `json:"status"`
	BotToken      string `json:"bot_token"`
	BotID         string `json:"ilink_bot_id"`
	UserID        string `json:"ilink_user_id"`
	BaseURL       string `json:"baseurl"`
	ClientTimeout bool   `json:"-"`
}

func (c *Client) GetBotQRCode(ctx context.Context) (*QRCode, error) {
	qctx, cancel := context.WithTimeout(ctx, requestTimeout)
	defer cancel()
	var out QRCode
	if err := c.get(qctx, "/ilink/bot/get_bot_qrcode?bot_type=3", &out); err != nil {
		return nil, err
	}
	if out.QRCode == "" {
		return nil, errors.New("ilink: get_bot_qrcode returned empty qrcode")
	}
	return &out, nil
}

// GetQRCodeStatus treats a client-side timeout as "wait" because the server holds the request open.
func (c *Client) GetQRCodeStatus(ctx context.Context, qrcode string) (*QRStatus, error) {
	pctx, cancel := context.WithTimeout(ctx, c.PollTimeout)
	defer cancel()
	var out QRStatus
	err := c.get(pctx, "/ilink/bot/get_qrcode_status?qrcode="+url.QueryEscape(qrcode), &out)
	if err != nil {
		if errors.Is(err, context.DeadlineExceeded) && ctx.Err() == nil {
			return &QRStatus{Status: "wait", ClientTimeout: true}, nil
		}
		return nil, err
	}
	return &out, nil
}

// SendText returns the server-assigned message_id (empty when the server omits it).
// A nil error does not prove delivery: the server also returns success for a stale context_token.
func (c *Client) SendText(ctx context.Context, to, contextToken, text string) (string, error) {
	msg := map[string]any{
		"from_user_id":  "",
		"to_user_id":    to,
		"client_id":     newClientID(),
		"message_type":  messageTypeBot,
		"message_state": messageStateFinish,
		"context_token": contextToken,
		"item_list":     []map[string]any{{"type": itemTypeText, "text_item": map[string]string{"text": text}}},
	}
	sctx, cancel := context.WithTimeout(ctx, requestTimeout)
	defer cancel()
	var out struct {
		MessageID json.Number `json:"message_id"`
	}
	if err := c.post(sctx, "/ilink/bot/sendmessage", map[string]any{"msg": msg}, &out); err != nil {
		return "", err
	}
	return out.MessageID.String(), nil
}

func (c *Client) get(ctx context.Context, pathAndQuery string, out any) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.BaseURL+pathAndQuery, nil)
	if err != nil {
		return err
	}
	req.Header.Set("iLink-App-ClientVersion", "1")
	return c.do(req, out)
}

func (c *Client) post(ctx context.Context, endpoint string, body map[string]any, out any) error {
	body["base_info"] = map[string]string{"channel_version": channelVersion}
	raw, err := json.Marshal(body)
	if err != nil {
		return err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.BaseURL+endpoint, bytes.NewReader(raw))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("AuthorizationType", "ilink_bot_token")
	req.Header.Set("Authorization", "Bearer "+c.Token)
	req.Header.Set("X-WECHAT-UIN", randomUIN())
	return c.do(req, out)
}

func (c *Client) do(req *http.Request, out any) error {
	resp, err := c.HTTP.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	data, err := io.ReadAll(io.LimitReader(resp.Body, 4<<20))
	if err != nil {
		return err
	}
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("ilink %s %s: http %d: %s", req.Method, req.URL.Path, resp.StatusCode, truncate(maskSecrets(data), 200))
	}
	if len(bytes.TrimSpace(data)) == 0 {
		return nil
	}
	var head struct {
		Ret     int    `json:"ret"`
		ErrCode int    `json:"errcode"`
		ErrMsg  string `json:"errmsg"`
	}
	if err := json.Unmarshal(data, &head); err != nil {
		return fmt.Errorf("ilink %s: decode: %w", req.URL.Path, err)
	}
	if head.Ret != 0 || head.ErrCode != 0 {
		return &APIError{Ret: head.Ret, ErrCode: head.ErrCode, ErrMsg: head.ErrMsg}
	}
	if out == nil {
		return nil
	}
	return json.Unmarshal(data, out)
}

var secretFieldRe = regexp.MustCompile(`"(bot_token|context_token)"\s*:\s*"[^"]*"`)

func maskSecrets(data []byte) string {
	return secretFieldRe.ReplaceAllString(string(data), `"$1":"***"`)
}

func randomUIN() string {
	var b [4]byte
	_, _ = rand.Read(b[:])
	n := binary.BigEndian.Uint32(b[:])
	return base64.StdEncoding.EncodeToString([]byte(strconv.FormatUint(uint64(n), 10)))
}

func newClientID() string {
	var b [4]byte
	_, _ = rand.Read(b[:])
	return fmt.Sprintf("sixath:%d-%s", time.Now().UnixMilli(), hex.EncodeToString(b[:]))
}

func truncate(s string, n int) string {
	r := []rune(s)
	if len(r) <= n {
		return s
	}
	return string(r[:n]) + "…"
}
```

`portal/internal/ilink/qrcode.go`：

```go
package ilink

import (
	"encoding/base64"
	"errors"
	"strings"

	qrcode "github.com/skip2/go-qrcode"
)

// QRCodePNGDataURL renders qrcode_img_content (a URL) as a PNG data URL for <img src>.
func QRCodePNGDataURL(content string) (string, error) {
	if strings.TrimSpace(content) == "" {
		return "", errors.New("ilink: empty qrcode content")
	}
	png, err := qrcode.Encode(content, qrcode.Medium, 256)
	if err != nil {
		return "", err
	}
	return "data:image/png;base64," + base64.StdEncoding.EncodeToString(png), nil
}
```

- [ ] **Step 5: 运行确认通过并整理依赖**

```powershell
go test ./internal/ilink/ -count=1
go mod tidy
go build ./...
```

Expected: `ok  backend/internal/ilink`；`go.mod` 中 go-qrcode 为直接依赖（无 `// indirect`）。`go mod tidy` 还会把 `github.com/gorilla/websocket` 从 indirect 改为直接依赖——这是 tidy 对现状的修正，随本提交一起保留。

- [ ] **Step 6: Commit**

```powershell
git add internal/ilink go.mod go.sum
git commit -m "feat(ilink): Portal 侧微信 iLink HTTP 客户端与二维码 PNG"
```

---

### Task 2: 数据模型、`ChannelMeta` 字段与渠道仓储改动

**Files:**
- Modify: `portal/internal/data/model/channel.go`、`portal/internal/biz/channel.go`、`portal/internal/data/channel_mysql.go`、`portal/internal/data/data.go`、`portal/internal/data/channel_mysql_test.go`
- Create: `portal/internal/data/model/channel_ilink.go`、`portal/migrations/023_weixin_ilink_channel.sql`

- [ ] **Step 1: 新模型与 Channel 新列**

`portal/internal/data/model/channel_ilink.go`：

```go
package model

import "time"

// ChannelIlinkPeer is the weixin_ilink roster; Allowed is the only source of truth for the peer allowlist.
type ChannelIlinkPeer struct {
	ChannelUUID    string     `gorm:"column:channel_uuid;primaryKey;size:36"`
	PeerID         string     `gorm:"column:peer_id;primaryKey;size:128"`
	Allowed        bool       `gorm:"column:allowed;not null;default:false"`
	Remark         string     `gorm:"column:remark;size:128"`
	ContextToken   string     `gorm:"column:context_token;size:512"`
	ContextTokenAt *time.Time `gorm:"column:context_token_at"`
	LastInboundAt  *time.Time `gorm:"column:last_inbound_at"`
	LastPreview    string     `gorm:"column:last_preview;size:160"`
	CreatedAt      time.Time  `gorm:"column:created_at;not null"`
	UpdatedAt      time.Time  `gorm:"column:updated_at;not null"`
}

func (ChannelIlinkPeer) TableName() string {
	return "channel_ilink_peers"
}

// ChannelIlinkState holds the getupdates cursor; Generation must match channels.ilink_generation.
type ChannelIlinkState struct {
	ChannelUUID   string    `gorm:"column:channel_uuid;primaryKey;size:36"`
	Generation    int       `gorm:"column:generation;not null;default:0"`
	GetUpdatesBuf string    `gorm:"column:get_updates_buf;type:text"`
	UpdatedAt     time.Time `gorm:"column:updated_at;not null"`
}

func (ChannelIlinkState) TableName() string {
	return "channel_ilink_state"
}
```

`portal/internal/data/model/channel.go`：把 `Type` 行注释改为 `// web, api, webhook, wxpusher, wecom, wecom_bot, weixin_ilink`；在 `DefaultReplyMode` 字段之后、`CreatedAt` 之前插入：

```go
	// 微信 iLink（type=weixin_ilink）；只由扫码登录 / 会话失效流程写入，不在 Update 白名单中
	IlinkBotToken         string     `gorm:"column:ilink_bot_token;size:512"`
	IlinkBotID            string     `gorm:"column:ilink_bot_id;size:128"`
	IlinkOwnerUserID      string     `gorm:"column:ilink_owner_user_id;size:128"`
	IlinkBaseURL          string     `gorm:"column:ilink_base_url;size:512"`
	IlinkGeneration       int        `gorm:"column:ilink_generation;not null;default:0"`
	IlinkSessionExpiredAt *time.Time `gorm:"column:ilink_session_expired_at"`
```

- [ ] **Step 2: 注册 AutoMigrate，写 DDL 记录**

`portal/internal/data/data.go` 的 AutoMigrate 列表中，把

```go
		&model.Channel{}, &model.ChannelRuntimeStatus{}, &model.ChannelPeerSession{}, &model.CronTask{}, &model.CronRun{},
```

改为

```go
		&model.Channel{}, &model.ChannelRuntimeStatus{}, &model.ChannelPeerSession{}, &model.ChannelIlinkPeer{}, &model.ChannelIlinkState{}, &model.CronTask{}, &model.CronRun{},
```

`portal/migrations/023_weixin_ilink_channel.sql`：

```sql
-- portal/migrations/023_weixin_ilink_channel.sql
-- weixin_ilink 渠道（docs/superpowers/specs/2026-10-18-weixin-ilink-channel-design.md §5）。
-- 启动时 GORM AutoMigrate 自动加列 / 建表；本文件仅作手工修复与记录。
ALTER TABLE channels
  ADD COLUMN ilink_bot_token VARCHAR(512) NULL,
  ADD COLUMN ilink_bot_id VARCHAR(128) NULL,
  ADD COLUMN ilink_owner_user_id VARCHAR(128) NULL,
  ADD COLUMN ilink_base_url VARCHAR(512) NULL,
  ADD COLUMN ilink_generation INT NOT NULL DEFAULT 0,
  ADD COLUMN ilink_session_expired_at DATETIME(3) NULL;

CREATE TABLE IF NOT EXISTS channel_ilink_peers (
  channel_uuid VARCHAR(36) NOT NULL,
  peer_id VARCHAR(128) NOT NULL,
  allowed TINYINT(1) NOT NULL DEFAULT 0,
  remark VARCHAR(128) NULL,
  context_token VARCHAR(512) NULL,
  context_token_at DATETIME(3) NULL,
  last_inbound_at DATETIME(3) NULL,
  last_preview VARCHAR(160) NULL,
  created_at DATETIME(3) NOT NULL,
  updated_at DATETIME(3) NOT NULL,
  PRIMARY KEY (channel_uuid, peer_id)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;

CREATE TABLE IF NOT EXISTS channel_ilink_state (
  channel_uuid VARCHAR(36) NOT NULL PRIMARY KEY,
  generation INT NOT NULL DEFAULT 0,
  get_updates_buf TEXT NULL,
  updated_at DATETIME(3) NOT NULL
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;
```

- [ ] **Step 3: `ChannelMeta` 新字段**

`portal/internal/biz/channel.go` 的 `ChannelMeta`（不是 `ChannelCreate`）中，在 `DefaultReplyMode string` 之后、`CreatedAt` 之前插入：

```go
	// 微信 iLink（type=weixin_ilink）
	IlinkBotToken         string
	IlinkBotID            string
	IlinkOwnerUserID      string
	IlinkBaseURL          string
	IlinkGeneration       int
	IlinkSessionExpiredAt *time.Time
```

- [ ] **Step 4: 写失败测试**

`portal/internal/data/channel_mysql_test.go`：`openChannelTestDB` 中把

```go
	if err := db.AutoMigrate(&model.Channel{}); err != nil {
```

改为

```go
	if err := db.AutoMigrate(&model.Channel{}, &model.ChannelIlinkPeer{}, &model.ChannelIlinkState{}); err != nil {
```

并在文件末尾追加：

```go
func TestChannelRepo_ListGatewayChannelsIncludesWeixinIlink(t *testing.T) {
	db := openChannelTestDB(t)
	repo := newChannelRepoForTest(db)
	ctx := context.Background()

	ch, err := repo.Create(ctx, &biz.ChannelCreate{ChannelID: "wx-1", Type: "weixin_ilink", Enabled: true})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	expired := time.Date(2026, 10, 18, 2, 0, 0, 0, time.UTC)
	if err := db.Model(&model.Channel{}).Where("id = ?", ch.ID).Updates(map[string]any{
		"ilink_bot_token": "bt", "ilink_bot_id": "b@im.bot", "ilink_owner_user_id": "u@im.wechat",
		"ilink_generation": 2, "ilink_session_expired_at": expired,
	}).Error; err != nil {
		t.Fatalf("seed ilink columns: %v", err)
	}

	list, err := repo.ListGatewayChannels(ctx)
	if err != nil {
		t.Fatalf("ListGatewayChannels: %v", err)
	}
	if len(list) != 1 || list[0].Type != "weixin_ilink" {
		t.Fatalf("list = %+v", list)
	}
	got := list[0]
	if got.IlinkBotToken != "bt" || got.IlinkBotID != "b@im.bot" || got.IlinkOwnerUserID != "u@im.wechat" || got.IlinkGeneration != 2 {
		t.Fatalf("ilink fields = %+v", got)
	}
	if got.IlinkSessionExpiredAt == nil || !got.IlinkSessionExpiredAt.Equal(expired) {
		t.Fatalf("IlinkSessionExpiredAt = %v", got.IlinkSessionExpiredAt)
	}
}

func TestChannelRepo_UpdateIgnoresIlinkCredentialKeys(t *testing.T) {
	db := openChannelTestDB(t)
	repo := newChannelRepoForTest(db)
	ctx := context.Background()

	ch, err := repo.Create(ctx, &biz.ChannelCreate{ChannelID: "wx-1", Type: "weixin_ilink", Enabled: true})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	updated, err := repo.Update(ctx, ch.ID, map[string]any{
		"ilink_bot_token": "injected", "ilink_generation": 99, "enabled": false,
	})
	if err != nil {
		t.Fatalf("Update: %v", err)
	}
	if updated.IlinkBotToken != "" || updated.IlinkGeneration != 0 || updated.Enabled {
		t.Fatalf("updated = %+v", updated)
	}
}

func TestChannelRepo_DeleteCascadesIlinkRows(t *testing.T) {
	db := openChannelTestDB(t)
	repo := newChannelRepoForTest(db)
	ctx := context.Background()

	ch, err := repo.Create(ctx, &biz.ChannelCreate{ChannelID: "wx-1", Type: "weixin_ilink", Enabled: true})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	now := time.Now()
	if err := db.Create(&model.ChannelIlinkPeer{ChannelUUID: ch.ID, PeerID: "p1", CreatedAt: now, UpdatedAt: now}).Error; err != nil {
		t.Fatalf("seed peer: %v", err)
	}
	if err := db.Create(&model.ChannelIlinkState{ChannelUUID: ch.ID, GetUpdatesBuf: "buf", UpdatedAt: now}).Error; err != nil {
		t.Fatalf("seed state: %v", err)
	}

	if err := repo.Delete(ctx, ch.ID); err != nil {
		t.Fatalf("Delete: %v", err)
	}
	var peers, states int64
	db.Model(&model.ChannelIlinkPeer{}).Where("channel_uuid = ?", ch.ID).Count(&peers)
	db.Model(&model.ChannelIlinkState{}).Where("channel_uuid = ?", ch.ID).Count(&states)
	if peers != 0 || states != 0 {
		t.Fatalf("leftover peers=%d states=%d", peers, states)
	}
	if err := repo.Delete(ctx, ch.ID); err != ErrNotFound {
		t.Fatalf("second Delete err = %v, want ErrNotFound", err)
	}
}
```

如文件尚未导入 `time`，在 import 块补上 `"time"`。

- [ ] **Step 5: 运行确认失败**

Run: `go test ./internal/data/ -run "TestChannelRepo" -count=1`  
Expected: FAIL（`IlinkBotToken` 等为空 / weixin_ilink 不在列表中 / 级联未删除）。

- [ ] **Step 6: 实现仓储改动**

`portal/internal/data/channel_mysql.go`：

1. `ListGatewayChannels` 的注释改为 `// ListGatewayChannels returns webhook/wecom_bot/weixin_ilink channels including disabled, with plaintext secrets.`，过滤条件改为：

```go
		Where("type IN ?", []string{"webhook", "wecom_bot", "weixin_ilink"}).
```

2. `Delete` 整个函数替换为：

```go
func (r *channelRepo) Delete(ctx context.Context, id string) error {
	return r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		res := tx.Where("id = ?", id).Delete(&model.Channel{})
		if res.Error != nil {
			return res.Error
		}
		if res.RowsAffected == 0 {
			return ErrNotFound
		}
		if err := tx.Where("channel_uuid = ?", id).Delete(&model.ChannelIlinkPeer{}).Error; err != nil {
			return err
		}
		return tx.Where("channel_uuid = ?", id).Delete(&model.ChannelIlinkState{}).Error
	})
}
```

3. `channelModelToBiz` 返回的结构体中，在 `DefaultReplyMode: m.DefaultReplyMode,` 之后加：

```go
		IlinkBotToken:         m.IlinkBotToken,
		IlinkBotID:            m.IlinkBotID,
		IlinkOwnerUserID:      m.IlinkOwnerUserID,
		IlinkBaseURL:          m.IlinkBaseURL,
		IlinkGeneration:       m.IlinkGeneration,
		IlinkSessionExpiredAt: m.IlinkSessionExpiredAt,
```

`Update` 的可改字段白名单**不要**加入任何 `ilink_*` 键。

- [ ] **Step 7: 运行确认通过**

```powershell
gofmt -w internal/data/model/channel.go internal/data/model/channel_ilink.go internal/data/channel_mysql.go internal/data/channel_mysql_test.go internal/biz/channel.go
go test ./internal/data/ -run "TestChannelRepo" -count=1
go build ./...
```

Expected: `ok  backend/internal/data`。

- [ ] **Step 8: Commit**

```powershell
git add internal/data/model/channel.go internal/data/model/channel_ilink.go internal/data/data.go internal/data/channel_mysql.go internal/data/channel_mysql_test.go internal/biz/channel.go migrations/023_weixin_ilink_channel.sql
git commit -m "feat(channel): weixin_ilink 数据模型、网关下发过滤与删除级联"
```

---

### Task 3: `AuthEphemeralRepo.Get`（非消费读）

**Files:**
- Modify: `portal/internal/biz/identity.go`、`portal/internal/data/auth_ephemeral_mysql.go`、`portal/internal/biz/auth_usecase_test.go`
- Test: `portal/internal/data/auth_ephemeral_mysql_test.go`

- [ ] **Step 1: 写失败测试**

在 `portal/internal/data/auth_ephemeral_mysql_test.go` 末尾追加（沿用该文件已有的建库 / 建仓储辅助函数；若名称不同，按文件内实际名称替换 `openAuthEphemeralTestDB` / `newAuthEphemeralRepoForTest`）：

```go
func TestAuthEphemeralRepo_GetDoesNotConsume(t *testing.T) {
	db := openAuthEphemeralTestDB(t)
	repo := newAuthEphemeralRepoForTest(db)
	ctx := context.Background()
	now := time.Date(2026, 10, 18, 10, 0, 0, 0, time.UTC)

	if err := repo.Put(ctx, "qr-key", "ilink_qrcode", `{"channel_uuid":"c1"}`, now.Add(5*time.Minute)); err != nil {
		t.Fatalf("Put: %v", err)
	}
	for i := 0; i < 2; i++ {
		payload, err := repo.Get(ctx, "qr-key", "ilink_qrcode", now)
		if err != nil || payload != `{"channel_uuid":"c1"}` {
			t.Fatalf("Get #%d = %q, %v", i, payload, err)
		}
	}
	if _, err := repo.Get(ctx, "qr-key", "wecom_state", now); err != ErrNotFound {
		t.Fatalf("wrong kind err = %v, want ErrNotFound", err)
	}
	if _, err := repo.Get(ctx, "qr-key", "ilink_qrcode", now.Add(6*time.Minute)); err != ErrNotFound {
		t.Fatalf("expired err = %v, want ErrNotFound", err)
	}
	if _, err := repo.Consume(ctx, "qr-key", "ilink_qrcode", now); err != nil {
		t.Fatalf("Consume: %v", err)
	}
	if _, err := repo.Get(ctx, "qr-key", "ilink_qrcode", now); err != ErrNotFound {
		t.Fatalf("consumed err = %v, want ErrNotFound", err)
	}
}
```

- [ ] **Step 2: 运行确认失败**

Run: `go test ./internal/data/ -run TestAuthEphemeralRepo -count=1`  
Expected: FAIL，`repo.Get undefined`。

- [ ] **Step 3: 实现**

`portal/internal/biz/identity.go` 的 `AuthEphemeralRepo` 接口中，在 `Consume` 之前加：

```go
	// Get returns the payload without consuming; ErrNotFound if missing/expired/already used.
	Get(ctx context.Context, id, kind string, now time.Time) (payloadJSON string, err error)
```

`portal/internal/data/auth_ephemeral_mysql.go` 末尾追加：

```go
func (r *authEphemeralRepo) Get(ctx context.Context, id, kind string, now time.Time) (string, error) {
	var m model.AuthEphemeral
	err := r.db.WithContext(ctx).
		Where("id = ? AND kind = ? AND consumed_at IS NULL AND expires_at > ?", id, kind, now).
		First(&m).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return "", ErrNotFound
	}
	if err != nil {
		return "", err
	}
	return m.PayloadJSON, nil
}
```

并在该文件 import 中加入 `"errors"`。

`portal/internal/biz/auth_usecase_test.go` 的 `authEphemeralFake` 在 `Consume` 方法之后追加：

```go
func (f *authEphemeralFake) Get(_ context.Context, id, kind string, now time.Time) (string, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	item, ok := f.items[id]
	if !ok || item.kind != kind || item.consumed || !item.expires.After(now) {
		return "", pkgErrors.ErrNotFound
	}
	return item.payload, nil
}
```

- [ ] **Step 4: 运行确认通过**

```powershell
gofmt -w internal/biz/identity.go internal/data/auth_ephemeral_mysql.go internal/data/auth_ephemeral_mysql_test.go internal/biz/auth_usecase_test.go
go test ./internal/data/ -run TestAuthEphemeralRepo -count=1
go test ./internal/biz/ -run "Auth|WeCom" -count=1
go build ./...
```

Expected: 均 `ok`（`go build` 通过说明没有其它 `AuthEphemeralRepo` 实现漏改）。

- [ ] **Step 5: Commit**

```powershell
git add internal/biz/identity.go internal/data/auth_ephemeral_mysql.go internal/data/auth_ephemeral_mysql_test.go internal/biz/auth_usecase_test.go
git commit -m "feat(auth): AuthEphemeralRepo 新增非消费读 Get"
```

---

### Task 4: `DeriveRuntimeStatus` 支持 `weixin_ilink`

**Files:**
- Modify: `portal/internal/biz/channel_runtime.go`
- Create: `portal/internal/biz/weixin_ilink.go`（本任务只放类型常量，Task 5 补全）
- Test: `portal/internal/biz/channel_runtime_test.go`

- [ ] **Step 1: 写失败测试**

`portal/internal/biz/channel_runtime_test.go` 末尾追加：

```go
func TestDeriveRuntimeStatus_WeixinIlinkWithoutToken(t *testing.T) {
	now := time.Now()
	ch := &ChannelMeta{Type: ChannelTypeWeixinIlink, Enabled: true}
	got := DeriveRuntimeStatus(ch, &RuntimeStatusRow{State: "connected", LastHeartbeatAt: now}, now)
	if got == nil || got.State != "login_required" || !strings.Contains(got.LastError, "未登录") {
		t.Fatalf("got %#v", got)
	}
}

func TestDeriveRuntimeStatus_WeixinIlinkExpiredBeatsFreshHeartbeat(t *testing.T) {
	now := time.Now()
	expired := now.Add(-time.Minute)
	ch := &ChannelMeta{Type: ChannelTypeWeixinIlink, Enabled: true, IlinkBotToken: "bt", IlinkSessionExpiredAt: &expired}
	got := DeriveRuntimeStatus(ch, &RuntimeStatusRow{State: "connected", LastHeartbeatAt: now}, now)
	if got == nil || got.State != "login_required" || !strings.Contains(got.LastError, "过期") {
		t.Fatalf("got %#v", got)
	}
}

func TestDeriveRuntimeStatus_WeixinIlinkDisabledFirst(t *testing.T) {
	ch := &ChannelMeta{Type: ChannelTypeWeixinIlink, Enabled: false}
	got := DeriveRuntimeStatus(ch, nil, time.Now())
	if got == nil || got.State != "disabled" {
		t.Fatalf("got %#v", got)
	}
}

func TestDeriveRuntimeStatus_WeixinIlinkConnectedAndStale(t *testing.T) {
	now := time.Now()
	ch := &ChannelMeta{Type: ChannelTypeWeixinIlink, Enabled: true, IlinkBotToken: "bt"}
	if got := DeriveRuntimeStatus(ch, &RuntimeStatusRow{State: "connected", LastHeartbeatAt: now}, now); got == nil || got.State != "connected" {
		t.Fatalf("fresh = %#v", got)
	}
	stale := &RuntimeStatusRow{State: "connected", LastHeartbeatAt: now.Add(-(RuntimeStatusStaleAfter + time.Second))}
	if got := DeriveRuntimeStatus(ch, stale, now); got == nil || got.State != "unknown" {
		t.Fatalf("stale = %#v", got)
	}
}
```

在该文件 import 中加入 `"strings"`。

- [ ] **Step 2: 运行确认失败**

Run: `go test ./internal/biz/ -run TestDeriveRuntimeStatus -count=1`  
Expected: FAIL，`undefined: ChannelTypeWeixinIlink`。

- [ ] **Step 3: 实现**

`portal/internal/biz/weixin_ilink.go`：

```go
package biz

// ChannelTypeWeixinIlink is the channel type for WeChat iLink bots.
const ChannelTypeWeixinIlink = "weixin_ilink"
```

`portal/internal/biz/channel_runtime.go` 的 `DeriveRuntimeStatus` 中，把开头

```go
	if ch == nil || ch.Type != "wecom_bot" {
		return nil
	}
	if !ch.Enabled {
		return &RuntimeStatusView{State: "disabled"}
	}
```

替换为

```go
	if ch == nil || (ch.Type != "wecom_bot" && ch.Type != ChannelTypeWeixinIlink) {
		return nil
	}
	if !ch.Enabled {
		return &RuntimeStatusView{State: "disabled"}
	}
	if ch.Type == ChannelTypeWeixinIlink {
		// Login state is persisted on the channel, so it must not decay into "unknown" with the heartbeat.
		if ch.IlinkBotToken == "" {
			return &RuntimeStatusView{State: "login_required", LastError: "未登录，请扫码登录"}
		}
		if ch.IlinkSessionExpiredAt != nil {
			return &RuntimeStatusView{State: "login_required", LastError: "会话已过期，请重新扫码"}
		}
	}
```

并把函数上方注释改为 `// DeriveRuntimeStatus applies the read-path derivation (wecom_bot / weixin_ilink).`

- [ ] **Step 4: 运行确认通过**

```powershell
gofmt -w internal/biz/channel_runtime.go internal/biz/channel_runtime_test.go internal/biz/weixin_ilink.go
go test ./internal/biz/ -run TestDeriveRuntimeStatus -count=1
```

Expected: `ok`（含既有的 `NonWecomBot` 等测试）。

- [ ] **Step 5: Commit**

```powershell
git add internal/biz/channel_runtime.go internal/biz/channel_runtime_test.go internal/biz/weixin_ilink.go
git commit -m "feat(channel): weixin_ilink 运行状态派生 login_required"
```

---

### Task 5: `WeixinIlinkRepo` 接口与 GORM 实现

**Files:**
- Modify: `portal/internal/biz/weixin_ilink.go`、`portal/internal/data/data.go`
- Create: `portal/internal/data/channel_ilink_mysql.go`
- Test: `portal/internal/data/channel_ilink_mysql_test.go`

- [ ] **Step 1: biz 类型与接口**

把 `portal/internal/biz/weixin_ilink.go` 整体替换为：

```go
package biz

import (
	"context"
	"time"

	kratosErrors "github.com/go-kratos/kratos/v2/errors"
)

// ChannelTypeWeixinIlink is the channel type for WeChat iLink bots.
const ChannelTypeWeixinIlink = "weixin_ilink"

var (
	ErrIlinkNotIlinkChannel    = kratosErrors.BadRequest("ILINK_NOT_ILINK_CHANNEL", "channel type is not weixin_ilink")
	ErrIlinkQRCodeNotFound     = kratosErrors.NotFound("ILINK_QRCODE_NOT_FOUND", "二维码不存在或已过期，请重新获取")
	ErrIlinkGenerationMismatch = kratosErrors.Conflict("ILINK_GENERATION_MISMATCH", "weixin_ilink generation mismatch")
	ErrIlinkPeerNotFound       = kratosErrors.NotFound("ILINK_PEER_NOT_FOUND", "peer not found")
)

// IlinkPeer is one roster row of a weixin_ilink channel.
type IlinkPeer struct {
	ChannelUUID    string
	PeerID         string
	Allowed        bool
	Remark         string
	ContextToken   string
	ContextTokenAt *time.Time
	LastInboundAt  *time.Time
	LastPreview    string
	CreatedAt      time.Time
	UpdatedAt      time.Time
}

type IlinkPeerPatch struct {
	Allowed *bool
	Remark  *string
}

// IlinkPeerReport is what the Gateway reports for each inbound message.
type IlinkPeerReport struct {
	PeerID       string
	ContextToken string
	Preview      string
	At           time.Time
}

type IlinkCursor struct {
	Generation    int
	GetUpdatesBuf string
}

type IlinkLoginResult struct {
	BotToken    string
	BotID       string
	OwnerUserID string
	BaseURL     string
}

// WeixinIlinkRepo persists weixin_ilink login state, roster and cursor. channelUUID is channels.id.
type WeixinIlinkRepo interface {
	// ConfirmLogin writes credentials, bumps ilink_generation, clears the expiry mark and cursor,
	// clears every roster context_token (each scan yields a new bot) and allows the owner, in one transaction.
	ConfirmLogin(ctx context.Context, channelUUID string, in IlinkLoginResult, now time.Time) (*ChannelMeta, error)
	// MarkSessionExpired applies only when generation matches; reports whether it applied.
	MarkSessionExpired(ctx context.Context, channelUUID string, generation int, now time.Time) (bool, error)
	// UpsertPeerReport returns ErrIlinkGenerationMismatch when generation is stale; new peers start not allowed.
	UpsertPeerReport(ctx context.Context, channelUUID string, generation int, in IlinkPeerReport) (*IlinkPeer, error)
	ListPeers(ctx context.Context, channelUUID string) ([]*IlinkPeer, error)
	UpdatePeer(ctx context.Context, channelUUID, peerID string, patch IlinkPeerPatch) (*IlinkPeer, error)
	DeletePeer(ctx context.Context, channelUUID, peerID string) error
	ListAllowedPeerIDs(ctx context.Context, channelUUID string) ([]string, error)
	// GetCursor returns an empty cursor (generation 0) when none is stored.
	GetCursor(ctx context.Context, channelUUID string) (*IlinkCursor, error)
	// PutCursor returns ErrIlinkGenerationMismatch unless cur.Generation equals channels.ilink_generation.
	PutCursor(ctx context.Context, channelUUID string, cur IlinkCursor, now time.Time) error
}
```

- [ ] **Step 2: 写失败测试**

`portal/internal/data/channel_ilink_mysql_test.go`：

```go
package data

import (
	"context"
	"errors"
	"testing"
	"time"

	"backend/internal/biz"
	"backend/internal/data/model"

	"github.com/go-kratos/kratos/v2/log"
	"gorm.io/gorm"
)

func newIlinkRepoForTest(db *gorm.DB) *weixinIlinkRepo {
	return &weixinIlinkRepo{db: db, log: log.NewHelper(log.DefaultLogger)}
}

func seedIlinkChannel(t *testing.T, db *gorm.DB) *biz.ChannelMeta {
	t.Helper()
	ch, err := newChannelRepoForTest(db).Create(context.Background(), &biz.ChannelCreate{ChannelID: "wx-1", Type: "weixin_ilink", Enabled: true})
	if err != nil {
		t.Fatalf("seed channel: %v", err)
	}
	return ch
}

func TestIlinkRepo_ConfirmLoginBumpsGenerationAndResetsState(t *testing.T) {
	db := openChannelTestDB(t)
	repo := newIlinkRepoForTest(db)
	ctx := context.Background()
	ch := seedIlinkChannel(t, db)
	now := time.Date(2026, 10, 18, 10, 0, 0, 0, time.UTC)

	if _, err := repo.UpsertPeerReport(ctx, ch.ID, 0, biz.IlinkPeerReport{PeerID: "p1", ContextToken: "old", Preview: "hi", At: now}); err != nil {
		t.Fatalf("UpsertPeerReport: %v", err)
	}
	if err := repo.PutCursor(ctx, ch.ID, biz.IlinkCursor{Generation: 0, GetUpdatesBuf: "buf"}, now); err != nil {
		t.Fatalf("PutCursor: %v", err)
	}
	if applied, err := repo.MarkSessionExpired(ctx, ch.ID, 0, now); err != nil || !applied {
		t.Fatalf("MarkSessionExpired = %v, %v", applied, err)
	}

	meta, err := repo.ConfirmLogin(ctx, ch.ID, biz.IlinkLoginResult{BotToken: "bt", BotID: "b@im.bot", OwnerUserID: "owner@im.wechat"}, now)
	if err != nil {
		t.Fatalf("ConfirmLogin: %v", err)
	}
	if meta.IlinkGeneration != 1 || meta.IlinkBotToken != "bt" || meta.IlinkBotID != "b@im.bot" || meta.IlinkOwnerUserID != "owner@im.wechat" {
		t.Fatalf("meta = %+v", meta)
	}
	if meta.IlinkSessionExpiredAt != nil {
		t.Fatalf("expiry not cleared: %v", meta.IlinkSessionExpiredAt)
	}
	cur, err := repo.GetCursor(ctx, ch.ID)
	if err != nil || cur.GetUpdatesBuf != "" {
		t.Fatalf("cursor = %+v, %v", cur, err)
	}
	peers, err := repo.ListPeers(ctx, ch.ID)
	if err != nil {
		t.Fatalf("ListPeers: %v", err)
	}
	byID := map[string]*biz.IlinkPeer{}
	for _, p := range peers {
		byID[p.PeerID] = p
	}
	if p1 := byID["p1"]; p1 == nil || p1.ContextToken != "" || p1.ContextTokenAt != nil || p1.LastPreview != "hi" {
		t.Fatalf("p1 = %+v", byID["p1"])
	}
	if owner := byID["owner@im.wechat"]; owner == nil || !owner.Allowed {
		t.Fatalf("owner = %+v", byID["owner@im.wechat"])
	}
	allowed, err := repo.ListAllowedPeerIDs(ctx, ch.ID)
	if err != nil || len(allowed) != 1 || allowed[0] != "owner@im.wechat" {
		t.Fatalf("allowed = %v, %v", allowed, err)
	}
}

func TestIlinkRepo_ConfirmLoginUnknownChannel(t *testing.T) {
	db := openChannelTestDB(t)
	_, err := newIlinkRepoForTest(db).ConfirmLogin(context.Background(), "missing", biz.IlinkLoginResult{BotToken: "bt"}, time.Now())
	if err != ErrNotFound {
		t.Fatalf("err = %v, want ErrNotFound", err)
	}
}

func TestIlinkRepo_UpsertPeerReportGenerationMismatch(t *testing.T) {
	db := openChannelTestDB(t)
	repo := newIlinkRepoForTest(db)
	ch := seedIlinkChannel(t, db)

	_, err := repo.UpsertPeerReport(context.Background(), ch.ID, 5, biz.IlinkPeerReport{PeerID: "p1", At: time.Now()})
	if !errors.Is(err, biz.ErrIlinkGenerationMismatch) {
		t.Fatalf("err = %v, want generation mismatch", err)
	}
}

func TestIlinkRepo_UpsertPeerReportKeepsTokenWhenEmpty(t *testing.T) {
	db := openChannelTestDB(t)
	repo := newIlinkRepoForTest(db)
	ctx := context.Background()
	ch := seedIlinkChannel(t, db)
	t1 := time.Date(2026, 10, 18, 10, 0, 0, 0, time.UTC)

	first, err := repo.UpsertPeerReport(ctx, ch.ID, 0, biz.IlinkPeerReport{PeerID: "p1", ContextToken: "tok-1", Preview: "first", At: t1})
	if err != nil {
		t.Fatalf("first report: %v", err)
	}
	if first.Allowed {
		t.Fatal("new peer must start not allowed")
	}
	second, err := repo.UpsertPeerReport(ctx, ch.ID, 0, biz.IlinkPeerReport{PeerID: "p1", Preview: "second", At: t1.Add(time.Minute)})
	if err != nil {
		t.Fatalf("second report: %v", err)
	}
	if second.ContextToken != "tok-1" || second.LastPreview != "second" {
		t.Fatalf("second = %+v", second)
	}
	if second.LastInboundAt == nil || !second.LastInboundAt.Equal(t1.Add(time.Minute)) {
		t.Fatalf("LastInboundAt = %v", second.LastInboundAt)
	}
}

func TestIlinkRepo_PutCursorCAS(t *testing.T) {
	db := openChannelTestDB(t)
	repo := newIlinkRepoForTest(db)
	ctx := context.Background()
	ch := seedIlinkChannel(t, db)
	now := time.Now()

	if err := repo.PutCursor(ctx, ch.ID, biz.IlinkCursor{Generation: 0, GetUpdatesBuf: "a"}, now); err != nil {
		t.Fatalf("PutCursor gen0: %v", err)
	}
	if err := repo.PutCursor(ctx, ch.ID, biz.IlinkCursor{Generation: 0, GetUpdatesBuf: "b"}, now); err != nil {
		t.Fatalf("PutCursor overwrite: %v", err)
	}
	cur, err := repo.GetCursor(ctx, ch.ID)
	if err != nil || cur.GetUpdatesBuf != "b" || cur.Generation != 0 {
		t.Fatalf("cursor = %+v, %v", cur, err)
	}
	if err := repo.PutCursor(ctx, ch.ID, biz.IlinkCursor{Generation: 1, GetUpdatesBuf: "c"}, now); !errors.Is(err, biz.ErrIlinkGenerationMismatch) {
		t.Fatalf("stale PutCursor err = %v", err)
	}
}

func TestIlinkRepo_MarkSessionExpiredIgnoresStaleGeneration(t *testing.T) {
	db := openChannelTestDB(t)
	repo := newIlinkRepoForTest(db)
	ctx := context.Background()
	ch := seedIlinkChannel(t, db)

	applied, err := repo.MarkSessionExpired(ctx, ch.ID, 7, time.Now())
	if err != nil || applied {
		t.Fatalf("applied=%v err=%v, want not applied", applied, err)
	}
	got, err := newChannelRepoForTest(db).GetByID(ctx, ch.ID)
	if err != nil || got.IlinkSessionExpiredAt != nil {
		t.Fatalf("channel = %+v, %v", got, err)
	}
}

func TestIlinkRepo_UpdateAndDeletePeer(t *testing.T) {
	db := openChannelTestDB(t)
	repo := newIlinkRepoForTest(db)
	ctx := context.Background()
	ch := seedIlinkChannel(t, db)
	yes := true
	remark := "张三"

	if _, err := repo.UpdatePeer(ctx, ch.ID, "p1", biz.IlinkPeerPatch{Allowed: &yes}); err != ErrNotFound {
		t.Fatalf("update missing err = %v", err)
	}
	if _, err := repo.UpsertPeerReport(ctx, ch.ID, 0, biz.IlinkPeerReport{PeerID: "p1", At: time.Now()}); err != nil {
		t.Fatalf("report: %v", err)
	}
	p, err := repo.UpdatePeer(ctx, ch.ID, "p1", biz.IlinkPeerPatch{Allowed: &yes, Remark: &remark})
	if err != nil || !p.Allowed || p.Remark != "张三" {
		t.Fatalf("UpdatePeer = %+v, %v", p, err)
	}
	if err := repo.DeletePeer(ctx, ch.ID, "p1"); err != nil {
		t.Fatalf("DeletePeer: %v", err)
	}
	if err := repo.DeletePeer(ctx, ch.ID, "p1"); err != ErrNotFound {
		t.Fatalf("second DeletePeer err = %v", err)
	}
	var count int64
	db.Model(&model.ChannelIlinkPeer{}).Where("channel_uuid = ?", ch.ID).Count(&count)
	if count != 0 {
		t.Fatalf("rows left = %d", count)
	}
}
```

- [ ] **Step 3: 运行确认失败**

Run: `go test ./internal/data/ -run TestIlinkRepo -count=1`  
Expected: FAIL，`undefined: weixinIlinkRepo`。

- [ ] **Step 4: 实现**

`portal/internal/data/channel_ilink_mysql.go`：

```go
package data

import (
	"context"
	"errors"
	"time"

	"backend/internal/biz"
	"backend/internal/data/model"

	"github.com/go-kratos/kratos/v2/log"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

var _ biz.WeixinIlinkRepo = (*weixinIlinkRepo)(nil)

type weixinIlinkRepo struct {
	db  *gorm.DB
	log *log.Helper
}

// NewWeixinIlinkRepo creates the GORM weixin_ilink repository.
func NewWeixinIlinkRepo(data *Data, logger log.Logger) biz.WeixinIlinkRepo {
	if data == nil || data.db == nil {
		panic("NewWeixinIlinkRepo: Data.db is nil, database config required")
	}
	return &weixinIlinkRepo{db: data.db, log: log.NewHelper(logger)}
}

var ilinkPeerKey = []clause.Column{{Name: "channel_uuid"}, {Name: "peer_id"}}

// ilinkGeneration row-locks the channel so a concurrent ConfirmLogin cannot slip between check and write.
func ilinkGeneration(tx *gorm.DB, channelUUID string) (int, error) {
	var ch model.Channel
	err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).
		Select("id", "ilink_generation").Where("id = ?", channelUUID).First(&ch).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return 0, ErrNotFound
	}
	if err != nil {
		return 0, err
	}
	return ch.IlinkGeneration, nil
}

func (r *weixinIlinkRepo) ConfirmLogin(ctx context.Context, channelUUID string, in biz.IlinkLoginResult, now time.Time) (*biz.ChannelMeta, error) {
	var out model.Channel
	err := r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		gen, err := ilinkGeneration(tx, channelUUID)
		if err != nil {
			return err
		}
		if err := tx.Model(&model.Channel{}).Where("id = ?", channelUUID).Updates(map[string]any{
			"ilink_bot_token":          in.BotToken,
			"ilink_bot_id":             in.BotID,
			"ilink_owner_user_id":      in.OwnerUserID,
			"ilink_base_url":           in.BaseURL,
			"ilink_generation":         gen + 1,
			"ilink_session_expired_at": nil,
			"updated_at":               now,
		}).Error; err != nil {
			return err
		}
		if err := tx.Where("channel_uuid = ?", channelUUID).Delete(&model.ChannelIlinkState{}).Error; err != nil {
			return err
		}
		if err := tx.Model(&model.ChannelIlinkPeer{}).Where("channel_uuid = ?", channelUUID).Updates(map[string]any{
			"context_token":    "",
			"context_token_at": nil,
			"updated_at":       now,
		}).Error; err != nil {
			return err
		}
		if in.OwnerUserID != "" {
			owner := model.ChannelIlinkPeer{ChannelUUID: channelUUID, PeerID: in.OwnerUserID, Allowed: true, CreatedAt: now, UpdatedAt: now}
			if err := tx.Clauses(clause.OnConflict{
				Columns:   ilinkPeerKey,
				DoUpdates: clause.Assignments(map[string]any{"allowed": true, "updated_at": now}),
			}).Create(&owner).Error; err != nil {
				return err
			}
		}
		return tx.Where("id = ?", channelUUID).First(&out).Error
	})
	if err != nil {
		return nil, err
	}
	return channelModelToBiz(&out), nil
}

func (r *weixinIlinkRepo) MarkSessionExpired(ctx context.Context, channelUUID string, generation int, now time.Time) (bool, error) {
	applied := false
	err := r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		res := tx.Model(&model.Channel{}).
			Where("id = ? AND ilink_generation = ?", channelUUID, generation).
			Updates(map[string]any{"ilink_session_expired_at": now, "updated_at": now})
		if res.Error != nil {
			return res.Error
		}
		if res.RowsAffected == 0 {
			return nil
		}
		applied = true
		return tx.Where("channel_uuid = ?", channelUUID).Delete(&model.ChannelIlinkState{}).Error
	})
	return applied, err
}

func (r *weixinIlinkRepo) UpsertPeerReport(ctx context.Context, channelUUID string, generation int, in biz.IlinkPeerReport) (*biz.IlinkPeer, error) {
	var out model.ChannelIlinkPeer
	err := r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		gen, err := ilinkGeneration(tx, channelUUID)
		if err != nil {
			return err
		}
		if gen != generation {
			return biz.ErrIlinkGenerationMismatch
		}
		at := in.At
		row := model.ChannelIlinkPeer{
			ChannelUUID:   channelUUID,
			PeerID:        in.PeerID,
			LastInboundAt: &at,
			LastPreview:   in.Preview,
			CreatedAt:     at,
			UpdatedAt:     at,
		}
		updates := map[string]any{"last_inbound_at": at, "last_preview": in.Preview, "updated_at": at}
		if in.ContextToken != "" {
			row.ContextToken = in.ContextToken
			row.ContextTokenAt = &at
			updates["context_token"] = in.ContextToken
			updates["context_token_at"] = at
		}
		if err := tx.Clauses(clause.OnConflict{Columns: ilinkPeerKey, DoUpdates: clause.Assignments(updates)}).Create(&row).Error; err != nil {
			return err
		}
		return tx.Where("channel_uuid = ? AND peer_id = ?", channelUUID, in.PeerID).First(&out).Error
	})
	if err != nil {
		return nil, err
	}
	return ilinkPeerModelToBiz(&out), nil
}

func (r *weixinIlinkRepo) ListPeers(ctx context.Context, channelUUID string) ([]*biz.IlinkPeer, error) {
	var rows []model.ChannelIlinkPeer
	if err := r.db.WithContext(ctx).Where("channel_uuid = ?", channelUUID).
		Order("updated_at DESC").Order("peer_id").Find(&rows).Error; err != nil {
		return nil, err
	}
	out := make([]*biz.IlinkPeer, 0, len(rows))
	for i := range rows {
		out = append(out, ilinkPeerModelToBiz(&rows[i]))
	}
	return out, nil
}

func (r *weixinIlinkRepo) UpdatePeer(ctx context.Context, channelUUID, peerID string, patch biz.IlinkPeerPatch) (*biz.IlinkPeer, error) {
	var out model.ChannelIlinkPeer
	err := r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		where := tx.Where("channel_uuid = ? AND peer_id = ?", channelUUID, peerID)
		if err := where.First(&out).Error; err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return ErrNotFound
			}
			return err
		}
		updates := map[string]any{"updated_at": time.Now()}
		if patch.Allowed != nil {
			updates["allowed"] = *patch.Allowed
		}
		if patch.Remark != nil {
			updates["remark"] = *patch.Remark
		}
		if err := tx.Model(&model.ChannelIlinkPeer{}).Where("channel_uuid = ? AND peer_id = ?", channelUUID, peerID).Updates(updates).Error; err != nil {
			return err
		}
		return tx.Where("channel_uuid = ? AND peer_id = ?", channelUUID, peerID).First(&out).Error
	})
	if err != nil {
		return nil, err
	}
	return ilinkPeerModelToBiz(&out), nil
}

func (r *weixinIlinkRepo) DeletePeer(ctx context.Context, channelUUID, peerID string) error {
	res := r.db.WithContext(ctx).Where("channel_uuid = ? AND peer_id = ?", channelUUID, peerID).Delete(&model.ChannelIlinkPeer{})
	if res.Error != nil {
		return res.Error
	}
	if res.RowsAffected == 0 {
		return ErrNotFound
	}
	return nil
}

func (r *weixinIlinkRepo) ListAllowedPeerIDs(ctx context.Context, channelUUID string) ([]string, error) {
	var ids []string
	if err := r.db.WithContext(ctx).Model(&model.ChannelIlinkPeer{}).
		Where("channel_uuid = ? AND allowed = ?", channelUUID, true).
		Order("peer_id").Pluck("peer_id", &ids).Error; err != nil {
		return nil, err
	}
	return ids, nil
}

func (r *weixinIlinkRepo) GetCursor(ctx context.Context, channelUUID string) (*biz.IlinkCursor, error) {
	var row model.ChannelIlinkState
	err := r.db.WithContext(ctx).Where("channel_uuid = ?", channelUUID).First(&row).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return &biz.IlinkCursor{}, nil
	}
	if err != nil {
		return nil, err
	}
	return &biz.IlinkCursor{Generation: row.Generation, GetUpdatesBuf: row.GetUpdatesBuf}, nil
}

func (r *weixinIlinkRepo) PutCursor(ctx context.Context, channelUUID string, cur biz.IlinkCursor, now time.Time) error {
	return r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		gen, err := ilinkGeneration(tx, channelUUID)
		if err != nil {
			return err
		}
		if gen != cur.Generation {
			return biz.ErrIlinkGenerationMismatch
		}
		row := model.ChannelIlinkState{ChannelUUID: channelUUID, Generation: cur.Generation, GetUpdatesBuf: cur.GetUpdatesBuf, UpdatedAt: now}
		return tx.Clauses(clause.OnConflict{
			Columns:   []clause.Column{{Name: "channel_uuid"}},
			DoUpdates: clause.AssignmentColumns([]string{"generation", "get_updates_buf", "updated_at"}),
		}).Create(&row).Error
	})
}

func ilinkPeerModelToBiz(m *model.ChannelIlinkPeer) *biz.IlinkPeer {
	return &biz.IlinkPeer{
		ChannelUUID:    m.ChannelUUID,
		PeerID:         m.PeerID,
		Allowed:        m.Allowed,
		Remark:         m.Remark,
		ContextToken:   m.ContextToken,
		ContextTokenAt: m.ContextTokenAt,
		LastInboundAt:  m.LastInboundAt,
		LastPreview:    m.LastPreview,
		CreatedAt:      m.CreatedAt,
		UpdatedAt:      m.UpdatedAt,
	}
}
```

`portal/internal/data/data.go` 的 `ProviderSet` 中，在 `NewChannelPeerSessionRepo` 之后加 `NewWeixinIlinkRepo`。

- [ ] **Step 5: 运行确认通过**

```powershell
gofmt -w internal/biz/weixin_ilink.go internal/data/channel_ilink_mysql.go internal/data/channel_ilink_mysql_test.go internal/data/data.go
go test ./internal/data/ -run "TestIlinkRepo|TestChannelRepo" -count=1
go build ./...
```

Expected: `ok  backend/internal/data`。

- [ ] **Step 6: Commit**

```powershell
git add internal/biz/weixin_ilink.go internal/data/channel_ilink_mysql.go internal/data/channel_ilink_mysql_test.go internal/data/data.go
git commit -m "feat(channel): weixin_ilink 仓储（扫码确认事务、名册、游标 generation CAS）"
```

---

### Task 6: `WeixinIlinkUsecase`

**Files:**
- Create: `portal/internal/biz/weixin_ilink_usecase.go`
- Modify: `portal/internal/biz/biz.go`
- Test: `portal/internal/biz/weixin_ilink_usecase_test.go`

- [ ] **Step 1: 写失败测试**

`portal/internal/biz/weixin_ilink_usecase_test.go`：

```go
package biz

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"
	"unicode/utf8"

	"backend/internal/ilink"
	pkgErrors "backend/internal/pkg/errors"
)

type ilinkFakeChannels struct {
	ChannelRepo
	byID map[string]*ChannelMeta
}

func (f *ilinkFakeChannels) GetByID(_ context.Context, id string) (*ChannelMeta, error) {
	ch, ok := f.byID[id]
	if !ok {
		return nil, pkgErrors.ErrNotFound
	}
	return ch, nil
}

func (f *ilinkFakeChannels) GetByChannelID(_ context.Context, slug string) (*ChannelMeta, error) {
	for _, ch := range f.byID {
		if ch.ChannelID == slug {
			return ch, nil
		}
	}
	return nil, pkgErrors.ErrNotFound
}

type ilinkFakeRepo struct {
	WeixinIlinkRepo
	confirmed   []IlinkLoginResult
	cursor      *IlinkCursor
	reports     []IlinkPeerReport
	reportGens  []int
	expiredGens []int
}

func (f *ilinkFakeRepo) ConfirmLogin(_ context.Context, channelUUID string, in IlinkLoginResult, _ time.Time) (*ChannelMeta, error) {
	f.confirmed = append(f.confirmed, in)
	return &ChannelMeta{ID: channelUUID, Type: ChannelTypeWeixinIlink, IlinkBotID: in.BotID}, nil
}

func (f *ilinkFakeRepo) GetCursor(context.Context, string) (*IlinkCursor, error) {
	if f.cursor == nil {
		return &IlinkCursor{}, nil
	}
	cp := *f.cursor
	return &cp, nil
}

func (f *ilinkFakeRepo) UpsertPeerReport(_ context.Context, _ string, generation int, in IlinkPeerReport) (*IlinkPeer, error) {
	f.reports = append(f.reports, in)
	f.reportGens = append(f.reportGens, generation)
	return &IlinkPeer{PeerID: in.PeerID}, nil
}

func (f *ilinkFakeRepo) MarkSessionExpired(_ context.Context, _ string, generation int, _ time.Time) (bool, error) {
	f.expiredGens = append(f.expiredGens, generation)
	return true, nil
}

type ilinkFakeLogin struct {
	qr       *ilink.QRCode
	statuses []*ilink.QRStatus
}

func (f *ilinkFakeLogin) GetBotQRCode(context.Context) (*ilink.QRCode, error) { return f.qr, nil }

func (f *ilinkFakeLogin) GetQRCodeStatus(context.Context, string) (*ilink.QRStatus, error) {
	if len(f.statuses) == 0 {
		return &ilink.QRStatus{Status: "wait"}, nil
	}
	s := f.statuses[0]
	f.statuses = f.statuses[1:]
	return s, nil
}

var ilinkTestNow = time.Date(2026, 10, 18, 10, 0, 0, 0, time.UTC)

func newIlinkUsecaseForTest(login *ilinkFakeLogin) (*WeixinIlinkUsecase, *ilinkFakeRepo, *authEphemeralFake) {
	channels := &ilinkFakeChannels{byID: map[string]*ChannelMeta{
		"uuid-1":   {ID: "uuid-1", ChannelID: "wx-1", Type: ChannelTypeWeixinIlink, Enabled: true, IlinkGeneration: 3},
		"uuid-2":   {ID: "uuid-2", ChannelID: "wx-2", Type: ChannelTypeWeixinIlink, Enabled: true},
		"uuid-web": {ID: "uuid-web", ChannelID: "web-1", Type: "web"},
	}}
	repo := &ilinkFakeRepo{}
	eph := newAuthEphemeralFake()
	uc := NewWeixinIlinkUsecaseWith(channels, repo, eph, login, func(content string) (string, error) {
		return "data:image/png;base64,FAKE-" + content, nil
	})
	uc.now = func() time.Time { return ilinkTestNow }
	return uc, repo, eph
}

func newQRLogin(statuses ...*ilink.QRStatus) *ilinkFakeLogin {
	return &ilinkFakeLogin{qr: &ilink.QRCode{QRCode: "qrc-1", ImgContent: "https://liteapp.weixin.qq.com/q/x"}, statuses: statuses}
}

func TestIlinkStartLogin_StoresQRCodeBinding(t *testing.T) {
	uc, _, eph := newIlinkUsecaseForTest(newQRLogin())

	res, err := uc.StartLogin(context.Background(), "uuid-1")
	if err != nil {
		t.Fatalf("StartLogin: %v", err)
	}
	if res.QRCode != "qrc-1" || !strings.HasPrefix(res.QRImage, "data:image/png;base64,") {
		t.Fatalf("res = %+v", res)
	}
	if !res.ExpiresAt.Equal(ilinkTestNow.Add(ilinkQRCodeTTL)) {
		t.Fatalf("ExpiresAt = %v", res.ExpiresAt)
	}
	item, ok := eph.get(ilinkQRCodeKey("qrc-1"))
	if !ok || item.kind != ilinkQRCodeKind || !strings.Contains(item.payload, "uuid-1") {
		t.Fatalf("ephemeral = %+v ok=%v", item, ok)
	}
}

func TestIlinkStartLogin_RejectsNonIlinkChannel(t *testing.T) {
	uc, _, _ := newIlinkUsecaseForTest(newQRLogin())
	if _, err := uc.StartLogin(context.Background(), "uuid-web"); !errors.Is(err, ErrIlinkNotIlinkChannel) {
		t.Fatalf("err = %v", err)
	}
	if _, err := uc.StartLogin(context.Background(), "missing"); !errors.Is(err, ErrChannelNotFound) {
		t.Fatalf("missing err = %v", err)
	}
}

func TestIlinkPollLogin_WaitKeepsBinding(t *testing.T) {
	uc, repo, _ := newIlinkUsecaseForTest(newQRLogin(&ilink.QRStatus{Status: "wait"}, &ilink.QRStatus{Status: "scaned"}))
	ctx := context.Background()
	if _, err := uc.StartLogin(ctx, "uuid-1"); err != nil {
		t.Fatalf("StartLogin: %v", err)
	}
	for _, want := range []string{"wait", "scaned", "wait"} {
		res, err := uc.PollLogin(ctx, "uuid-1", "qrc-1")
		if err != nil || res.Status != want {
			t.Fatalf("PollLogin = %+v, %v; want %s", res, err, want)
		}
	}
	if len(repo.confirmed) != 0 {
		t.Fatalf("confirmed = %+v", repo.confirmed)
	}
}

func TestIlinkPollLogin_ConfirmedPersistsAndConsumes(t *testing.T) {
	uc, repo, eph := newIlinkUsecaseForTest(newQRLogin(&ilink.QRStatus{
		Status: "confirmed", BotToken: "bt", BotID: "b@im.bot", UserID: "u@im.wechat", BaseURL: "https://ilinkai.weixin.qq.com",
	}))
	ctx := context.Background()
	if _, err := uc.StartLogin(ctx, "uuid-1"); err != nil {
		t.Fatalf("StartLogin: %v", err)
	}
	res, err := uc.PollLogin(ctx, "uuid-1", "qrc-1")
	if err != nil || res.Status != "confirmed" || res.BotID != "b@im.bot" {
		t.Fatalf("PollLogin = %+v, %v", res, err)
	}
	if len(repo.confirmed) != 1 {
		t.Fatalf("confirmed = %+v", repo.confirmed)
	}
	got := repo.confirmed[0]
	if got.BotToken != "bt" || got.BotID != "b@im.bot" || got.OwnerUserID != "u@im.wechat" || got.BaseURL != "https://ilinkai.weixin.qq.com" {
		t.Fatalf("confirm input = %+v", got)
	}
	if item, _ := eph.get(ilinkQRCodeKey("qrc-1")); !item.consumed {
		t.Fatal("qrcode binding not consumed")
	}
	if _, err := uc.PollLogin(ctx, "uuid-1", "qrc-1"); !errors.Is(err, ErrIlinkQRCodeNotFound) {
		t.Fatalf("poll after confirm err = %v", err)
	}
}

func TestIlinkPollLogin_ConfirmedWithoutTokenFails(t *testing.T) {
	uc, repo, _ := newIlinkUsecaseForTest(newQRLogin(&ilink.QRStatus{Status: "confirmed", BotID: "b@im.bot"}))
	ctx := context.Background()
	if _, err := uc.StartLogin(ctx, "uuid-1"); err != nil {
		t.Fatalf("StartLogin: %v", err)
	}
	if _, err := uc.PollLogin(ctx, "uuid-1", "qrc-1"); err == nil {
		t.Fatal("want error when bot_token is missing")
	}
	if len(repo.confirmed) != 0 {
		t.Fatalf("confirmed = %+v", repo.confirmed)
	}
}

func TestIlinkPollLogin_QRCodeOfOtherChannelNotFound(t *testing.T) {
	uc, _, _ := newIlinkUsecaseForTest(newQRLogin())
	ctx := context.Background()
	if _, err := uc.StartLogin(ctx, "uuid-1"); err != nil {
		t.Fatalf("StartLogin: %v", err)
	}
	if _, err := uc.PollLogin(ctx, "uuid-2", "qrc-1"); !errors.Is(err, ErrIlinkQRCodeNotFound) {
		t.Fatalf("err = %v", err)
	}
	if _, err := uc.PollLogin(ctx, "uuid-1", "never-issued"); !errors.Is(err, ErrIlinkQRCodeNotFound) {
		t.Fatalf("unknown qrcode err = %v", err)
	}
}

func TestIlinkPollLogin_ExpiredConsumes(t *testing.T) {
	uc, _, eph := newIlinkUsecaseForTest(newQRLogin(&ilink.QRStatus{Status: "expired"}))
	ctx := context.Background()
	if _, err := uc.StartLogin(ctx, "uuid-1"); err != nil {
		t.Fatalf("StartLogin: %v", err)
	}
	res, err := uc.PollLogin(ctx, "uuid-1", "qrc-1")
	if err != nil || res.Status != "expired" {
		t.Fatalf("PollLogin = %+v, %v", res, err)
	}
	if item, _ := eph.get(ilinkQRCodeKey("qrc-1")); !item.consumed {
		t.Fatal("expired qrcode binding not consumed")
	}
}

func TestIlinkGetCursor_GenerationMismatchReturnsEmpty(t *testing.T) {
	uc, repo, _ := newIlinkUsecaseForTest(newQRLogin())
	ctx := context.Background()

	repo.cursor = &IlinkCursor{Generation: 2, GetUpdatesBuf: "old"}
	cur, err := uc.GetCursor(ctx, "wx-1")
	if err != nil || cur.Generation != 3 || cur.GetUpdatesBuf != "" {
		t.Fatalf("stale cursor = %+v, %v", cur, err)
	}
	repo.cursor = &IlinkCursor{Generation: 3, GetUpdatesBuf: "cur"}
	cur, err = uc.GetCursor(ctx, "wx-1")
	if err != nil || cur.GetUpdatesBuf != "cur" {
		t.Fatalf("current cursor = %+v, %v", cur, err)
	}
}

func TestIlinkReportPeer_TruncatesPreviewAndDefaultsTime(t *testing.T) {
	uc, repo, _ := newIlinkUsecaseForTest(newQRLogin())

	_, err := uc.ReportPeer(context.Background(), "wx-1", 3, IlinkPeerReport{PeerID: " p1 ", Preview: strings.Repeat("字", 100)})
	if err != nil {
		t.Fatalf("ReportPeer: %v", err)
	}
	got := repo.reports[0]
	if got.PeerID != "p1" || utf8.RuneCountInString(got.Preview) != ilinkPreviewRunes || !got.At.Equal(ilinkTestNow) {
		t.Fatalf("report = %+v", got)
	}
	if repo.reportGens[0] != 3 {
		t.Fatalf("generation = %d", repo.reportGens[0])
	}
}

func TestIlinkReportPeer_Validation(t *testing.T) {
	uc, _, _ := newIlinkUsecaseForTest(newQRLogin())
	ctx := context.Background()
	if _, err := uc.ReportPeer(ctx, "missing", 0, IlinkPeerReport{PeerID: "p1"}); !errors.Is(err, ErrChannelNotFound) {
		t.Fatalf("unknown slug err = %v", err)
	}
	if _, err := uc.ReportPeer(ctx, "web-1", 0, IlinkPeerReport{PeerID: "p1"}); !errors.Is(err, ErrIlinkNotIlinkChannel) {
		t.Fatalf("non-ilink err = %v", err)
	}
	if _, err := uc.ReportPeer(ctx, "wx-1", 3, IlinkPeerReport{PeerID: "  "}); err == nil {
		t.Fatal("want error for empty peer_id")
	}
}

func TestIlinkReportSessionExpired_PassesGeneration(t *testing.T) {
	uc, repo, _ := newIlinkUsecaseForTest(newQRLogin())
	if err := uc.ReportSessionExpired(context.Background(), "wx-1", 3); err != nil {
		t.Fatalf("ReportSessionExpired: %v", err)
	}
	if len(repo.expiredGens) != 1 || repo.expiredGens[0] != 3 {
		t.Fatalf("expiredGens = %v", repo.expiredGens)
	}
}

func TestIlinkSafeBaseURL(t *testing.T) {
	cases := map[string]string{
		"":                                  "",
		"https://ilinkai.weixin.qq.com":     "https://ilinkai.weixin.qq.com",
		"https://ilinkai.weixin.qq.com/":    "https://ilinkai.weixin.qq.com",
		"https://weixin.qq.com":             "https://weixin.qq.com",
		"http://ilinkai.weixin.qq.com":      "",
		"https://evil.example.com":          "",
		"https://weixin.qq.com.evil.com":    "",
		"https://user@ilinkai.weixin.qq.com": "",
		"::not a url":                       "",
	}
	for in, want := range cases {
		if got := ilinkSafeBaseURL(in); got != want {
			t.Errorf("ilinkSafeBaseURL(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestIlinkPollLogin_UntrustedBaseURLFallsBackToDefault(t *testing.T) {
	uc, repo, _ := newIlinkUsecaseForTest(newQRLogin(&ilink.QRStatus{
		Status: "confirmed", BotToken: "bt", BotID: "b@im.bot", UserID: "u@im.wechat", BaseURL: "https://evil.example.com",
	}))
	ctx := context.Background()
	if _, err := uc.StartLogin(ctx, "uuid-1"); err != nil {
		t.Fatalf("StartLogin: %v", err)
	}
	if _, err := uc.PollLogin(ctx, "uuid-1", "qrc-1"); err != nil {
		t.Fatalf("PollLogin: %v", err)
	}
	if got := repo.confirmed[0].BaseURL; got != "" {
		t.Fatalf("BaseURL = %q, want empty (default)", got)
	}
}

func TestIlinkUpdatePeer_Validation(t *testing.T) {
	uc, _, _ := newIlinkUsecaseForTest(newQRLogin())
	ctx := context.Background()
	if _, err := uc.UpdatePeer(ctx, "uuid-1", "p1", IlinkPeerPatch{}); err == nil {
		t.Fatal("want error for empty patch")
	}
	long := strings.Repeat("名", ilinkRemarkMaxRunes+1)
	if _, err := uc.UpdatePeer(ctx, "uuid-1", "p1", IlinkPeerPatch{Remark: &long}); err == nil {
		t.Fatal("want error for long remark")
	}
}
```

- [ ] **Step 2: 运行确认失败**

Run: `go test ./internal/biz/ -run TestIlink -count=1`  
Expected: FAIL，`undefined: NewWeixinIlinkUsecaseWith` 等。

- [ ] **Step 3: 实现**

`portal/internal/biz/weixin_ilink_usecase.go`：

```go
package biz

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"net/url"
	"strings"
	"time"
	"unicode/utf8"

	"backend/internal/ilink"
	pkgErrors "backend/internal/pkg/errors"

	kratosErrors "github.com/go-kratos/kratos/v2/errors"
)

const (
	ilinkQRCodeKind     = "ilink_qrcode"
	ilinkQRCodeTTL      = 5 * time.Minute
	ilinkPreviewRunes   = 40
	ilinkRemarkMaxRunes = 64
)

// IlinkLoginClient is the QR login subset of the iLink client (*ilink.Client satisfies it).
type IlinkLoginClient interface {
	GetBotQRCode(ctx context.Context) (*ilink.QRCode, error)
	GetQRCodeStatus(ctx context.Context, qrcode string) (*ilink.QRStatus, error)
}

type IlinkLoginStart struct {
	QRCode    string
	QRImage   string
	ExpiresAt time.Time
}

type IlinkLoginPoll struct {
	Status string // wait | scaned | confirmed | expired
	BotID  string
}

// WeixinIlinkUsecase orchestrates weixin_ilink QR login, roster management and Gateway runtime calls.
type WeixinIlinkUsecase struct {
	channels  ChannelRepo
	repo      WeixinIlinkRepo
	ephemeral AuthEphemeralRepo
	login     IlinkLoginClient
	qrImage   func(content string) (string, error)
	now       func() time.Time
}

func NewWeixinIlinkUsecase(channels ChannelRepo, repo WeixinIlinkRepo, ephemeral AuthEphemeralRepo) *WeixinIlinkUsecase {
	return NewWeixinIlinkUsecaseWith(channels, repo, ephemeral, ilink.NewClient(ilink.DefaultBaseURL, ""), ilink.QRCodePNGDataURL)
}

func NewWeixinIlinkUsecaseWith(channels ChannelRepo, repo WeixinIlinkRepo, ephemeral AuthEphemeralRepo, login IlinkLoginClient, qrImage func(string) (string, error)) *WeixinIlinkUsecase {
	return &WeixinIlinkUsecase{
		channels:  channels,
		repo:      repo,
		ephemeral: ephemeral,
		login:     login,
		qrImage:   qrImage,
		now:       time.Now,
	}
}

type ilinkQRCodePayload struct {
	ChannelUUID string `json:"channel_uuid"`
}

func ilinkQRCodeKey(qrcode string) string {
	sum := sha256.Sum256([]byte(qrcode))
	return hex.EncodeToString(sum[:])
}

func ilinkTruncate(s string, n int) string {
	r := []rune(s)
	if len(r) <= n {
		return s
	}
	return string(r[:n])
}

// ilinkSafeBaseURL keeps the server-provided base URL only if it is https on weixin.qq.com,
// because the bot_token is later sent there; "" means the default base URL.
func ilinkSafeBaseURL(raw string) string {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return ""
	}
	u, err := url.Parse(raw)
	if err != nil || u.Scheme != "https" || u.User != nil {
		return ""
	}
	host := strings.ToLower(u.Hostname())
	if host != "weixin.qq.com" && !strings.HasSuffix(host, ".weixin.qq.com") {
		return ""
	}
	return strings.TrimRight(raw, "/")
}

func ilinkUpstreamErr(op string, err error) error {
	return kratosErrors.ServiceUnavailable("ILINK_UPSTREAM", op+"失败："+err.Error())
}

func (uc *WeixinIlinkUsecase) checkIlink(ch *ChannelMeta, err error) (*ChannelMeta, error) {
	if errors.Is(err, pkgErrors.ErrNotFound) {
		return nil, ErrChannelNotFound
	}
	if err != nil {
		return nil, err
	}
	if ch.Type != ChannelTypeWeixinIlink {
		return nil, ErrIlinkNotIlinkChannel
	}
	return ch, nil
}

func (uc *WeixinIlinkUsecase) channelByID(ctx context.Context, channelUUID string) (*ChannelMeta, error) {
	return uc.checkIlink(uc.channels.GetByID(ctx, strings.TrimSpace(channelUUID)))
}

func (uc *WeixinIlinkUsecase) channelBySlug(ctx context.Context, channelID string) (*ChannelMeta, error) {
	return uc.checkIlink(uc.channels.GetByChannelID(ctx, strings.TrimSpace(channelID)))
}

// StartLogin fetches a QR code and binds it to the channel for ilinkQRCodeTTL.
func (uc *WeixinIlinkUsecase) StartLogin(ctx context.Context, channelUUID string) (*IlinkLoginStart, error) {
	ch, err := uc.channelByID(ctx, channelUUID)
	if err != nil {
		return nil, err
	}
	qr, err := uc.login.GetBotQRCode(ctx)
	if err != nil {
		return nil, ilinkUpstreamErr("获取二维码", err)
	}
	img, err := uc.qrImage(qr.ImgContent)
	if err != nil {
		return nil, kratosErrors.InternalServer("ILINK_QRCODE_RENDER", err.Error())
	}
	payload, err := json.Marshal(ilinkQRCodePayload{ChannelUUID: ch.ID})
	if err != nil {
		return nil, err
	}
	expires := uc.now().Add(ilinkQRCodeTTL)
	if err := uc.ephemeral.Put(ctx, ilinkQRCodeKey(qr.QRCode), ilinkQRCodeKind, string(payload), expires); err != nil {
		return nil, err
	}
	return &IlinkLoginStart{QRCode: qr.QRCode, QRImage: img, ExpiresAt: expires}, nil
}

// PollLogin relays one get_qrcode_status round (the server may hold it ~30s) and persists on confirmed.
func (uc *WeixinIlinkUsecase) PollLogin(ctx context.Context, channelUUID, qrcode string) (*IlinkLoginPoll, error) {
	qrcode = strings.TrimSpace(qrcode)
	if qrcode == "" {
		return nil, kratosErrors.BadRequest("INVALID_ARGUMENT", "qrcode is required")
	}
	ch, err := uc.channelByID(ctx, channelUUID)
	if err != nil {
		return nil, err
	}
	key := ilinkQRCodeKey(qrcode)
	raw, err := uc.ephemeral.Get(ctx, key, ilinkQRCodeKind, uc.now())
	if errors.Is(err, pkgErrors.ErrNotFound) {
		return nil, ErrIlinkQRCodeNotFound
	}
	if err != nil {
		return nil, err
	}
	var p ilinkQRCodePayload
	if json.Unmarshal([]byte(raw), &p) != nil || p.ChannelUUID != ch.ID {
		return nil, ErrIlinkQRCodeNotFound
	}

	st, err := uc.login.GetQRCodeStatus(ctx, qrcode)
	if err != nil {
		return nil, ilinkUpstreamErr("查询扫码状态", err)
	}
	switch st.Status {
	case "confirmed":
		if strings.TrimSpace(st.BotToken) == "" {
			return nil, kratosErrors.ServiceUnavailable("ILINK_UPSTREAM", "扫码确认结果缺少 bot_token")
		}
		meta, err := uc.repo.ConfirmLogin(ctx, ch.ID, IlinkLoginResult{
			BotToken:    st.BotToken,
			BotID:       st.BotID,
			OwnerUserID: st.UserID,
			BaseURL:     ilinkSafeBaseURL(st.BaseURL),
		}, uc.now())
		if errors.Is(err, pkgErrors.ErrNotFound) {
			return nil, ErrChannelNotFound
		}
		if err != nil {
			return nil, err
		}
		if _, err := uc.ephemeral.Consume(ctx, key, ilinkQRCodeKind, uc.now()); err != nil && !errors.Is(err, pkgErrors.ErrNotFound) {
			return nil, err
		}
		return &IlinkLoginPoll{Status: "confirmed", BotID: meta.IlinkBotID}, nil
	case "expired":
		_, _ = uc.ephemeral.Consume(ctx, key, ilinkQRCodeKind, uc.now())
		return &IlinkLoginPoll{Status: "expired"}, nil
	case "scaned":
		return &IlinkLoginPoll{Status: "scaned"}, nil
	default:
		return &IlinkLoginPoll{Status: "wait"}, nil
	}
}

func (uc *WeixinIlinkUsecase) ListPeers(ctx context.Context, channelUUID string) ([]*IlinkPeer, error) {
	ch, err := uc.channelByID(ctx, channelUUID)
	if err != nil {
		return nil, err
	}
	return uc.repo.ListPeers(ctx, ch.ID)
}

func (uc *WeixinIlinkUsecase) UpdatePeer(ctx context.Context, channelUUID, peerID string, patch IlinkPeerPatch) (*IlinkPeer, error) {
	peerID = strings.TrimSpace(peerID)
	if peerID == "" {
		return nil, kratosErrors.BadRequest("INVALID_ARGUMENT", "peer_id is required")
	}
	if patch.Allowed == nil && patch.Remark == nil {
		return nil, kratosErrors.BadRequest("INVALID_ARGUMENT", "allowed or remark is required")
	}
	if patch.Remark != nil {
		remark := strings.TrimSpace(*patch.Remark)
		if utf8.RuneCountInString(remark) > ilinkRemarkMaxRunes {
			return nil, kratosErrors.BadRequest("INVALID_ARGUMENT", "remark is too long")
		}
		patch.Remark = &remark
	}
	ch, err := uc.channelByID(ctx, channelUUID)
	if err != nil {
		return nil, err
	}
	p, err := uc.repo.UpdatePeer(ctx, ch.ID, peerID, patch)
	if errors.Is(err, pkgErrors.ErrNotFound) {
		return nil, ErrIlinkPeerNotFound
	}
	return p, err
}

func (uc *WeixinIlinkUsecase) DeletePeer(ctx context.Context, channelUUID, peerID string) error {
	ch, err := uc.channelByID(ctx, channelUUID)
	if err != nil {
		return err
	}
	err = uc.repo.DeletePeer(ctx, ch.ID, strings.TrimSpace(peerID))
	if errors.Is(err, pkgErrors.ErrNotFound) {
		return ErrIlinkPeerNotFound
	}
	return err
}

// AllowedPeers lists allowed peer IDs by channels.id (Gateway channel sync).
func (uc *WeixinIlinkUsecase) AllowedPeers(ctx context.Context, channelUUID string) ([]string, error) {
	return uc.repo.ListAllowedPeerIDs(ctx, channelUUID)
}

// ReportPeer records an inbound message; channelID is the channel slug used by the Gateway.
func (uc *WeixinIlinkUsecase) ReportPeer(ctx context.Context, channelID string, generation int, in IlinkPeerReport) (*IlinkPeer, error) {
	in.PeerID = strings.TrimSpace(in.PeerID)
	if in.PeerID == "" {
		return nil, kratosErrors.BadRequest("INVALID_ARGUMENT", "peer_id is required")
	}
	ch, err := uc.channelBySlug(ctx, channelID)
	if err != nil {
		return nil, err
	}
	if in.At.IsZero() {
		in.At = uc.now()
	}
	in.Preview = ilinkTruncate(strings.TrimSpace(in.Preview), ilinkPreviewRunes)
	return uc.repo.UpsertPeerReport(ctx, ch.ID, generation, in)
}

// GetCursor returns an empty cursor for the current generation when the stored one is stale.
func (uc *WeixinIlinkUsecase) GetCursor(ctx context.Context, channelID string) (*IlinkCursor, error) {
	ch, err := uc.channelBySlug(ctx, channelID)
	if err != nil {
		return nil, err
	}
	cur, err := uc.repo.GetCursor(ctx, ch.ID)
	if err != nil {
		return nil, err
	}
	if cur.Generation != ch.IlinkGeneration {
		return &IlinkCursor{Generation: ch.IlinkGeneration}, nil
	}
	return cur, nil
}

func (uc *WeixinIlinkUsecase) PutCursor(ctx context.Context, channelID string, cur IlinkCursor) error {
	ch, err := uc.channelBySlug(ctx, channelID)
	if err != nil {
		return err
	}
	return uc.repo.PutCursor(ctx, ch.ID, cur, uc.now())
}

// ReportSessionExpired marks the channel login_required; ignored when generation is stale.
func (uc *WeixinIlinkUsecase) ReportSessionExpired(ctx context.Context, channelID string, generation int) error {
	ch, err := uc.channelBySlug(ctx, channelID)
	if err != nil {
		return err
	}
	_, err = uc.repo.MarkSessionExpired(ctx, ch.ID, generation, uc.now())
	return err
}
```

`portal/internal/biz/biz.go` 的 `ProviderSet` 中，在 `NewChannelPeerUsecase` 之后加 `NewWeixinIlinkUsecase`（**不要**加入 `NewWeixinIlinkUsecaseWith`）。

- [ ] **Step 4: 运行确认通过**

```powershell
gofmt -w internal/biz/weixin_ilink_usecase.go internal/biz/weixin_ilink_usecase_test.go internal/biz/biz.go
go test ./internal/biz/ -run "TestIlink|TestDeriveRuntimeStatus|Auth" -count=1
go build ./...
```

Expected: `ok  backend/internal/biz`。

- [ ] **Step 5: Commit**

```powershell
git add internal/biz/weixin_ilink_usecase.go internal/biz/weixin_ilink_usecase_test.go internal/biz/biz.go
git commit -m "feat(channel): WeixinIlinkUsecase 扫码登录、名册与 Runtime 操作"
```

---

### Task 7: Runtime API（Gateway 侧）

**Files:**
- Create: `portal/internal/runtime/ilink.go`
- Modify: `portal/internal/runtime/service.go`、`portal/internal/runtime/http.go`
- Test: `portal/internal/runtime/ilink_test.go`

- [ ] **Step 1: 写失败测试**

`portal/internal/runtime/ilink_test.go`：

```go
package runtime

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"backend/internal/biz"
	"backend/internal/ilink"
)

type fakeIlinkBackend struct {
	allowed     map[string][]string
	reportSlug  string
	reportGen   int
	report      biz.IlinkPeerReport
	reportErr   error
	cursor      *biz.IlinkCursor
	putSlug     string
	put         *biz.IlinkCursor
	putErr      error
	expiredSlug string
	expiredGen  int
}

func (f *fakeIlinkBackend) AllowedPeers(_ context.Context, channelUUID string) ([]string, error) {
	return f.allowed[channelUUID], nil
}

func (f *fakeIlinkBackend) ReportPeer(_ context.Context, channelID string, generation int, in biz.IlinkPeerReport) (*biz.IlinkPeer, error) {
	f.reportSlug, f.reportGen, f.report = channelID, generation, in
	if f.reportErr != nil {
		return nil, f.reportErr
	}
	return &biz.IlinkPeer{PeerID: in.PeerID, Allowed: true}, nil
}

func (f *fakeIlinkBackend) GetCursor(context.Context, string) (*biz.IlinkCursor, error) {
	return f.cursor, nil
}

func (f *fakeIlinkBackend) PutCursor(_ context.Context, channelID string, cur biz.IlinkCursor) error {
	f.putSlug = channelID
	f.put = &cur
	return f.putErr
}

func (f *fakeIlinkBackend) ReportSessionExpired(_ context.Context, channelID string, generation int) error {
	f.expiredSlug, f.expiredGen = channelID, generation
	return nil
}

func serveIlinkRuntime(t *testing.T, svc *Service, method, path, body string) *httptest.ResponseRecorder {
	t.Helper()
	srv := testRuntimeServer(t, svc)
	rec := httptest.NewRecorder()
	srv.ServeHTTP(rec, runtimeReq(method, path, body, "", true))
	return rec
}

func TestGatewayListChannels_WeixinIlinkFields(t *testing.T) {
	expired := time.Date(2026, 10, 18, 2, 0, 0, 0, time.UTC)
	svc := newTestService(nil, nil, nil)
	svc.channels = &fakeGatewayChannels{byID: map[string]*biz.ChannelMeta{}, list: []*biz.ChannelMeta{{
		ID: "uuid-1", ChannelID: "wx-1", Type: biz.ChannelTypeWeixinIlink, Enabled: true,
		IlinkBotToken: "bt", IlinkGeneration: 4, IlinkSessionExpiredAt: &expired,
	}}}
	svc.SetIlink(&fakeIlinkBackend{allowed: map[string][]string{"uuid-1": {"p1@im.wechat"}}})

	rec := serveIlinkRuntime(t, svc, http.MethodGet, "/runtime/v1/gateway/channels", "")
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d body=%s", rec.Code, rec.Body.String())
	}
	var body struct {
		Channels []map[string]any `json:"channels"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode: %v", err)
	}
	item := body.Channels[0]
	if item["id"] != "wx-1" || item["bot_token"] != "bt" || item["base_url"] != ilink.DefaultBaseURL {
		t.Fatalf("item = %+v", item)
	}
	if item["generation"] != float64(4) || item["session_expired"] != true {
		t.Fatalf("generation/session_expired = %v / %v", item["generation"], item["session_expired"])
	}
	peers, _ := item["allowed_peers"].([]any)
	if len(peers) != 1 || peers[0] != "p1@im.wechat" {
		t.Fatalf("allowed_peers = %v", item["allowed_peers"])
	}
}

func TestIlinkReportPeer_PassesThrough(t *testing.T) {
	backend := &fakeIlinkBackend{}
	svc := newTestService(nil, nil, nil)
	svc.SetIlink(backend)

	rec := serveIlinkRuntime(t, svc, http.MethodPost, "/runtime/v1/gateway/channels/wx-1/ilink/peers",
		`{"generation":4,"peer_id":"p1","context_token":"c","preview":"hi","at":"2026-10-18T02:00:00Z"}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d body=%s", rec.Code, rec.Body.String())
	}
	if backend.reportSlug != "wx-1" || backend.reportGen != 4 || backend.report.PeerID != "p1" || backend.report.ContextToken != "c" {
		t.Fatalf("backend got slug=%q gen=%d report=%+v", backend.reportSlug, backend.reportGen, backend.report)
	}
	if !backend.report.At.Equal(time.Date(2026, 10, 18, 2, 0, 0, 0, time.UTC)) {
		t.Fatalf("at = %v", backend.report.At)
	}
	var out map[string]any
	_ = json.Unmarshal(rec.Body.Bytes(), &out)
	if out["allowed"] != true {
		t.Fatalf("body = %s", rec.Body.String())
	}
}

func TestIlinkReportPeer_GenerationMismatchIs409(t *testing.T) {
	svc := newTestService(nil, nil, nil)
	svc.SetIlink(&fakeIlinkBackend{reportErr: biz.ErrIlinkGenerationMismatch})

	rec := serveIlinkRuntime(t, svc, http.MethodPost, "/runtime/v1/gateway/channels/wx-1/ilink/peers", `{"generation":1,"peer_id":"p1"}`)
	if rec.Code != http.StatusConflict {
		t.Fatalf("status = %d, want 409; body=%s", rec.Code, rec.Body.String())
	}
}

func TestIlinkReportPeer_BadAt(t *testing.T) {
	svc := newTestService(nil, nil, nil)
	svc.SetIlink(&fakeIlinkBackend{})

	rec := serveIlinkRuntime(t, svc, http.MethodPost, "/runtime/v1/gateway/channels/wx-1/ilink/peers", `{"peer_id":"p1","at":"yesterday"}`)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", rec.Code)
	}
}

func TestIlinkCursor_GetAndPut(t *testing.T) {
	backend := &fakeIlinkBackend{cursor: &biz.IlinkCursor{Generation: 4, GetUpdatesBuf: "buf-1"}}
	svc := newTestService(nil, nil, nil)
	svc.SetIlink(backend)

	rec := serveIlinkRuntime(t, svc, http.MethodGet, "/runtime/v1/gateway/channels/wx-1/ilink/cursor", "")
	if rec.Code != http.StatusOK {
		t.Fatalf("GET status = %d body=%s", rec.Code, rec.Body.String())
	}
	var got map[string]any
	_ = json.Unmarshal(rec.Body.Bytes(), &got)
	if got["generation"] != float64(4) || got["get_updates_buf"] != "buf-1" {
		t.Fatalf("GET body = %s", rec.Body.String())
	}

	rec = serveIlinkRuntime(t, svc, http.MethodPut, "/runtime/v1/gateway/channels/wx-1/ilink/cursor", `{"generation":4,"get_updates_buf":"buf-2"}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("PUT status = %d body=%s", rec.Code, rec.Body.String())
	}
	if backend.putSlug != "wx-1" || backend.put == nil || backend.put.Generation != 4 || backend.put.GetUpdatesBuf != "buf-2" {
		t.Fatalf("put = %q %+v", backend.putSlug, backend.put)
	}

	backend.putErr = biz.ErrIlinkGenerationMismatch
	rec = serveIlinkRuntime(t, svc, http.MethodPut, "/runtime/v1/gateway/channels/wx-1/ilink/cursor", `{"generation":3,"get_updates_buf":"x"}`)
	if rec.Code != http.StatusConflict {
		t.Fatalf("stale PUT status = %d, want 409", rec.Code)
	}
}

func TestIlinkSessionExpired(t *testing.T) {
	backend := &fakeIlinkBackend{}
	svc := newTestService(nil, nil, nil)
	svc.SetIlink(backend)

	rec := serveIlinkRuntime(t, svc, http.MethodPost, "/runtime/v1/gateway/channels/wx-1/ilink/session-expired", `{"generation":4,"reason":"-14"}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d body=%s", rec.Code, rec.Body.String())
	}
	if backend.expiredSlug != "wx-1" || backend.expiredGen != 4 {
		t.Fatalf("expired = %q %d", backend.expiredSlug, backend.expiredGen)
	}
}

func TestIlinkEndpoints_UnavailableWithoutBackend(t *testing.T) {
	svc := newTestService(nil, nil, nil)
	rec := serveIlinkRuntime(t, svc, http.MethodGet, "/runtime/v1/gateway/channels/wx-1/ilink/cursor", "")
	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, want 500", rec.Code)
	}
}

func TestIlinkEndpoints_RequireToken(t *testing.T) {
	svc := newTestService(nil, nil, nil)
	svc.SetIlink(&fakeIlinkBackend{})
	srv := testRuntimeServer(t, svc)
	rec := httptest.NewRecorder()
	srv.ServeHTTP(rec, runtimeReq(http.MethodGet, "/runtime/v1/gateway/channels/wx-1/ilink/cursor", "", "", false))
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401", rec.Code)
	}
}
```

- [ ] **Step 2: 运行确认失败**

Run: `go test ./internal/runtime/ -run "Ilink|WeixinIlink" -count=1`  
Expected: FAIL，`svc.SetIlink undefined`。

- [ ] **Step 3: 实现**

`portal/internal/runtime/ilink.go`：

```go
package runtime

import (
	"context"
	"strings"
	"time"

	"backend/internal/biz"
	"backend/internal/ilink"

	kratosErrors "github.com/go-kratos/kratos/v2/errors"
	khttp "github.com/go-kratos/kratos/v2/transport/http"
)

// ilinkBackend is the weixin_ilink surface used by the Gateway; channelID arguments are channel slugs.
type ilinkBackend interface {
	AllowedPeers(ctx context.Context, channelUUID string) ([]string, error)
	ReportPeer(ctx context.Context, channelID string, generation int, in biz.IlinkPeerReport) (*biz.IlinkPeer, error)
	GetCursor(ctx context.Context, channelID string) (*biz.IlinkCursor, error)
	PutCursor(ctx context.Context, channelID string, cur biz.IlinkCursor) error
	ReportSessionExpired(ctx context.Context, channelID string, generation int) error
}

var _ ilinkBackend = (*biz.WeixinIlinkUsecase)(nil)

// SetIlink wires the weixin_ilink backend; without it the ilink endpoints return 500.
func (s *Service) SetIlink(b ilinkBackend) {
	if s != nil {
		s.ilink = b
	}
}

type ilinkPeerReportRequest struct {
	Generation   int    `json:"generation"`
	PeerID       string `json:"peer_id"`
	ContextToken string `json:"context_token"`
	Preview      string `json:"preview"`
	At           string `json:"at"`
}

type ilinkCursorBody struct {
	Generation    int    `json:"generation"`
	GetUpdatesBuf string `json:"get_updates_buf"`
}

type ilinkSessionExpiredRequest struct {
	Generation int    `json:"generation"`
	Reason     string `json:"reason"`
}

func (s *Service) ilinkBackendOrErr() (ilinkBackend, error) {
	if s.ilink == nil {
		return nil, kratosErrors.InternalServer("UNAVAILABLE", "weixin_ilink backend unavailable")
	}
	return s.ilink, nil
}

// fillIlinkGatewayItem adds weixin_ilink fields to a gateway channel item; allowlist errors fail the sync.
func (s *Service) fillIlinkGatewayItem(ctx context.Context, ch *biz.ChannelMeta, item *gatewayChannelItem) error {
	item.BotToken = ch.IlinkBotToken
	item.BaseURL = ch.IlinkBaseURL
	if item.BaseURL == "" {
		item.BaseURL = ilink.DefaultBaseURL
	}
	item.Generation = ch.IlinkGeneration
	item.SessionExpired = ch.IlinkSessionExpiredAt != nil
	if s.ilink == nil {
		return nil
	}
	peers, err := s.ilink.AllowedPeers(ctx, ch.ID)
	if err != nil {
		return err
	}
	item.AllowedPeers = peers
	return nil
}

func (s *Service) handleIlinkReportPeer(ctx context.Context, hctx khttp.Context) error {
	b, err := s.ilinkBackendOrErr()
	if err != nil {
		return err
	}
	var req ilinkPeerReportRequest
	if err := decodeJSON(hctx, &req); err != nil {
		return kratosErrors.BadRequest("INVALID_ARGUMENT", "invalid body")
	}
	in := biz.IlinkPeerReport{PeerID: req.PeerID, ContextToken: req.ContextToken, Preview: req.Preview}
	if strings.TrimSpace(req.At) != "" {
		at, err := time.Parse(time.RFC3339, req.At)
		if err != nil {
			return kratosErrors.BadRequest("INVALID_ARGUMENT", "at must be RFC3339")
		}
		in.At = at
	}
	peer, err := b.ReportPeer(ctx, hctx.Vars().Get("channel_id"), req.Generation, in)
	if err != nil {
		return err
	}
	return hctx.JSON(200, map[string]any{"allowed": peer.Allowed})
}

func (s *Service) handleIlinkGetCursor(ctx context.Context, hctx khttp.Context) error {
	b, err := s.ilinkBackendOrErr()
	if err != nil {
		return err
	}
	cur, err := b.GetCursor(ctx, hctx.Vars().Get("channel_id"))
	if err != nil {
		return err
	}
	return hctx.JSON(200, ilinkCursorBody{Generation: cur.Generation, GetUpdatesBuf: cur.GetUpdatesBuf})
}

func (s *Service) handleIlinkPutCursor(ctx context.Context, hctx khttp.Context) error {
	b, err := s.ilinkBackendOrErr()
	if err != nil {
		return err
	}
	var req ilinkCursorBody
	if err := decodeJSON(hctx, &req); err != nil {
		return kratosErrors.BadRequest("INVALID_ARGUMENT", "invalid body")
	}
	if err := b.PutCursor(ctx, hctx.Vars().Get("channel_id"), biz.IlinkCursor{Generation: req.Generation, GetUpdatesBuf: req.GetUpdatesBuf}); err != nil {
		return err
	}
	return hctx.JSON(200, map[string]any{"ok": true})
}

func (s *Service) handleIlinkSessionExpired(ctx context.Context, hctx khttp.Context) error {
	b, err := s.ilinkBackendOrErr()
	if err != nil {
		return err
	}
	var req ilinkSessionExpiredRequest
	if err := decodeJSON(hctx, &req); err != nil {
		return kratosErrors.BadRequest("INVALID_ARGUMENT", "invalid body")
	}
	if err := b.ReportSessionExpired(ctx, hctx.Vars().Get("channel_id"), req.Generation); err != nil {
		return err
	}
	return hctx.JSON(200, map[string]any{"ok": true})
}
```

`portal/internal/runtime/service.go`：

1. `Service` 结构体在 `router routeBackend` 之后加一行 `ilink         ilinkBackend`。
2. `gatewayChannelItem` 在 `UpdatedAt` 字段之前加：

```go
	BotToken         string   `json:"bot_token,omitempty"`
	BaseURL          string   `json:"base_url,omitempty"`
	Generation       int      `json:"generation,omitempty"`
	SessionExpired   bool     `json:"session_expired,omitempty"`
	AllowedPeers     []string `json:"allowed_peers,omitempty"`
```

3. `listGatewayChannels` 中，在 `if !ch.UpdatedAt.IsZero() {` 之前插入：

```go
		if ch.Type == biz.ChannelTypeWeixinIlink {
			if err := s.fillIlinkGatewayItem(ctx, ch, &item); err != nil {
				return nil, err
			}
		}
```

`portal/internal/runtime/http.go` 的 `RegisterRoutes` 中，在 `.../{channel_id}/status` 那一行之后插入：

```go
	r.POST("/runtime/v1/gateway/channels/{channel_id}/ilink/peers", svc.wrap(svc.handleIlinkReportPeer))
	r.GET("/runtime/v1/gateway/channels/{channel_id}/ilink/cursor", svc.wrap(svc.handleIlinkGetCursor))
	r.PUT("/runtime/v1/gateway/channels/{channel_id}/ilink/cursor", svc.wrap(svc.handleIlinkPutCursor))
	r.POST("/runtime/v1/gateway/channels/{channel_id}/ilink/session-expired", svc.wrap(svc.handleIlinkSessionExpired))
```

- [ ] **Step 4: 运行确认通过**

```powershell
gofmt -w internal/runtime/ilink.go internal/runtime/ilink_test.go internal/runtime/service.go internal/runtime/http.go
go test ./internal/runtime/ -count=1
go build ./...
```

Expected: `ok  backend/internal/runtime`（含既有 Gateway 测试）。

- [ ] **Step 5: Commit**

```powershell
git add internal/runtime/ilink.go internal/runtime/ilink_test.go internal/runtime/service.go internal/runtime/http.go
git commit -m "feat(runtime): weixin_ilink 网关下发字段与名册 / 游标 / 会话失效接口"
```

---

### Task 8: Admin 接口与依赖接线

**Files:**
- Create: `portal/internal/server/weixin_ilink.go`、`portal/internal/server/weixin_ilink_test.go`
- Modify: `portal/internal/server/http.go`、`portal/cmd/backend/wire_gen.go`

- [ ] **Step 1: 写失败测试**

`portal/internal/server/weixin_ilink_test.go`：

```go
package server

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	"backend/internal/biz"
)

func TestIlinkPeerToView_HidesContextToken(t *testing.T) {
	at := time.Date(2026, 10, 18, 2, 0, 0, 0, time.UTC)
	v := ilinkPeerToView(&biz.IlinkPeer{
		PeerID: "p1@im.wechat", Allowed: true, Remark: "张三",
		ContextToken: "secret-ctx", ContextTokenAt: &at, LastInboundAt: &at, LastPreview: "hi",
	})
	if !v.HasContextToken || v.ContextTokenAt != "2026-10-18T02:00:00Z" || v.LastInboundAt != "2026-10-18T02:00:00Z" {
		t.Fatalf("view = %+v", v)
	}
	raw, err := json.Marshal(v)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	if strings.Contains(string(raw), "secret-ctx") {
		t.Fatalf("view leaks context_token: %s", raw)
	}
}

func TestIlinkPeerToView_NoToken(t *testing.T) {
	v := ilinkPeerToView(&biz.IlinkPeer{PeerID: "p1"})
	if v.HasContextToken || v.ContextTokenAt != "" || v.LastInboundAt != "" {
		t.Fatalf("view = %+v", v)
	}
}
```

- [ ] **Step 2: 运行确认失败**

Run: `go test ./internal/server/ -run TestIlinkPeerToView -count=1`  
Expected: FAIL，`undefined: ilinkPeerToView`。

- [ ] **Step 3: 实现处理器**

`portal/internal/server/weixin_ilink.go`：

```go
package server

import (
	"context"
	"strings"
	"time"

	"backend/internal/biz"

	kratosErrors "github.com/go-kratos/kratos/v2/errors"
	kratoshttp "github.com/go-kratos/kratos/v2/transport/http"
)

// WeixinIlinkHandlers serves /api/v1/channels/{id}/ilink/*: login only, same as the Channel admin APIs.
type WeixinIlinkHandlers struct {
	uc *biz.WeixinIlinkUsecase
}

func NewWeixinIlinkHandlers(uc *biz.WeixinIlinkUsecase) *WeixinIlinkHandlers {
	return &WeixinIlinkHandlers{uc: uc}
}

type ilinkPeerView struct {
	PeerID          string `json:"peer_id"`
	Allowed         bool   `json:"allowed"`
	Remark          string `json:"remark"`
	HasContextToken bool   `json:"has_context_token"`
	ContextTokenAt  string `json:"context_token_at,omitempty"`
	LastInboundAt   string `json:"last_inbound_at,omitempty"`
	LastPreview     string `json:"last_preview"`
}

type ilinkPeerPatchRequest struct {
	Allowed *bool   `json:"allowed"`
	Remark  *string `json:"remark"`
}

func ilinkPeerToView(p *biz.IlinkPeer) ilinkPeerView {
	v := ilinkPeerView{
		PeerID:          p.PeerID,
		Allowed:         p.Allowed,
		Remark:          p.Remark,
		HasContextToken: p.ContextToken != "",
		LastPreview:     p.LastPreview,
	}
	if p.ContextTokenAt != nil {
		v.ContextTokenAt = p.ContextTokenAt.UTC().Format(time.RFC3339)
	}
	if p.LastInboundAt != nil {
		v.LastInboundAt = p.LastInboundAt.UTC().Format(time.RFC3339)
	}
	return v
}

func (h *WeixinIlinkHandlers) serve(ctx kratoshttp.Context, fn func(context.Context) (any, error)) error {
	if h == nil || h.uc == nil {
		return kratosErrors.ServiceUnavailable("ILINK_DISABLED", "weixin_ilink is not configured")
	}
	out, err := runWithMiddleware(ctx, fn)
	if err != nil {
		return err
	}
	return ctx.JSON(200, out)
}

// POST /api/v1/channels/{id}/ilink/login
func (h *WeixinIlinkHandlers) StartLogin() func(kratoshttp.Context) error {
	return func(ctx kratoshttp.Context) error {
		id := strings.TrimSpace(ctx.Vars().Get("id"))
		return h.serve(ctx, func(c context.Context) (any, error) {
			res, err := h.uc.StartLogin(c, id)
			if err != nil {
				return nil, err
			}
			return map[string]any{
				"qrcode":       res.QRCode,
				"qrcode_image": res.QRImage,
				"expires_at":   res.ExpiresAt.UTC().Format(time.RFC3339),
			}, nil
		})
	}
}

// GET /api/v1/channels/{id}/ilink/login/status?qrcode=
func (h *WeixinIlinkHandlers) LoginStatus() func(kratoshttp.Context) error {
	return func(ctx kratoshttp.Context) error {
		id := strings.TrimSpace(ctx.Vars().Get("id"))
		qrcode := ctx.Request().URL.Query().Get("qrcode")
		return h.serve(ctx, func(c context.Context) (any, error) {
			res, err := h.uc.PollLogin(c, id, qrcode)
			if err != nil {
				return nil, err
			}
			return map[string]any{"status": res.Status, "bot_id": res.BotID}, nil
		})
	}
}

// GET /api/v1/channels/{id}/ilink/peers
func (h *WeixinIlinkHandlers) ListPeers() func(kratoshttp.Context) error {
	return func(ctx kratoshttp.Context) error {
		id := strings.TrimSpace(ctx.Vars().Get("id"))
		return h.serve(ctx, func(c context.Context) (any, error) {
			peers, err := h.uc.ListPeers(c, id)
			if err != nil {
				return nil, err
			}
			items := make([]ilinkPeerView, 0, len(peers))
			for _, p := range peers {
				items = append(items, ilinkPeerToView(p))
			}
			return map[string]any{"items": items}, nil
		})
	}
}

// PATCH /api/v1/channels/{id}/ilink/peers/{peer_id}
func (h *WeixinIlinkHandlers) PatchPeer() func(kratoshttp.Context) error {
	return func(ctx kratoshttp.Context) error {
		id := strings.TrimSpace(ctx.Vars().Get("id"))
		peerID := ctx.Vars().Get("peer_id")
		var req ilinkPeerPatchRequest
		if err := decodeJSONBody(ctx, &req); err != nil {
			return err
		}
		return h.serve(ctx, func(c context.Context) (any, error) {
			p, err := h.uc.UpdatePeer(c, id, peerID, biz.IlinkPeerPatch{Allowed: req.Allowed, Remark: req.Remark})
			if err != nil {
				return nil, err
			}
			return ilinkPeerToView(p), nil
		})
	}
}

// DELETE /api/v1/channels/{id}/ilink/peers/{peer_id}
func (h *WeixinIlinkHandlers) DeletePeer() func(kratoshttp.Context) error {
	return func(ctx kratoshttp.Context) error {
		id := strings.TrimSpace(ctx.Vars().Get("id"))
		peerID := ctx.Vars().Get("peer_id")
		return h.serve(ctx, func(c context.Context) (any, error) {
			if err := h.uc.DeletePeer(c, id, peerID); err != nil {
				return nil, err
			}
			return map[string]any{"ok": true}, nil
		})
	}
}
```

- [ ] **Step 4: 注册路由并接线**

`portal/internal/server/http.go`：

1. `NewHTTPServer` 参数列表末尾（`handbookUC *biz.HandbookUsecase` 之后）加 `, ilinkUC *biz.WeixinIlinkUsecase`。
2. 在 `r.POST("/api/v1/agents/{agent_id}/repo-bindings/copy-from/{other_id}", repoH.CopyBindings())` 之后插入：

```go
	// weixin_ilink QR login and roster: login only, same as Channel admin APIs.
	ilinkH := NewWeixinIlinkHandlers(ilinkUC)
	r.POST("/api/v1/channels/{id}/ilink/login", ilinkH.StartLogin())
	r.GET("/api/v1/channels/{id}/ilink/login/status", ilinkH.LoginStatus())
	r.GET("/api/v1/channels/{id}/ilink/peers", ilinkH.ListPeers())
	r.PATCH("/api/v1/channels/{id}/ilink/peers/{peer_id}", ilinkH.PatchPeer())
	r.DELETE("/api/v1/channels/{id}/ilink/peers/{peer_id}", ilinkH.DeletePeer())
```

`portal/cmd/backend/wire_gen.go`（**手工编辑，不要运行 wire**）：

1. 在 `runtimeService := runtime.NewService(...)` 这一行之后插入：

```go
	weixinIlinkRepo := data.NewWeixinIlinkRepo(dataData, logger)
	weixinIlinkUsecase := biz.NewWeixinIlinkUsecase(channelRepo, weixinIlinkRepo, authEphemeralRepo)
	runtimeService.SetIlink(weixinIlinkUsecase)
```

2. `server.NewHTTPServer(...)` 调用的参数末尾（`handbookUsecase` 之后）加 `, weixinIlinkUsecase`。

- [ ] **Step 5: 编译与测试**

```powershell
gofmt -w internal/server/weixin_ilink.go internal/server/weixin_ilink_test.go internal/server/http.go cmd/backend/wire_gen.go
go build ./...
go vet ./internal/server/ ./internal/runtime/ ./internal/biz/ ./internal/data/ ./internal/ilink/ ./cmd/backend/
go test ./internal/server/ -count=1
```

Expected: 编译通过；`go vet` 无新告警；`ok  backend/internal/server`。

- [ ] **Step 6: Commit**

```powershell
git add internal/server/weixin_ilink.go internal/server/weixin_ilink_test.go internal/server/http.go cmd/backend/wire_gen.go
git commit -m "feat(server): weixin_ilink 扫码登录与名册 Admin 接口并接线"
```

---

### Task 9: `ChannelReply` 新字段

**Files:**
- Modify: `portal/api/channel/v1/channel.proto`、`portal/api/channel/v1/channel.pb.go`（生成）、`portal/openapi.yaml`（生成）、`portal/internal/service/channel.go`
- Test: `portal/internal/service/channel_test.go`

- [ ] **Step 1: 改 proto 并生成**

`portal/api/channel/v1/channel.proto` 的 `message ChannelReply` 中，在 `bool auto_route_classifier = 23;` 之后加：

```proto
  // weixin_ilink（bot_token 永不明文返回）
  bool has_bot_token = 24;
  string ilink_bot_id = 25;
  string ilink_owner_user_id = 26;
```

生成（在 `portal/`）：

```powershell
protoc --proto_path=./api --proto_path=./third_party --go_out=paths=source_relative:./api --go-http_out=paths=source_relative:./api --go-grpc_out=paths=source_relative:./api channel/v1/channel.proto
$root = (Resolve-Path api).Path
$protos = Get-ChildItem api -Recurse -Filter *.proto | ForEach-Object { $_.FullName.Substring($root.Length + 1).Replace('\', '/') }
protoc --proto_path=./api --proto_path=./third_party --openapi_out=fq_schema_naming=true,default_response=false:. $protos
git diff --stat -- api openapi.yaml
```

Expected: `git diff --stat` 只列出 `api/channel/v1/channel.proto`、`api/channel/v1/channel.pb.go`、`openapi.yaml`。`git status` 可能还把 `channel_grpc.pb.go` / `channel_http.pb.go` 标为修改——那只是生成器写出 LF 换行（`core.autocrlf=true`），内容无变化，用 `git checkout -- api/channel/v1/channel_grpc.pb.go api/channel/v1/channel_http.pb.go` 还原。若其它文件出现真实差异，同样还原。

- [ ] **Step 2: 写失败测试**

`portal/internal/service/channel_test.go` 末尾追加：

```go
func TestChannelMetaToReply_WeixinIlink(t *testing.T) {
	r := channelMetaToReply(&biz.ChannelMeta{
		ID: "uuid-1", ChannelID: "wx-1", Type: biz.ChannelTypeWeixinIlink,
		IlinkBotToken: "secret-bt", IlinkBotID: "b@im.bot", IlinkOwnerUserID: "u@im.wechat",
	})
	if !r.GetHasBotToken() || r.GetIlinkBotId() != "b@im.bot" || r.GetIlinkOwnerUserId() != "u@im.wechat" {
		t.Fatalf("reply = %+v", r)
	}
	if strings.Contains(r.String(), "secret-bt") {
		t.Fatal("reply leaks bot token")
	}

	other := channelMetaToReply(&biz.ChannelMeta{Type: "webhook", IlinkBotID: "stray"})
	if other.GetHasBotToken() || other.GetIlinkBotId() != "" {
		t.Fatalf("non-ilink reply = %+v", other)
	}
}
```

若文件未导入 `strings`，补上。

- [ ] **Step 3: 运行确认失败**

Run: `go test ./internal/service/ -run TestChannelMetaToReply -count=1`  
Expected: FAIL（`GetHasBotToken()` 为 false）。

- [ ] **Step 4: 实现**

`portal/internal/service/channel.go` 的 `channelMetaToReply` 中，在 `if ch.Type == "wecom" {` 代码块之后、`return r` 之前插入：

```go
	if ch.Type == biz.ChannelTypeWeixinIlink {
		r.HasBotToken = ch.IlinkBotToken != ""
		r.IlinkBotId = ch.IlinkBotID
		r.IlinkOwnerUserId = ch.IlinkOwnerUserID
	}
```

- [ ] **Step 5: 运行确认通过**

```powershell
gofmt -w internal/service/channel.go internal/service/channel_test.go
go test ./internal/service/ -count=1
go build ./...
```

Expected: `ok  backend/internal/service`。

- [ ] **Step 6: Commit**

```powershell
git add api/channel/v1/channel.proto api/channel/v1/channel.pb.go openapi.yaml internal/service/channel.go internal/service/channel_test.go
git commit -m "feat(channel): ChannelReply 返回 weixin_ilink 登录信息（不含 token）"
```

---

### Task 10: Web API 客户端与纯函数

**Files:**
- Modify: `web/src/api/client.ts`
- Create: `web/src/utils/ilink.ts`
- Test: `web/tests/ilink.test.ts`

- [ ] **Step 1: 写失败测试**

`web/tests/ilink.test.ts`：

```ts
import { describe, it } from 'node:test'
import assert from 'node:assert/strict'
import { ilinkLoginHint, ilinkPeerActivation, ilinkPeerLabel, ilinkPeerTail } from '../src/utils/ilink.ts'

describe('ilinkPeerTail', () => {
  it('returns the last 4 chars of the local part', () => {
    assert.equal(ilinkPeerTail('o9cq800VxswKIZHziZ1yez1Ngkx4@im.wechat'), 'gkx4')
  })
  it('keeps short ids as-is', () => {
    assert.equal(ilinkPeerTail('ab@im.bot'), 'ab')
    assert.equal(ilinkPeerTail(''), '')
  })
})

describe('ilinkPeerLabel', () => {
  it('prefers the remark', () => {
    assert.equal(ilinkPeerLabel({ peer_id: 'xxxxabcd@im.wechat', remark: ' 张三 ' }), '张三（尾号 abcd）')
  })
  it('falls back to the tail', () => {
    assert.equal(ilinkPeerLabel({ peer_id: 'xxxxabcd@im.wechat', remark: '' }), '尾号 abcd')
  })
})

describe('ilinkLoginHint', () => {
  it('asks recipients to message the bot after login', () => {
    assert.match(ilinkLoginHint('confirmed'), /发一条消息/)
  })
  it('reports expiry', () => {
    assert.match(ilinkLoginHint('expired'), /过期/)
  })
  it('defaults to scan instructions without relying on scaned', () => {
    assert.equal(ilinkLoginHint('wait'), ilinkLoginHint('scaned'))
    assert.match(ilinkLoginHint(''), /扫码/)
  })
})

describe('ilinkPeerActivation', () => {
  it('maps context token presence', () => {
    assert.equal(ilinkPeerActivation({ has_context_token: true }), '已激活')
    assert.equal(ilinkPeerActivation({ has_context_token: false }), '待激活')
  })
})
```

- [ ] **Step 2: 运行确认失败**

Run: `npm test`  
Expected: FAIL，找不到 `../src/utils/ilink.ts`。

- [ ] **Step 3: 实现纯函数**

`web/src/utils/ilink.ts`：

```ts
export type IlinkLoginStatus = 'wait' | 'scaned' | 'confirmed' | 'expired'

/** Last 4 chars of the part before '@' — iLink IDs are opaque, so the tail is what admins compare. */
export function ilinkPeerTail(peerId: string): string {
  const local = peerId.split('@')[0] ?? ''
  return local.length <= 4 ? local : local.slice(-4)
}

export function ilinkPeerLabel(peer: { peer_id: string; remark?: string }): string {
  const remark = peer.remark?.trim()
  const tail = `尾号 ${ilinkPeerTail(peer.peer_id)}`
  return remark ? `${remark}（${tail}）` : tail
}

/** The server may skip "scaned", so wait and scaned share one hint. */
export function ilinkLoginHint(status: IlinkLoginStatus | ''): string {
  switch (status) {
    case 'confirmed':
      return '登录成功。请让需要接收推送的人先给 bot 发一条消息。'
    case 'expired':
      return '二维码已过期，请刷新。'
    default:
      return '请用微信扫码，并在手机上确认登录。'
  }
}

/** Peers without a context token (never messaged, or not since the last login) cannot receive pushes. */
export function ilinkPeerActivation(peer: { has_context_token?: boolean }): '已激活' | '待激活' {
  return peer.has_context_token ? '已激活' : '待激活'
}
```

- [ ] **Step 4: 扩展 API 客户端**

`web/src/api/client.ts`：

1. `ChannelRuntimeStatus` 上方注释改为 `/** Admin-facing derived gateway status (wecom_bot / weixin_ilink). */`，`state` 行注释改为 `// connected|disconnected|reconnecting|disabled|unknown|login_required`。
2. `Channel` 接口在 `runtime_status?: ChannelRuntimeStatus` 之前加：

```ts
  has_bot_token?: boolean
  ilink_bot_id?: string
  ilink_owner_user_id?: string
```

3. `CreateChannelRequest.type` 改为：

```ts
  type: 'web' | 'api' | 'webhook' | 'wxpusher' | 'wecom' | 'wecom_bot' | 'weixin_ilink'
```

4. `normalizeChannel` 返回对象在 `runtime_status:` 那一行之前加：

```ts
    has_bot_token: (raw.has_bot_token as boolean | undefined) ?? (raw.hasBotToken as boolean | undefined),
    ilink_bot_id: (raw.ilink_bot_id as string | undefined) ?? (raw.ilinkBotId as string | undefined),
    ilink_owner_user_id:
      (raw.ilink_owner_user_id as string | undefined) ?? (raw.ilinkOwnerUserId as string | undefined),
```

5. 在 `export const channelApi = { ... }` 结束的 `}` 之后追加：

```ts
export interface IlinkLoginStart {
  qrcode: string
  qrcode_image: string
  expires_at: string
}

export interface IlinkLoginStatusReply {
  status: 'wait' | 'scaned' | 'confirmed' | 'expired'
  bot_id?: string
}

export interface IlinkPeer {
  peer_id: string
  allowed: boolean
  remark: string
  has_context_token: boolean
  context_token_at?: string
  last_inbound_at?: string
  last_preview: string
}

/** weixin_ilink QR login and roster; channelId is the channel's id (uuid). */
export const ilinkApi = {
  startLogin: (channelId: string, signal?: AbortSignal) =>
    request<IlinkLoginStart>(`/channels/${channelId}/ilink/login`, { method: 'POST', body: '{}', signal }),
  loginStatus: (channelId: string, qrcode: string, signal?: AbortSignal) =>
    request<IlinkLoginStatusReply>(
      `/channels/${channelId}/ilink/login/status?qrcode=${encodeURIComponent(qrcode)}`,
      { signal },
    ),
  listPeers: async (channelId: string) => {
    const res = await request<{ items?: IlinkPeer[] }>(`/channels/${channelId}/ilink/peers`)
    return res.items ?? []
  },
  patchPeer: (channelId: string, peerId: string, patch: { allowed?: boolean; remark?: string }) =>
    request<IlinkPeer>(`/channels/${channelId}/ilink/peers/${encodeURIComponent(peerId)}`, {
      method: 'PATCH',
      body: JSON.stringify(patch),
    }),
  deletePeer: async (channelId: string, peerId: string) => {
    await request<{ ok: boolean }>(`/channels/${channelId}/ilink/peers/${encodeURIComponent(peerId)}`, {
      method: 'DELETE',
    })
  },
}
```

- [ ] **Step 5: 运行确认通过**

```powershell
npm test
npx tsc
```

Expected: 测试全部通过；`tsc` 无错误。

- [ ] **Step 6: Commit**

```powershell
git add src/api/client.ts src/utils/ilink.ts tests/ilink.test.ts
git commit -m "feat(web): weixin_ilink API 客户端与名册 / 登录提示纯函数"
```

---

### Task 11: Web 渠道页：扫码弹窗与名册面板

**Files:**
- Create: `web/src/components/IlinkLoginDialog.tsx`、`web/src/components/IlinkRosterPanel.tsx`
- Modify: `web/src/pages/ChannelForm.tsx`、`web/src/pages/ChannelList.tsx`、`web/src/App.css`

无组件单测（仓库只测纯函数），以 `tsc` + 构建 + Task 12 手工验收验证。

- [ ] **Step 1: 扫码弹窗**

`web/src/components/IlinkLoginDialog.tsx`：

```tsx
import { useEffect, useRef, useState } from 'react'
import { ilinkApi, type IlinkLoginStatusReply } from '../api/client'
import { ilinkLoginHint } from '../utils/ilink'
import './ConfirmDialog.css'

interface IlinkLoginDialogProps {
  channelId: string
  open: boolean
  onClose: () => void
  onConfirmed: () => void
}

export function IlinkLoginDialog({ channelId, open, onClose, onConfirmed }: IlinkLoginDialogProps) {
  const [image, setImage] = useState('')
  const [status, setStatus] = useState<IlinkLoginStatusReply['status'] | ''>('')
  const [error, setError] = useState('')
  const [round, setRound] = useState(0)
  const onConfirmedRef = useRef(onConfirmed)

  useEffect(() => {
    onConfirmedRef.current = onConfirmed
  }, [onConfirmed])

  useEffect(() => {
    if (!open) return
    const controller = new AbortController()
    const { signal } = controller
    setImage('')
    setStatus('')
    setError('')
    const run = async () => {
      try {
        const start = await ilinkApi.startLogin(channelId, signal)
        if (signal.aborted) return
        setImage(start.qrcode_image)
        setStatus('wait')
        // Each status call is held open by the server for up to ~30s; abort releases it on close.
        while (!signal.aborted) {
          const res = await ilinkApi.loginStatus(channelId, start.qrcode, signal)
          if (signal.aborted) return
          setStatus(res.status)
          if (res.status === 'confirmed') {
            onConfirmedRef.current()
            return
          }
          if (res.status === 'expired') return
          await new Promise((resolve) => setTimeout(resolve, 1000))
        }
      } catch (e) {
        if (!signal.aborted) setError(e instanceof Error ? e.message : String(e))
      }
    }
    void run()
    return () => controller.abort()
  }, [open, channelId, round])

  if (!open) return null
  const canRetry = status === 'expired' || !!error
  return (
    <div className="confirm-dialog-backdrop" role="presentation">
      <div className="confirm-dialog form-dialog" role="dialog" aria-modal="true" aria-labelledby="ilink-login-title">
        <h2 id="ilink-login-title">微信扫码登录</h2>
        <div className="form-dialog__body" style={{ textAlign: 'center' }}>
          {image ? (
            <img
              src={image}
              alt="微信 iLink 登录二维码"
              width={240}
              height={240}
              style={{ opacity: status === 'expired' || status === 'confirmed' ? 0.3 : 1 }}
            />
          ) : !error ? (
            <p className="page-sub">正在获取二维码…</p>
          ) : null}
          <p className="page-sub">{ilinkLoginHint(status)}</p>
        </div>
        {error ? <div className="error form-dialog__error">{error}</div> : null}
        <div className="confirm-dialog-actions">
          {canRetry ? (
            <button type="button" className="btn btn-secondary" onClick={() => setRound((n) => n + 1)}>
              刷新二维码
            </button>
          ) : null}
          <button type="button" className="btn" onClick={onClose}>
            {status === 'confirmed' ? '完成' : '关闭'}
          </button>
        </div>
      </div>
    </div>
  )
}
```

- [ ] **Step 2: 名册面板**

`web/src/components/IlinkRosterPanel.tsx`：

```tsx
import { useCallback, useEffect, useState } from 'react'
import { ilinkApi, type IlinkPeer } from '../api/client'
import { ilinkPeerActivation, ilinkPeerTail } from '../utils/ilink'

interface IlinkRosterPanelProps {
  channelId: string
  refreshKey: number
}

function formatTime(iso?: string): string {
  if (!iso) return '—'
  const d = new Date(iso)
  return Number.isNaN(d.getTime()) ? '—' : d.toLocaleString()
}

export function IlinkRosterPanel({ channelId, refreshKey }: IlinkRosterPanelProps) {
  const [peers, setPeers] = useState<IlinkPeer[]>([])
  const [remarks, setRemarks] = useState<Record<string, string>>({})
  const [busyPeer, setBusyPeer] = useState('')
  const [error, setError] = useState('')

  const load = useCallback(async () => {
    try {
      const items = await ilinkApi.listPeers(channelId)
      setPeers(items)
      setRemarks(Object.fromEntries(items.map((p) => [p.peer_id, p.remark])))
      setError('')
    } catch (e) {
      setError(e instanceof Error ? e.message : String(e))
    }
  }, [channelId])

  useEffect(() => {
    void load()
  }, [load, refreshKey])

  const run = async (peerId: string, action: () => Promise<unknown>) => {
    setBusyPeer(peerId)
    try {
      await action()
      await load()
    } catch (e) {
      setError(e instanceof Error ? e.message : String(e))
    } finally {
      setBusyPeer('')
    }
  }

  const saveRemark = (peer: IlinkPeer) => {
    const next = (remarks[peer.peer_id] ?? '').trim()
    if (next === peer.remark) return
    void run(peer.peer_id, () => ilinkApi.patchPeer(channelId, peer.peer_id, { remark: next }))
  }

  return (
    <div className="section-card" style={{ maxWidth: 640, marginTop: '1rem' }}>
      <h2 style={{ fontSize: '1rem', margin: '0 0 0.5rem' }}>微信名册（白名单）</h2>
      <p className="page-sub">
        给 bot 发过消息的人会出现在这里，默认无权限；放行后约 15 秒生效。「待激活」表示对方还没发过消息（或 bot
        重新登录后还没再发），暂时收不到推送。
      </p>
      <div style={{ margin: '0.5rem 0' }}>
        <button type="button" className="btn btn-secondary btn-sm" onClick={() => void load()}>
          刷新
        </button>
      </div>
      {error && <div className="error">{error}</div>}
      {peers.length === 0 ? (
        <p className="page-sub">暂无对端。让对方给 bot 发一条消息后刷新。</p>
      ) : (
        <table>
          <thead>
            <tr>
              <th>对端</th>
              <th>备注</th>
              <th>权限</th>
              <th>推送</th>
              <th>最近消息</th>
              <th>操作</th>
            </tr>
          </thead>
          <tbody>
            {peers.map((peer) => {
              const busy = busyPeer === peer.peer_id
              return (
                <tr key={peer.peer_id}>
                  <td title={peer.peer_id}>尾号 {ilinkPeerTail(peer.peer_id)}</td>
                  <td>
                    <input
                      value={remarks[peer.peer_id] ?? ''}
                      onChange={(e) => setRemarks((prev) => ({ ...prev, [peer.peer_id]: e.target.value }))}
                      onBlur={() => saveRemark(peer)}
                      maxLength={64}
                      placeholder="备注"
                      disabled={busy}
                    />
                  </td>
                  <td>{peer.allowed ? '已放行' : '无权限'}</td>
                  <td>{ilinkPeerActivation(peer)}</td>
                  <td title={formatTime(peer.last_inbound_at)}>{peer.last_preview || '—'}</td>
                  <td>
                    <div className="row-actions">
                      <button
                        type="button"
                        className="btn btn-ghost btn-sm"
                        disabled={busy}
                        onClick={() =>
                          void run(peer.peer_id, () =>
                            ilinkApi.patchPeer(channelId, peer.peer_id, { allowed: !peer.allowed }),
                          )
                        }
                      >
                        {peer.allowed ? '取消放行' : '放行'}
                      </button>
                      <button
                        type="button"
                        className="btn btn-ghost btn-sm"
                        disabled={busy}
                        onClick={() => void run(peer.peer_id, () => ilinkApi.deletePeer(channelId, peer.peer_id))}
                      >
                        移出
                      </button>
                    </div>
                  </td>
                </tr>
              )
            })}
          </tbody>
        </table>
      )}
    </div>
  )
}
```

- [ ] **Step 3: 接入 `ChannelForm.tsx`**

`web/src/pages/ChannelForm.tsx`：

1. import 区在 `import ResourceGrantPanel from '../components/ResourceGrantPanel'` 之后加：

```tsx
import { IlinkLoginDialog } from '../components/IlinkLoginDialog'
import { IlinkRosterPanel } from '../components/IlinkRosterPanel'
import { ilinkPeerTail } from '../utils/ilink'
```

2. `RUNTIME_DOT_COLORS` 上方注释改为 `/** Admin-facing states: connected|disconnected|reconnecting|disabled|unknown|login_required */`，并在 `unknown: '#94a3b8',` 之后加 `login_required: '#7c3aed',`。
3. 在 `const [runtimeStatus, setRuntimeStatus] = ...` 之后加：

```tsx
  const [hasBotToken, setHasBotToken] = useState(false)
  const [ilinkBotId, setIlinkBotId] = useState('')
  const [ilinkOwner, setIlinkOwner] = useState('')
  const [ilinkLoginOpen, setIlinkLoginOpen] = useState(false)
  const [rosterKey, setRosterKey] = useState(0)
```

4. 加载渠道的 `useEffect` 中，在 `setRuntimeStatus(channel.runtime_status)` 之后加（effect 依赖保持 `[id, isEdit]` 不变）：

```tsx
        setHasBotToken(!!channel.has_bot_token)
        setIlinkBotId(channel.ilink_bot_id || '')
        setIlinkOwner(channel.ilink_owner_user_id || '')
```

   并在该 effect 之后新增（扫码成功后只刷新登录相关字段，避免覆盖表单里尚未保存的编辑）：

```tsx
  const refreshIlinkLogin = () => {
    if (!id) return
    channelApi.get(id)
      .then((channel) => {
        setHasBotToken(!!channel.has_bot_token)
        setIlinkBotId(channel.ilink_bot_id || '')
        setIlinkOwner(channel.ilink_owner_user_id || '')
        setRuntimeStatus(channel.runtime_status)
      })
      .catch((e) => setError(e.message))
    setRosterKey((n) => n + 1)
  }
```
5. `{isEdit && type === 'wecom_bot' && <RuntimePanel status={runtimeStatus} />}` 改为：

```tsx
        {isEdit && (type === 'wecom_bot' || type === 'weixin_ilink') && <RuntimePanel status={runtimeStatus} />}
```

6. Type 下拉在 `<option value="wecom_bot">WeCom Bot</option>` 之后加 `<option value="weixin_ilink">微信 iLink</option>`。
7. 在 `{error && <div className="error">{error}</div>}` 之前插入：

```tsx
          {type === 'weixin_ilink' && (
            <div className="form-panel">
              <div className="form-panel__desc">
                个人微信机器人（iLink）。保存后在本页扫码登录。每次扫码都会换一个新 bot，需要接收推送的人要重新给 bot
                发一条消息。
              </div>
              {isEdit ? (
                <div style={{ display: 'flex', alignItems: 'center', gap: '0.75rem', flexWrap: 'wrap' }}>
                  <span className="page-sub">
                    {hasBotToken
                      ? `已登录：bot 尾号 ${ilinkPeerTail(ilinkBotId)}，扫码人尾号 ${ilinkPeerTail(ilinkOwner)}`
                      : '尚未登录'}
                  </span>
                  <button type="button" className="btn btn-secondary" onClick={() => setIlinkLoginOpen(true)}>
                    {hasBotToken ? '重新扫码登录' : '扫码登录'}
                  </button>
                </div>
              ) : (
                <div className="page-sub">先保存渠道，再在编辑页扫码登录。</div>
              )}
            </div>
          )}
```

8. 在 `{isEdit && id ? <ResourceGrantPanel resourceType="channel" payloadRef={id} /> : null}` 之前插入：

```tsx
      {isEdit && id && type === 'weixin_ilink' ? (
        <>
          <IlinkRosterPanel channelId={id} refreshKey={rosterKey} />
          <IlinkLoginDialog
            channelId={id}
            open={ilinkLoginOpen}
            onClose={() => setIlinkLoginOpen(false)}
            onConfirmed={refreshIlinkLogin}
          />
        </>
      ) : null}
```

- [ ] **Step 4: 接入 `ChannelList.tsx` 与样式**

`web/src/pages/ChannelList.tsx`：

1. `RUNTIME_DOT_COLORS` 注释同 Step 3 第 2 条，并加 `login_required: '#7c3aed',`。
2. `RuntimeStatusCell` 首行改为：

```tsx
  if (channel.type !== 'wecom_bot' && channel.type !== 'weixin_ilink') return <>—</>
```

3. 类型筛选在 `<option value="wecom_bot">企业微信 Bot</option>` 之后加 `<option value="weixin_ilink">微信 iLink</option>`。

`web/src/App.css` 在 `.badge-wecom_bot { ... }` 之后加：

```css
.badge-weixin_ilink { background: rgba(34, 197, 94, 0.12); color: #15803d; }
```

- [ ] **Step 5: 类型检查与构建**

```powershell
npx tsc
npm run build
npm test
```

Expected: 均无错误。

- [ ] **Step 6: Commit**

`npm run build` 可能改动 `tsconfig.tsbuildinfo` / 产物目录，**不要**加入：

```powershell
git add src/components/IlinkLoginDialog.tsx src/components/IlinkRosterPanel.tsx src/pages/ChannelForm.tsx src/pages/ChannelList.tsx src/App.css
git commit -m "feat(web): weixin_ilink 渠道扫码登录弹窗与名册面板"
```

---

### Task 12: 全量验证与手工验收

**Files:** 无（只在发现问题时修复并单独提交）

- [ ] **Step 1: 全量 Go 测试**

```powershell
Set-Location portal
Remove-Item Env:GOROOT -ErrorAction SilentlyContinue
gofmt -l (git diff --name-only 2da1a27 -- . | Where-Object { $_ -like "*.go" } | ForEach-Object { $_ -replace "^portal/", "" })
go vet ./...
go test ./internal/... -count=1 2>&1 | Select-String -Pattern "^(ok|FAIL|---)"
```

Expected: `gofmt -l`（只检查本计划改动过的 Go 文件）无输出；与 Task 0 基线相比没有新增 `FAIL`。

- [ ] **Step 2: 手工验收（需要用户扫码配合）**

按项目 README 启动 Portal 与 Web（MySQL 已就绪），然后：

1. Channels → 新建 `weixin_ilink` 渠道（如 `wx-demo`），保存 → 列表"运行时"列显示 `login_required`。
2. 进入编辑页 → 运行时面板显示 `login_required` 与"未登录，请扫码登录"。
3. 点"扫码登录" → 弹窗出现二维码 → 用户扫码并在手机确认 → 弹窗提示"登录成功…" → 关闭后页面显示"已登录：bot 尾号 xxxx，扫码人尾号 xxxx"；名册出现扫码人，权限"已放行"、推送"待激活"。
4. 重新扫码 → bot 尾号变化；名册中扫码人仍为"已放行"。
5. 名册：修改备注后失焦保存；"取消放行" / "放行" 切换生效；"移出"后行消失。
6. 用 service token 调 `GET /runtime/v1/gateway/channels`（`Authorization: Bearer <runtime token>`），确认 `wx-demo` 带 `bot_token`、`generation`、`allowed_peers`。
7. 调 `GET /api/v1/channels/{id}`，确认响应**不含**明文 token，只有 `has_bot_token`。

把结果（通过 / 问题）记录到最终报告。发现问题时修复、补测试、单独提交。

---

## 完成标准

- Task 1–11 的测试全部通过，Task 12 全量测试无新增失败。
- 手工验收 1–7 通过（或问题已修复）。
- 未提交任何与本计划无关的文件；未明文暴露 `bot_token` / `context_token`。
