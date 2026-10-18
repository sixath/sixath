# 微信 iLink 渠道 Phase 0 协议验证 Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** 写一个独立的 Go 命令行验证工具 `ilink-spike`，用真实微信号回答 spec §10 的 5 个协议问题，并把结论回写 spec。

**Architecture:** 独立 Go 模块 `scripts/ilink-spike/`（不进 Gateway/Portal 模块与镜像），只用标准库 + 一个二维码库。四个子命令：`login`（扫码并记录状态接口耗时）、`listen`（长轮询打印消息并保存 `context_token`）、`send`（用已存 token 发文本 / 长度测试）、`peers`（列出已记录对端）。状态与原始日志存本地 `.state/`（gitignore）。

**Tech Stack:** Go 1.26、`net/http`、`github.com/skip2/go-qrcode`

**Spec:** `docs/superpowers/specs/2026-10-18-weixin-ilink-channel-design.md`（§3 协议要点、§10 待验证问题）

---

## 约定

- **工作目录**：Task 1 Step 1 进入 `scripts/ilink-spike` 后，**之后所有命令都在该目录执行**（PowerShell）。`git add` 一律用相对该目录的路径（如 `ilink.go`、`../../.gitignore`）。
- **环境前提**：若 `go build` / `go test` 报 `compile: version "go1.25.x" does not match go tool version "go1.26.0"`，说明 shell 里残留了旧的 `GOROOT`，先执行 `Remove-Item Env:GOROOT` 再重试。
- **日志编码**：Task 5 用 `Tee-Object` 保存输出前，先执行 `[Console]::OutputEncoding = [Text.UTF8Encoding]::new()`，避免中文乱码；需要连 stderr 一起保存时在命令后加 `2>&1`。
- **隐私**：实测原始输出只保存在 `.state/`（已 gitignore）。回写 spec 时只写结论，微信 / bot ID 只保留末 4 位。

## 文件结构

| 文件 | 职责 |
|------|------|
| `scripts/ilink-spike/go.mod` / `go.sum` | 独立模块 `github.com/sixath/ilink-spike` |
| `scripts/ilink-spike/ilink.go` | iLink HTTP 客户端：请求头、`get_bot_qrcode`、`get_qrcode_status`、`getupdates`、`sendmessage`、`-14` 识别、原始响应日志（脱敏） |
| `scripts/ilink-spike/ilink_test.go` | 客户端 `httptest` 测试 |
| `scripts/ilink-spike/state.go` | 本地状态 JSON 读写、入站记录、长度测试文本 |
| `scripts/ilink-spike/state_test.go` | 状态与长度文本测试 |
| `scripts/ilink-spike/main.go` | 子命令 CLI |
| `.gitignore` | 忽略 `scripts/ilink-spike/.state/` |

---

### Task 1: 模块脚手架

**Files:**
- Create: `scripts/ilink-spike/go.mod`、`scripts/ilink-spike/go.sum`
- Modify: `.gitignore`

- [ ] **Step 1: 创建模块（在仓库根执行，结束时位于 `scripts/ilink-spike`）**

```powershell
New-Item -ItemType Directory -Force scripts/ilink-spike | Out-Null
Set-Location scripts/ilink-spike
go mod init github.com/sixath/ilink-spike
go mod edit -go=1.26.0
go get github.com/skip2/go-qrcode@latest
```

Expected: `go.mod` 含 `go 1.26.0` 与 `require github.com/skip2/go-qrcode … // indirect`（此时还没有源码引用，Task 4 会 `go mod tidy` 去掉 `// indirect`）；生成 `go.sum`。

- [ ] **Step 2: 忽略本地状态目录**

在仓库根 `.gitignore` 的 `# Local secrets / debug artifacts` 段落下追加一行：

```gitignore
scripts/ilink-spike/.state/
```

- [ ] **Step 3: Commit**

```powershell
git add go.mod go.sum ../../.gitignore
git commit -m "chore(spike): 新建 ilink-spike 验证工具模块"
```

---

### Task 2: iLink HTTP 客户端

**Files:**
- Create: `scripts/ilink-spike/ilink.go`
- Test: `scripts/ilink-spike/ilink_test.go`

- [ ] **Step 1: 写失败测试**

`ilink_test.go`：

```go
package main

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
	"time"
)

func TestPostSetsHeadersAndBaseInfo(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got := r.Header.Get("AuthorizationType"); got != "ilink_bot_token" {
			t.Errorf("AuthorizationType = %q", got)
		}
		if got := r.Header.Get("Authorization"); got != "Bearer tok-1" {
			t.Errorf("Authorization = %q", got)
		}
		uin, err := base64.StdEncoding.DecodeString(r.Header.Get("X-WECHAT-UIN"))
		if err != nil {
			t.Errorf("X-WECHAT-UIN not base64: %v", err)
		}
		if _, err := strconv.ParseUint(string(uin), 10, 32); err != nil {
			t.Errorf("X-WECHAT-UIN not uint32 decimal: %q", uin)
		}
		var body map[string]any
		raw, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(raw, &body)
		info, _ := body["base_info"].(map[string]any)
		if info["channel_version"] != channelVersion {
			t.Errorf("base_info = %v", body["base_info"])
		}
		if body["get_updates_buf"] != "buf-0" {
			t.Errorf("get_updates_buf = %v", body["get_updates_buf"])
		}
		_, _ = w.Write([]byte(`{"ret":0,"msgs":[],"get_updates_buf":"buf-1"}`))
	}))
	defer srv.Close()

	c := NewClient(srv.URL, "tok-1")
	up, err := c.GetUpdates(context.Background(), "buf-0")
	if err != nil {
		t.Fatalf("GetUpdates: %v", err)
	}
	if up.GetUpdatesBuf != "buf-1" {
		t.Fatalf("cursor = %q, want buf-1", up.GetUpdatesBuf)
	}
}

func TestGetUpdatesSessionExpired(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"ret":-14,"errcode":-14,"errmsg":"session timeout"}`))
	}))
	defer srv.Close()

	_, err := NewClient(srv.URL, "tok").GetUpdates(context.Background(), "")
	var apiErr *APIError
	if !errors.As(err, &apiErr) || !apiErr.SessionExpired() {
		t.Fatalf("err = %v, want session expired APIError", err)
	}
}

func TestErrcodeOnlySessionExpired(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"errcode":-14,"errmsg":"session timeout"}`))
	}))
	defer srv.Close()

	_, err := NewClient(srv.URL, "tok").GetUpdates(context.Background(), "")
	var apiErr *APIError
	if !errors.As(err, &apiErr) || !apiErr.SessionExpired() {
		t.Fatalf("err = %v, want session expired APIError", err)
	}
}

func TestGetUpdatesClientTimeoutReturnsEmptyBatch(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		select {
		case <-time.After(500 * time.Millisecond):
		case <-r.Context().Done():
		}
	}))
	defer srv.Close()

	c := NewClient(srv.URL, "tok")
	c.PollTimeout = 50 * time.Millisecond
	up, err := c.GetUpdates(context.Background(), "keep-me")
	if err != nil {
		t.Fatalf("GetUpdates: %v", err)
	}
	if len(up.Msgs) != 0 || up.GetUpdatesBuf != "keep-me" {
		t.Fatalf("got %+v, want empty batch with same cursor", up)
	}
}

func TestGetQRCodeStatusClientTimeoutIsWait(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("iLink-App-ClientVersion") != "1" {
			t.Errorf("missing iLink-App-ClientVersion")
		}
		select {
		case <-time.After(500 * time.Millisecond):
		case <-r.Context().Done():
		}
	}))
	defer srv.Close()

	c := NewClient(srv.URL, "")
	c.PollTimeout = 50 * time.Millisecond
	s, err := c.GetQRCodeStatus(context.Background(), "qrc_1")
	if err != nil {
		t.Fatalf("GetQRCodeStatus: %v", err)
	}
	if s.Status != "wait" || !s.ClientTimeout {
		t.Fatalf("got %+v, want wait with ClientTimeout", s)
	}
}

func TestGetQRCodeStatusConfirmed(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("qrcode") != "qrc_1" {
			t.Errorf("qrcode = %q", r.URL.Query().Get("qrcode"))
		}
		_, _ = w.Write([]byte(`{"status":"confirmed","bot_token":"bt","ilink_bot_id":"b@im.bot","ilink_user_id":"u@im.wechat","baseurl":"https://x"}`))
	}))
	defer srv.Close()

	s, err := NewClient(srv.URL, "").GetQRCodeStatus(context.Background(), "qrc_1")
	if err != nil {
		t.Fatalf("GetQRCodeStatus: %v", err)
	}
	if s.Status != "confirmed" || s.BotToken != "bt" || s.BotID != "b@im.bot" || s.UserID != "u@im.wechat" || s.BaseURL != "https://x" {
		t.Fatalf("got %+v", s)
	}
}

func TestSendTextBody(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer func() { _, _ = w.Write([]byte(`{}`)) }()
		if r.URL.Path != "/ilink/bot/sendmessage" {
			t.Errorf("path = %s", r.URL.Path)
		}
		var body struct {
			Msg map[string]any `json:"msg"`
		}
		raw, _ := io.ReadAll(r.Body)
		if err := json.Unmarshal(raw, &body); err != nil {
			t.Errorf("decode: %v", err)
			return
		}
		m := body.Msg
		if v, ok := m["from_user_id"]; !ok || v != "" {
			t.Errorf("from_user_id = %v (present=%v), want empty string", v, ok)
		}
		if m["to_user_id"] != "u@im.wechat" || m["context_token"] != "ctx-1" {
			t.Errorf("to/context_token = %v / %v", m["to_user_id"], m["context_token"])
		}
		if m["message_type"] != float64(2) || m["message_state"] != float64(2) {
			t.Errorf("message_type/state = %v / %v", m["message_type"], m["message_state"])
		}
		items, ok := m["item_list"].([]any)
		if !ok || len(items) != 1 {
			t.Errorf("item_list = %v", m["item_list"])
			return
		}
		item, ok := items[0].(map[string]any)
		if !ok {
			t.Errorf("item = %v", items[0])
			return
		}
		textItem, ok := item["text_item"].(map[string]any)
		if !ok || item["type"] != float64(1) || textItem["text"] != "你好" {
			t.Errorf("item = %v", item)
		}
	}))
	defer srv.Close()

	if err := NewClient(srv.URL, "tok").SendText(context.Background(), "u@im.wechat", "ctx-1", "你好"); err != nil {
		t.Fatalf("SendText: %v", err)
	}
}

func TestHTTPErrorStatus(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "boom", http.StatusBadGateway)
	}))
	defer srv.Close()

	_, err := NewClient(srv.URL, "tok").GetUpdates(context.Background(), "")
	if err == nil {
		t.Fatal("want error for 502")
	}
	var apiErr *APIError
	if errors.As(err, &apiErr) {
		t.Fatalf("502 should not be APIError: %v", err)
	}
}

func TestRawLogMasksSecrets(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"status":"confirmed","bot_token":"secret-bt","msgs":[{"context_token": "secret-ctx"}]}`))
	}))
	defer srv.Close()

	var buf bytes.Buffer
	c := NewClient(srv.URL, "")
	c.RawLog = &buf
	if _, err := c.GetQRCodeStatus(context.Background(), "q"); err != nil {
		t.Fatalf("GetQRCodeStatus: %v", err)
	}
	out := buf.String()
	if strings.Contains(out, "secret-bt") || strings.Contains(out, "secret-ctx") {
		t.Fatalf("raw log leaked secrets: %s", out)
	}
	if !strings.Contains(out, `"bot_token":"***"`) || !strings.Contains(out, "get_qrcode_status") {
		t.Fatalf("raw log = %s", out)
	}
}

func TestMessageText(t *testing.T) {
	m := Message{ItemList: []MessageItem{
		{Type: itemTypeText, TextItem: &TextItem{Text: "a"}},
		{Type: 2},
		{Type: itemTypeText, TextItem: &TextItem{Text: "b"}},
	}}
	if got := m.Text(); got != "a\nb" {
		t.Fatalf("Text() = %q", got)
	}
	if got := m.ItemTypes(); len(got) != 3 || got[1] != 2 {
		t.Fatalf("ItemTypes() = %v", got)
	}
}
```

- [ ] **Step 2: 运行确认失败**

Run: `go test ./... -count=1`  
Expected: FAIL，编译错误 `undefined: NewClient` 等。

- [ ] **Step 3: 实现客户端**

`ilink.go`：

```go
package main

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
	defaultBaseURL     = "https://ilinkai.weixin.qq.com"
	channelVersion     = "1.0.0"
	defaultPollTimeout = 40 * time.Second
	sendTimeout        = 15 * time.Second

	itemTypeText       = 1
	messageTypeUser    = 1
	messageTypeBot     = 2
	messageStateFinish = 2
)

// APIError 是 iLink 以 HTTP 200 返回的业务错误（ret / errcode 非 0）。
type APIError struct {
	Ret     int
	ErrCode int
	ErrMsg  string
}

func (e *APIError) Error() string {
	return fmt.Sprintf("ilink ret=%d errcode=%d errmsg=%s", e.Ret, e.ErrCode, e.ErrMsg)
}

func (e *APIError) SessionExpired() bool { return e.Ret == -14 || e.ErrCode == -14 }

type Client struct {
	BaseURL     string
	Token       string
	HTTP        *http.Client
	PollTimeout time.Duration
	// RawLog 非空时写入每个响应的原始 JSON（bot_token / context_token 已打码）。
	RawLog io.Writer
}

func NewClient(baseURL, token string) *Client {
	if baseURL == "" {
		baseURL = defaultBaseURL
	}
	return &Client{
		BaseURL:     strings.TrimRight(baseURL, "/"),
		Token:       token,
		HTTP:        &http.Client{},
		PollTimeout: defaultPollTimeout,
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

type TextItem struct {
	Text string `json:"text"`
}

type MessageItem struct {
	Type     int       `json:"type"`
	TextItem *TextItem `json:"text_item,omitempty"`
}

type Message struct {
	MessageID    int64         `json:"message_id,omitempty"`
	FromUserID   string        `json:"from_user_id"`
	ToUserID     string        `json:"to_user_id"`
	ClientID     string        `json:"client_id,omitempty"`
	CreateTimeMs int64         `json:"create_time_ms,omitempty"`
	GroupID      string        `json:"group_id,omitempty"`
	MessageType  int           `json:"message_type"`
	MessageState int           `json:"message_state,omitempty"`
	ContextToken string        `json:"context_token,omitempty"`
	ItemList     []MessageItem `json:"item_list"`
}

func (m Message) Text() string {
	var parts []string
	for _, it := range m.ItemList {
		if it.Type == itemTypeText && it.TextItem != nil {
			parts = append(parts, it.TextItem.Text)
		}
	}
	return strings.Join(parts, "\n")
}

func (m Message) ItemTypes() []int {
	out := make([]int, 0, len(m.ItemList))
	for _, it := range m.ItemList {
		out = append(out, it.Type)
	}
	return out
}

type Updates struct {
	Msgs          []Message `json:"msgs"`
	GetUpdatesBuf string    `json:"get_updates_buf"`
}

func (c *Client) GetBotQRCode(ctx context.Context) (*QRCode, error) {
	var out QRCode
	if err := c.get(ctx, "/ilink/bot/get_bot_qrcode?bot_type=3", &out); err != nil {
		return nil, err
	}
	if out.QRCode == "" {
		return nil, errors.New("get_bot_qrcode: empty qrcode")
	}
	return &out, nil
}

// GetQRCodeStatus 客户端超时视为 wait（服务端可能挂起长轮询），并用 ClientTimeout 标记。
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

// GetUpdates 客户端超时视为空批次，游标保持不变。
func (c *Client) GetUpdates(ctx context.Context, buf string) (*Updates, error) {
	pctx, cancel := context.WithTimeout(ctx, c.PollTimeout)
	defer cancel()
	var out Updates
	err := c.post(pctx, "/ilink/bot/getupdates", map[string]any{"get_updates_buf": buf}, &out)
	if err != nil {
		if errors.Is(err, context.DeadlineExceeded) && ctx.Err() == nil {
			return &Updates{GetUpdatesBuf: buf}, nil
		}
		return nil, err
	}
	if out.GetUpdatesBuf == "" {
		out.GetUpdatesBuf = buf
	}
	return &out, nil
}

func (c *Client) SendText(ctx context.Context, to, contextToken, text string) error {
	msg := Message{
		FromUserID:   "",
		ToUserID:     to,
		ClientID:     newClientID(),
		MessageType:  messageTypeBot,
		MessageState: messageStateFinish,
		ContextToken: contextToken,
		ItemList:     []MessageItem{{Type: itemTypeText, TextItem: &TextItem{Text: text}}},
	}
	sctx, cancel := context.WithTimeout(ctx, sendTimeout)
	defer cancel()
	return c.post(sctx, "/ilink/bot/sendmessage", map[string]any{"msg": msg}, nil)
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
	if c.RawLog != nil {
		fmt.Fprintf(c.RawLog, "<- %s %s http=%d %s\n", req.Method, req.URL.Path, resp.StatusCode, maskSecrets(data))
	}
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("%s %s: http %d: %s", req.Method, req.URL.Path, resp.StatusCode, truncate(string(data), 200))
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
		return fmt.Errorf("%s: decode: %w", req.URL.Path, err)
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
	return fmt.Sprintf("sixath-spike:%d-%s", time.Now().UnixMilli(), hex.EncodeToString(b[:]))
}

func truncate(s string, n int) string {
	r := []rune(s)
	if len(r) <= n {
		return s
	}
	return string(r[:n]) + "…"
}
```

- [ ] **Step 4: 运行确认通过**

Run: `go test ./... -count=1`  
Expected: `ok  github.com/sixath/ilink-spike`

- [ ] **Step 5: Commit**

```powershell
git add ilink.go ilink_test.go
git commit -m "feat(spike): ilink-spike HTTP 客户端（扫码、长轮询、发文本、-14 识别、脱敏原始日志）"
```

---

### Task 3: 本地状态与长度测试文本

**Files:**
- Create: `scripts/ilink-spike/state.go`
- Test: `scripts/ilink-spike/state_test.go`

- [ ] **Step 1: 写失败测试**

`state_test.go`：

```go
package main

import (
	"path/filepath"
	"strings"
	"testing"
	"time"
	"unicode/utf8"
)

func TestLoadStateMissingFileReturnsEmpty(t *testing.T) {
	st, err := loadState(filepath.Join(t.TempDir(), "nope.json"))
	if err != nil {
		t.Fatalf("loadState: %v", err)
	}
	if st.Peers == nil || st.BotToken != "" {
		t.Fatalf("got %+v", st)
	}
}

func TestStateRoundTrip(t *testing.T) {
	path := filepath.Join(t.TempDir(), "sub", "a.json")
	st := &State{BotToken: "bt", BotID: "b@im.bot", Cursor: "c1", Peers: map[string]*PeerState{}}
	st.recordInbound(Message{
		FromUserID:   "u@im.wechat",
		MessageType:  messageTypeUser,
		ContextToken: "ctx-1",
		ItemList:     []MessageItem{{Type: itemTypeText, TextItem: &TextItem{Text: "hello"}}},
	}, time.Unix(100, 0))
	if err := st.save(path); err != nil {
		t.Fatalf("save: %v", err)
	}
	got, err := loadState(path)
	if err != nil {
		t.Fatalf("loadState: %v", err)
	}
	p := got.Peers["u@im.wechat"]
	if got.Cursor != "c1" || p == nil || p.ContextToken != "ctx-1" || p.LastText != "hello" || p.BotIDAtToken != "b@im.bot" {
		t.Fatalf("got %+v peer %+v", got, p)
	}
}

func TestRecordInboundIgnoresBotMessagesAndKeepsTokenWhenMissing(t *testing.T) {
	st := &State{BotID: "b", Peers: map[string]*PeerState{}}
	st.recordInbound(Message{FromUserID: "u", MessageType: messageTypeUser, ContextToken: "ctx-1"}, time.Unix(1, 0))
	st.recordInbound(Message{FromUserID: "u", MessageType: messageTypeUser}, time.Unix(2, 0))
	st.recordInbound(Message{FromUserID: "b", MessageType: messageTypeBot, ContextToken: "x"}, time.Unix(3, 0))
	if st.Peers["u"].ContextToken != "ctx-1" {
		t.Fatalf("token overwritten: %+v", st.Peers["u"])
	}
	if _, ok := st.Peers["b"]; ok {
		t.Fatal("bot message must not be recorded as peer")
	}
}

func TestBuildLengthTextASCII(t *testing.T) {
	for _, n := range []int{10, 500, 1800, 4000} {
		s := buildLengthText(n, false)
		if got := utf8.RuneCountInString(s); got != n {
			t.Fatalf("n=%d rune count = %d", n, got)
		}
		if n >= 100 && !strings.HasSuffix(s, "【END】") {
			t.Fatalf("n=%d missing END marker", n)
		}
	}
}

func TestBuildLengthTextChinese(t *testing.T) {
	s := buildLengthText(1000, true)
	if got := utf8.RuneCountInString(s); got != 1000 {
		t.Fatalf("rune count = %d", got)
	}
	if len(s) < 2500 {
		t.Fatalf("byte length = %d, want ~3 bytes per rune", len(s))
	}
	if !strings.Contains(s, "测") || !strings.HasSuffix(s, "【END】") {
		t.Fatalf("unexpected content: %q", s[:60])
	}
}
```

- [ ] **Step 2: 运行确认失败**

Run: `go test ./... -count=1`  
Expected: FAIL，`undefined: loadState`、`undefined: buildLengthText`。

- [ ] **Step 3: 实现**

`state.go`：

```go
package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"time"
)

type PeerState struct {
	ContextToken   string    `json:"context_token"`
	ContextTokenAt time.Time `json:"context_token_at"`
	BotIDAtToken   string    `json:"bot_id_at_token"`
	LastText       string    `json:"last_text"`
}

type State struct {
	BaseURL     string                `json:"base_url"`
	BotToken    string                `json:"bot_token"`
	BotID       string                `json:"bot_id"`
	OwnerUserID string                `json:"owner_user_id"`
	LoginCount  int                   `json:"login_count"`
	SavedAt     time.Time             `json:"saved_at"`
	Cursor      string                `json:"cursor"`
	Peers       map[string]*PeerState `json:"peers"`
}

func loadState(path string) (*State, error) {
	data, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return &State{Peers: map[string]*PeerState{}}, nil
	}
	if err != nil {
		return nil, err
	}
	var st State
	if err := json.Unmarshal(data, &st); err != nil {
		return nil, fmt.Errorf("decode %s: %w", path, err)
	}
	if st.Peers == nil {
		st.Peers = map[string]*PeerState{}
	}
	return &st, nil
}

func (s *State) save(path string) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	data, err := json.MarshalIndent(s, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, data, 0o600)
}

func (s *State) recordInbound(m Message, now time.Time) {
	if m.MessageType != messageTypeUser || m.FromUserID == "" {
		return
	}
	p := s.Peers[m.FromUserID]
	if p == nil {
		p = &PeerState{}
		s.Peers[m.FromUserID] = p
	}
	if m.ContextToken != "" {
		p.ContextToken = m.ContextToken
		p.ContextTokenAt = now
		p.BotIDAtToken = s.BotID
	}
	if text := m.Text(); text != "" {
		p.LastText = truncate(text, 40)
	}
}

// buildLengthText 生成恰好 n 个字符的文本：头部标明长度，每第 100 个字符为 '|'，以【END】结尾，
// 便于在微信里判断是否被截断或拆分。zh=true 时用中文填充（每字 3 字节），用于探测按字节计的上限。
func buildLengthText(n int, zh bool) string {
	fill := func(pos int) rune {
		switch {
		case pos%100 == 0:
			return '|'
		case zh:
			return '测'
		default:
			return rune('0' + pos%10)
		}
	}
	head := []rune(fmt.Sprintf("【长度测试 %d】", n))
	tail := []rune("【END】")
	out := make([]rune, 0, n)
	if n < len(head)+len(tail) {
		for len(out) < n {
			out = append(out, fill(len(out)+1))
		}
		return string(out)
	}
	out = append(out, head...)
	for len(out) < n-len(tail) {
		out = append(out, fill(len(out)+1))
	}
	out = append(out, tail...)
	return string(out)
}
```

- [ ] **Step 4: 运行确认通过**

Run: `go test ./... -count=1`  
Expected: `ok  github.com/sixath/ilink-spike`

- [ ] **Step 5: Commit**

```powershell
git add state.go state_test.go
git commit -m "feat(spike): ilink-spike 本地状态与长度测试文本"
```

---

### Task 4: 子命令 CLI

**Files:**
- Create: `scripts/ilink-spike/main.go`
- Modify: `scripts/ilink-spike/go.mod`、`go.sum`（`go mod tidy`）

CLI 无单测（纯 IO 编排），以编译 + `go vet` + 冒烟验证。

- [ ] **Step 1: 实现**

`main.go`：

```go
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"path/filepath"
	"sort"
	"time"

	qrcode "github.com/skip2/go-qrcode"
)

var defaultStatePath = filepath.Join(".state", "default.json")

func main() {
	if len(os.Args) < 2 {
		usage()
		os.Exit(2)
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()

	var err error
	switch os.Args[1] {
	case "login":
		err = runLogin(ctx, os.Args[2:])
	case "listen":
		err = runListen(ctx, os.Args[2:])
	case "send":
		err = runSend(ctx, os.Args[2:])
	case "peers":
		err = runPeers(os.Args[2:])
	default:
		usage()
		os.Exit(2)
	}
	if err != nil {
		var apiErr *APIError
		if errors.As(err, &apiErr) && apiErr.SessionExpired() {
			fmt.Fprintln(os.Stderr, "会话已过期（-14），需要重新 login：", err)
			os.Exit(3)
		}
		fmt.Fprintln(os.Stderr, "error:", err)
		os.Exit(1)
	}
}

func usage() {
	fmt.Fprintln(os.Stderr, `用法：
  ilink-spike login  [-state path] [-raw]                扫码登录，记录状态接口耗时
  ilink-spike listen [-state path] [-raw] [-echo]        长轮询收消息，保存 context_token
  ilink-spike send   [-state path] [-to peer] (-text 内容 | -len N [-zh])
  ilink-spike peers  [-state path]                       列出已记录的对端`)
}

func runLogin(ctx context.Context, args []string) error {
	fs := flag.NewFlagSet("login", flag.ExitOnError)
	statePath := fs.String("state", defaultStatePath, "状态文件")
	raw := fs.Bool("raw", false, "打印各接口原始 JSON（token 已打码）")
	_ = fs.Parse(args)

	st, err := loadState(*statePath)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(*statePath), 0o700); err != nil {
		return err
	}
	c := NewClient(defaultBaseURL, "")
	if *raw {
		c.RawLog = os.Stdout
	}
	for attempt := 1; attempt <= 3; attempt++ {
		qr, err := c.GetBotQRCode(ctx)
		if err != nil {
			return err
		}
		pngPath := filepath.Join(filepath.Dir(*statePath), "qrcode.png")
		if err := qrcode.WriteFile(qr.ImgContent, qrcode.Medium, 320, pngPath); err != nil {
			fmt.Printf("二维码图片生成失败（%v）；qrcode_img_content 长度=%d 前 200 字=%q\n",
				err, len(qr.ImgContent), truncate(qr.ImgContent, 200))
		} else {
			if q, err := qrcode.New(qr.ImgContent, qrcode.Low); err == nil {
				fmt.Println(q.ToSmallString(false))
			}
			fmt.Printf("二维码已写入 %s（终端显示不清时打开该图片），请用微信扫码（第 %d 次）\n", pngPath, attempt)
		}

		res, err := pollQRCode(ctx, c, qr.QRCode)
		if err != nil {
			return err
		}
		if res == nil {
			fmt.Println("二维码已过期，重新获取")
			continue
		}
		return finishLogin(st, *statePath, res)
	}
	return errors.New("二维码连续过期 3 次")
}

// pollQRCode 打印每次调用的时刻与耗时，用于判断 get_qrcode_status 是否为服务端挂起（spec §10 问题 2）。
// 返回 nil, nil 表示二维码过期。
func pollQRCode(ctx context.Context, c *Client, code string) (*QRStatus, error) {
	start := time.Now()
	for {
		t0 := time.Now()
		s, err := c.GetQRCodeStatus(ctx, code)
		if err != nil {
			return nil, err
		}
		took := time.Since(t0)
		fmt.Printf("%s [+%6.1fs] status=%-9s 本次耗时=%5.1fs 客户端超时=%v\n",
			time.Now().Format("15:04:05"), time.Since(start).Seconds(), s.Status, took.Seconds(), s.ClientTimeout)
		switch s.Status {
		case "confirmed":
			return s, nil
		case "expired":
			return nil, nil
		}
		if took < time.Second {
			select {
			case <-ctx.Done():
				return nil, ctx.Err()
			case <-time.After(time.Second):
			}
		}
	}
}

func finishLogin(st *State, path string, s *QRStatus) error {
	if st.BotID != "" {
		fmt.Printf("上次 bot_id=%s，本次 bot_id=%s，同一 bot：%v\n", st.BotID, s.BotID, st.BotID == s.BotID)
		fmt.Printf("上次 owner=%s，本次 owner=%s\n", st.OwnerUserID, s.UserID)
	}
	st.BaseURL = s.BaseURL
	if st.BaseURL == "" {
		st.BaseURL = defaultBaseURL
	}
	st.BotToken = s.BotToken
	st.BotID = s.BotID
	st.OwnerUserID = s.UserID
	st.Cursor = ""
	st.LoginCount++
	st.SavedAt = time.Now()
	fmt.Printf("登录成功：bot_id=%s owner=%s base_url=%s；保留 %d 个旧对端的 context_token（用于问题 3）\n",
		st.BotID, st.OwnerUserID, st.BaseURL, len(st.Peers))
	return st.save(path)
}

func runListen(ctx context.Context, args []string) error {
	fs := flag.NewFlagSet("listen", flag.ExitOnError)
	statePath := fs.String("state", defaultStatePath, "状态文件")
	raw := fs.Bool("raw", false, "打印各接口原始 JSON（token 已打码）")
	echo := fs.Bool("echo", false, "收到用户消息时回一条确认")
	_ = fs.Parse(args)

	st, err := loadState(*statePath)
	if err != nil {
		return err
	}
	if st.BotToken == "" {
		return errors.New("状态文件里没有 bot_token，先运行 login")
	}
	c := NewClient(st.BaseURL, st.BotToken)
	if *raw {
		c.RawLog = os.Stdout
	}
	fmt.Printf("开始监听 bot_id=%s owner=%s（Ctrl+C 退出）\n", st.BotID, st.OwnerUserID)
	for ctx.Err() == nil {
		up, err := c.GetUpdates(ctx, st.Cursor)
		if err != nil {
			if ctx.Err() != nil {
				return nil
			}
			var apiErr *APIError
			if errors.As(err, &apiErr) {
				if apiErr.SessionExpired() {
					fmt.Fprintf(os.Stderr, "登录（%s）后 %s 出现 -14\n",
						st.SavedAt.Format(time.RFC3339), time.Since(st.SavedAt).Round(time.Second))
				}
				return err
			}
			fmt.Fprintf(os.Stderr, "getupdates 失败（%v），5s 后重试\n", err)
			select {
			case <-ctx.Done():
				return nil
			case <-time.After(5 * time.Second):
			}
			continue
		}
		for _, m := range up.Msgs {
			fmt.Printf("%s msg_id=%d type=%d from=%s owner=%v group=%q items=%v has_context_token=%v text=%q\n",
				time.Now().Format("15:04:05"), m.MessageID, m.MessageType, m.FromUserID,
				m.FromUserID == st.OwnerUserID, m.GroupID, m.ItemTypes(), m.ContextToken != "", truncate(m.Text(), 80))
			st.recordInbound(m, time.Now())
			if *echo && m.MessageType == messageTypeUser && m.ContextToken != "" {
				if err := c.SendText(ctx, m.FromUserID, m.ContextToken, "spike 收到："+truncate(m.Text(), 40)); err != nil {
					fmt.Fprintln(os.Stderr, "echo 发送失败：", err)
				}
			}
		}
		st.Cursor = up.GetUpdatesBuf
		if err := st.save(*statePath); err != nil {
			return err
		}
	}
	return nil
}

func runSend(ctx context.Context, args []string) error {
	fs := flag.NewFlagSet("send", flag.ExitOnError)
	statePath := fs.String("state", defaultStatePath, "状态文件")
	to := fs.String("to", "", "目标 peer_id，默认扫码人")
	text := fs.String("text", "", "发送内容")
	n := fs.Int("len", 0, "发送恰好 N 个字符的长度测试文本（优先于 -text）")
	zh := fs.Bool("zh", false, "长度测试用中文填充（每字 3 字节）")
	_ = fs.Parse(args)

	st, err := loadState(*statePath)
	if err != nil {
		return err
	}
	if st.BotToken == "" {
		return errors.New("状态文件里没有 bot_token，先运行 login")
	}
	if *to == "" {
		*to = st.OwnerUserID
	}
	peer := st.Peers[*to]
	if peer == nil || peer.ContextToken == "" {
		return fmt.Errorf("没有 %s 的 context_token：让对方先给 bot 发一条消息，并运行 listen 记录", *to)
	}
	body := *text
	if *n > 0 {
		body = buildLengthText(*n, *zh)
	}
	if body == "" {
		return errors.New("需要 -text 或 -len")
	}
	fmt.Printf("发送给 %s：%d 字 / %d 字节；context_token 获取于 %s（当时 bot_id=%s，当前 bot_id=%s）\n",
		*to, len([]rune(body)), len(body), peer.ContextTokenAt.Format(time.RFC3339), peer.BotIDAtToken, st.BotID)
	if err := NewClient(st.BaseURL, st.BotToken).SendText(ctx, *to, peer.ContextToken, body); err != nil {
		return err
	}
	fmt.Println("sendmessage 返回成功。注意：200 不代表已送达，请在微信里确认是否收到、是否完整。")
	return nil
}

func runPeers(args []string) error {
	fs := flag.NewFlagSet("peers", flag.ExitOnError)
	statePath := fs.String("state", defaultStatePath, "状态文件")
	_ = fs.Parse(args)

	st, err := loadState(*statePath)
	if err != nil {
		return err
	}
	fmt.Printf("bot_id=%s owner=%s login_count=%d\n", st.BotID, st.OwnerUserID, st.LoginCount)
	ids := make([]string, 0, len(st.Peers))
	for id := range st.Peers {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	for _, id := range ids {
		p := st.Peers[id]
		age := "-"
		if !p.ContextTokenAt.IsZero() {
			age = time.Since(p.ContextTokenAt).Round(time.Second).String()
		}
		fmt.Printf("%s owner=%v has_token=%v token_age=%s token_bot=%s last=%q\n",
			id, id == st.OwnerUserID, p.ContextToken != "", age, p.BotIDAtToken, p.LastText)
	}
	return nil
}
```

- [ ] **Step 2: 整理依赖、编译、vet、冒烟**

Run:

```powershell
go mod tidy
go vet ./...
go build -o ilink-spike.exe .
./ilink-spike.exe; $LASTEXITCODE
```

Expected: `go.mod` 中 go-qrcode 不再带 `// indirect`；`go vet` 无输出；打印用法后输出 `2`。

- [ ] **Step 3: 全量测试**

Run: `go test ./... -count=1`  
Expected: `ok  github.com/sixath/ilink-spike`

- [ ] **Step 4: Commit**

只加源码与模块文件，`ilink-spike.exe` 不入库：

```powershell
git add main.go go.mod go.sum
git commit -m "feat(spike): ilink-spike 子命令 login/listen/send/peers"
```

---

### Task 5: 用真实微信号验证并回写 spec

**需要人工操作**：执行者需要微信号 A（扫码人）；问题 1、4 需要第二个微信号 B。每一步的完整终端输出用 `| Tee-Object -FilePath .state/<步骤>.log` 保存到 `.state/`（gitignore），**不入库**。

**Files:**
- Modify: `docs/superpowers/specs/2026-10-18-weixin-ilink-channel-design.md`（§10 与受影响章节）

- [ ] **Step 1: 问题 2 —— `get_qrcode_status` 是否服务端挂起**

Run: `./ilink-spike.exe login -raw | Tee-Object -FilePath .state/q2.log`。**先不扫码**观察约 1 分钟，然后用 A 扫码，**记下扫码时的时钟时间**，再在手机上确认。

判定（看 `本次耗时` 与 `客户端超时`）：

| 观察 | 结论 |
|------|------|
| 单次耗时明显 > 1s 且 `客户端超时=false` | 服务端挂起；挂起时长 ≈ 本次耗时 |
| `客户端超时` 一直为 `true`（每次约 40s） | 服务端挂起 ≥ 40s；spec 的 25s 等待参数照样可用 |
| 单次耗时 < 1s | 短轮询 |

另外记录：
- 扫码后 `scaned` 一行出现的时刻与扫码时刻之差（挂起中的请求能否被扫码事件及时唤醒，决定 §6.1 前端体验）；
- 若出现 `expired`，二维码从生成到过期的时长；
- `-raw` 输出中有无计划未预料的字段 / status 值。

- [ ] **Step 2: 问题 1 —— 非扫码人能否私聊 bot**

1. 另开终端（同一目录）：`./ilink-spike.exe listen -echo -raw | Tee-Object -FilePath .state/q1.log`。
2. A 在微信里给 bot 发"q1-owner"。确认 listen 输出 `owner=true` 并收到 echo。
   - **若 A 的消息显示 `owner=false`**：说明扫码返回的 `ilink_user_id` 与消息 `from_user_id` 格式不同，记录两者（仅尾号）—— 这会推翻 spec §6.1"owner 入名册并放行"的前提，回写时必须处理。
3. 在 A 的微信里查看 bot 会话，尝试任何能让 B 找到该 bot 的方式（转发名片 / 分享 / 搜索），让 B 发"q1-other"。
4. listen 出现 `owner=false` 的消息 → 可以；所有途径都找不到入口 → 不可以（记录尝试过的途径）。

- [ ] **Step 3: 问题 5 —— 单条文本长度上限**

1. 测试前让 A 先给 bot 发一条消息并确认 listen 已记录（只有对端发消息才会刷新 token）。
2. 另一终端依次执行，并在 A 的微信里确认每条：是否收到、是否以【END】结尾、是否被拆成多条、`sendmessage` 是否报错：

```powershell
./ilink-spike.exe send -len 1000
./ilink-spike.exe send -len 2000
./ilink-spike.exe send -len 4000
./ilink-spike.exe send -len 8000
./ilink-spike.exe send -len 1000 -zh
./ilink-spike.exe send -len 2000 -zh
./ilink-spike.exe send -len 4000 -zh
```

3. 若在相邻两档之间开始失败 / 截断，二分定位上限（如 2000 成功、4000 失败 → 试 3000，依此类推，精确到 ±250 字）。同时记录对应的字节数（`send` 输出的"字节"）。

- [ ] **Step 4: 问题 3 —— 同一 bot 重扫后旧 `context_token` 是否有效**

1. 让 A 给 bot 发一条消息，确认 listen 已记录；停止 listen。**从此刻起 A 不再给 bot 发任何消息。**
2. 对照组：`./ilink-spike.exe send -text "q3 对照"`，在微信确认**收到**（证明此时 token 有效）。
3. 备份旧状态：`Copy-Item .state/default.json .state/before-relogin.json`
4. 立即重扫：`./ilink-spike.exe login`，A 扫码确认。记录输出中的"同一 bot：true/false"与前后 owner。
5. `./ilink-spike.exe send -text "q3 旧 token 测试"`，在微信确认是否收到。
6. 验证旧 `bot_token` 是否失效：`./ilink-spike.exe listen -state .state/before-relogin.json 2>&1`，观察是否报 `-14`。未立即报错时至少等待 **40s**（一轮长轮询）仍无 `-14` 才判定"旧 token 仍可用"，然后 Ctrl+C 结束。

判定：
- 第 5 步收到 → 同号重扫可保留 `context_token`；未收到 → 重扫必须清空 token。
- 第 6 步报 `-14` → 旧 runner 会因 `-14` 停下（佐证 spec §6.1 / §7）；不报 → 旧 token 仍可用，spec 的 generation CAS 是唯一保护，需在回写中注明。
- 第 4 步若 bot_id 变化，顺带记录同一用户 A 的 owner ID 前后是否一致（部分回答问题 4）。

- [ ] **Step 5: 问题 4 —— 换 bot 后同一微信用户的 `from_user_id` 是否不变**

- 问题 1 = "可以"时：
  1. B 扫码登录第二个 bot：`./ilink-spike.exe login -state .state/b.json`。
  2. `./ilink-spike.exe listen -state .state/b.json`，让 A 给 B 的 bot 发"q4-from-A"。
  3. 对比 `./ilink-spike.exe peers` 中 A 的 ID（`owner=true` 那行）与 `./ilink-spike.exe peers -state .state/b.json` 中 `last="q4-from-A"` 那行的 ID。
- 问题 1 = "不可以"时：只记录 Step 4 第 4 步顺带得到的结论（若 bot_id 未变化则记"未观察到"），并注明"只有扫码人能对话，换号即换人，影响有限"。

- [ ] **Step 6: 额外观察（顺带记录）**

- listen 期间是否出现 `group` 非空的消息（是否有群聊）；
- 是否出现过 `-14` 以及登录后多久（listen 会打印）；
- `type=2`（bot 自己发的）消息是否会出现在 getupdates 中。

- [ ] **Step 7: 回写 spec（在仓库根相对路径 `../../docs/...`）**

在 spec §10 每个问题后追加"**结论（实测日期）**：…"（ID 只写末 4 位，不贴原始日志），并按结论调整受影响章节：

| 结论 | 调整 |
|------|------|
| 问题 1 = 只有扫码人能对话 | §0、§2、§5.2、§6.4：名册简化为仅 owner；白名单 / 名册审批 UI 移出一期；§6.3 推送目标仅 owner |
| Step 2 中 owner 消息显示 `owner=false` | §6.1 confirmed：owner 入名册改为"以首条入站消息的 `from_user_id` 为准"，或记录两种 ID 的映射规则 |
| 问题 2 = 短轮询 | §6.1 第 3 步：Portal 不挂起，前端固定 2s 轮询 |
| 问题 2 = 挂起但扫码不能及时唤醒 | §6.1 第 3 步：缩短 Portal 单次等待（如 10s）以改善提示延迟 |
| 问题 3 = 同号重扫 token 失效 | §6.1 confirmed：无论是否换号都清空名册 `context_token` |
| 问题 3 第 6 步旧 token 不报 `-14` | §7：注明旧 runner 只能靠 generation CAS / 同步重启停下 |
| 问题 4 = ID 随 bot 变化 | §6.1 confirmed：换号时清空整个名册（除新 owner） |
| 问题 5 = 实测上限 L 字（或 B 字节） | §6.2 / §6.3：切分改为同时按字符与字节限制，取 `min(1800, L - 余量)`，写成配置项 |
| 出现 `type=2` 回推 | §6.2：明确只处理 `message_type == 1`（已有），实现计划补测试 |
| 出现群聊消息 | §2.2：注明协议确有群聊，一期仍忽略 `group_id` 非空消息并补测试 |

- [ ] **Step 8: Commit**

```powershell
git add ../../docs/superpowers/specs/2026-10-18-weixin-ilink-channel-design.md
git commit -m "docs(spec): weixin_ilink Phase 0 实测结论回写"
```

---

## 完成标准

- `scripts/ilink-spike` 下 `go test ./... -count=1` 与 `go vet ./...` 通过。
- spec §10 五个问题均有实测结论（或"不适用"及原因），受影响章节已调整，且未提交任何完整 ID / token / 原始日志。
- 之后据此编写第 1～3 份实现计划（Portal 基础、Gateway 入站、出站推送）。
