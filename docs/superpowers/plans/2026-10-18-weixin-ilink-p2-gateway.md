# 微信 iLink 渠道 P2：Gateway 入站 Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Gateway 支持 `type=weixin_ilink` 渠道：用 Portal 同步下发的 `bot_token` 长轮询 iLink `getupdates`，按"名册上报 → 游标 CAS → 进程内幂等 → 白名单 → 非文本拦截 → peer 串行队列"处理入站，复用现有 pending `/switch` / slash / 自动路由 / Resolve / `TurnsStream` 流水线，把助手正文简化为纯文本、切分后用 `sendmessage` 回复；独立心跳上报连接态，`-14` 上报会话过期后挂起。完成后，P1 中扫码登录的渠道可以在微信里与 Agent 对话。

**Architecture:** 新增叶子包 `gateway/internal/ilink`（iLink HTTP 客户端、入站归一化、文本整形；只做 HTTP，不依赖 runtimeclient）；`channel.Channel` 增加 iLink 字段，`runtimeclient` 映射这些字段并新增 4 个 iLink Runtime API 方法；`adapter.RunWeixinIlink` 是单渠道 runner（轮询 goroutine + 心跳 goroutine + 每个对端一个串行 worker）；`adapter.WeixinIlinkManager` 对齐 `WecomBotManager` 的 diff 启停，用独立的 `IlinkConnectionChanged`（不含 `AllowedPeers`）；`channelsync.MultiReconciler` 让同步器同时驱动 `WecomBotManager` 与 `WeixinIlinkManager`。

**Tech Stack:** Go 1.26，标准库 `net/http` / `net/http/httptest`，无新依赖。

**Spec:** `docs/superpowers/specs/2026-10-18-weixin-ilink-channel-design.md`（§3 协议与 Phase 0 实测、§4.2 Gateway 改动清单、§5.5 状态派生、§5.6 Runtime API、§6.2 入站流程、§7 错误处理、§8 测试、§10 实测结论）

**前置：** P1（`docs/superpowers/plans/2026-10-18-weixin-ilink-p1-portal.md`）已在 `main` 上：Portal Runtime API、`gateway/channels` 下发 iLink 字段、Gateway `/hooks` 只接受 `type=webhook`（P1 Task 0A）。

**不在本计划内：** Portal 改动（P1 已完成）；Cron 投递、`send_to_weixin`、推回发起人（P3）；图片 / 文件、群聊、微信内 HITL、`sendtyping`、多副本 leader 租约（一期非目标）。

---

## 约定

- 除特别说明，命令在 `gateway/` 目录执行；每个 shell 会话先执行 `Remove-Item Env:GOROOT -ErrorAction SilentlyContinue`（本机残留旧 GOROOT，否则编译器版本不匹配）。
- 工作区 `main` 上已有与本计划无关的未提交改动（`evals/runner/runner.exe`、`sixath.exe`、`web/tsconfig.tsbuildinfo`、`evals/results/*.log` 等）。**只 `git add` 每个任务列出的文件**，禁止 `git add -A` / `git add .` / `git commit -a`。
- 本仓库 `core.autocrlf=true`，Windows 检出的 `.go` 文件是 CRLF，`gofmt -l` 会把**所有**未改动文件也列出来。只对本任务文件执行 `gofmt -w`（会转成 LF，Git 入库时本来就是 LF），不要用 `gofmt -l` 判断整个目录。
- 新增 Go 注释只写代码本身表达不了的约束。日志统一用 `log.Printf("weixin_ilink %s: ...", channelID, ...)`；**绝不**打印 `bot_token` / `context_token`，对端只打 ID 尾号。
- 测试用 `httptest` 假 iLink 服务器 + 假 Portal Runtime；时间相关参数通过 `WeixinIlinkDeps` 缩短。假服务器 handler **必须先读完请求体**：Go 服务端只有在请求体读到 EOF 之后才会感知客户端断开，否则 `r.Context()` 不会取消，`srv.Close()` 会一直卡住。
- 完整代码块是**整个文件**的内容（新建文件）；修改既有文件时给出"查找 / 替换为"片段，查找文本在原文件中唯一。

## Runtime API 契约（P1 已实现，摘自 `portal/internal/runtime/ilink.go` 与 `service.go`）

| 调用 | 请求 | 响应 |
|------|------|------|
| `GET /runtime/v1/gateway/channels` | — | `weixin_ilink` 条目额外带 `bot_token`、`base_url`（空时 Portal 填默认基址）、`generation`、`session_expired`、`allowed_peers`（原始微信 ID，无 `user:` 前缀） |
| `POST .../{channel_id}/ilink/peers` | `{generation, peer_id, context_token, preview, at}`（`at` 为 RFC3339，可省略） | `{allowed}`；generation 不符 → `409 ILINK_GENERATION_MISMATCH` |
| `GET .../{channel_id}/ilink/cursor` | — | `{generation, get_updates_buf}` |
| `PUT .../{channel_id}/ilink/cursor` | `{generation, get_updates_buf}` | `{ok:true}`；generation 不符 → `409` |
| `POST .../{channel_id}/ilink/session-expired` | `{generation, reason}` | `{ok:true}`（迟到的旧 generation 被忽略） |
| `POST .../{channel_id}/status` | 既有 `StatusBody` | — |

## 实现决策（规格未写明、本计划自行确定的部分）

1. **群消息在名册上报之前就丢弃**（不只是不派发）：群里的 `context_token` 绑在群会话上，写进对端名册会让后续单聊推送失败。一期未观察到群消息，只打一行日志。
2. **非文本判定**：`item_list` 中没有任何非空 `type=1` 文本即为非文本；图文混合消息只处理文字部分。名册预览取正文前 40 字，非文本记 `[非文本消息]`；`at` 取 `create_time_ms`，缺省用当前时间。
3. **游标**：启动时 `GET` 失败按退避重试（不回退到空游标，避免重放旧消息）；`get_updates_buf` 未变化时不 `PUT`；`PUT` 非 409 失败共尝试 4 次（首次 + 重试 3 次，间隔 200ms），仍失败记日志后继续处理本批。
4. **409 / `-14` 都是"挂起"而不是"退出"**：runner 停在 `<-ctx.Done()`，仍留在 manager 的 active 表里，`Reconcile` 不会每 15s 重新拉起；下次同步看到新 `generation`（重启）或 `session_expired=true`（停止，不报 `disabled`）再处理。挂起时停止心跳。`session-expired` 上报失败按退避重试直到成功。
5. **runner 停止**（停用 / 换号 / 进程退出）会取消在途 Turn，并且**不再发送**最终回复（旧 bot 凭证已不属于本 runner）；队列中未处理的消息丢弃（至多一次语义）。
6. **"处理中…"计时从消息出队开始**（不是从 Turn 流开始），每条入站最多发一次。
7. **失败文案**：Resolve / `TurnsStream` 返回的错误、Turn 超时用 `mapRuntimeUserError`；SSE `error` 事件文本用 `mapTurnFailureMessage`（与 `wecom_bot` 一致，超时归一为中文提示，其它原样）。统一加前缀"处理失败："。
8. Turn 正文直接用用户文本（`@Agent` 命中时用去掉提及后的文本），不像企微那样拼发起人卡片。
9. 所有回复（含 slash 结果、无权限、非文本提示）都经过同一个 `ShapeReply`（Markdown 简化 + 切分）。`_强调_` 只在两侧不是字母 / 数字时才去掉，避免误伤 `snake_case`。
10. `Channel` 的 iLink 字段 `yaml:"-"`：只能由 Portal 同步写入，不能写进旧的 `channels.yaml`。
11. `split_runes` 落为 Gateway 进程配置 `ilink_split_runes`（可选，0 = 4000），不做成渠道级字段。

## 文件结构

| 文件 | 职责 |
|------|------|
| `gateway/internal/ilink/client.go`（新） | `Client`：`GetUpdates`（40s 客户端超时 → 空批次）、`SendText`（返回 message_id）、`APIError` / `IsSessionExpired`、请求头与 `base_info` |
| `gateway/internal/ilink/message.go`（新） | `Normalize`：取文本 / 判非文本 / message_id / 预览 |
| `gateway/internal/ilink/text.go`（新） | `SimplifyMarkdown`、`SplitText`、`ShapeReply` |
| `gateway/internal/ilink/*_test.go`（新） | 协议、归一化、整形单测 |
| `gateway/internal/channel/registry.go` | `Channel` 新增 `BotToken`、`BaseURL`、`Generation`、`SessionExpired`、`AllowedPeers` |
| `gateway/internal/runtimeclient/client.go` | 映射新字段；`ReportIlinkPeer`、`GetIlinkCursor`、`PutIlinkCursor`、`ReportIlinkSessionExpired`、`IsIlinkGenerationMismatch` |
| `gateway/internal/runtimeclient/ilink_test.go`（新） | 上述方法的契约测试 |
| `gateway/internal/channelsync/sync.go` + `multi_test.go`（新） | `MultiReconciler` |
| `gateway/internal/adapter/weixin_ilink.go`（新） | `WeixinIlinkDeps`、`RunWeixinIlink` runner、peer 队列、单条消息处理 |
| `gateway/internal/adapter/weixin_ilink_fixture_test.go`（新） | 假 iLink / 假 Portal / 事件序列 fixture |
| `gateway/internal/adapter/weixin_ilink_test.go`（新） | runner 行为测试 |
| `gateway/internal/adapter/weixin_ilink_manager.go` + `_test.go`（新） | `WeixinIlinkManager`、`IlinkConnectionChanged` |
| `gateway/internal/config/config.go` | `ilink_split_runes` |
| `gateway/cmd/gateway/main.go` | 装配 manager 与 `MultiReconciler` |
| `gateway/README.md` | 渠道一览、单副本约束、`weixin_ilink` 章节与验收清单 |

---

### Task 0: 基线

**Files:** 无

- [ ] **Step 1: 记录基线**

```powershell
Set-Location gateway
Remove-Item Env:GOROOT -ErrorAction SilentlyContinue
go build ./...
go test ./... -count=1 2>&1 | Select-String -Pattern "^(ok|FAIL|---)"
```

Expected: 所有包 `ok`（写计划时 `c2c163a` 上 14 个包全部通过）。后续任务只要求**不新增**失败；若基线已有失败，在最终报告中注明。

---

### Task 1: `internal/ilink` 客户端与入站归一化

协议逻辑移植自已实测的 `scripts/ilink-spike/ilink.go` 与 `portal/internal/ilink/client.go`：请求头 `AuthorizationType` / `Authorization: Bearer` / `X-WECHAT-UIN`、`base_info.channel_version`、`-14` 同时看 `ret` 与 `errcode`、客户端超时视为空批次、`url.Error` 只保留方法与路径、`message_id` 用 `json.RawMessage` 保精度。

**Files:**
- Create: `gateway/internal/ilink/client.go`、`gateway/internal/ilink/message.go`
- Test: `gateway/internal/ilink/client_test.go`、`gateway/internal/ilink/message_test.go`

- [ ] **Step 1: 写失败测试**

`gateway/internal/ilink/client_test.go`（完整内容）：

```go
package ilink

import (
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

func checkCommonHeaders(t *testing.T, r *http.Request) {
	t.Helper()
	if r.Header.Get("Content-Type") != "application/json" {
		t.Errorf("content-type = %q", r.Header.Get("Content-Type"))
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
}

func TestGetUpdatesBodyAndCursor(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/ilink/bot/getupdates" {
			t.Errorf("path = %s", r.URL.Path)
		}
		checkCommonHeaders(t, r)
		var body struct {
			Buf      string            `json:"get_updates_buf"`
			BaseInfo map[string]string `json:"base_info"`
		}
		raw, _ := io.ReadAll(r.Body)
		if err := json.Unmarshal(raw, &body); err != nil {
			t.Errorf("decode: %v", err)
		}
		if body.Buf != "buf-1" || body.BaseInfo["channel_version"] != channelVersion {
			t.Errorf("body = %s", raw)
		}
		_, _ = w.Write([]byte(`{"ret":0,"msgs":[{"message_id":7517413341670998920,"from_user_id":"u@im.wechat","message_type":1,"context_token":"ctx","create_time_ms":1760000000000,"item_list":[{"type":1,"text_item":{"text":"hi"}}]}],"get_updates_buf":"buf-2","sync_buf":"ignored"}`))
	}))
	defer srv.Close()

	upd, err := NewClient(srv.URL, "tok").GetUpdates(context.Background(), "buf-1")
	if err != nil {
		t.Fatalf("GetUpdates: %v", err)
	}
	if upd.GetUpdatesBuf != "buf-2" || len(upd.Msgs) != 1 {
		t.Fatalf("got %+v", upd)
	}
	in := Normalize(upd.Msgs[0])
	if in.MessageID != "7517413341670998920" || in.Text != "hi" || in.ContextToken != "ctx" || in.NonText {
		t.Fatalf("inbound = %+v", in)
	}
	if !upd.Msgs[0].IsUserMessage() {
		t.Fatal("message_type 1 should be a user message")
	}
}

func TestGetUpdatesEmptyBufKeepsCursor(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"ret":0,"msgs":[]}`))
	}))
	defer srv.Close()

	upd, err := NewClient(srv.URL, "tok").GetUpdates(context.Background(), "keep")
	if err != nil || upd.GetUpdatesBuf != "keep" {
		t.Fatalf("upd=%+v err=%v", upd, err)
	}
}

func TestGetUpdatesClientTimeoutIsEmptyBatch(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.Copy(io.Discard, r.Body)
		select {
		case <-time.After(2 * time.Second):
		case <-r.Context().Done():
		}
	}))
	defer srv.Close()

	c := NewClient(srv.URL, "tok")
	c.PollTimeout = 50 * time.Millisecond
	upd, err := c.GetUpdates(context.Background(), "buf-1")
	if err != nil {
		t.Fatalf("GetUpdates: %v", err)
	}
	if len(upd.Msgs) != 0 || upd.GetUpdatesBuf != "buf-1" {
		t.Fatalf("got %+v", upd)
	}
}

func TestGetUpdatesParentCanceledIsError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.Copy(io.Discard, r.Body)
		<-r.Context().Done()
	}))
	defer srv.Close()

	ctx, cancel := context.WithCancel(context.Background())
	time.AfterFunc(50*time.Millisecond, cancel)
	_, err := NewClient(srv.URL, "tok").GetUpdates(ctx, "")
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("err = %v, want context.Canceled", err)
	}
}

func TestSessionExpiredRetOrErrcode(t *testing.T) {
	for _, body := range []string{`{"ret":-14}`, `{"errcode":-14,"errmsg":"session timeout"}`} {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			_, _ = w.Write([]byte(body))
		}))
		_, err := NewClient(srv.URL, "tok").GetUpdates(context.Background(), "")
		srv.Close()
		if !IsSessionExpired(err) {
			t.Fatalf("body %s: err = %v, want session expired", body, err)
		}
	}
}

func TestHTTPErrorIsNotSessionExpiredAndMasksSecrets(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadGateway)
		_, _ = w.Write([]byte(`{"context_token":"leak-ctx"}`))
	}))
	defer srv.Close()

	_, err := NewClient(srv.URL, "tok").GetUpdates(context.Background(), "")
	if err == nil || IsSessionExpired(err) {
		t.Fatalf("err = %v", err)
	}
	if strings.Contains(err.Error(), "leak-ctx") {
		t.Fatalf("error leaks secret: %v", err)
	}
}

func TestSendTextBodyAndMessageID(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/ilink/bot/sendmessage" {
			t.Errorf("path = %s", r.URL.Path)
		}
		checkCommonHeaders(t, r)
		raw, _ := io.ReadAll(r.Body)
		var body struct {
			Msg map[string]any `json:"msg"`
		}
		if err := json.Unmarshal(raw, &body); err != nil {
			t.Errorf("decode: %v", err)
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
		if !strings.Contains(string(raw), `"text_item":{"text":"浣犲ソ"}`) {
			t.Errorf("item_list missing text: %s", raw)
		}
		_, _ = w.Write([]byte(`{"message_id":7517413341670998920}`))
	}))
	defer srv.Close()

	id, err := NewClient(srv.URL, "tok").SendText(context.Background(), "u@im.wechat", "ctx-1", "浣犲ソ")
	if err != nil || id != "7517413341670998920" {
		t.Fatalf("id=%q err=%v", id, err)
	}
}

func TestNetworkErrorNamesMethodAndPath(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))
	base := srv.URL
	srv.Close()

	_, err := NewClient(base, "tok").SendText(context.Background(), "u", "c", "x")
	if err == nil {
		t.Fatal("want error for closed port")
	}
	if !strings.Contains(err.Error(), "ilink POST /ilink/bot/sendmessage") {
		t.Fatalf("err = %v", err)
	}
}

func TestNewClientDefaults(t *testing.T) {
	c := NewClient("", "")
	if c.BaseURL != DefaultBaseURL || c.PollTimeout != DefaultPollTimeout {
		t.Fatalf("got base=%q poll=%s", c.BaseURL, c.PollTimeout)
	}
	if NewClient("https://x/", "").BaseURL != "https://x" {
		t.Fatal("trailing slash not trimmed")
	}
}
```

`gateway/internal/ilink/message_test.go`（完整内容）：

```go
package ilink

import (
	"encoding/json"
	"strings"
	"testing"
)

func decodeMessage(t *testing.T, raw string) Message {
	t.Helper()
	var m Message
	if err := json.Unmarshal([]byte(raw), &m); err != nil {
		t.Fatal(err)
	}
	return m
}

func TestNormalizeTextItems(t *testing.T) {
	m := decodeMessage(t, `{"message_id":"m-1","from_user_id":" u@im.wechat ","message_type":1,"context_token":"ctx","item_list":[{"type":1,"text_item":{"text":"第一段"}},{"type":2},{"type":1,"text_item":{"text":"第二段"}}]}`)
	in := Normalize(m)
	if in.MessageID != "m-1" || in.FromUserID != "u@im.wechat" || in.Text != "第一段\n第二段" || in.NonText {
		t.Fatalf("inbound = %+v", in)
	}
	if !in.CreatedAt.IsZero() {
		t.Fatalf("created_at without create_time_ms = %v", in.CreatedAt)
	}
}

func TestNormalizeNonText(t *testing.T) {
	m := decodeMessage(t, `{"message_id":1,"from_user_id":"u","message_type":1,"item_list":[{"type":2}]}`)
	in := Normalize(m)
	if !in.NonText || in.Text != "" || in.Preview() != "[非文本消息]" {
		t.Fatalf("inbound = %+v preview=%q", in, in.Preview())
	}
	blank := Normalize(decodeMessage(t, `{"message_id":2,"from_user_id":"u","message_type":1,"item_list":[{"type":1,"text_item":{"text":"  "}}]}`))
	if !blank.NonText {
		t.Fatalf("blank text should be NonText: %+v", blank)
	}
}

func TestNormalizeGroupAndBotMessage(t *testing.T) {
	m := decodeMessage(t, `{"message_id":3,"from_user_id":"u","group_id":"g1","message_type":2,"item_list":[]}`)
	if m.IsUserMessage() {
		t.Fatal("message_type 2 is a bot message")
	}
	if Normalize(m).GroupID != "g1" {
		t.Fatal("group id lost")
	}
}

func TestPreviewTruncatesTo40Runes(t *testing.T) {
	in := Inbound{Text: strings.Repeat("字", 50)}
	if got := []rune(in.Preview()); len(got) != 40 {
		t.Fatalf("preview runes = %d", len(got))
	}
}
```

- [ ] **Step 2: 运行确认失败**

```powershell
go test ./internal/ilink/ -count=1
```

Expected: FAIL，编译错误 `undefined: NewClient` / `undefined: Normalize` 等。

- [ ] **Step 3: 实现**

`gateway/internal/ilink/client.go`（完整内容）：

```go
// Package ilink is a minimal HTTP client for the WeChat iLink bot gateway (long-poll inbound and text replies).
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
	// DefaultPollTimeout must exceed the ~35s the server holds getupdates open.
	DefaultPollTimeout = 40 * time.Second

	channelVersion     = "1.0.0"
	sendTimeout        = 15 * time.Second
	itemTypeText       = 1
	messageTypeUser    = 1
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
		PollTimeout: DefaultPollTimeout,
	}
}

type TextItem struct {
	Text string `json:"text"`
}

type MessageItem struct {
	Type     int       `json:"type"`
	TextItem *TextItem `json:"text_item,omitempty"`
}

// Message is one getupdates entry; message_id stays raw because it exceeds float64 precision.
type Message struct {
	MessageID    json.RawMessage `json:"message_id,omitempty"`
	FromUserID   string          `json:"from_user_id"`
	ToUserID     string          `json:"to_user_id"`
	CreateTimeMs int64           `json:"create_time_ms,omitempty"`
	GroupID      string          `json:"group_id,omitempty"`
	MessageType  int             `json:"message_type"`
	ContextToken string          `json:"context_token,omitempty"`
	ItemList     []MessageItem   `json:"item_list"`
}

type Updates struct {
	Msgs          []Message `json:"msgs"`
	GetUpdatesBuf string    `json:"get_updates_buf"`
}

// GetUpdates treats its own poll timeout as an empty batch that keeps the cursor; parent cancellation is an error.
func (c *Client) GetUpdates(ctx context.Context, buf string) (*Updates, error) {
	timeout := c.PollTimeout
	if timeout <= 0 {
		timeout = DefaultPollTimeout
	}
	pctx, cancel := context.WithTimeout(ctx, timeout)
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
	sctx, cancel := context.WithTimeout(ctx, sendTimeout)
	defer cancel()
	var out struct {
		MessageID json.RawMessage `json:"message_id"`
	}
	if err := c.post(sctx, "/ilink/bot/sendmessage", map[string]any{"msg": msg}, &out); err != nil {
		return "", err
	}
	return messageIDString(out.MessageID), nil
}

// messageIDString accepts a JSON string or number; any other shape yields "".
func messageIDString(raw json.RawMessage) string {
	raw = bytes.TrimSpace(raw)
	if len(raw) == 0 {
		return ""
	}
	var s string
	if json.Unmarshal(raw, &s) == nil {
		return s
	}
	var n json.Number
	if json.Unmarshal(raw, &n) == nil {
		return n.String()
	}
	return ""
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
	hc := c.HTTP
	if hc == nil {
		hc = http.DefaultClient
	}
	resp, err := hc.Do(req)
	if err != nil {
		// url.Error embeds the full URL; keep only method and path in logs.
		var ue *url.Error
		if errors.As(err, &ue) {
			return fmt.Errorf("ilink %s %s: %w", req.Method, req.URL.Path, ue.Err)
		}
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

`gateway/internal/ilink/message.go`（完整内容）：

```go
package ilink

import (
	"strings"
	"time"
)

const previewRunes = 40

// Inbound is a user message reduced to what the Gateway pipeline needs.
type Inbound struct {
	MessageID    string
	FromUserID   string
	GroupID      string
	ContextToken string
	// Text joins all text items; empty means the message carried no text (NonText).
	Text      string
	NonText   bool
	CreatedAt time.Time
}

// IsUserMessage filters out bot echoes (message_type 2).
func (m Message) IsUserMessage() bool { return m.MessageType == messageTypeUser }

// Normalize extracts text from item_list (type=1); messages without any non-blank text are NonText.
func Normalize(m Message) Inbound {
	var parts []string
	for _, it := range m.ItemList {
		if it.Type == itemTypeText && it.TextItem != nil && strings.TrimSpace(it.TextItem.Text) != "" {
			parts = append(parts, it.TextItem.Text)
		}
	}
	in := Inbound{
		MessageID:    messageIDString(m.MessageID),
		FromUserID:   strings.TrimSpace(m.FromUserID),
		GroupID:      strings.TrimSpace(m.GroupID),
		ContextToken: m.ContextToken,
		Text:         strings.TrimSpace(strings.Join(parts, "\n")),
	}
	in.NonText = in.Text == ""
	if m.CreateTimeMs > 0 {
		in.CreatedAt = time.UnixMilli(m.CreateTimeMs)
	}
	return in
}

// Preview is the roster preview: first 40 runes of the text, or a placeholder for non-text.
func (in Inbound) Preview() string {
	if in.NonText {
		return "[非文本消息]"
	}
	r := []rune(in.Text)
	if len(r) <= previewRunes {
		return in.Text
	}
	return string(r[:previewRunes])
}
```

- [ ] **Step 4: 运行确认通过**

```powershell
gofmt -w internal/ilink/client.go internal/ilink/message.go internal/ilink/client_test.go internal/ilink/message_test.go
go test ./internal/ilink/ -count=1 -v 2>&1 | Select-String -Pattern "^(ok|FAIL|---)"
```

Expected: 13 个 `--- PASS`，`ok  github.com/sixath/gateway/internal/ilink`。

- [ ] **Step 5: Commit**

```powershell
git add internal/ilink/client.go internal/ilink/message.go internal/ilink/client_test.go internal/ilink/message_test.go
git commit -m "feat(gateway): 新增 internal/ilink 客户端与入站归一化"
```

---

### Task 2: 回复文本整形（Markdown 简化与切分）

**Files:**
- Create: `gateway/internal/ilink/text.go`
- Test: `gateway/internal/ilink/text_test.go`

- [ ] **Step 1: 写失败测试**

`gateway/internal/ilink/text_test.go`（完整内容）：

````go
package ilink

import (
	"strings"
	"testing"
	"unicode/utf8"
)

func TestSimplifyMarkdown(t *testing.T) {
	cases := []struct{ name, in, want string }{
		{"heading", "# 标题\n## 小节\n正文", "标题\n小节\n正文"},
		{"bold italic", "**加粗** 和 *斜体* 以及 __下划线__ 和 _强调_", "加粗 和 斜体 以及 下划线 和 强调"},
		{"snake_case kept", "变量 user_id_name 不变", "变量 user_id_name 不变"},
		{"link", "见 [文档](https://example.com/a) 和 ![图](https://x/y.png)", "见 文档 (https://example.com/a) 和 图 (https://x/y.png)"},
		{"inline code", "运行 `go test **./...**` 即可", "运行 go test **./...** 即可"},
		{"strike", "~~旧~~新", "旧新"},
		{"code block kept", "前\n```go\n# not heading\nx := **y**\n```\n后", "前\n# not heading\nx := **y**\n后"},
		{"list kept", "- 一\n- 二", "- 一\n- 二"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := SimplifyMarkdown(tc.in); got != tc.want {
				t.Fatalf("got %q want %q", got, tc.want)
			}
		})
	}
}

func TestSplitTextShortUnchanged(t *testing.T) {
	got := SplitText("你好", 10)
	if len(got) != 1 || got[0] != "你好" {
		t.Fatalf("got %q", got)
	}
	if SplitText("  \n ", 10) != nil {
		t.Fatal("blank input should yield no chunks")
	}
}

func TestSplitTextPrefersNewline(t *testing.T) {
	got := SplitText("第一行内容\n第二行内容\n第三行", 12)
	want := []string{"第一行内容\n第二行内容", "第三行"}
	if strings.Join(got, "|") != strings.Join(want, "|") {
		t.Fatalf("got %q want %q", got, want)
	}
}

func TestSplitTextHardCutWithoutNewline(t *testing.T) {
	s := strings.Repeat("字", 25)
	got := SplitText(s, 10)
	if len(got) != 3 || strings.Join(got, "") != s {
		t.Fatalf("got %q", got)
	}
	for _, c := range got {
		if utf8.RuneCountInString(c) > 10 {
			t.Fatalf("chunk too long: %q", c)
		}
	}
}

func TestSplitTextDefaultLimit(t *testing.T) {
	s := strings.Repeat("a", DefaultSplitRunes+1)
	got := SplitText(s, 0)
	if len(got) != 2 || utf8.RuneCountInString(got[0]) != DefaultSplitRunes {
		t.Fatalf("chunks=%d first=%d", len(got), utf8.RuneCountInString(got[0]))
	}
}

func TestShapeReply(t *testing.T) {
	got := ShapeReply("# 结论\n**是**", 100)
	if len(got) != 1 || got[0] != "结论\n是" {
		t.Fatalf("got %q", got)
	}
}
````

- [ ] **Step 2: 运行确认失败**

```powershell
go test ./internal/ilink/ -run "Simplify|Split|Shape" -count=1
```

Expected: FAIL，编译错误 `undefined: SimplifyMarkdown` / `undefined: SplitText` / `undefined: DefaultSplitRunes`。

- [ ] **Step 3: 实现**

`gateway/internal/ilink/text.go`（完整内容）：

````go
package ilink

import (
	"regexp"
	"strconv"
	"strings"
)

// DefaultSplitRunes is the per-message cap; Phase 0 delivered 4000 CJK runes intact.
const DefaultSplitRunes = 4000

var (
	headingRe    = regexp.MustCompile(`^\s{0,3}#{1,6}\s+`)
	linkRe       = regexp.MustCompile(`!?\[([^\]]*)\]\(([^)\s]+)\)`)
	boldStarRe   = regexp.MustCompile(`\*\*([^*\n]+?)\*\*`)
	boldUnderRe  = regexp.MustCompile(`__([^_\n]+?)__`)
	italicStarRe = regexp.MustCompile(`\*([^*\s][^*\n]*?)\*`)
	italicUndRe  = regexp.MustCompile(`(^|[^\pL\pN_])_([^_\s][^_\n]*?)_($|[^\pL\pN_])`)
	strikeRe     = regexp.MustCompile(`~~([^~\n]+?)~~`)
	inlineCodeRe = regexp.MustCompile("`([^`\n]+)`")
)

// SimplifyMarkdown turns assistant Markdown into WeChat-friendly plain text.
// Fenced code blocks keep their content verbatim (fence lines dropped).
func SimplifyMarkdown(s string) string {
	lines := strings.Split(strings.ReplaceAll(s, "\r\n", "\n"), "\n")
	out := make([]string, 0, len(lines))
	inFence := false
	for _, line := range lines {
		if strings.HasPrefix(strings.TrimSpace(line), "```") {
			inFence = !inFence
			continue
		}
		if inFence {
			out = append(out, line)
			continue
		}
		out = append(out, simplifyInline(headingRe.ReplaceAllString(line, "")))
	}
	return strings.TrimSpace(strings.Join(out, "\n"))
}

func simplifyInline(line string) string {
	// Protect inline code so emphasis rules do not touch it.
	var codes []string
	line = inlineCodeRe.ReplaceAllStringFunc(line, func(m string) string {
		codes = append(codes, inlineCodeRe.FindStringSubmatch(m)[1])
		return "\x00" + strconv.Itoa(len(codes)-1) + "\x00"
	})
	line = linkRe.ReplaceAllString(line, "$1 ($2)")
	line = boldStarRe.ReplaceAllString(line, "$1")
	line = boldUnderRe.ReplaceAllString(line, "$1")
	line = italicStarRe.ReplaceAllString(line, "$1")
	line = italicUndRe.ReplaceAllString(line, "$1$2$3")
	line = strikeRe.ReplaceAllString(line, "$1")
	for i, c := range codes {
		line = strings.Replace(line, "\x00"+strconv.Itoa(i)+"\x00", c, 1)
	}
	return line
}

// SplitText cuts s into chunks of at most maxRunes runes, preferring the last newline in each window.
func SplitText(s string, maxRunes int) []string {
	if maxRunes <= 0 {
		maxRunes = DefaultSplitRunes
	}
	var out []string
	rest := []rune(s)
	for len(rest) > maxRunes {
		cut := -1
		for i := maxRunes - 1; i > 0; i-- {
			if rest[i] == '\n' {
				cut = i
				break
			}
		}
		var chunk string
		if cut > 0 {
			chunk = string(rest[:cut])
			rest = rest[cut+1:]
		} else {
			chunk = string(rest[:maxRunes])
			rest = rest[maxRunes:]
		}
		if strings.TrimSpace(chunk) != "" {
			out = append(out, strings.TrimRight(chunk, "\n"))
		}
	}
	if strings.TrimSpace(string(rest)) != "" {
		out = append(out, strings.TrimRight(string(rest), "\n"))
	}
	return out
}

// ShapeReply simplifies Markdown then splits for sendmessage.
func ShapeReply(s string, maxRunes int) []string {
	return SplitText(SimplifyMarkdown(s), maxRunes)
}
````

- [ ] **Step 4: 运行确认通过**

```powershell
gofmt -w internal/ilink/text.go internal/ilink/text_test.go
go test ./internal/ilink/ -count=1
```

Expected: `ok  github.com/sixath/gateway/internal/ilink`。

- [ ] **Step 5: Commit**

```powershell
git add internal/ilink/text.go internal/ilink/text_test.go
git commit -m "feat(gateway): iLink 回复文本整形（Markdown 简化与按字数切分）"
```

---

### Task 3: `Channel` 字段与 runtimeclient iLink 方法

**Files:**
- Modify: `gateway/internal/channel/registry.go`、`gateway/internal/runtimeclient/client.go`
- Test: `gateway/internal/runtimeclient/ilink_test.go`（新）

- [ ] **Step 1: 写失败测试**

`gateway/internal/runtimeclient/ilink_test.go`（完整内容）：

```go
package runtimeclient

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestClient_ListGatewayChannelsMapsIlinkFields(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{
			"channels": []map[string]any{{
				"id":              "wx-1",
				"type":            "weixin_ilink",
				"enabled":         true,
				"bot_token":       "bt",
				"base_url":        "https://ilinkai.weixin.qq.com",
				"generation":      3,
				"session_expired": true,
				"allowed_peers":   []string{"a@im.wechat", "b@im.wechat"},
			}},
		})
	}))
	defer srv.Close()

	chs, err := New(srv.URL, "rt").ListGatewayChannels(context.Background())
	if err != nil {
		t.Fatalf("ListGatewayChannels: %v", err)
	}
	ch := chs[0]
	if ch.BotToken != "bt" || ch.BaseURL != "https://ilinkai.weixin.qq.com" || ch.Generation != 3 || !ch.SessionExpired {
		t.Fatalf("ch=%+v", ch)
	}
	if len(ch.AllowedPeers) != 2 || ch.AllowedPeers[1] != "b@im.wechat" {
		t.Fatalf("allowed_peers=%v", ch.AllowedPeers)
	}
}

func TestClient_IlinkRuntimeEndpoints(t *testing.T) {
	var last callSeen
	conflict := false
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		last = callSeen{method: r.Method, path: r.URL.Path, auth: r.Header.Get("Authorization"), body: string(body)}
		if conflict {
			w.WriteHeader(http.StatusConflict)
			_, _ = w.Write([]byte(`{"code":409,"reason":"ILINK_GENERATION_MISMATCH"}`))
			return
		}
		switch {
		case r.URL.Path == "/runtime/v1/gateway/channels/wx-1/ilink/peers":
			_, _ = w.Write([]byte(`{"allowed":true}`))
		case r.Method == http.MethodGet && r.URL.Path == "/runtime/v1/gateway/channels/wx-1/ilink/cursor":
			_, _ = w.Write([]byte(`{"generation":2,"get_updates_buf":"buf-1"}`))
		default:
			_, _ = w.Write([]byte(`{"ok":true}`))
		}
	}))
	defer srv.Close()

	c := New(srv.URL, "rt")
	ctx := context.Background()

	allowed, err := c.ReportIlinkPeer(ctx, "wx-1", IlinkPeerReport{
		Generation: 2, PeerID: "a@im.wechat", ContextToken: "ctx", Preview: "hi", At: "2026-10-18T09:00:00Z",
	})
	if err != nil || !allowed {
		t.Fatalf("allowed=%v err=%v", allowed, err)
	}
	assertCall(t, last, http.MethodPost, "/runtime/v1/gateway/channels/wx-1/ilink/peers", "Bearer rt", "")
	for _, want := range []string{`"generation":2`, `"peer_id":"a@im.wechat"`, `"context_token":"ctx"`, `"preview":"hi"`, `"at":"2026-10-18T09:00:00Z"`} {
		if !strings.Contains(last.body, want) {
			t.Fatalf("peer body %s missing %s", last.body, want)
		}
	}

	cur, err := c.GetIlinkCursor(ctx, "wx-1")
	if err != nil || cur.Generation != 2 || cur.GetUpdatesBuf != "buf-1" {
		t.Fatalf("cur=%+v err=%v", cur, err)
	}
	assertCall(t, last, http.MethodGet, "/runtime/v1/gateway/channels/wx-1/ilink/cursor", "Bearer rt", "")

	if err := c.PutIlinkCursor(ctx, "wx-1", IlinkCursor{Generation: 2, GetUpdatesBuf: "buf-2"}); err != nil {
		t.Fatalf("PutIlinkCursor: %v", err)
	}
	assertCall(t, last, http.MethodPut, "/runtime/v1/gateway/channels/wx-1/ilink/cursor", "Bearer rt", "")
	if last.body != `{"generation":2,"get_updates_buf":"buf-2"}` {
		t.Fatalf("cursor body=%s", last.body)
	}

	if err := c.ReportIlinkSessionExpired(ctx, "wx-1", 2, "-14"); err != nil {
		t.Fatalf("ReportIlinkSessionExpired: %v", err)
	}
	assertCall(t, last, http.MethodPost, "/runtime/v1/gateway/channels/wx-1/ilink/session-expired", "Bearer rt", "")
	if last.body != `{"generation":2,"reason":"-14"}` {
		t.Fatalf("session-expired body=%s", last.body)
	}

	conflict = true
	err = c.PutIlinkCursor(ctx, "wx-1", IlinkCursor{Generation: 1})
	if !IsIlinkGenerationMismatch(err) {
		t.Fatalf("err=%v want generation mismatch", err)
	}
	if _, err := c.ReportIlinkPeer(ctx, "wx-1", IlinkPeerReport{Generation: 1, PeerID: "a"}); !IsIlinkGenerationMismatch(err) {
		t.Fatalf("peer err=%v want generation mismatch", err)
	}
	if IsIlinkGenerationMismatch(nil) {
		t.Fatal("nil is not a mismatch")
	}
}
```

- [ ] **Step 2: 运行确认失败**

```powershell
go test ./internal/runtimeclient/ -run Ilink -count=1
```

Expected: FAIL，编译错误 `ch.BotToken undefined` / `undefined: IlinkPeerReport` 等。

- [ ] **Step 3: 实现**

1. `gateway/internal/channel/registry.go`，查找：

```go
	CorpSecret string `yaml:"corp_secret" json:"corp_secret,omitempty"`
}
```

替换为：

```go
	CorpSecret string `yaml:"corp_secret" json:"corp_secret,omitempty"`
	// weixin_ilink fields are synced from Portal only; BotToken must never be logged.
	BotToken       string   `yaml:"-" json:"bot_token,omitempty"`
	BaseURL        string   `yaml:"-" json:"base_url,omitempty"`
	Generation     int      `yaml:"-" json:"generation,omitempty"`
	SessionExpired bool     `yaml:"-" json:"session_expired,omitempty"`
	AllowedPeers   []string `yaml:"-" json:"allowed_peers,omitempty"`
}
```

2. `gateway/internal/runtimeclient/client.go` import 块，查找：

```go
	"encoding/json"
	"fmt"
	"io"
```

替换为：

```go
	"encoding/json"
	"errors"
	"fmt"
	"io"
```

3. 同文件 `gatewayChannelItem` 末尾，查找：

```go
	CorpID           string   `json:"corp_id"`
	CorpSecret       string   `json:"corp_secret"`
}
```

替换为：

```go
	CorpID           string   `json:"corp_id"`
	CorpSecret       string   `json:"corp_secret"`
	BotToken         string   `json:"bot_token"`
	BaseURL          string   `json:"base_url"`
	Generation       int      `json:"generation"`
	SessionExpired   bool     `json:"session_expired"`
	AllowedPeers     []string `json:"allowed_peers"`
}

// IlinkPeerReport is POST /runtime/v1/gateway/channels/{channel_id}/ilink/peers body.
type IlinkPeerReport struct {
	Generation   int    `json:"generation"`
	PeerID       string `json:"peer_id"`
	ContextToken string `json:"context_token"`
	Preview      string `json:"preview"`
	// At is RFC3339; empty lets Portal use its own clock.
	At string `json:"at,omitempty"`
}

// IlinkCursor is the GET / PUT .../ilink/cursor body.
type IlinkCursor struct {
	Generation    int    `json:"generation"`
	GetUpdatesBuf string `json:"get_updates_buf"`
}

type ilinkSessionExpiredBody struct {
	Generation int    `json:"generation"`
	Reason     string `json:"reason"`
}

// IsIlinkGenerationMismatch reports Portal's 409 ILINK_GENERATION_MISMATCH (runner is stale).
func IsIlinkGenerationMismatch(err error) bool {
	var he *HTTPError
	return errors.As(err, &he) && he != nil && he.StatusCode == http.StatusConflict
}
```

4. 同文件 `ListGatewayChannels` 中，查找：

```go
			CorpID:           item.CorpID,
			CorpSecret:       item.CorpSecret,
		}
```

替换为：

```go
			CorpID:           item.CorpID,
			CorpSecret:       item.CorpSecret,
			BotToken:         item.BotToken,
			BaseURL:          item.BaseURL,
			Generation:       item.Generation,
			SessionExpired:   item.SessionExpired,
			AllowedPeers:     item.AllowedPeers,
		}
```

5. 同文件，在 `RouteChannel` 的注释行之前插入 iLink 方法。查找：

```go
// RouteChannel asks Portal to classify which allowlisted agent should handle text.
```

替换为：

```go
func ilinkPath(channelID, suffix string) string {
	return "/runtime/v1/gateway/channels/" + url.PathEscape(strings.TrimSpace(channelID)) + "/ilink/" + suffix
}

// ReportIlinkPeer upserts the roster entry; allowed is informational (the synced AllowedPeers decides).
func (c *Client) ReportIlinkPeer(ctx context.Context, channelID string, req IlinkPeerReport) (bool, error) {
	var out struct {
		Allowed bool `json:"allowed"`
	}
	if err := c.doJSON(ctx, http.MethodPost, ilinkPath(channelID, "peers"), "", nil, req, &out); err != nil {
		return false, err
	}
	return out.Allowed, nil
}

// GetIlinkCursor returns the stored long-poll cursor and the generation it was written under.
func (c *Client) GetIlinkCursor(ctx context.Context, channelID string) (*IlinkCursor, error) {
	var out IlinkCursor
	if err := c.doJSON(ctx, http.MethodGet, ilinkPath(channelID, "cursor"), "", nil, nil, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// PutIlinkCursor is a CAS on generation; a stale generation yields a 409 HTTPError.
func (c *Client) PutIlinkCursor(ctx context.Context, channelID string, cur IlinkCursor) error {
	return c.doJSON(ctx, http.MethodPut, ilinkPath(channelID, "cursor"), "", nil, cur, nil)
}

// ReportIlinkSessionExpired marks the bot session expired (-14); Portal ignores stale generations.
func (c *Client) ReportIlinkSessionExpired(ctx context.Context, channelID string, generation int, reason string) error {
	body := ilinkSessionExpiredBody{Generation: generation, Reason: reason}
	return c.doJSON(ctx, http.MethodPost, ilinkPath(channelID, "session-expired"), "", nil, body, nil)
}

// RouteChannel asks Portal to classify which allowlisted agent should handle text.
```

- [ ] **Step 4: 运行确认通过**

```powershell
gofmt -w internal/channel/registry.go internal/runtimeclient/client.go internal/runtimeclient/ilink_test.go
go test ./internal/runtimeclient/ ./internal/channel/ -count=1
```

Expected: 两个包均 `ok`（含既有测试）。

- [ ] **Step 5: Commit**

```powershell
git add internal/channel/registry.go internal/runtimeclient/client.go internal/runtimeclient/ilink_test.go
git commit -m "feat(gateway): Channel 与 runtimeclient 支持 weixin_ilink 字段和 Runtime API"
```

---

### Task 4: `channelsync.MultiReconciler`

**Files:**
- Modify: `gateway/internal/channelsync/sync.go`
- Test: `gateway/internal/channelsync/multi_test.go`（新；复用 `sync_test.go` 中的 `fakeLister` / `fakeManager`）

- [ ] **Step 1: 写失败测试**

`gateway/internal/channelsync/multi_test.go`（完整内容）：

```go
package channelsync_test

import (
	"context"
	"testing"

	"github.com/sixath/gateway/internal/channel"
	"github.com/sixath/gateway/internal/channelsync"
)

func TestMultiReconciler_FansOutSameSnapshots(t *testing.T) {
	a, b := &fakeManager{}, &fakeManager{}
	reg := channel.NewRegistry()
	lister := &fakeLister{chs: []channel.Channel{
		{ID: "bot1", Type: "wecom_bot", Enabled: true, BotID: "b", Secret: "s"},
		{ID: "wx1", Type: "weixin_ilink", Enabled: true, BotToken: "t", Generation: 1},
	}}
	r := channelsync.NewRunner(channelsync.Config{
		Registry: reg,
		Lister:   lister,
		Manager:  channelsync.MultiReconciler{a, nil, b},
	})
	if err := r.SyncOnce(context.Background()); err != nil {
		t.Fatalf("SyncOnce: %v", err)
	}
	for name, m := range map[string]*fakeManager{"a": a, "b": b} {
		if m.callCount() != 1 {
			t.Fatalf("%s calls=%d want 1", name, m.callCount())
		}
		if len(m.calls[0].next) != 2 || len(m.calls[0].prev) != 0 {
			t.Fatalf("%s call=%+v", name, m.calls[0])
		}
	}
}
```

- [ ] **Step 2: 运行确认失败**

```powershell
go test ./internal/channelsync/ -count=1
```

Expected: FAIL，编译错误 `undefined: channelsync.MultiReconciler`。

- [ ] **Step 3: 实现**

`gateway/internal/channelsync/sync.go`，查找：

```go
// Reconciler applies wecom_bot start/stop based on snapshot diffs.
```

替换为：

```go
// Reconciler applies long-connection runner start/stop based on snapshot diffs.
```

再查找：

```go
// Config wires the sync runner.
```

替换为：

```go
// MultiReconciler fans one snapshot diff out to several reconcilers, in order.
type MultiReconciler []Reconciler

// Reconcile calls every non-nil reconciler with the same prev / next snapshots.
func (m MultiReconciler) Reconcile(prev, next []channel.Channel) {
	for _, r := range m {
		if r != nil {
			r.Reconcile(prev, next)
		}
	}
}

// Config wires the sync runner.
```

- [ ] **Step 4: 运行确认通过**

```powershell
gofmt -w internal/channelsync/sync.go internal/channelsync/multi_test.go
go test ./internal/channelsync/ -count=1
```

Expected: `ok  github.com/sixath/gateway/internal/channelsync`（既有测试不变）。

- [ ] **Step 5: Commit**

```powershell
git add internal/channelsync/sync.go internal/channelsync/multi_test.go
git commit -m "feat(gateway): channelsync 支持组合多个 Reconciler"
```

---

### Task 5: `weixin_ilink` 长轮询 runner

runner 结构：

- `run`：`GET` 游标（generation 不符 → 空游标）→ 上报 `connected` 并启动心跳 goroutine → 循环 `GetUpdates`。网络 / 5xx / 其它 iLink 错误：上报 `reconnecting`，退避 `BackoffMin` → 60s；`-14`：停心跳、上报 `session-expired`（带 generation，失败重试）、挂起。
- `handleBatch`：只取 `message_type=1`、无 `group_id` 的消息 →（1）逐条上报名册（409 → 挂起）→（2）游标变化时 `PUT`（409 → 挂起；其它失败共 4 次后继续）→（3）逐条 `dispatch`。
- `dispatch`（轮询 goroutine 内同步执行）：幂等键 `ilink:{channel_id}:{message_id}` → 从 `Registry.Get` 读 `AllowedPeers` → 无权限回复（每 peer 10 分钟一次）→ 非文本回复 → 入 peer 队列（满则回复"消息太多"）。
- `peerWorker`：每 peer 一个 goroutine，串行处理，空闲 `PeerIdleTimeout` 退出；`enqueue` 持锁发送、worker 持锁确认队列为空后才删除自己，不会丢消息。
- `process`：对齐 `HandleWecomMsgCallback`（pending `/switch` → slash → `prepareAutoRoute` → `resolveMaybeRebind` / `Sessions.Resolve` → `TurnsStream`），`consumeIlinkTurnStream` 复用 `scanSSEEvents` + `ProgressState`，到点发一次"处理中…"。

**Files:**
- Create: `gateway/internal/adapter/weixin_ilink.go`
- Test: `gateway/internal/adapter/weixin_ilink_fixture_test.go`、`gateway/internal/adapter/weixin_ilink_test.go`（fixture 复用 `wecom_manager_test.go` 的 `fakeStatusReporter` 与 `wecom_bot_test.go` 的 `writeRuntimeSSEOK` / `writeRuntimeSSEHITL`）

- [ ] **Step 1: 写测试 fixture**

`gateway/internal/adapter/weixin_ilink_fixture_test.go`（完整内容）：

```go
package adapter_test

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/sixath/gateway/internal/adapter"
	"github.com/sixath/gateway/internal/channel"
	"github.com/sixath/gateway/internal/idempotency"
	"github.com/sixath/gateway/internal/pendingswitch"
	"github.com/sixath/gateway/internal/runtimeclient"
	"github.com/sixath/gateway/internal/session"
)

// ilinkEventLog records iLink and Portal calls in one order so tests can assert sequencing.
type ilinkEventLog struct {
	mu     sync.Mutex
	events []string
}

func (l *ilinkEventLog) add(e string) {
	l.mu.Lock()
	l.events = append(l.events, e)
	l.mu.Unlock()
}

func (l *ilinkEventLog) index(prefix string) int {
	l.mu.Lock()
	defer l.mu.Unlock()
	for i, e := range l.events {
		if strings.HasPrefix(e, prefix) {
			return i
		}
	}
	return -1
}

type ilinkSent struct {
	to, contextToken, text string
}

type fakeIlinkServer struct {
	srv     *httptest.Server
	log     *ilinkEventLog
	mu      sync.Mutex
	batches []string
	polls   []string
	sent    []ilinkSent
}

func newFakeIlink(t *testing.T, log *ilinkEventLog) *fakeIlinkServer {
	t.Helper()
	f := &fakeIlinkServer{log: log}
	f.srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		raw, _ := io.ReadAll(r.Body)
		switch r.URL.Path {
		case "/ilink/bot/getupdates":
			var body struct {
				Buf string `json:"get_updates_buf"`
			}
			_ = json.Unmarshal(raw, &body)
			f.mu.Lock()
			f.polls = append(f.polls, body.Buf)
			var next string
			if len(f.batches) > 0 {
				next, f.batches = f.batches[0], f.batches[1:]
			}
			f.mu.Unlock()
			switch next {
			case "":
				select {
				case <-time.After(10 * time.Millisecond):
				case <-r.Context().Done():
				}
				_, _ = fmt.Fprintf(w, `{"ret":0,"msgs":[],"get_updates_buf":%q}`, body.Buf)
			case "HTTP500":
				http.Error(w, "boom", http.StatusInternalServerError)
			default:
				_, _ = w.Write([]byte(next))
			}
		case "/ilink/bot/sendmessage":
			var body struct {
				Msg struct {
					To           string `json:"to_user_id"`
					ContextToken string `json:"context_token"`
					ItemList     []struct {
						TextItem struct {
							Text string `json:"text"`
						} `json:"text_item"`
					} `json:"item_list"`
				} `json:"msg"`
			}
			_ = json.Unmarshal(raw, &body)
			text := ""
			if len(body.Msg.ItemList) > 0 {
				text = body.Msg.ItemList[0].TextItem.Text
			}
			f.mu.Lock()
			f.sent = append(f.sent, ilinkSent{to: body.Msg.To, contextToken: body.Msg.ContextToken, text: text})
			n := len(f.sent)
			f.mu.Unlock()
			log.add("send:" + text)
			_, _ = fmt.Fprintf(w, `{"message_id":%d}`, 9000+n)
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(f.srv.Close)
	return f
}

func (f *fakeIlinkServer) push(raw string) {
	f.mu.Lock()
	f.batches = append(f.batches, raw)
	f.mu.Unlock()
}

func (f *fakeIlinkServer) pushMsgs(buf string, msgs ...string) {
	f.push(fmt.Sprintf(`{"ret":0,"msgs":[%s],"get_updates_buf":%q}`, strings.Join(msgs, ","), buf))
}

func (f *fakeIlinkServer) pollCount() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return len(f.polls)
}

func (f *fakeIlinkServer) firstPoll() string {
	f.mu.Lock()
	defer f.mu.Unlock()
	if len(f.polls) == 0 {
		return "<none>"
	}
	return f.polls[0]
}

func (f *fakeIlinkServer) sentSnapshot() []ilinkSent {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]ilinkSent(nil), f.sent...)
}

func (f *fakeIlinkServer) waitSent(t *testing.T, n int) []ilinkSent {
	t.Helper()
	waitFor(t, 3*time.Second, func() bool { return len(f.sentSnapshot()) >= n }, fmt.Sprintf("%d sendmessage calls", n))
	return f.sentSnapshot()
}

func sentTexts(sent []ilinkSent) []string {
	out := make([]string, len(sent))
	for i, s := range sent {
		out[i] = s.text
	}
	return out
}

func ilinkTextMsg(id int64, from, text string) string {
	return fmt.Sprintf(`{"message_id":%d,"from_user_id":%q,"to_user_id":"bot@im.bot","message_type":1,"context_token":"ctx-%d","create_time_ms":1760000000000,"item_list":[{"type":1,"text_item":{"text":%q}}]}`, id, from, id, text)
}

func ilinkImageMsg(id int64, from string) string {
	return fmt.Sprintf(`{"message_id":%d,"from_user_id":%q,"message_type":1,"context_token":"ctx-%d","item_list":[{"type":2}]}`, id, from, id)
}

func ilinkGroupMsg(id int64, from string) string {
	return fmt.Sprintf(`{"message_id":%d,"from_user_id":%q,"group_id":"g-1","message_type":1,"context_token":"ctx-%d","item_list":[{"type":1,"text_item":{"text":"群消息"}}]}`, id, from, id)
}

type fakeIlinkPortal struct {
	srv *httptest.Server
	log *ilinkEventLog

	mu              sync.Mutex
	cursorGen       int
	cursorBuf       string
	peerStatus      int
	cursorPutStatus int
	peers           []map[string]any
	cursorPuts      []map[string]any
	expired         []map[string]any
	turnHandler     func(w http.ResponseWriter, r *http.Request, body map[string]any)
	turns           int32
}

func newFakeIlinkPortal(t *testing.T, log *ilinkEventLog) *fakeIlinkPortal {
	t.Helper()
	p := &fakeIlinkPortal{log: log, cursorGen: 2}
	p.srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		raw, _ := io.ReadAll(r.Body)
		body := map[string]any{}
		_ = json.Unmarshal(raw, &body)
		const base = "/runtime/v1/gateway/channels/wx-1"
		p.mu.Lock()
		peerStatus, cursorPutStatus, turnHandler := p.peerStatus, p.cursorPutStatus, p.turnHandler
		p.mu.Unlock()
		switch {
		case r.URL.Path == base+"/ilink/peers":
			p.mu.Lock()
			p.peers = append(p.peers, body)
			p.mu.Unlock()
			log.add(fmt.Sprintf("peer:%v", body["peer_id"]))
			if peerStatus != 0 {
				w.WriteHeader(peerStatus)
				_, _ = w.Write([]byte(`{"reason":"ILINK_GENERATION_MISMATCH"}`))
				return
			}
			_, _ = w.Write([]byte(`{"allowed":false}`))
		case r.Method == http.MethodGet && r.URL.Path == base+"/ilink/cursor":
			p.mu.Lock()
			out := map[string]any{"generation": p.cursorGen, "get_updates_buf": p.cursorBuf}
			p.mu.Unlock()
			_ = json.NewEncoder(w).Encode(out)
		case r.Method == http.MethodPut && r.URL.Path == base+"/ilink/cursor":
			p.mu.Lock()
			p.cursorPuts = append(p.cursorPuts, body)
			p.mu.Unlock()
			log.add(fmt.Sprintf("cursor:%v", body["get_updates_buf"]))
			if cursorPutStatus != 0 {
				w.WriteHeader(cursorPutStatus)
				_, _ = w.Write([]byte(`{"reason":"ILINK_GENERATION_MISMATCH"}`))
				return
			}
			_, _ = w.Write([]byte(`{"ok":true}`))
		case r.URL.Path == base+"/ilink/session-expired":
			p.mu.Lock()
			p.expired = append(p.expired, body)
			p.mu.Unlock()
			log.add("expired")
			_, _ = w.Write([]byte(`{"ok":true}`))
		case r.URL.Path == base+"/status":
			_, _ = w.Write([]byte(`{"ok":true}`))
		case r.Method == http.MethodGet && r.URL.Path == "/runtime/v1/channels/wx-1/agents":
			_ = json.NewEncoder(w).Encode(map[string]any{
				"default_agent":      "agent-1",
				"auto_route_enabled": false,
				"agents": []map[string]string{
					{"id": "agent-1", "name": "Default"},
					{"id": "agent-2", "name": "Ops"},
				},
			})
		case r.URL.Path == "/runtime/v1/sessions/resolve":
			_ = json.NewEncoder(w).Encode(map[string]any{
				"session_id": "sess-1", "agent_id": "agent-1", "user_id": "u1", "created": true,
			})
		case r.URL.Path == "/runtime/v1/turns":
			atomic.AddInt32(&p.turns, 1)
			log.add(fmt.Sprintf("turn:%v", body["peer_id"]))
			if turnHandler != nil {
				turnHandler(w, r, body)
				return
			}
			writeRuntimeSSEOK(w, "ok")
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(p.srv.Close)
	return p
}

func (p *fakeIlinkPortal) set(fn func(p *fakeIlinkPortal)) {
	p.mu.Lock()
	fn(p)
	p.mu.Unlock()
}

func (p *fakeIlinkPortal) counts() (peers, cursorPuts, expired int) {
	p.mu.Lock()
	defer p.mu.Unlock()
	return len(p.peers), len(p.cursorPuts), len(p.expired)
}

func (p *fakeIlinkPortal) turnCount() int { return int(atomic.LoadInt32(&p.turns)) }

type ilinkFixture struct {
	log      *ilinkEventLog
	ilink    *fakeIlinkServer
	portal   *fakeIlinkPortal
	reg      *channel.Registry
	ch       channel.Channel
	deps     adapter.WeixinIlinkDeps
	reporter *fakeStatusReporter
}

func newIlinkFixture(t *testing.T, allowed ...string) *ilinkFixture {
	t.Helper()
	log := &ilinkEventLog{}
	il := newFakeIlink(t, log)
	portal := newFakeIlinkPortal(t, log)
	ch := channel.Channel{
		ID: "wx-1", Type: "weixin_ilink", Enabled: true,
		BotToken: "bt-secret", BaseURL: il.srv.URL, Generation: 2, AllowedPeers: allowed,
	}
	reg := channel.NewRegistry()
	reg.ReplaceAll([]channel.Channel{ch})
	rt := runtimeclient.New(portal.srv.URL, "dev-runtime-token")
	reporter := &fakeStatusReporter{}
	return &ilinkFixture{
		log: log, ilink: il, portal: portal, reg: reg, ch: ch, reporter: reporter,
		deps: adapter.WeixinIlinkDeps{
			Registry:      reg,
			Runtime:       rt,
			Sessions:      session.NewRouter(rt, 30*time.Second),
			Idempotency:   idempotency.NewStore(10 * time.Minute),
			PendingSwitch: pendingswitch.New(),
			TurnTimeout:   5 * time.Second,
			Reporter:      reporter,
			PollTimeout:   2 * time.Second,
			BackoffMin:    20 * time.Millisecond,
		},
	}
}

// run starts RunWeixinIlink; cleanup cancels it and waits before the fake servers close.
func (f *ilinkFixture) run(t *testing.T) {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		adapter.RunWeixinIlink(ctx, f.ch, f.deps)
		close(done)
	}()
	t.Cleanup(func() {
		cancel()
		select {
		case <-done:
		case <-time.After(5 * time.Second):
			t.Error("runner did not exit after cancel")
		}
	})
}

func waitFor(t *testing.T, timeout time.Duration, cond func() bool, what string) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for !cond() {
		if time.Now().After(deadline) {
			t.Fatalf("timed out waiting for %s", what)
		}
		time.Sleep(5 * time.Millisecond)
	}
}
```

- [ ] **Step 2: 写失败测试**

`gateway/internal/adapter/weixin_ilink_test.go`（完整内容）：

```go
package adapter_test

import (
	"io"
	"net/http"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"
	"unicode/utf8"

	"github.com/sixath/gateway/internal/channel"
)

const (
	ilinkAlice = "wxid_alice1234@im.wechat"
	ilinkBob   = "wxid_bob5678@im.wechat"
)

func TestWeixinIlink_PeerReportThenCursorThenTurn(t *testing.T) {
	f := newIlinkFixture(t, ilinkAlice)
	f.portal.set(func(p *fakeIlinkPortal) { p.cursorBuf = "buf-0" })
	f.ilink.pushMsgs("buf-1", ilinkTextMsg(101, ilinkAlice, "你好"))
	f.run(t)

	sent := f.ilink.waitSent(t, 1)
	if sent[0].text != "ok" || sent[0].to != ilinkAlice || sent[0].contextToken != "ctx-101" {
		t.Fatalf("sent=%+v", sent[0])
	}
	if got := f.ilink.firstPoll(); got != "buf-0" {
		t.Fatalf("first poll buf=%q want buf-0", got)
	}
	peer, cursor, turn, send := f.log.index("peer:"), f.log.index("cursor:buf-1"), f.log.index("turn:"), f.log.index("send:ok")
	if peer < 0 || cursor < 0 || turn < 0 || send < 0 || !(peer < cursor && cursor < turn && turn < send) {
		t.Fatalf("order peer=%d cursor=%d turn=%d send=%d", peer, cursor, turn, send)
	}
	f.portal.mu.Lock()
	defer f.portal.mu.Unlock()
	p := f.portal.peers[0]
	if p["generation"] != float64(2) || p["peer_id"] != ilinkAlice || p["context_token"] != "ctx-101" || p["preview"] != "你好" || p["at"] != "2025-10-09T08:53:20Z" {
		t.Fatalf("peer report=%v", p)
	}
	c := f.portal.cursorPuts[0]
	if c["generation"] != float64(2) || c["get_updates_buf"] != "buf-1" {
		t.Fatalf("cursor put=%v", c)
	}
}

func TestWeixinIlink_StaleCursorGenerationStartsEmpty(t *testing.T) {
	f := newIlinkFixture(t, ilinkAlice)
	f.portal.set(func(p *fakeIlinkPortal) { p.cursorGen, p.cursorBuf = 1, "old-bot-buf" })
	f.run(t)

	waitFor(t, 2*time.Second, func() bool { return f.ilink.pollCount() > 0 }, "first poll")
	if got := f.ilink.firstPoll(); got != "" {
		t.Fatalf("first poll buf=%q want empty", got)
	}
}

func TestWeixinIlink_DeniedRateLimitedThenAllowedViaRegistry(t *testing.T) {
	f := newIlinkFixture(t)
	f.ilink.pushMsgs("b1", ilinkTextMsg(1, ilinkBob, "hi"), ilinkTextMsg(2, ilinkBob, "hi again"))
	f.run(t)

	f.ilink.waitSent(t, 1)
	waitFor(t, 2*time.Second, func() bool { peers, _, _ := f.portal.counts(); return peers == 2 }, "2 peer reports")
	time.Sleep(50 * time.Millisecond)
	sent := f.ilink.sentSnapshot()
	if len(sent) != 1 || sent[0].text != "无权限，请联系管理员开通（ID 尾号 5678）" {
		t.Fatalf("denied replies=%q", sentTexts(sent))
	}
	if f.portal.turnCount() != 0 {
		t.Fatalf("turns=%d want 0", f.portal.turnCount())
	}

	allowed := f.ch
	allowed.AllowedPeers = []string{ilinkBob}
	f.reg.ReplaceAll([]channel.Channel{allowed})
	f.ilink.pushMsgs("b2", ilinkTextMsg(3, ilinkBob, "现在呢"))

	sent = f.ilink.waitSent(t, 2)
	if sent[1].text != "ok" || f.portal.turnCount() != 1 {
		t.Fatalf("after allow: sent=%q turns=%d", sentTexts(sent), f.portal.turnCount())
	}
}

func TestWeixinIlink_NonTextRepliesAndGroupIgnored(t *testing.T) {
	f := newIlinkFixture(t, ilinkAlice)
	f.ilink.pushMsgs("b1", ilinkGroupMsg(1, ilinkAlice), ilinkImageMsg(2, ilinkAlice))
	f.run(t)

	sent := f.ilink.waitSent(t, 1)
	time.Sleep(50 * time.Millisecond)
	sent = f.ilink.sentSnapshot()
	if len(sent) != 1 || sent[0].text != "暂只支持文字消息" || sent[0].contextToken != "ctx-2" {
		t.Fatalf("sent=%+v", sent)
	}
	if peers, _, _ := f.portal.counts(); peers != 1 {
		t.Fatalf("peer reports=%d want 1 (group message skipped)", peers)
	}
	if f.portal.turnCount() != 0 {
		t.Fatalf("turns=%d want 0", f.portal.turnCount())
	}
}

func TestWeixinIlink_DuplicateMessageIDProcessedOnce(t *testing.T) {
	f := newIlinkFixture(t, ilinkAlice)
	f.ilink.pushMsgs("b1", ilinkTextMsg(7, ilinkAlice, "hi"))
	f.ilink.pushMsgs("b2", ilinkTextMsg(7, ilinkAlice, "hi"))
	f.run(t)

	waitFor(t, 2*time.Second, func() bool { _, puts, _ := f.portal.counts(); return puts == 2 }, "both batches")
	f.ilink.waitSent(t, 1)
	time.Sleep(50 * time.Millisecond)
	if n := len(f.ilink.sentSnapshot()); n != 1 || f.portal.turnCount() != 1 {
		t.Fatalf("sent=%d turns=%d want 1/1", n, f.portal.turnCount())
	}
}

func TestWeixinIlink_SlashCommandNoTurn(t *testing.T) {
	f := newIlinkFixture(t, ilinkAlice)
	f.ilink.pushMsgs("b1", ilinkTextMsg(1, ilinkAlice, "/agents"))
	f.run(t)

	sent := f.ilink.waitSent(t, 1)
	if !strings.Contains(sent[0].text, "可用 Agent") || !strings.Contains(sent[0].text, "Ops") {
		t.Fatalf("reply=%q", sent[0].text)
	}
	if f.portal.turnCount() != 0 {
		t.Fatalf("turns=%d want 0", f.portal.turnCount())
	}
}

func TestWeixinIlink_HITLRepliesWebOnly(t *testing.T) {
	f := newIlinkFixture(t, ilinkAlice)
	f.portal.set(func(p *fakeIlinkPortal) {
		p.turnHandler = func(w http.ResponseWriter, _ *http.Request, _ map[string]any) { writeRuntimeSSEHITL(w) }
	})
	f.ilink.pushMsgs("b1", ilinkTextMsg(1, ilinkAlice, "删库"))
	f.run(t)

	sent := f.ilink.waitSent(t, 1)
	if sent[0].text != "该操作需要在 Web 端确认" {
		t.Fatalf("reply=%q", sent[0].text)
	}
}

func TestWeixinIlink_TurnErrorRepliesFailure(t *testing.T) {
	f := newIlinkFixture(t, ilinkAlice)
	f.portal.set(func(p *fakeIlinkPortal) {
		p.turnHandler = func(w http.ResponseWriter, _ *http.Request, _ map[string]any) {
			http.Error(w, "boom", http.StatusInternalServerError)
		}
	})
	f.ilink.pushMsgs("b1", ilinkTextMsg(1, ilinkAlice, "hi"))
	f.run(t)

	sent := f.ilink.waitSent(t, 1)
	if sent[0].text != "处理失败：操作失败，请稍后重试" {
		t.Fatalf("reply=%q", sent[0].text)
	}
}

func TestWeixinIlink_ProcessingNotice(t *testing.T) {
	t.Run("slow turn sends one notice", func(t *testing.T) {
		f := newIlinkFixture(t, ilinkAlice)
		f.deps.ProcessingNoticeAfter = 50 * time.Millisecond
		f.portal.set(func(p *fakeIlinkPortal) {
			p.turnHandler = func(w http.ResponseWriter, r *http.Request, _ map[string]any) {
				w.Header().Set("Content-Type", "text/event-stream")
				w.WriteHeader(http.StatusOK)
				w.(http.Flusher).Flush()
				select {
				case <-time.After(250 * time.Millisecond):
				case <-r.Context().Done():
					return
				}
				_, _ = io.WriteString(w, "event: chunk\ndata: {\"content\":\"ok\"}\n\nevent: done\ndata: {}\n\n")
			}
		})
		f.ilink.pushMsgs("b1", ilinkTextMsg(1, ilinkAlice, "慢问题"))
		f.run(t)

		sent := f.ilink.waitSent(t, 2)
		time.Sleep(50 * time.Millisecond)
		if got := sentTexts(f.ilink.sentSnapshot()); !slices.Equal(got, []string{"处理中…", "ok"}) {
			t.Fatalf("sent=%q", got)
		}
		if sent[0].contextToken != "ctx-1" {
			t.Fatalf("notice context_token=%q", sent[0].contextToken)
		}
	})
	t.Run("fast turn sends no notice", func(t *testing.T) {
		f := newIlinkFixture(t, ilinkAlice)
		f.deps.ProcessingNoticeAfter = time.Second
		f.ilink.pushMsgs("b1", ilinkTextMsg(1, ilinkAlice, "快问题"))
		f.run(t)

		f.ilink.waitSent(t, 1)
		time.Sleep(50 * time.Millisecond)
		if got := sentTexts(f.ilink.sentSnapshot()); !slices.Equal(got, []string{"ok"}) {
			t.Fatalf("sent=%q", got)
		}
	})
}

func TestWeixinIlink_LongReplyShapedAndSplit(t *testing.T) {
	f := newIlinkFixture(t, ilinkAlice)
	f.deps.SplitRunes = 10
	f.portal.set(func(p *fakeIlinkPortal) {
		p.turnHandler = func(w http.ResponseWriter, _ *http.Request, _ map[string]any) {
			writeRuntimeSSEOK(w, "# 标题\n**第一段内容**\n第二段内容很长很长很长很长")
		}
	})
	f.ilink.pushMsgs("b1", ilinkTextMsg(1, ilinkAlice, "长回答"))
	f.run(t)

	f.ilink.waitSent(t, 3)
	time.Sleep(50 * time.Millisecond)
	sent := f.ilink.sentSnapshot()
	var joined strings.Builder
	for _, s := range sent {
		if utf8.RuneCountInString(s.text) > 10 || s.contextToken != "ctx-1" {
			t.Fatalf("bad part %+v", s)
		}
		joined.WriteString(strings.ReplaceAll(s.text, "\n", ""))
	}
	if joined.String() != "标题第一段内容第二段内容很长很长很长很长" {
		t.Fatalf("parts=%q", sentTexts(sent))
	}
}

func TestWeixinIlink_SamePeerSerialCrossPeerConcurrent(t *testing.T) {
	f := newIlinkFixture(t, ilinkAlice, ilinkBob)
	release := make(chan struct{})
	concurrent := make(chan struct{}, 1)
	var mu sync.Mutex
	active, maxActive, total := map[string]int{}, map[string]int{}, 0
	f.portal.set(func(p *fakeIlinkPortal) {
		p.turnHandler = func(w http.ResponseWriter, r *http.Request, body map[string]any) {
			peer, _ := body["peer_id"].(string)
			mu.Lock()
			active[peer]++
			maxActive[peer] = max(maxActive[peer], active[peer])
			total++
			if total >= 2 {
				select {
				case concurrent <- struct{}{}:
				default:
				}
			}
			mu.Unlock()
			select {
			case <-release:
			case <-r.Context().Done():
			}
			mu.Lock()
			active[peer]--
			total--
			mu.Unlock()
			writeRuntimeSSEOK(w, "ok")
		}
	})
	f.ilink.pushMsgs("b1", ilinkTextMsg(1, ilinkAlice, "a1"), ilinkTextMsg(2, ilinkAlice, "a2"), ilinkTextMsg(3, ilinkBob, "b1"))
	f.run(t)

	select {
	case <-concurrent:
	case <-time.After(3 * time.Second):
		t.Fatal("alice and bob turns never ran concurrently")
	}
	close(release)
	f.ilink.waitSent(t, 3)
	mu.Lock()
	defer mu.Unlock()
	if maxActive["user:"+ilinkAlice] != 1 {
		t.Fatalf("alice max concurrent turns=%d want 1", maxActive["user:"+ilinkAlice])
	}
}

func TestWeixinIlink_PeerQueueOverflow(t *testing.T) {
	f := newIlinkFixture(t, ilinkAlice)
	f.deps.PeerQueueSize = 1
	release := make(chan struct{})
	started := make(chan struct{}, 4)
	f.portal.set(func(p *fakeIlinkPortal) {
		p.turnHandler = func(w http.ResponseWriter, r *http.Request, _ map[string]any) {
			started <- struct{}{}
			select {
			case <-release:
			case <-r.Context().Done():
			}
			writeRuntimeSSEOK(w, "ok")
		}
	})
	f.ilink.pushMsgs("b1", ilinkTextMsg(1, ilinkAlice, "first"))
	f.run(t)
	select {
	case <-started:
	case <-time.After(3 * time.Second):
		t.Fatal("first turn never started")
	}

	f.ilink.pushMsgs("b2", ilinkTextMsg(2, ilinkAlice, "queued"), ilinkTextMsg(3, ilinkAlice, "overflow"))
	sent := f.ilink.waitSent(t, 1)
	if sent[0].text != "消息太多，请稍后再试" || sent[0].contextToken != "ctx-3" {
		t.Fatalf("overflow reply=%+v", sent[0])
	}
	close(release)
	f.ilink.waitSent(t, 3)
	time.Sleep(50 * time.Millisecond)
	if f.portal.turnCount() != 2 {
		t.Fatalf("turns=%d want 2", f.portal.turnCount())
	}
}

func TestWeixinIlink_CursorConflictSuspends(t *testing.T) {
	f := newIlinkFixture(t, ilinkAlice)
	f.portal.set(func(p *fakeIlinkPortal) { p.cursorPutStatus = http.StatusConflict })
	f.ilink.pushMsgs("b1", ilinkTextMsg(1, ilinkAlice, "hi"))
	f.run(t)

	waitFor(t, 2*time.Second, func() bool { _, puts, _ := f.portal.counts(); return puts == 1 }, "cursor put")
	time.Sleep(150 * time.Millisecond)
	if f.ilink.pollCount() != 1 || f.portal.turnCount() != 0 || len(f.ilink.sentSnapshot()) != 0 {
		t.Fatalf("polls=%d turns=%d sent=%d want 1/0/0", f.ilink.pollCount(), f.portal.turnCount(), len(f.ilink.sentSnapshot()))
	}
}

func TestWeixinIlink_PeerReportConflictSuspends(t *testing.T) {
	f := newIlinkFixture(t, ilinkAlice)
	f.portal.set(func(p *fakeIlinkPortal) { p.peerStatus = http.StatusConflict })
	f.ilink.pushMsgs("b1", ilinkTextMsg(1, ilinkAlice, "hi"))
	f.run(t)

	waitFor(t, 2*time.Second, func() bool { peers, _, _ := f.portal.counts(); return peers == 1 }, "peer report")
	time.Sleep(150 * time.Millisecond)
	_, puts, _ := f.portal.counts()
	if f.ilink.pollCount() != 1 || puts != 0 || f.portal.turnCount() != 0 {
		t.Fatalf("polls=%d cursorPuts=%d turns=%d want 1/0/0", f.ilink.pollCount(), puts, f.portal.turnCount())
	}
}

func TestWeixinIlink_CursorPutFailureRetriedThenContinues(t *testing.T) {
	f := newIlinkFixture(t, ilinkAlice)
	f.portal.set(func(p *fakeIlinkPortal) { p.cursorPutStatus = http.StatusBadGateway })
	f.ilink.pushMsgs("b1", ilinkTextMsg(1, ilinkAlice, "hi"))
	f.run(t)

	sent := f.ilink.waitSent(t, 1)
	if sent[0].text != "ok" {
		t.Fatalf("reply=%q", sent[0].text)
	}
	if _, puts, _ := f.portal.counts(); puts != 4 {
		t.Fatalf("cursor put attempts=%d want 4", puts)
	}
}

func TestWeixinIlink_SessionExpiredReportsAndSuspends(t *testing.T) {
	f := newIlinkFixture(t, ilinkAlice)
	f.ilink.push(`{"errcode":-14,"errmsg":"session timeout"}`)
	f.run(t)

	waitFor(t, 2*time.Second, func() bool { _, _, exp := f.portal.counts(); return exp == 1 }, "session-expired report")
	time.Sleep(150 * time.Millisecond)
	if f.ilink.pollCount() != 1 {
		t.Fatalf("polls=%d want 1 (suspended)", f.ilink.pollCount())
	}
	f.portal.mu.Lock()
	defer f.portal.mu.Unlock()
	if e := f.portal.expired[0]; e["generation"] != float64(2) || e["reason"] != "-14" {
		t.Fatalf("session-expired body=%v", e)
	}
}

func TestWeixinIlink_HeartbeatAndReconnecting(t *testing.T) {
	f := newIlinkFixture(t, ilinkAlice)
	f.deps.HeartbeatInterval = 30 * time.Millisecond
	f.ilink.push("HTTP500")
	f.run(t)

	waitFor(t, 2*time.Second, func() bool {
		states := f.reporter.statesFor("wx-1")
		i := slices.Index(states, "reconnecting")
		if i < 0 {
			return false
		}
		connectedAfter := 0
		for _, s := range states[i+1:] {
			if s == "connected" {
				connectedAfter++
			}
		}
		return connectedAfter >= 3
	}, "reconnecting followed by connected heartbeats")
}
```

- [ ] **Step 3: 运行确认失败**

```powershell
go test ./internal/adapter/ -run WeixinIlink -count=1
```

Expected: FAIL，编译错误 `undefined: adapter.WeixinIlinkDeps` / `undefined: adapter.RunWeixinIlink`。

- [ ] **Step 4: 实现**

`gateway/internal/adapter/weixin_ilink.go`（完整内容）：

```go
package adapter

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/sixath/gateway/internal/channel"
	"github.com/sixath/gateway/internal/command"
	"github.com/sixath/gateway/internal/idempotency"
	"github.com/sixath/gateway/internal/ilink"
	"github.com/sixath/gateway/internal/pendingswitch"
	"github.com/sixath/gateway/internal/runtimeclient"
	"github.com/sixath/gateway/internal/session"
)

// WeixinIlinkDeps wires weixin_ilink long-poll runners.
type WeixinIlinkDeps struct {
	Registry      *channel.Registry
	Runtime       *runtimeclient.Client
	Sessions      *session.Router
	Idempotency   idempotency.Store
	PendingSwitch *pendingswitch.Store
	TurnTimeout   time.Duration
	// Reporter posts runtime status; when nil, Runtime is used if non-nil.
	Reporter StatusReporter
	// SplitRunes caps each outgoing message; zero means ilink.DefaultSplitRunes.
	SplitRunes int

	// Zero values below mean production defaults; tests shrink them.
	PollTimeout           time.Duration // getupdates client timeout (40s)
	HeartbeatInterval     time.Duration // status heartbeat, independent of polling (30s)
	ProcessingNoticeAfter time.Duration // send 「处理中…」 once when no final reply yet (15s)
	DeniedReplyInterval   time.Duration // 「无权限」 at most once per peer per interval (10m)
	PeerQueueSize         int           // pending messages per peer beyond the one in progress (5)
	PeerIdleTimeout       time.Duration // idle peer worker exits (5m)
	BackoffMin            time.Duration // first retry delay for iLink / Portal failures (1s), doubling to 60s
}

const (
	ilinkDeniedTemplate    = "无权限，请联系管理员开通（ID 尾号 %s）"
	ilinkNonTextReply      = "暂只支持文字消息"
	ilinkQueueFullReply    = "消息太多，请稍后再试"
	ilinkHITLReply         = "该操作需要在 Web 端确认"
	ilinkFailurePrefix     = "处理失败："
	ilinkProcessingReply   = "处理中…"
	ilinkBackoffMax        = 60 * time.Second
	ilinkCursorPutAttempts = 4
	ilinkCursorRetryDelay  = 200 * time.Millisecond
)

func normalizeWeixinIlinkDeps(deps WeixinIlinkDeps) WeixinIlinkDeps {
	if deps.TurnTimeout <= 0 {
		deps.TurnTimeout = 120 * time.Second
	}
	if deps.Idempotency == nil {
		deps.Idempotency = idempotency.NewStore(0)
	}
	if deps.SplitRunes <= 0 {
		deps.SplitRunes = ilink.DefaultSplitRunes
	}
	if deps.PollTimeout <= 0 {
		deps.PollTimeout = ilink.DefaultPollTimeout
	}
	if deps.HeartbeatInterval <= 0 {
		deps.HeartbeatInterval = 30 * time.Second
	}
	if deps.ProcessingNoticeAfter <= 0 {
		deps.ProcessingNoticeAfter = 15 * time.Second
	}
	if deps.DeniedReplyInterval <= 0 {
		deps.DeniedReplyInterval = 10 * time.Minute
	}
	if deps.PeerQueueSize <= 0 {
		deps.PeerQueueSize = 5
	}
	if deps.PeerIdleTimeout <= 0 {
		deps.PeerIdleTimeout = 5 * time.Minute
	}
	if deps.BackoffMin <= 0 {
		deps.BackoffMin = time.Second
	}
	return deps
}

func reportIlinkStatus(deps WeixinIlinkDeps, channelID string, body runtimeclient.StatusBody) {
	var rep StatusReporter
	if deps.Reporter != nil {
		rep = deps.Reporter
	} else if deps.Runtime != nil {
		rep = deps.Runtime
	}
	if rep == nil || channelID == "" || body.State == "" {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := rep.ReportChannelStatus(ctx, channelID, body); err != nil {
		log.Printf("weixin_ilink %s: report status %s: %v", channelID, body.State, err)
	}
}

type ilinkQueuedMsg struct {
	in   ilink.Inbound
	key  string
	corr string
}

type ilinkRunner struct {
	ch     channel.Channel
	deps   WeixinIlinkDeps
	client *ilink.Client

	stateMu sync.Mutex
	state   string

	mu       sync.Mutex
	queues   map[string]chan ilinkQueuedMsg
	deniedAt map[string]time.Time
	// workers tracks the heartbeat and peer workers; Add is only called from the poll goroutine.
	workers sync.WaitGroup
}

// RunWeixinIlink runs one channel's long-poll loop until ctx is canceled (exported for tests).
// On -14 or a 409 generation conflict it suspends (blocks without calling iLink) until ctx is canceled.
func RunWeixinIlink(ctx context.Context, ch channel.Channel, deps WeixinIlinkDeps) {
	deps = normalizeWeixinIlinkDeps(deps)
	client := ilink.NewClient(ch.BaseURL, ch.BotToken)
	client.PollTimeout = deps.PollTimeout
	r := &ilinkRunner{
		ch:       ch,
		deps:     deps,
		client:   client,
		queues:   make(map[string]chan ilinkQueuedMsg),
		deniedAt: make(map[string]time.Time),
	}
	r.run(ctx)
}

func (r *ilinkRunner) run(ctx context.Context) {
	defer r.workers.Wait()
	buf, ok := r.loadCursor(ctx)
	if !ok {
		return
	}
	hbCtx, stopHeartbeat := context.WithCancel(ctx)
	defer stopHeartbeat()
	r.setState("connected", "")
	r.workers.Add(1)
	go r.heartbeatLoop(hbCtx)

	backoff := r.deps.BackoffMin
	for ctx.Err() == nil {
		upd, err := r.client.GetUpdates(ctx, buf)
		if err != nil {
			if ctx.Err() != nil {
				return
			}
			if ilink.IsSessionExpired(err) {
				stopHeartbeat()
				log.Printf("weixin_ilink %s: session expired (-14); suspended until re-login", r.ch.ID)
				r.reportSessionExpired(ctx)
				<-ctx.Done()
				return
			}
			r.setState("reconnecting", err.Error())
			log.Printf("weixin_ilink %s: getupdates: %v; retry in %s", r.ch.ID, err, backoff)
			if !sleepIlink(ctx, backoff) {
				return
			}
			backoff = nextIlinkBackoff(backoff)
			continue
		}
		backoff = r.deps.BackoffMin
		r.setState("connected", "")
		if !r.handleBatch(ctx, buf, upd) {
			stopHeartbeat()
			log.Printf("weixin_ilink %s: generation %d is stale; suspended until restart", r.ch.ID, r.ch.Generation)
			<-ctx.Done()
			return
		}
		buf = upd.GetUpdatesBuf
	}
}

func (r *ilinkRunner) loadCursor(ctx context.Context) (string, bool) {
	backoff := r.deps.BackoffMin
	for {
		cur, err := r.deps.Runtime.GetIlinkCursor(ctx, r.ch.ID)
		if err == nil {
			if cur.Generation != r.ch.Generation {
				return "", true
			}
			return cur.GetUpdatesBuf, true
		}
		if ctx.Err() != nil {
			return "", false
		}
		r.setState("reconnecting", err.Error())
		log.Printf("weixin_ilink %s: get cursor: %v; retry in %s", r.ch.ID, err, backoff)
		if !sleepIlink(ctx, backoff) {
			return "", false
		}
		backoff = nextIlinkBackoff(backoff)
	}
}

func (r *ilinkRunner) setState(state, lastErr string) {
	r.stateMu.Lock()
	changed := r.state != state
	r.state = state
	r.stateMu.Unlock()
	if changed {
		reportIlinkStatus(r.deps, r.ch.ID, runtimeclient.StatusBody{State: state, LastError: &lastErr})
	}
}

func (r *ilinkRunner) heartbeatLoop(ctx context.Context) {
	defer r.workers.Done()
	ticker := time.NewTicker(r.deps.HeartbeatInterval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			r.stateMu.Lock()
			state := r.state
			r.stateMu.Unlock()
			reportIlinkStatus(r.deps, r.ch.ID, runtimeclient.StatusBody{State: state})
		}
	}
}

func (r *ilinkRunner) reportSessionExpired(ctx context.Context) {
	backoff := r.deps.BackoffMin
	for {
		err := r.deps.Runtime.ReportIlinkSessionExpired(ctx, r.ch.ID, r.ch.Generation, "-14")
		if err == nil || ctx.Err() != nil {
			return
		}
		log.Printf("weixin_ilink %s: report session expired: %v; retry in %s", r.ch.ID, err, backoff)
		if !sleepIlink(ctx, backoff) {
			return
		}
		backoff = nextIlinkBackoff(backoff)
	}
}

// handleBatch returns false when Portal rejected this runner's generation (409).
func (r *ilinkRunner) handleBatch(ctx context.Context, prevBuf string, upd *ilink.Updates) bool {
	var msgs []ilink.Inbound
	for _, m := range upd.Msgs {
		if !m.IsUserMessage() {
			continue
		}
		in := ilink.Normalize(m)
		if in.FromUserID == "" {
			continue
		}
		if in.GroupID != "" {
			log.Printf("weixin_ilink %s: ignoring group message %s", r.ch.ID, in.MessageID)
			continue
		}
		msgs = append(msgs, in)
	}
	// Roster first: a denied peer must still appear in Portal so an admin can allow it.
	for _, in := range msgs {
		if !r.reportPeer(ctx, in) {
			return false
		}
	}
	// At-most-once: commit the cursor before any turn runs.
	if upd.GetUpdatesBuf != prevBuf {
		if !r.putCursor(ctx, upd.GetUpdatesBuf) {
			return false
		}
	}
	for _, in := range msgs {
		r.dispatch(ctx, in)
	}
	return true
}

func (r *ilinkRunner) reportPeer(ctx context.Context, in ilink.Inbound) bool {
	at := in.CreatedAt
	if at.IsZero() {
		at = time.Now()
	}
	_, err := r.deps.Runtime.ReportIlinkPeer(ctx, r.ch.ID, runtimeclient.IlinkPeerReport{
		Generation:   r.ch.Generation,
		PeerID:       in.FromUserID,
		ContextToken: in.ContextToken,
		Preview:      in.Preview(),
		At:           at.UTC().Format(time.RFC3339),
	})
	if runtimeclient.IsIlinkGenerationMismatch(err) {
		return false
	}
	if err != nil {
		log.Printf("weixin_ilink %s: report peer …%s: %v", r.ch.ID, ilinkPeerTail(in.FromUserID), err)
	}
	return true
}

func (r *ilinkRunner) putCursor(ctx context.Context, buf string) bool {
	cur := runtimeclient.IlinkCursor{Generation: r.ch.Generation, GetUpdatesBuf: buf}
	for attempt := 1; attempt <= ilinkCursorPutAttempts; attempt++ {
		err := r.deps.Runtime.PutIlinkCursor(ctx, r.ch.ID, cur)
		if err == nil {
			return true
		}
		if runtimeclient.IsIlinkGenerationMismatch(err) {
			return false
		}
		log.Printf("weixin_ilink %s: put cursor (attempt %d/%d): %v", r.ch.ID, attempt, ilinkCursorPutAttempts, err)
		if attempt < ilinkCursorPutAttempts && !sleepIlink(ctx, ilinkCursorRetryDelay) {
			return true
		}
	}
	log.Printf("weixin_ilink %s: put cursor failed; continuing (a process restart may replay this batch)", r.ch.ID)
	return true
}

func (r *ilinkRunner) dispatch(ctx context.Context, in ilink.Inbound) {
	key := ""
	if in.MessageID != "" {
		key = "ilink:" + r.ch.ID + ":" + in.MessageID
	}
	corr := newCorrelationID()
	if _, reused, _ := r.deps.Idempotency.Begin(ctx, key, corr); reused {
		return
	}
	if !r.peerAllowed(in.FromUserID) {
		r.replyDenied(ctx, in)
		_ = r.deps.Idempotency.Complete(ctx, key, "denied")
		return
	}
	if in.NonText {
		r.reply(ctx, in, ilinkNonTextReply)
		_ = r.deps.Idempotency.Complete(ctx, key, "non_text")
		return
	}
	r.enqueue(ctx, ilinkQueuedMsg{in: in, key: key, corr: corr})
}

// peerAllowed reads the Registry on every message so a Portal allowlist change applies without a restart.
func (r *ilinkRunner) peerAllowed(peer string) bool {
	if r.deps.Registry == nil {
		return false
	}
	cur, err := r.deps.Registry.Get(r.ch.ID)
	if err != nil {
		return false
	}
	return slices.Contains(cur.AllowedPeers, peer)
}

func (r *ilinkRunner) replyDenied(ctx context.Context, in ilink.Inbound) {
	now := time.Now()
	r.mu.Lock()
	last, seen := r.deniedAt[in.FromUserID]
	if seen && now.Sub(last) < r.deps.DeniedReplyInterval {
		r.mu.Unlock()
		return
	}
	r.deniedAt[in.FromUserID] = now
	r.mu.Unlock()
	r.reply(ctx, in, fmt.Sprintf(ilinkDeniedTemplate, ilinkPeerTail(in.FromUserID)))
}

func (r *ilinkRunner) reply(ctx context.Context, in ilink.Inbound, text string) {
	for _, part := range ilink.ShapeReply(text, r.deps.SplitRunes) {
		if _, err := r.client.SendText(ctx, in.FromUserID, in.ContextToken, part); err != nil {
			log.Printf("weixin_ilink %s: send to …%s: %v", r.ch.ID, ilinkPeerTail(in.FromUserID), err)
			return
		}
	}
}

func (r *ilinkRunner) enqueue(ctx context.Context, msg ilinkQueuedMsg) {
	peer := msg.in.FromUserID
	r.mu.Lock()
	q, ok := r.queues[peer]
	if !ok {
		q = make(chan ilinkQueuedMsg, r.deps.PeerQueueSize)
		r.queues[peer] = q
		r.workers.Add(1)
		go r.peerWorker(ctx, peer, q)
	}
	select {
	case q <- msg:
		r.mu.Unlock()
	default:
		r.mu.Unlock()
		r.reply(ctx, msg.in, ilinkQueueFullReply)
		_ = r.deps.Idempotency.Complete(ctx, msg.key, "queue_full")
	}
}

// peerWorker serializes one peer's messages; it exits after PeerIdleTimeout without work.
func (r *ilinkRunner) peerWorker(ctx context.Context, peer string, q chan ilinkQueuedMsg) {
	defer r.workers.Done()
	idle := time.NewTimer(r.deps.PeerIdleTimeout)
	defer idle.Stop()
	for {
		select {
		case <-ctx.Done():
			r.mu.Lock()
			if r.queues[peer] == q {
				delete(r.queues, peer)
			}
			r.mu.Unlock()
			return
		case msg := <-q:
			r.process(ctx, msg)
			idle.Reset(r.deps.PeerIdleTimeout)
		case <-idle.C:
			// enqueue sends while holding mu, so an empty queue checked under mu cannot lose a message.
			r.mu.Lock()
			if len(q) > 0 {
				r.mu.Unlock()
				idle.Reset(r.deps.PeerIdleTimeout)
				continue
			}
			if r.queues[peer] == q {
				delete(r.queues, peer)
			}
			r.mu.Unlock()
			return
		}
	}
}

// process mirrors HandleWecomMsgCallback: pending /switch → slash → auto-route → resolve → TurnsStream.
func (r *ilinkRunner) process(ctx context.Context, msg ilinkQueuedMsg) {
	deps := r.deps
	in := msg.in
	chID := r.ch.ID
	peerID := "user:" + in.FromUserID
	noticeAt := time.Now().Add(deps.ProcessingNoticeAfter)
	defer func() { _ = deps.Idempotency.Complete(context.Background(), msg.key, "done") }()
	replyText := func(text string) { r.reply(ctx, in, text) }

	turnCtx, cancel := context.WithTimeout(ctx, deps.TurnTimeout)
	defer cancel()

	if replyPendingSwitchText(turnCtx, deps.Runtime, deps.Sessions, deps.PendingSwitch, chID, peerID, in.Text, replyText) {
		return
	}
	if cmdReply, isCmd := runSlashCommand(turnCtx, deps.Runtime, deps.Sessions, deps.PendingSwitch, chID, peerID, in.Text); isCmd {
		replyText(cmdReply)
		return
	}

	plan := prepareAutoRoute(turnCtx, deps.Runtime, chID, peerID, in.Text)
	content := in.Text
	if plan.TurnText != "" {
		content = plan.TurnText
	}

	var resolved *runtimeclient.ResolveReply
	var err error
	if plan.AgentID != "" {
		resolved, err = resolveMaybeRebind(turnCtx, deps.Sessions, chID, peerID, plan.AgentID, plan.Reason)
	} else {
		resolved, err = deps.Sessions.Resolve(turnCtx, "", runtimeclient.ResolveRequest{ChannelID: chID, PeerID: peerID})
	}
	if err != nil {
		replyText(ilinkFailurePrefix + mapRuntimeUserError(err))
		return
	}

	rc, _, err := deps.Runtime.TurnsStream(turnCtx, resolved.UserID, runtimeclient.TurnRequest{
		SessionID:      resolved.SessionID,
		Content:        content,
		ChannelID:      chID,
		PeerID:         peerID,
		CorrelationID:  msg.corr,
		IdempotencyKey: msg.key,
	})
	if err != nil {
		replyText(ilinkFailurePrefix + mapRuntimeUserError(err))
		return
	}
	defer rc.Close()

	res := consumeIlinkTurnStream(turnCtx, rc, noticeAt, func() { replyText(ilinkProcessingReply) })
	if ctx.Err() != nil {
		// Runner stopped (disable / re-login): this bot's credentials are no longer ours to use.
		return
	}
	switch {
	case res.HITL:
		replyText(ilinkHITLReply)
	case res.Failed:
		if ctxErr := turnCtx.Err(); ctxErr != nil {
			replyText(ilinkFailurePrefix + mapRuntimeUserError(ctxErr))
		} else {
			replyText(ilinkFailurePrefix + mapTurnFailureMessage(res.ErrMsg, ""))
		}
	default:
		replyText(res.Content)
	}
}

type ilinkTurnResult struct {
	Content string
	Failed  bool
	HITL    bool
	ErrMsg  string
}

// consumeIlinkTurnStream collects chunk content; notice fires once if the stream is still open at noticeAt.
func consumeIlinkTurnStream(ctx context.Context, body io.Reader, noticeAt time.Time, notice func()) ilinkTurnResult {
	st := NewProgressState(time.Now())
	events := make(chan sseEvent, 16)
	go scanSSEEvents(ctx, body, events)
	timer := time.NewTimer(time.Until(noticeAt))
	defer timer.Stop()

	var full strings.Builder
	cancelled := false
	for done := false; !done; {
		select {
		case <-ctx.Done():
			cancelled, done = true, true
			if c, ok := body.(io.Closer); ok {
				_ = c.Close()
			}
		case <-timer.C:
			notice()
		case ev, ok := <-events:
			if !ok || ev.event == "done" {
				done = true
				continue
			}
			st.ApplySSEEvent(ev.event, ev.data)
			if ev.event == "chunk" {
				full.WriteString(chunkContent(ev.data))
			}
		}
	}
	// Drain so scanSSEEvents cannot leak on a blocked send after cancel.
	if cancelled {
		for range events {
		}
	}

	res := ilinkTurnResult{Content: strings.TrimSpace(full.String())}
	switch {
	case st.HITL:
		res.Failed, res.HITL = true, true
	case st.Failed:
		res.Failed, res.ErrMsg = true, st.ErrMsg
	case ctx.Err() != nil:
		res.Failed, res.ErrMsg = true, ctx.Err().Error()
	case res.Content == "":
		res.Failed, res.ErrMsg = true, "turn failed"
	}
	return res
}

func chunkContent(data []byte) string {
	var payload struct {
		Content string `json:"content"`
	}
	if err := json.Unmarshal(data, &payload); err != nil {
		return ""
	}
	return payload.Content
}

// replyPendingSwitchText is the reply-func variant of replyPendingSwitch for plain-text channels.
func replyPendingSwitchText(ctx context.Context, rt *runtimeclient.Client, sessions *session.Router, store *pendingswitch.Store, channelID, peerID, text string, reply func(string)) bool {
	if store == nil {
		return false
	}
	ent, ok := store.Get(channelID, peerID, time.Now())
	if !ok {
		return false
	}
	text = strings.TrimSpace(text)
	if strings.HasPrefix(text, "/") {
		if cmd, isCmd := command.Parse(text); isCmd && command.PreservesPending(cmd.Kind) {
			return false
		}
		store.Delete(channelID, peerID)
		return false
	}
	if idx, isDigit := parseDigitChoice(text); isDigit && idx >= 1 && idx <= len(ent.Agents) {
		store.Delete(channelID, peerID)
		msg, err := switchChannelAgent(ctx, rt, sessions, channelID, peerID, ent.Agents[idx-1].ID)
		if err != nil {
			reply(ilinkFailurePrefix + mapRuntimeUserError(err))
			return true
		}
		reply(msg)
		return true
	}
	reply(formatPendingSwitchInvalidPrompt(len(ent.Agents)))
	return true
}

// ilinkPeerTail matches the Web roster label: last 4 chars of the part before "@".
func ilinkPeerTail(peerID string) string {
	local, _, _ := strings.Cut(peerID, "@")
	r := []rune(local)
	if len(r) <= 4 {
		return local
	}
	return string(r[len(r)-4:])
}

func sleepIlink(ctx context.Context, d time.Duration) bool {
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-ctx.Done():
		return false
	case <-t.C:
		return true
	}
}

func nextIlinkBackoff(d time.Duration) time.Duration {
	d *= 2
	if d > ilinkBackoffMax {
		d = ilinkBackoffMax
	}
	return d
}
```

- [ ] **Step 5: 运行确认通过**

```powershell
gofmt -w internal/adapter/weixin_ilink.go internal/adapter/weixin_ilink_fixture_test.go internal/adapter/weixin_ilink_test.go
go vet ./internal/adapter/
go test ./internal/adapter/ -run WeixinIlink -count=1 -v 2>&1 | Select-String -Pattern "^(ok|FAIL|---)"
go test ./internal/adapter/ -count=1
```

Expected: 17 个顶层 `--- PASS`；整个 adapter 包 `ok`（既有 wecom / webhook 测试不受影响）。

- [ ] **Step 6: Commit**

```powershell
git add internal/adapter/weixin_ilink.go internal/adapter/weixin_ilink_fixture_test.go internal/adapter/weixin_ilink_test.go
git commit -m "feat(gateway): weixin_ilink 长轮询 runner（名册上报、游标 CAS、白名单、peer 串行队列）"
```

---

### Task 6: `WeixinIlinkManager`

可运行条件 = `type=weixin_ilink && enabled && BotToken != "" && !SessionExpired`。`IlinkConnectionChanged` 只比较 `BotToken`、`BaseURL`、`Generation`；不改动现有 `ConnectionConfigChanged`。不接 leadership。

**Files:**
- Create: `gateway/internal/adapter/weixin_ilink_manager.go`
- Test: `gateway/internal/adapter/weixin_ilink_manager_test.go`

- [ ] **Step 1: 写失败测试**

`gateway/internal/adapter/weixin_ilink_manager_test.go`（完整内容）：

```go
package adapter_test

import (
	"context"
	"slices"
	"sync"
	"testing"
	"time"

	"github.com/sixath/gateway/internal/adapter"
	"github.com/sixath/gateway/internal/channel"
)

type ilinkLoopRecorder struct {
	mu          sync.Mutex
	starts      map[string]int
	generations []int
	stopped     chan string
}

func newIlinkLoopRecorder() *ilinkLoopRecorder {
	return &ilinkLoopRecorder{starts: map[string]int{}, stopped: make(chan string, 16)}
}

func (r *ilinkLoopRecorder) loop(ctx context.Context, ch channel.Channel, _ adapter.WeixinIlinkDeps) {
	r.mu.Lock()
	r.starts[ch.ID]++
	r.generations = append(r.generations, ch.Generation)
	r.mu.Unlock()
	<-ctx.Done()
	r.stopped <- ch.ID
}

func (r *ilinkLoopRecorder) startCount(id string) int {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.starts[id]
}

func (r *ilinkLoopRecorder) expectStop(t *testing.T, what string) {
	t.Helper()
	select {
	case <-r.stopped:
	case <-time.After(time.Second):
		t.Fatalf("expected stop on %s", what)
	}
}

func (r *ilinkLoopRecorder) expectNoStop(t *testing.T, what string) {
	t.Helper()
	select {
	case id := <-r.stopped:
		t.Fatalf("unexpected stop of %s on %s", id, what)
	case <-time.After(50 * time.Millisecond):
	}
}

func newTestIlinkManager(t *testing.T, reporter *fakeStatusReporter) (*adapter.WeixinIlinkManager, *ilinkLoopRecorder) {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	mgr := adapter.NewWeixinIlinkManager(ctx, adapter.WeixinIlinkDeps{Reporter: reporter})
	rec := newIlinkLoopRecorder()
	mgr.SetLoopForTest(rec.loop)
	return mgr, rec
}

func ilinkChannel() channel.Channel {
	return channel.Channel{
		ID: "wx-1", Type: "weixin_ilink", Enabled: true,
		BotToken: "t1", BaseURL: "https://ilinkai.weixin.qq.com", Generation: 1, AllowedPeers: []string{"a"},
	}
}

func TestWeixinIlinkManager_RestartOnGenerationNotOnAllowedPeers(t *testing.T) {
	reporter := &fakeStatusReporter{}
	mgr, rec := newTestIlinkManager(t, reporter)

	ch := ilinkChannel()
	mgr.Reconcile(nil, []channel.Channel{ch})
	waitFor(t, time.Second, func() bool { return rec.startCount("wx-1") == 1 }, "first start")

	peers := ch
	peers.AllowedPeers = []string{"a", "b"}
	mgr.Reconcile([]channel.Channel{ch}, []channel.Channel{peers})
	rec.expectNoStop(t, "allowed_peers change")
	if rec.startCount("wx-1") != 1 {
		t.Fatalf("starts=%d want 1 after allowed_peers change", rec.startCount("wx-1"))
	}

	relogin := peers
	relogin.Generation, relogin.BotToken = 2, "t2"
	mgr.Reconcile([]channel.Channel{peers}, []channel.Channel{relogin})
	rec.expectStop(t, "generation change")
	waitFor(t, time.Second, func() bool { return rec.startCount("wx-1") == 2 }, "restart")
	rec.mu.Lock()
	gens := slices.Clone(rec.generations)
	rec.mu.Unlock()
	if !slices.Equal(gens, []int{1, 2}) {
		t.Fatalf("generations=%v want [1 2]", gens)
	}

	disabled := relogin
	disabled.Enabled = false
	mgr.Reconcile([]channel.Channel{relogin}, []channel.Channel{disabled})
	rec.expectStop(t, "disable")
	if states := reporter.statesFor("wx-1"); len(states) == 0 || states[len(states)-1] != "disabled" {
		t.Fatalf("states=%v want trailing disabled", states)
	}
}

func TestWeixinIlinkManager_NotRunnableWithoutTokenOrWhenExpired(t *testing.T) {
	reporter := &fakeStatusReporter{}
	mgr, rec := newTestIlinkManager(t, reporter)

	noToken := ilinkChannel()
	noToken.ID, noToken.BotToken = "wx-none", ""
	expired := ilinkChannel()
	expired.ID, expired.SessionExpired = "wx-exp", true
	wecom := channel.Channel{ID: "bot1", Type: "wecom_bot", Enabled: true, BotID: "b", Secret: "s"}
	mgr.Reconcile(nil, []channel.Channel{noToken, expired, wecom})
	time.Sleep(50 * time.Millisecond)
	for _, id := range []string{"wx-none", "wx-exp", "bot1"} {
		if n := rec.startCount(id); n != 0 {
			t.Fatalf("%s starts=%d want 0", id, n)
		}
	}

	ch := ilinkChannel()
	mgr.Reconcile(nil, []channel.Channel{ch})
	waitFor(t, time.Second, func() bool { return rec.startCount("wx-1") == 1 }, "start")
	nowExpired := ch
	nowExpired.SessionExpired = true
	mgr.Reconcile([]channel.Channel{ch}, []channel.Channel{nowExpired})
	rec.expectStop(t, "session expired")
	if slices.Contains(reporter.statesFor("wx-1"), "disabled") {
		t.Fatalf("session expiry must not report disabled: %v", reporter.statesFor("wx-1"))
	}

	relogin := ch
	relogin.Generation = 2
	mgr.Reconcile([]channel.Channel{nowExpired}, []channel.Channel{relogin})
	waitFor(t, time.Second, func() bool { return rec.startCount("wx-1") == 2 }, "start after re-login")
}

func TestIlinkConnectionChanged(t *testing.T) {
	base := ilinkChannel()
	if adapter.IlinkConnectionChanged(base, base) {
		t.Fatal("identical should not change")
	}
	for name, mutate := range map[string]func(*channel.Channel){
		"bot_token":  func(c *channel.Channel) { c.BotToken = "x" },
		"base_url":   func(c *channel.Channel) { c.BaseURL = "https://other.weixin.qq.com" },
		"generation": func(c *channel.Channel) { c.Generation++ },
	} {
		other := base
		mutate(&other)
		if !adapter.IlinkConnectionChanged(base, other) {
			t.Fatalf("%s change should restart", name)
		}
	}
	other := base
	other.AllowedPeers = []string{"z"}
	if adapter.IlinkConnectionChanged(base, other) {
		t.Fatal("allowed_peers change must not restart")
	}
}

func TestWeixinIlinkManager_SuspendedRunnerNotRespawned(t *testing.T) {
	f := newIlinkFixture(t, ilinkAlice)
	f.ilink.push(`{"ret":-14}`)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	mgr := adapter.NewWeixinIlinkManager(ctx, f.deps)
	t.Cleanup(func() { mgr.Reconcile([]channel.Channel{f.ch}, nil) })

	mgr.Reconcile(nil, []channel.Channel{f.ch})
	waitFor(t, 2*time.Second, func() bool { _, _, exp := f.portal.counts(); return exp == 1 }, "session-expired report")
	for range 3 {
		mgr.Reconcile([]channel.Channel{f.ch}, []channel.Channel{f.ch})
	}
	time.Sleep(100 * time.Millisecond)
	if _, _, exp := f.portal.counts(); f.ilink.pollCount() != 1 || exp != 1 {
		t.Fatalf("polls=%d expired=%d want 1/1 (runner must stay suspended)", f.ilink.pollCount(), exp)
	}

	expired := f.ch
	expired.SessionExpired = true
	mgr.Reconcile([]channel.Channel{f.ch}, []channel.Channel{expired})
	time.Sleep(50 * time.Millisecond)
	if f.ilink.pollCount() != 1 {
		t.Fatalf("polls=%d after expired sync want 1", f.ilink.pollCount())
	}
}
```

- [ ] **Step 2: 运行确认失败**

```powershell
go test ./internal/adapter/ -run "WeixinIlinkManager|IlinkConnectionChanged" -count=1
```

Expected: FAIL，编译错误 `undefined: adapter.NewWeixinIlinkManager` / `undefined: adapter.IlinkConnectionChanged`。

- [ ] **Step 3: 实现**

`gateway/internal/adapter/weixin_ilink_manager.go`（完整内容）：

```go
package adapter

import (
	"context"
	"log"
	"sync"
	"time"

	"github.com/sixath/gateway/internal/channel"
	"github.com/sixath/gateway/internal/runtimeclient"
)

const weixinIlinkType = "weixin_ilink"

type weixinIlinkLoopFunc func(ctx context.Context, ch channel.Channel, deps WeixinIlinkDeps)

// WeixinIlinkManager starts/stops one long-poll runner per runnable weixin_ilink channel.
// A runner that hit -14 or a generation conflict suspends in place, so it stays in active
// and Reconcile does not respawn it until the channel config changes.
type WeixinIlinkManager struct {
	parent context.Context
	deps   WeixinIlinkDeps

	mu           sync.Mutex
	active       map[string]*runnerHandle
	loopFn       weixinIlinkLoopFunc
	drainTimeout time.Duration
}

// NewWeixinIlinkManager builds a manager bound to parent ctx (process lifetime).
func NewWeixinIlinkManager(parent context.Context, deps WeixinIlinkDeps) *WeixinIlinkManager {
	return &WeixinIlinkManager{
		parent:       parent,
		deps:         normalizeWeixinIlinkDeps(deps),
		active:       make(map[string]*runnerHandle),
		loopFn:       RunWeixinIlink,
		drainTimeout: runnerDrainTimeout,
	}
}

// SetLoopForTest replaces the runner loop (tests only).
func (m *WeixinIlinkManager) SetLoopForTest(fn func(ctx context.Context, ch channel.Channel, deps WeixinIlinkDeps)) {
	if m == nil || fn == nil {
		return
	}
	m.mu.Lock()
	m.loopFn = fn
	m.mu.Unlock()
}

// SetDrainTimeoutForTest overrides how long stop/restart waits for the old goroutine.
func (m *WeixinIlinkManager) SetDrainTimeoutForTest(d time.Duration) {
	if m == nil || d <= 0 {
		return
	}
	m.mu.Lock()
	m.drainTimeout = d
	m.mu.Unlock()
}

func ilinkRunnable(ch channel.Channel) bool {
	return ch.Type == weixinIlinkType && ch.Enabled && ch.BotToken != "" && !ch.SessionExpired
}

// IlinkConnectionChanged reports whether a running weixin_ilink runner must restart.
// AllowedPeers is deliberately excluded: the runner re-reads it from the Registry per message.
func IlinkConnectionChanged(a, b channel.Channel) bool {
	return a.BotToken != b.BotToken || a.BaseURL != b.BaseURL || a.Generation != b.Generation
}

// Reconcile applies weixin_ilink start/stop/restart against prev vs next snapshots.
func (m *WeixinIlinkManager) Reconcile(prev, next []channel.Channel) {
	if m == nil {
		return
	}
	prevMap := indexChannels(prev)
	nextMap := indexChannels(next)

	for id, pch := range prevMap {
		if pch.Type != weixinIlinkType {
			continue
		}
		nch, ok := nextMap[id]
		if !ok || nch.Type != weixinIlinkType {
			m.stop(id, false)
			continue
		}
		if !nch.Enabled {
			if pch.Enabled || m.isRunning(id) {
				m.stop(id, true)
			}
			continue
		}
		if !ilinkRunnable(nch) {
			// Missing token / session expired: Portal derives login_required, so do not report disabled.
			m.stop(id, false)
			continue
		}
		if !m.isRunning(id) {
			m.start(nch)
			continue
		}
		if IlinkConnectionChanged(pch, nch) {
			m.restart(nch)
		}
	}

	for id, nch := range nextMap {
		if !ilinkRunnable(nch) {
			continue
		}
		if _, ok := prevMap[id]; ok {
			continue
		}
		m.start(nch)
	}
}

func (m *WeixinIlinkManager) isRunning(id string) bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	_, ok := m.active[id]
	return ok
}

func (m *WeixinIlinkManager) start(ch channel.Channel) {
	m.stop(ch.ID, false)

	m.mu.Lock()
	ctx, cancel := context.WithCancel(m.parent)
	done := make(chan struct{})
	m.active[ch.ID] = &runnerHandle{cancel: cancel, done: done}
	loop := m.loopFn
	deps := m.deps
	m.mu.Unlock()

	log.Printf("weixin_ilink %s: starting runner generation=%d", ch.ID, ch.Generation)
	go func() {
		defer close(done)
		loop(ctx, ch, deps)
	}()
}

func (m *WeixinIlinkManager) stop(id string, reportDisabled bool) {
	m.mu.Lock()
	h, ok := m.active[id]
	if ok {
		delete(m.active, id)
	}
	deps := m.deps
	timeout := m.drainTimeout
	m.mu.Unlock()

	if ok && h != nil {
		log.Printf("weixin_ilink %s: stopping runner", id)
		h.cancel()
		waitRunnerDrain("weixin_ilink", id, h.done, timeout)
	}
	if reportDisabled {
		reportIlinkStatus(deps, id, runtimeclient.StatusBody{State: "disabled"})
	}
}

func (m *WeixinIlinkManager) restart(ch channel.Channel) {
	m.stop(ch.ID, false)
	m.start(ch)
}

func waitRunnerDrain(kind, id string, done <-chan struct{}, timeout time.Duration) {
	if done == nil {
		return
	}
	if timeout <= 0 {
		timeout = runnerDrainTimeout
	}
	timer := time.NewTimer(timeout)
	defer timer.Stop()
	select {
	case <-done:
	case <-timer.C:
		log.Printf("%s %s: timed out waiting for runner to exit after %s", kind, id, timeout)
	}
}
```

- [ ] **Step 4: 运行确认通过**

```powershell
gofmt -w internal/adapter/weixin_ilink_manager.go internal/adapter/weixin_ilink_manager_test.go
go vet ./internal/adapter/
go test ./internal/adapter/ -run "WeixinIlinkManager|IlinkConnectionChanged" -count=1 -v 2>&1 | Select-String -Pattern "^(ok|FAIL|---)"
go test ./internal/adapter/ -count=1
```

Expected: 4 个 `--- PASS`；adapter 包整体 `ok`。

- [ ] **Step 5: Commit**

```powershell
git add internal/adapter/weixin_ilink_manager.go internal/adapter/weixin_ilink_manager_test.go
git commit -m "feat(gateway): WeixinIlinkManager 按渠道配置启停 runner（改白名单不重启）"
```

---

### Task 7: 装配与 README

**Files:**
- Modify: `gateway/internal/config/config.go`、`gateway/cmd/gateway/main.go`、`gateway/README.md`

- [ ] **Step 1: 进程配置 `ilink_split_runes`**

`gateway/internal/config/config.go`，查找：

```go
	TurnTimeoutSec int    `yaml:"turn_timeout_sec"`
```

替换为：

```go
	TurnTimeoutSec int    `yaml:"turn_timeout_sec"`
	// IlinkSplitRunes caps each weixin_ilink reply message; 0 means 4000.
	IlinkSplitRunes int `yaml:"ilink_split_runes"`
```

- [ ] **Step 2: 装配 manager**

`gateway/cmd/gateway/main.go`，查找：

```go
	mgr := adapter.NewWecomBotManager(ctx, botDeps)
	syncer := channelsync.NewRunner(channelsync.Config{
		Registry: reg,
		Lister:   rt,
		Manager:  mgr,
		Interval: 15 * time.Second,
	})
```

替换为：

```go
	mgr := adapter.NewWecomBotManager(ctx, botDeps)
	// Same Sessions / Idempotency / PendingSwitch as wecom_bot so slash commands and dedupe behave alike.
	ilinkMgr := adapter.NewWeixinIlinkManager(ctx, adapter.WeixinIlinkDeps{
		Registry:      reg,
		Runtime:       rt,
		Sessions:      sessions,
		Idempotency:   idem,
		PendingSwitch: pendingSwitch,
		TurnTimeout:   turnTimeout,
		SplitRunes:    cfg.IlinkSplitRunes,
	})
	syncer := channelsync.NewRunner(channelsync.Config{
		Registry: reg,
		Lister:   rt,
		Manager:  channelsync.MultiReconciler{mgr, ilinkMgr},
		Interval: 15 * time.Second,
	})
```

- [ ] **Step 3: 编译与全量测试**

```powershell
gofmt -w internal/config/config.go cmd/gateway/main.go
go build ./...
go vet ./...
go test ./... -count=1 2>&1 | Select-String -Pattern "^(ok|FAIL|---)"
```

Expected: 编译、vet 无输出；所有包 `ok`（含新包 `internal/ilink`）。

- [ ] **Step 4: README**

`gateway/README.md` 按顺序做以下 8 处修改。

(1) 查找：

```markdown
统一入站对话入口：浏览器 **Web**、通用 **Webhook**，以及企业微信 **智能机器人 AI+** 的 **WebSocket 长连接**（`type=wecom_bot`）。
```

替换为：

```markdown
统一入站对话入口：浏览器 **Web**、通用 **Webhook**、企业微信 **智能机器人 AI+** 的 **WebSocket 长连接**（`type=wecom_bot`），以及个人微信 **iLink 机器人** 的 **HTTP 长轮询**（`type=weixin_ilink`）。
```

(2) 查找：

```markdown
| [渠道 Portal 真相源 + Runtime Status](../docs/superpowers/specs/2026-08-11-channel-portal-runtime-status-design.md) | 配置同步、连接态 |
```

替换为：

```markdown
| [渠道 Portal 真相源 + Runtime Status](../docs/superpowers/specs/2026-08-11-channel-portal-runtime-status-design.md) | 配置同步、连接态 |
| [微信 iLink 渠道设计](../docs/superpowers/specs/2026-10-18-weixin-ilink-channel-design.md) | `weixin_ilink` 长轮询、名册白名单、会话过期 |
```

(3) 渠道一览表，查找：

```markdown
| `wecom_bot` | 企微 WSS 推送 `aibot_msg_callback` | `aibot_respond_msg` stream 卡片 | 群 `chat:{chatid}` / 单聊 `user:{userid}` |
```

替换为：

```markdown
| `wecom_bot` | 企微 WSS 推送 `aibot_msg_callback` | `aibot_respond_msg` stream 卡片 | 群 `chat:{chatid}` / 单聊 `user:{userid}` |
| `weixin_ilink` | Gateway 长轮询 iLink `getupdates`（仅私聊文本） | `sendmessage` 纯文本（Markdown 简化 + 按 4000 字切分） | `user:{from_user_id}` |
```

(4) 查找：

```markdown
### Runtime Status（仅 `wecom_bot`）

Web Channels 列表 / 编辑页展示连接态。Gateway 上报 `connected` / `disconnected` / `reconnecting` / `disabled`；Portal 读路径再派生 **`unknown`**。
```

替换为：

```markdown
### Runtime Status（`wecom_bot` / `weixin_ilink`）

Web Channels 列表 / 编辑页展示连接态。Gateway 上报 `connected` / `disconnected` / `reconnecting` / `disabled`；Portal 读路径再派生 **`unknown`**，`weixin_ilink` 另有 **`login_required`**（未扫码或会话过期，优先级高于心跳，见下文 [微信 iLink](#微信-ilink-机器人长轮询-weixin_ilink)）。
```

(5) 单副本约束，查找：

```markdown
同一 `bot_id` **不要**在多台 Gateway（含 K8s 多副本）上同时 `enabled`。新的 `aibot_subscribe` 会踢掉旧连接；重复订阅时日志常见 `wecom_bot <id> disconnected` 重连循环。
```

替换为：

```markdown
同一 `bot_id` **不要**在多台 Gateway（含 K8s 多副本）上同时 `enabled`。新的 `aibot_subscribe` 会踢掉旧连接；重复订阅时日志常见 `wecom_bot <id> disconnected` 重连循环。

`weixin_ilink` 同样**只能单副本**：两个 Gateway 同时长轮询同一个 bot 会重复收到消息、重复回复，且游标互相覆盖。Gateway 不接 leader 租约，部署时保证只有一个实例。
```

(6) 企微章节末尾，查找：

```markdown
失败时仍 `finish=true` 出站失败卡片（含发起人/问题），不静默丢弃。

---
```

替换为：

````markdown
失败时仍 `finish=true` 出站失败卡片（含发起人/问题），不静默丢弃。

---

## 微信 iLink 机器人（长轮询 `weixin_ilink`）

一个 Portal 渠道对应一个 iLink bot 账号。扫码登录、名册（白名单）与出站推送都在 Portal；Gateway 只负责长轮询收消息并回复。设计见 [微信 iLink 渠道设计](../docs/superpowers/specs/2026-10-18-weixin-ilink-channel-design.md)。

### 1. Portal 侧

1. Web **Channels** 新建 `type=weixin_ilink` 渠道，保存后在编辑页点「扫码登录」，用微信扫码并在手机上确认。
2. 扫码人自动进入名册并放行；其他人首次给 bot 发消息后出现在名册中（默认拒绝），在名册里点「放行」。
3. Gateway 约 15s 内同步到 `bot_token` / `generation` / `allowed_peers` 并启动 runner；改白名单**不会**重启 runner（每条消息现读）。

### 2. 消息路径

```text
getupdates 长轮询（客户端超时 40s → 空批次，游标不变）
    → 只取 message_type=1、无 group_id 的消息
    → 每条先上报名册（context_token / 预览，在白名单判断之前）
    → PUT 游标（CAS；409 = 已换号 → runner 挂起）
    → 进程内幂等 ilink:{channel_id}:{message_id}
    → 白名单：不在名单 → 「无权限，请联系管理员开通（ID 尾号 xxxx）」（同一人 10 分钟一次）
    → 非文本 → 「暂只支持文字消息」
    → 该 peer 的串行队列（排队上限 5，满则「消息太多，请稍后再试」；空闲 5 分钟回收）
        → pending /switch 序号 → slash 指令 → @Agent / 分类器 → Resolve → TurnsStream
        → 15s 内无结果先回一条「处理中…」
        → 回复正文：Markdown 简化为纯文本，按 4000 字切分，逐条 sendmessage（带本条 context_token）
```

- 投递语义：**至多一次**（游标先于处理提交）。进程崩溃时在途 / 排队中的消息丢失。
- HITL fail-closed：回复「该操作需要在 Web 端确认」。Turn 失败回复「处理失败：…」。
- 心跳：runner 运行期间独立于长轮询每 30s 上报一次 `connected` / `reconnecting`。
- 会话过期（iLink 返回 `-14`）：上报 Portal 后 runner **挂起**（不再请求 iLink、不被重启），Portal 显示 `login_required`；重新扫码后 `generation` 变化，runner 自动重启。
- 日志从不打印 `bot_token` / `context_token`；对端只打 ID 尾号。
- 进程配置可选 `ilink_split_runes`（单条回复最大字数，默认 4000）。

### 3. 验收清单

1. 创建渠道 → 扫码 → ≤15s Runtime Status 为 `connected`；名册里扫码人已放行。
2. 非白名单微信号发消息 → 收到「无权限…（ID 尾号 xxxx）」；名册出现该对端与最近预览；10 分钟内再发不再回复。
3. 名册放行 → ≤15s 后对话正常（runner 未重启）；`/who`、`/switch` + 序号、`/new` 可用。
4. 发图片 → 「暂只支持文字消息」。
5. 让 Agent 输出长 Markdown → 微信收到纯文本、无 `#` / `**`，超长时分多条。
6. 重新扫码（换 bot）→ 日志出现旧 runner 停止、新 runner 启动；旧 runner 不再写游标。
7. 让会话过期：用 `scripts/ilink-spike` 等其它客户端对同一微信号重新扫码，使 Portal 保存的旧 `bot_token` 失效（`-14`）→ 持续显示 `login_required`（不会退化为 `unknown`），Gateway 不再轮询该 bot；在 Portal 重新扫码后恢复 `connected`。
8. 停 Gateway → ≤90s `unknown`。

---
````

(7) 开发提示，查找：

```markdown
go test ./internal/wecom/ ./internal/channel/ ./internal/adapter/ ./internal/channelsync/ -count=1
```

替换为：

```markdown
go test ./internal/wecom/ ./internal/ilink/ ./internal/channel/ ./internal/adapter/ ./internal/channelsync/ -count=1
```

(8) 确认 `git diff -- README.md` 只包含以上 7 处改动（第 (6) 处为新章节）。

- [ ] **Step 5: Commit**

```powershell
git add internal/config/config.go cmd/gateway/main.go README.md
git commit -m "feat(gateway): 装配 WeixinIlinkManager 并补充 weixin_ilink README"
```

---

### Task 8: 全量验证与手工验收

**Files:** 无（只在发现问题时修复并单独提交）

- [ ] **Step 1: 全量检查**

```powershell
Remove-Item Env:GOROOT -ErrorAction SilentlyContinue
gofmt -l internal/ilink internal/adapter/weixin_ilink.go internal/adapter/weixin_ilink_manager.go internal/adapter/weixin_ilink_test.go internal/adapter/weixin_ilink_fixture_test.go internal/adapter/weixin_ilink_manager_test.go internal/runtimeclient/ilink_test.go internal/channelsync/multi_test.go
go build ./...
go vet ./...
go test ./... -count=1 2>&1 | Select-String -Pattern "^(ok|FAIL|---)"
go test ./internal/adapter/ -run "WeixinIlink|IlinkConnectionChanged" -count=5
```

Expected: `gofmt -l`（只列本计划新建的文件）无输出；build / vet 无输出；所有包 `ok`；重复 5 次的 iLink 测试稳定通过。可选（需要 CGO 与 gcc）：`go test -race ./internal/adapter/ ./internal/ilink/ ./internal/runtimeclient/ ./internal/channelsync/ -count=2`。

- [ ] **Step 2: 手工验收（需要用户扫码配合）**

按项目 README 启动 Portal、Web 与 Gateway（`go run ./cmd/gateway -config ./configs/config.example.yaml`），然后按 `gateway/README.md`「微信 iLink 机器人 → 验收清单」第 1–8 条逐项验证，并确认 Gateway 日志中没有出现 `bot_token` / `context_token` 明文。

把结果（通过 / 问题）记录到最终报告。发现问题时修复、补测试、单独提交。

---

## 完成标准

- Task 1–7 的测试全部通过，Task 8 全量测试无新增失败。
- 手工验收 1–8 通过（或问题已修复）。
- 未提交任何与本计划无关的文件；日志与错误信息中不出现 `bot_token` / `context_token` 明文。
