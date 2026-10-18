# 微信 iLink 渠道 P3：出站推送 Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Portal 能主动给 `weixin_ilink` 名册中已放行的微信用户发消息：`WeixinIlinkUsecase.Push`（整形 + 4000 字切分 + 不重试 + `-14` 标记失效）；Cron 新增 `delivery_peer_id` 并在保存时校验、到点投递到微信；在微信里让 Agent 建的 Cron 自动"推回发起人"；Agent 工具 `send_to_weixin`；Web Cron 表单可选择接收人。

**Architecture:** 协议层只在叶子包 `internal/ilink` 加文本整形与切分（`text.go`）。biz 层：`WeixinIlinkUsecase.Push`（iLink 发送方可注入）、`ResolveSendTarget`（`send_to_weixin` 渠道 / 对端解析）；`CronUsecase` 通过 `SetWeixinIlink` 拿到渠道 / 会话映射 / 名册仓储，做保存校验与"会话 → 微信渠道 + 对端"反查（`IlinkOrigin`）。`cron.Executor` 通过 `SetIlinkPusher` 调 `Push`。Portal 的 `portalCronClient.Create` 在调用方没给任何投递参数时用 `IlinkOrigin` 回填。`send_to_weixin` 在 `internal/chat` 注册、在 `internal/service` 按 Agent 关联的启用 `weixin_ilink` 渠道决定是否注册，并通过 `CatalogWiringInput.Channels` 进入工具目录（与既有 wecom 槽位并存）。所有新依赖都用 setter 注入，`wire_gen.go` 手工追加 5 行（该文件含手写调用，**不要**重新运行 wire）。

**Tech Stack:** Go 1.26、Kratos v2.9.2、GORM（MySQL / 测试用 SQLite）、protoc 34；framework（`github.com/sixath/framework`，portal 经 `replace ../framework` 引用同仓库目录）；React 19 + TypeScript（无 UI 库）、`node --test`。

**Spec:** `docs/superpowers/specs/2026-10-18-weixin-ilink-channel-design.md`（§4.2 Portal / Framework / Web 行、§5.4、§6.3、§7、§8、§10 Phase 0 结论）

**前置：** P1（`2026-10-18-weixin-ilink-p1-portal.md`）已在 `main` 实现。端到端验收还依赖 P2（Gateway 写入名册与 `context_token`）。

**不在本计划内：** Gateway（P2，`gateway/` 目录本计划一律不改）；图片 / 文件推送；投递失败自动重试。

---

## 约定

- 除特别说明，Go 命令在 `portal/` 目录执行；每个 shell 会话先执行 `Remove-Item Env:GOROOT -ErrorAction SilentlyContinue`（本机残留旧 GOROOT，否则编译器版本不匹配）。
- framework 命令在 `framework/` 目录执行；Web 命令在 `web/` 目录执行。
- 工作区 `main` 上已有与本计划无关的未提交改动（`evals/runner/runner.exe`、`sixath.exe`、`web/tsconfig.tsbuildinfo`、`evals/results/*.log` 等）。**只 `git add` 每个任务列出的文件**，禁止 `git add -A` / `git add .` / `git commit -a`。
- `gofmt -w` 只作用于本任务改动的文件。部分既有文件本来就没按 gofmt 对齐（如 `agent_route_test.go`、`channel_allowed_agents_test.go`、`service/agent.go`），`gofmt -w` 会顺带修正几行既有对齐，属预期，一并提交即可。
- `cmd/backend/wire_gen.go` 手工编辑，**不要**运行 wire。
- 新增 Go 注释只写代码本身表达不了的约束。

## 实现决定（规格之外或对规格的细化）

1. **framework 依赖**：`portal/go.mod` 有 `replace github.com/sixath/framework => ../framework`（`framework/` 是本仓库的子目录，不是 submodule / vendor），`evals/runner` 同样 `replace ../../framework`，`gateway` 不依赖 framework。改 `framework/tool/cronjob_tool.go` 后 portal 立即生效，**不需要**发版或升级 `require v0.0.3`。若将来 framework 单独发布给外部使用，需要另打 tag。
2. **依赖注入用 setter**（`CronUsecase.SetWeixinIlink`、`Executor.SetIlinkPusher`、`WeixinIlinkUsecase.SetPeerSessions`、`ChatService/AgentService.SetWeixinIlink`），与既有 `runtimeService.SetIlink` 一致；构造函数签名不变，测试与 wire 注入文件无需改动。
3. **目录多渠道**：`CatalogWiringInput` 新增 `Channels []ChannelBinding`，原 `WecomChannelID` / `ChannelType` 保留，wecom 调用方零改动。Agent 关联多个微信渠道时，`send_to_weixin` 的目录绑定 `channel_id` 为逗号分隔的 slug 列表。
4. **`send_to_weixin` 渠道解析**：未传 `channel_id` 时严格按规格（会话所属微信渠道 → Agent 唯一渠道 → 报错列出 slug）；传了 `channel_id` 时，只要它是会话所属渠道或 Agent 的渠道之一就以它为准（规格把 `channel_id` 放在第 3 步，直接按字面实现会在"微信会话里给另一个渠道的人发消息"时忽略显式参数）。`peer_id` 默认值只在会话自己的渠道上生效。
5. **`Push` 检查顺序**：先渠道级（不存在 / 非 iLink / 已停用 / 需重扫），再对端级（白名单 / `context_token`）。**已停用渠道拒绝推送**（规格未写，避免管理员停用后仍有消息发出）。
6. **错误形态**：`Push` 返回普通 `error`（文案直接作为 Cron `delivery_error` 和工具输出）；Cron 保存校验返回 Kratos 400 `INVALID_ARGUMENT`。
7. **Cron 校验范围**：`update` 只在更新涉及 `delivery_mode` / `delivery_channel_id` / `delivery_peer_id` 时校验（暂停一个接收人后来被取消放行的任务仍可成功）。保存时只要求 `allowed=true`，不要求已有 `context_token`（同规格）。投递渠道不存在时保存不报错，到点由 Executor 报错（与其它渠道类型一致）。现有 Cron update 没有字段白名单（map 直接交给 GORM），加列后 `delivery_peer_id` 即可更新。
8. **回填条件**：调用方没传任何投递参数（`delivery_mode` / `delivery_channel_id` / `delivery_peer_id` / `delivery_webhook_url` / `delivery_session_id` 全空）才回填；会话来自非 iLink 渠道或无映射时仍默认 `none`。
9. **限流**：`send_to_weixin` 复用"按会话最小间隔"逻辑（抽出 `checkSessionRateLimit`），但与 `send_to_wecom` **分开计数**，避免同一轮先后调用两个工具互相拦截。
10. **Cron `agent_turn` 路径**（`AgentService.Chat`）同样注册 `send_to_weixin`；该路径没有会话，必须显式给 `peer_id`。
11. **Web**：编辑旧任务时若已保存的接收人已被取消放行，下拉仍显示该项并标注"（未放行）"，保存会被后端拒绝；非微信投递时提交 `delivery_peer_id: ""` 以清除残留值。

## 文件结构

| 文件 | 职责 |
|------|------|
| `portal/internal/ilink/text.go`（新） + `text_test.go`（新） | `PlainText`（Markdown → 纯文本）、`SplitText`（按 rune 切分，默认 4000） |
| `portal/internal/biz/weixin_ilink.go` | `WeixinIlinkRepo.GetPeer` |
| `portal/internal/biz/channel.go` | `ChannelRepo.ListIlinkByAgent` |
| `portal/internal/biz/channel_peer.go` | `ChannelPeerSessionRepo.GetBySessionID` |
| `portal/internal/data/channel_ilink_mysql.go` / `channel_mysql.go` / `channel_peer_mysql.go` | 上述三个方法的 GORM 实现 |
| `portal/internal/data/*_test.go` | 仓储测试（`channel_peer_mysql_test.go` 新建） |
| `portal/internal/biz/agent_route_test.go`、`channel_allowed_agents_test.go`、`channel_peer_test.go`、`portal/internal/service/channel_test.go` | 测试替身补新接口方法 |
| `portal/internal/biz/weixin_ilink_usecase.go` | 新字段 `newSender`、`peers` |
| `portal/internal/biz/weixin_ilink_push.go`（新） + 测试 | `IlinkSender`、`Push`、Push 错误 |
| `portal/internal/data/model/cron.go`、`biz/cron.go`、`data/cron_mysql.go` | `delivery_peer_id` |
| `portal/migrations/024_cron_delivery_peer.sql`（新） | 手工 DDL 记录 |
| `portal/api/cron/v1/cron.proto` + `cron.pb.go`（生成） + `portal/openapi.yaml`（生成） | create / reply 新字段 |
| `portal/internal/cron/cron_service.go` + `cron_service_test.go`（新） | 字段映射 |
| `portal/internal/biz/weixin_ilink_origin.go`（新） | 会话 → 微信渠道 + 原始对端 |
| `portal/internal/biz/cron_usecase.go`、`cron_ilink.go`（新） + 测试 | 校验、`SetWeixinIlink`、`IlinkOrigin` |
| `portal/internal/cron/executor.go` + `executor_ilink_test.go`（新） | `weixin_ilink` 投递 |
| `framework/tool/cronjob_tool.go` + 测试 | `delivery_peer_id`；不再默认 `none` |
| `portal/internal/chat/cronjob_wiring.go` + `cronjob_wiring_ilink_test.go`（新） | 推回发起人回填 |
| `portal/internal/biz/weixin_ilink_send.go`（新） + 测试 | `SetPeerSessions`、`ChannelsForAgent`、`ResolveSendTarget` |
| `portal/internal/chat/weixin_wiring.go`（新） + 测试、`wecom_wiring.go`、`catalog_wiring.go`、`channel_catalog.go` | `send_to_weixin`、限流抽取、目录多渠道 |
| `portal/internal/service/weixin_bind.go`（新） + 测试、`chat.go`、`agent.go` | 按 Agent 注册工具与目录绑定 |
| `portal/cmd/backend/wire_gen.go` | 手工接线 |
| `web/src/utils/ilink.ts`、`web/tests/ilink.test.ts`、`web/src/api/client.ts`、`web/src/pages/CronTaskForm.tsx` | Cron 接收人下拉 |

---

### Task 0: 基线

**Files:** 无

- [ ] **Step 1: 记录基线测试结果**

```powershell
Set-Location portal
Remove-Item Env:GOROOT -ErrorAction SilentlyContinue
go build ./...
go test ./internal/biz/ ./internal/data/ ./internal/cron/ ./internal/chat/ ./internal/service/ ./internal/server/ ./internal/runtime/ ./internal/ilink/ -count=1 2>&1 | Select-String -Pattern "^(ok|FAIL|---)"
Set-Location ../framework
go test ./tool/ -count=1 2>&1 | Select-String -Pattern "^(ok|FAIL|---)"
Set-Location ../web
npm ci
npm test
Set-Location ../portal
```

Expected: 除 `internal/chat` 外全部 `ok`。`internal/chat` 已知在改动前就失败：`TestToolDiscoveryIntegration_AskUserBlockedForWecomWebhook`（稳定失败）、`TestNotifySessionMessageIndexed_WithDetachedCaller`（全量并行时偶发失败）。后续任务只要求**不新增**失败。

---

### Task 1: `internal/ilink` 文本整形与切分

**Files:**
- Create: `portal/internal/ilink/text.go`
- Test: `portal/internal/ilink/text_test.go`

- [ ] **Step 1: 写失败测试**

`portal/internal/ilink/text_test.go`：

````go
package ilink

import (
	"strings"
	"testing"
	"unicode/utf8"
)

func TestPlainText(t *testing.T) {
	in := "# 标题\n\n**加粗** 与 __下划线__\n\n```go\nfmt.Println(\"**keep**\")\n```\n## 小节\n正文"
	want := "标题\n\n加粗 与 下划线\n\nfmt.Println(\"**keep**\")\n小节\n正文"
	if got := PlainText(in); got != want {
		t.Fatalf("PlainText =\n%q\nwant\n%q", got, want)
	}
}

func TestPlainText_KeepsPlainAndCRLF(t *testing.T) {
	if got := PlainText("a_b c#d\r\n#nospace"); got != "a_b c#d\n#nospace" {
		t.Fatalf("PlainText = %q", got)
	}
}

func TestSplitText_ShortAndBlank(t *testing.T) {
	if got := SplitText("  hi  ", 10); len(got) != 1 || got[0] != "hi" {
		t.Fatalf("short = %q", got)
	}
	if got := SplitText(" \n ", 10); len(got) != 0 {
		t.Fatalf("blank = %q", got)
	}
}

func TestSplitText_HardCutByRunes(t *testing.T) {
	s := strings.Repeat("字", 9)
	got := SplitText(s, 4)
	if len(got) != 3 || got[0] != "字字字字" || got[2] != "字" {
		t.Fatalf("parts = %q", got)
	}
}

func TestSplitText_PrefersNewline(t *testing.T) {
	got := SplitText("aaaa\nbbbbbb", 8)
	if len(got) != 2 || got[0] != "aaaa" || got[1] != "bbbbbb" {
		t.Fatalf("parts = %q", got)
	}
}

func TestSplitText_DefaultLimit(t *testing.T) {
	s := strings.Repeat("中", DefaultSplitRunes+1)
	got := SplitText(s, 0)
	if len(got) != 2 || utf8.RuneCountInString(got[0]) != DefaultSplitRunes || got[1] != "中" {
		t.Fatalf("parts lens = %d", len(got))
	}
}
````

- [ ] **Step 2: 运行确认失败**

Run: `go test ./internal/ilink/ -run "TestPlainText|TestSplitText" -count=1`  
Expected: FAIL，`undefined: PlainText` / `undefined: SplitText`。

- [ ] **Step 3: 实现**

`portal/internal/ilink/text.go`：

````go
package ilink

import (
	"regexp"
	"strings"
)

// DefaultSplitRunes is the per-message limit; Phase 0 delivered 4000 CJK runes (~11.9 KB) intact.
const DefaultSplitRunes = 4000

var (
	headingRe = regexp.MustCompile(`^\s{0,3}#{1,6}\s+`)
	fenceRe   = regexp.MustCompile("^\\s*(```|~~~)")
)

// PlainText simplifies Markdown for WeChat, which renders text only: it drops heading marks,
// bold markers and code fences while keeping code block contents verbatim.
func PlainText(md string) string {
	lines := strings.Split(strings.ReplaceAll(md, "\r\n", "\n"), "\n")
	out := make([]string, 0, len(lines))
	inFence := false
	for _, line := range lines {
		if fenceRe.MatchString(line) {
			inFence = !inFence
			continue
		}
		if inFence {
			out = append(out, line)
			continue
		}
		line = headingRe.ReplaceAllString(line, "")
		line = strings.ReplaceAll(line, "**", "")
		line = strings.ReplaceAll(line, "__", "")
		out = append(out, line)
	}
	return strings.TrimSpace(strings.Join(out, "\n"))
}

// SplitText cuts s into parts of at most maxRunes runes (<=0 means DefaultSplitRunes), preferring
// a newline in the second half of each window. Blank input yields no parts.
func SplitText(s string, maxRunes int) []string {
	if maxRunes <= 0 {
		maxRunes = DefaultSplitRunes
	}
	r := []rune(strings.TrimSpace(s))
	var parts []string
	for len(r) > 0 {
		if len(r) <= maxRunes {
			parts = append(parts, string(r))
			break
		}
		cut := maxRunes
		for i := maxRunes; i > maxRunes/2; i-- {
			if r[i-1] == '\n' {
				cut = i
				break
			}
		}
		if part := strings.TrimSpace(string(r[:cut])); part != "" {
			parts = append(parts, part)
		}
		r = r[cut:]
		for len(r) > 0 && (r[0] == '\n' || r[0] == '\r') {
			r = r[1:]
		}
	}
	return parts
}
````

- [ ] **Step 4: 运行确认通过**

```powershell
gofmt -w internal/ilink/text.go internal/ilink/text_test.go
go test ./internal/ilink/ -count=1
```

Expected: `ok  backend/internal/ilink`。

- [ ] **Step 5: Commit**

```powershell
git add internal/ilink/text.go internal/ilink/text_test.go
git commit -m "feat(ilink): 出站文本 Markdown 简化与按 4000 字切分"
```

---

### Task 2: 仓储新方法：`GetPeer` / `ListIlinkByAgent` / `GetBySessionID`

**Files:**
- Modify: `portal/internal/biz/weixin_ilink.go`、`portal/internal/biz/channel.go`、`portal/internal/biz/channel_peer.go`
- Modify: `portal/internal/data/channel_ilink_mysql.go`、`portal/internal/data/channel_mysql.go`、`portal/internal/data/channel_peer_mysql.go`
- Modify（测试替身）: `portal/internal/biz/agent_route_test.go`、`portal/internal/biz/channel_allowed_agents_test.go`、`portal/internal/biz/channel_peer_test.go`、`portal/internal/service/channel_test.go`
- Test: `portal/internal/data/channel_ilink_mysql_test.go`、`portal/internal/data/channel_mysql_test.go`、`portal/internal/data/channel_peer_mysql_test.go`（新）

- [ ] **Step 1: 写失败测试**

`portal/internal/data/channel_ilink_mysql_test.go` 末尾追加：

```go
func TestIlinkRepo_GetPeer(t *testing.T) {
	db := openChannelTestDB(t)
	repo := newIlinkRepoForTest(db)
	ctx := context.Background()
	ch := seedIlinkChannel(t, db)

	if _, err := repo.GetPeer(ctx, ch.ID, "p1"); err != ErrNotFound {
		t.Fatalf("missing peer err = %v, want ErrNotFound", err)
	}
	if _, err := repo.UpsertPeerReport(ctx, ch.ID, 0, biz.IlinkPeerReport{PeerID: "p1", ContextToken: "tok", At: time.Now()}); err != nil {
		t.Fatalf("report: %v", err)
	}
	p, err := repo.GetPeer(ctx, ch.ID, "p1")
	if err != nil || p.PeerID != "p1" || p.ContextToken != "tok" || p.Allowed {
		t.Fatalf("GetPeer = %+v, %v", p, err)
	}
}
```

`portal/internal/data/channel_mysql_test.go` 末尾追加：

```go
func TestChannelRepo_ListIlinkByAgent(t *testing.T) {
	db := openChannelTestDB(t)
	repo := newChannelRepoForTest(db)
	ctx := context.Background()

	for _, in := range []*biz.ChannelCreate{
		{ChannelID: "wx-b", Type: "weixin_ilink", DefaultAgent: "a1", Enabled: true},
		{ChannelID: "wx-a", Type: "weixin_ilink", DefaultAgent: "a2", AllowedAgents: []string{"a2", "a1"}, Enabled: true},
		{ChannelID: "wx-off", Type: "weixin_ilink", DefaultAgent: "a1", Enabled: false},
		{ChannelID: "wx-other", Type: "weixin_ilink", DefaultAgent: "a3", Enabled: true},
		{ChannelID: "wecom-1", Type: "wecom", DefaultAgent: "a1", Enabled: true},
	} {
		if _, err := repo.Create(ctx, in); err != nil {
			t.Fatalf("Create %s: %v", in.ChannelID, err)
		}
	}

	got, err := repo.ListIlinkByAgent(ctx, "a1")
	if err != nil {
		t.Fatalf("ListIlinkByAgent: %v", err)
	}
	if len(got) != 2 || got[0].ChannelID != "wx-a" || got[1].ChannelID != "wx-b" {
		t.Fatalf("channels = %+v", got)
	}
	if none, err := repo.ListIlinkByAgent(ctx, "  "); err != nil || len(none) != 0 {
		t.Fatalf("blank agent = %+v, %v", none, err)
	}
}
```

`portal/internal/data/channel_peer_mysql_test.go`（新）：

```go
package data

import (
	"context"
	"testing"
	"time"

	"backend/internal/biz"
	"backend/internal/data/model"

	"github.com/go-kratos/kratos/v2/log"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

func newPeerSessionRepoForTest(t *testing.T) *channelPeerSessionRepo {
	t.Helper()
	db, err := gorm.Open(sqlite.Open("file:"+t.Name()+"?mode=memory&cache=shared"), &gorm.Config{})
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	if err := db.AutoMigrate(&model.ChannelPeerSession{}); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	return &channelPeerSessionRepo{db: db, log: log.NewHelper(log.DefaultLogger)}
}

func TestChannelPeerSessionRepo_GetBySessionID(t *testing.T) {
	repo := newPeerSessionRepoForTest(t)
	ctx := context.Background()
	t0 := time.Date(2026, 10, 18, 10, 0, 0, 0, time.UTC)

	if _, err := repo.GetBySessionID(ctx, "s1"); err != ErrNotFound {
		t.Fatalf("missing err = %v, want ErrNotFound", err)
	}
	for _, row := range []*biz.ChannelPeerSession{
		{ChannelID: "wx-1", PeerID: "user:old", SessionID: "s1", AgentID: "a1", CreatedAt: t0, UpdatedAt: t0},
		{ChannelID: "wx-2", PeerID: "user:new", SessionID: "s1", AgentID: "a1", CreatedAt: t0, UpdatedAt: t0.Add(time.Minute)},
		{ChannelID: "wx-1", PeerID: "user:x", SessionID: "s2", AgentID: "a1", CreatedAt: t0, UpdatedAt: t0.Add(time.Hour)},
	} {
		if err := repo.Create(ctx, row); err != nil {
			t.Fatalf("Create: %v", err)
		}
	}
	got, err := repo.GetBySessionID(ctx, "s1")
	if err != nil || got.ChannelID != "wx-2" || got.PeerID != "user:new" {
		t.Fatalf("GetBySessionID = %+v, %v", got, err)
	}
}
```

- [ ] **Step 2: 运行确认失败**

Run: `go test ./internal/data/ -run "TestIlinkRepo_GetPeer|TestChannelRepo_ListIlinkByAgent|TestChannelPeerSessionRepo_GetBySessionID" -count=1`  
Expected: FAIL（编译错误：`repo.GetPeer undefined`、`repo.ListIlinkByAgent undefined`、`repo.GetBySessionID undefined`）。

- [ ] **Step 3: 扩展 biz 接口**

`portal/internal/biz/weixin_ilink.go` 的 `WeixinIlinkRepo` 中，把

```go
	ListPeers(ctx context.Context, channelUUID string) ([]*IlinkPeer, error)
	UpdatePeer(ctx context.Context, channelUUID, peerID string, patch IlinkPeerPatch) (*IlinkPeer, error)
```

改为

```go
	ListPeers(ctx context.Context, channelUUID string) ([]*IlinkPeer, error)
	// GetPeer returns ErrNotFound when the peer is not in the roster.
	GetPeer(ctx context.Context, channelUUID, peerID string) (*IlinkPeer, error)
	UpdatePeer(ctx context.Context, channelUUID, peerID string, patch IlinkPeerPatch) (*IlinkPeer, error)
```

`portal/internal/biz/channel.go` 的 `ChannelRepo` 中，在 `ListGatewayChannels(ctx context.Context) ([]*ChannelMeta, error)` 之后插入：

```go
	// ListIlinkByAgent lists enabled weixin_ilink channels whose default or allowed agents include agentID.
	ListIlinkByAgent(ctx context.Context, agentID string) ([]*ChannelMeta, error)
```

`portal/internal/biz/channel_peer.go` 的 `ChannelPeerSessionRepo` 中，在 `Get(ctx context.Context, channelID, peerID string) (*ChannelPeerSession, error)` 之后插入：

```go
	// GetBySessionID returns the most recently updated mapping of sessionID, or ErrNotFound.
	GetBySessionID(ctx context.Context, sessionID string) (*ChannelPeerSession, error)
```

- [ ] **Step 4: data 层实现**

`portal/internal/data/channel_ilink_mysql.go`：在 `func (r *weixinIlinkRepo) UpdatePeer(` 之前插入：

```go
func (r *weixinIlinkRepo) GetPeer(ctx context.Context, channelUUID, peerID string) (*biz.IlinkPeer, error) {
	var row model.ChannelIlinkPeer
	err := r.db.WithContext(ctx).Where("channel_uuid = ? AND peer_id = ?", channelUUID, peerID).Take(&row).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	return ilinkPeerModelToBiz(&row), nil
}

```

`portal/internal/data/channel_mysql.go`：

1. import 块改为：

```go
import (
	"context"
	"slices"
	"strings"

	"backend/internal/biz"
	"backend/internal/data/model"

	"github.com/go-kratos/kratos/v2/log"
	"github.com/google/uuid"
	"gorm.io/gorm"
)
```

2. 在 `func (r *channelRepo) Update(` 之前插入：

```go
// ListIlinkByAgent filters in Go: allowed_agents is a JSON column and the candidate set is small.
func (r *channelRepo) ListIlinkByAgent(ctx context.Context, agentID string) ([]*biz.ChannelMeta, error) {
	agentID = strings.TrimSpace(agentID)
	if agentID == "" {
		return nil, nil
	}
	var list []model.Channel
	if err := r.db.WithContext(ctx).
		Where("type = ? AND enabled = ?", biz.ChannelTypeWeixinIlink, true).
		Order("channel_id").
		Find(&list).Error; err != nil {
		return nil, err
	}
	var out []*biz.ChannelMeta
	for i := range list {
		if list[i].DefaultAgent == agentID || slices.Contains([]string(list[i].AllowedAgents), agentID) {
			out = append(out, channelModelToBiz(&list[i]))
		}
	}
	return out, nil
}

```

`portal/internal/data/channel_peer_mysql.go`：在 `func (r *channelPeerSessionRepo) Create(` 之前插入：

```go
func (r *channelPeerSessionRepo) GetBySessionID(ctx context.Context, sessionID string) (*biz.ChannelPeerSession, error) {
	var m model.ChannelPeerSession
	if err := r.db.WithContext(ctx).
		Where("session_id = ?", sessionID).
		Order("updated_at DESC").
		Take(&m).Error; err != nil {
		if err == gorm.ErrRecordNotFound {
			return nil, ErrNotFound
		}
		return nil, err
	}
	return channelPeerModelToBiz(&m), nil
}

```

- [ ] **Step 5: 补测试替身**

`portal/internal/biz/agent_route_test.go`：

1. 在 `func (f *fakeRouteChannelRepo) Update(` 之前插入：

```go
func (f *fakeRouteChannelRepo) ListIlinkByAgent(context.Context, string) ([]*ChannelMeta, error) {
	return nil, nil
}
```

2. 在 `func (f *fakeRoutePeerRepo) Create(context.Context, *ChannelPeerSession) error { return nil }` 之前插入：

```go
func (f *fakeRoutePeerRepo) GetBySessionID(context.Context, string) (*ChannelPeerSession, error) {
	return nil, pkgErrors.ErrNotFound
}
```

`portal/internal/biz/channel_allowed_agents_test.go`：在 `func (f *fakeChannelRepo) Update(` 之前插入：

```go
func (f *fakeChannelRepo) ListIlinkByAgent(context.Context, string) ([]*ChannelMeta, error) {
	return nil, nil
}

```

`portal/internal/biz/channel_peer_test.go`：

1. 在 `func (f *fakeChannelPeerSessionRepo) Create(` 之前插入：

```go
func (f *fakeChannelPeerSessionRepo) GetBySessionID(_ context.Context, sessionID string) (*ChannelPeerSession, error) {
	var best *ChannelPeerSession
	for _, row := range f.rows {
		if row.SessionID == sessionID && (best == nil || row.UpdatedAt.After(best.UpdatedAt)) {
			best = row
		}
	}
	if best == nil {
		return nil, pkgErrors.ErrNotFound
	}
	cp := *best
	return &cp, nil
}

```

2. 在 `func (f *conflictCreatePeerRepo) Create(` 之前插入：

```go
func (f *conflictCreatePeerRepo) GetBySessionID(context.Context, string) (*ChannelPeerSession, error) {
	return nil, pkgErrors.ErrNotFound
}

```

`portal/internal/service/channel_test.go`：在 `func (r *channelCreateRepo) Update(` 之前插入：

```go
func (r *channelCreateRepo) ListIlinkByAgent(context.Context, string) ([]*biz.ChannelMeta, error) {
	return nil, nil
}
```

- [ ] **Step 6: 运行确认通过**

```powershell
gofmt -w internal/biz/weixin_ilink.go internal/biz/channel.go internal/biz/channel_peer.go internal/data/channel_ilink_mysql.go internal/data/channel_mysql.go internal/data/channel_peer_mysql.go internal/data/channel_ilink_mysql_test.go internal/data/channel_mysql_test.go internal/data/channel_peer_mysql_test.go internal/biz/agent_route_test.go internal/biz/channel_allowed_agents_test.go internal/biz/channel_peer_test.go internal/service/channel_test.go
go build ./...
go vet ./internal/biz/ ./internal/data/ ./internal/service/
go test ./internal/data/ ./internal/biz/ ./internal/service/ -count=1
```

Expected: 编译通过；三个包 `ok`。

- [ ] **Step 7: Commit**

```powershell
git add internal/biz/weixin_ilink.go internal/biz/channel.go internal/biz/channel_peer.go internal/data/channel_ilink_mysql.go internal/data/channel_mysql.go internal/data/channel_peer_mysql.go internal/data/channel_ilink_mysql_test.go internal/data/channel_mysql_test.go internal/data/channel_peer_mysql_test.go internal/biz/agent_route_test.go internal/biz/channel_allowed_agents_test.go internal/biz/channel_peer_test.go internal/service/channel_test.go
git commit -m "feat(data): weixin_ilink 名册单查、按 Agent 列渠道、按会话反查渠道对端"
```

---

### Task 3: `WeixinIlinkUsecase.Push`

**Files:**
- Modify: `portal/internal/biz/weixin_ilink_usecase.go`
- Create: `portal/internal/biz/weixin_ilink_push.go`
- Test: `portal/internal/biz/weixin_ilink_push_test.go`

- [ ] **Step 1: 写失败测试**

`portal/internal/biz/weixin_ilink_push_test.go`：

```go
package biz

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"backend/internal/ilink"
	pkgErrors "backend/internal/pkg/errors"
)

type pushFakeRepo struct {
	*ilinkFakeRepo
	peers map[string]*IlinkPeer
}

func (f *pushFakeRepo) GetPeer(_ context.Context, _ string, peerID string) (*IlinkPeer, error) {
	p, ok := f.peers[peerID]
	if !ok {
		return nil, pkgErrors.ErrNotFound
	}
	cp := *p
	return &cp, nil
}

type fakeIlinkSender struct {
	baseURL, token string
	sent           []string
	toks           []string
	errs           []error
}

func (f *fakeIlinkSender) SendText(_ context.Context, to, contextToken, text string) (string, error) {
	i := len(f.sent)
	f.sent = append(f.sent, to+"|"+text)
	f.toks = append(f.toks, contextToken)
	if i < len(f.errs) && f.errs[i] != nil {
		return "", f.errs[i]
	}
	return "mid-" + string(rune('a'+i)), nil
}

func newPushUsecaseForTest() (*WeixinIlinkUsecase, *pushFakeRepo, *fakeIlinkSender, *int) {
	expired := ilinkTestNow.Add(-time.Hour)
	channels := &ilinkFakeChannels{byID: map[string]*ChannelMeta{
		"uuid-1":       {ID: "uuid-1", ChannelID: "wx-1", Type: ChannelTypeWeixinIlink, Enabled: true, IlinkBotToken: "bt", IlinkBaseURL: "https://x.weixin.qq.com", IlinkGeneration: 3},
		"uuid-nologin": {ID: "uuid-nologin", ChannelID: "wx-2", Type: ChannelTypeWeixinIlink, Enabled: true},
		"uuid-expired": {ID: "uuid-expired", ChannelID: "wx-3", Type: ChannelTypeWeixinIlink, Enabled: true, IlinkBotToken: "bt", IlinkSessionExpiredAt: &expired},
		"uuid-off":     {ID: "uuid-off", ChannelID: "wx-4", Type: ChannelTypeWeixinIlink, IlinkBotToken: "bt"},
		"uuid-web":     {ID: "uuid-web", ChannelID: "web-1", Type: "web"},
	}}
	repo := &pushFakeRepo{ilinkFakeRepo: &ilinkFakeRepo{}, peers: map[string]*IlinkPeer{
		"p1":      {PeerID: "p1", Allowed: true, ContextToken: "ctx-1"},
		"blocked": {PeerID: "blocked", ContextToken: "ctx-2"},
		"silent":  {PeerID: "silent", Allowed: true},
	}}
	sender := &fakeIlinkSender{}
	created := 0
	uc := NewWeixinIlinkUsecaseWith(channels, repo, newAuthEphemeralFake(), newQRLogin(), nil)
	uc.now = func() time.Time { return ilinkTestNow }
	uc.newSender = func(baseURL, token string) IlinkSender {
		created++
		sender.baseURL, sender.token = baseURL, token
		return sender
	}
	return uc, repo, sender, &created
}

func TestIlinkPush_SplitsAndSends(t *testing.T) {
	uc, _, sender, _ := newPushUsecaseForTest()
	text := "# 日报\n" + strings.Repeat("字", ilink.DefaultSplitRunes)

	ids, err := uc.Push(context.Background(), "uuid-1", " p1 ", text)
	if err != nil {
		t.Fatalf("Push: %v", err)
	}
	if len(ids) != 2 || ids[0] != "mid-a" || ids[1] != "mid-b" {
		t.Fatalf("ids = %v", ids)
	}
	if sender.baseURL != "https://x.weixin.qq.com" || sender.token != "bt" {
		t.Fatalf("sender = %q / %q", sender.baseURL, sender.token)
	}
	if !strings.HasPrefix(sender.sent[0], "p1|日报\n") || sender.toks[0] != "ctx-1" || sender.toks[1] != "ctx-1" {
		t.Fatalf("sent = %q toks = %v", sender.sent[0][:20], sender.toks)
	}
}

func TestIlinkPush_RejectsWithoutCallingIlink(t *testing.T) {
	uc, _, _, created := newPushUsecaseForTest()
	ctx := context.Background()
	cases := []struct {
		channel, peer string
		check         func(error) bool
	}{
		{"uuid-1", "p9", func(err error) bool {
			return errors.Is(err, ErrIlinkPeerNotAllowed) && err.Error() == "目标 p9 未在白名单"
		}},
		{"uuid-1", "blocked", func(err error) bool { return errors.Is(err, ErrIlinkPeerNotAllowed) }},
		{"uuid-1", "silent", func(err error) bool { return errors.Is(err, ErrIlinkNoContextToken) }},
		{"uuid-nologin", "p1", func(err error) bool { return errors.Is(err, ErrIlinkLoginRequired) }},
		{"uuid-expired", "p1", func(err error) bool { return errors.Is(err, ErrIlinkLoginRequired) }},
		{"uuid-off", "p1", func(err error) bool { return err != nil && strings.Contains(err.Error(), "已停用") }},
		{"uuid-web", "p1", func(err error) bool { return errors.Is(err, ErrIlinkNotIlinkChannel) }},
		{"missing", "p1", func(err error) bool { return errors.Is(err, ErrChannelNotFound) }},
		{"uuid-1", " ", func(err error) bool { return isIlinkInvalidArgument(err) }},
	}
	for _, c := range cases {
		if _, err := uc.Push(ctx, c.channel, c.peer, "hi"); !c.check(err) {
			t.Errorf("Push(%s, %q) err = %v", c.channel, c.peer, err)
		}
	}
	if *created != 0 {
		t.Fatalf("iLink client created %d times", *created)
	}
	if _, err := uc.Push(ctx, "uuid-1", "p1", " \n "); err == nil {
		t.Fatal("want error for blank text")
	}
}

func TestIlinkPush_SessionExpiredMarksChannel(t *testing.T) {
	uc, repo, sender, _ := newPushUsecaseForTest()
	sender.errs = []error{&ilink.APIError{ErrCode: -14, ErrMsg: "session timeout"}}

	ids, err := uc.Push(context.Background(), "uuid-1", "p1", strings.Repeat("字", ilink.DefaultSplitRunes+1))
	if !errors.Is(err, ErrIlinkLoginRequired) || len(ids) != 0 {
		t.Fatalf("Push = %v, %v", ids, err)
	}
	if len(repo.expiredGens) != 1 || repo.expiredGens[0] != 3 {
		t.Fatalf("expiredGens = %v", repo.expiredGens)
	}
	if len(sender.sent) != 1 {
		t.Fatalf("sent %d parts after -14", len(sender.sent))
	}
}

func TestIlinkPush_NoRetryOnFailure(t *testing.T) {
	uc, repo, sender, _ := newPushUsecaseForTest()
	sender.errs = []error{nil, errors.New("boom")}

	ids, err := uc.Push(context.Background(), "uuid-1", "p1", strings.Repeat("字", ilink.DefaultSplitRunes*3))
	if err == nil || !strings.Contains(err.Error(), "2/3") || len(ids) != 1 {
		t.Fatalf("Push = %v, %v", ids, err)
	}
	if len(sender.sent) != 2 || len(repo.expiredGens) != 0 {
		t.Fatalf("sent = %d expired = %v", len(sender.sent), repo.expiredGens)
	}
}
```

- [ ] **Step 2: 运行确认失败**

Run: `go test ./internal/biz/ -run TestIlinkPush -count=1`  
Expected: FAIL（编译错误：`uc.newSender undefined`、`undefined: IlinkSender`、`uc.Push undefined` 等）。

- [ ] **Step 3: 实现**

`portal/internal/biz/weixin_ilink_usecase.go`：

1. `WeixinIlinkUsecase` 结构体改为：

```go
type WeixinIlinkUsecase struct {
	channels  ChannelRepo
	repo      WeixinIlinkRepo
	ephemeral AuthEphemeralRepo
	peers     ChannelPeerSessionRepo
	login     IlinkLoginClient
	qrImage   func(content string) (string, error)
	newSender func(baseURL, token string) IlinkSender
	now       func() time.Time
}
```

（`peers` 在 Task 10 才使用，先加上避免再次改结构体。）

2. `NewWeixinIlinkUsecaseWith` 的返回值改为：

```go
	return &WeixinIlinkUsecase{
		channels:  channels,
		repo:      repo,
		ephemeral: ephemeral,
		login:     login,
		qrImage:   qrImage,
		newSender: func(baseURL, token string) IlinkSender { return ilink.NewClient(baseURL, token) },
		now:       time.Now,
	}
```

`portal/internal/biz/weixin_ilink_push.go`：

```go
package biz

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"backend/internal/ilink"
	pkgErrors "backend/internal/pkg/errors"
)

// IlinkSender is the outbound subset of the iLink client (*ilink.Client satisfies it).
type IlinkSender interface {
	SendText(ctx context.Context, to, contextToken, text string) (string, error)
}

// Push errors are plain errors: their text is stored as cron delivery_error and shown to the agent.
var (
	ErrIlinkLoginRequired  = errors.New("微信 bot 需重新扫码登录")
	ErrIlinkPeerNotAllowed = errors.New("未在白名单")
	ErrIlinkNoContextToken = errors.New("对方尚未与 bot 对话（或 bot 已重新登录），请对方先给 bot 发一条消息")
)

// Push sends text to an allowed roster peer of a weixin_ilink channel (channelUUID is channels.id).
// Markdown is flattened and split into ilink.DefaultSplitRunes parts. There is no retry: a failed
// sendmessage may still have been delivered. It returns the message ids of the parts sent before any error.
func (uc *WeixinIlinkUsecase) Push(ctx context.Context, channelUUID, peerID, text string) ([]string, error) {
	peerID = strings.TrimSpace(peerID)
	if err := ilinkValidPeerID(peerID); err != nil {
		return nil, err
	}
	ch, err := uc.channelByID(ctx, channelUUID)
	if err != nil {
		return nil, err
	}
	if !ch.Enabled {
		return nil, fmt.Errorf("微信渠道 %s 已停用", ch.ChannelID)
	}
	if ch.IlinkBotToken == "" || ch.IlinkSessionExpiredAt != nil {
		return nil, ErrIlinkLoginRequired
	}
	peer, err := uc.repo.GetPeer(ctx, ch.ID, peerID)
	if errors.Is(err, pkgErrors.ErrNotFound) || (err == nil && !peer.Allowed) {
		return nil, fmt.Errorf("目标 %s %w", peerID, ErrIlinkPeerNotAllowed)
	}
	if err != nil {
		return nil, err
	}
	if peer.ContextToken == "" {
		return nil, ErrIlinkNoContextToken
	}
	parts := ilink.SplitText(ilink.PlainText(text), ilink.DefaultSplitRunes)
	if len(parts) == 0 {
		return nil, errors.New("推送内容为空")
	}
	sender := uc.newSender(ch.IlinkBaseURL, ch.IlinkBotToken)
	ids := make([]string, 0, len(parts))
	for i, part := range parts {
		id, err := sender.SendText(ctx, peerID, peer.ContextToken, part)
		if err != nil {
			if ilink.IsSessionExpired(err) {
				if _, markErr := uc.repo.MarkSessionExpired(ctx, ch.ID, ch.IlinkGeneration, uc.now()); markErr != nil {
					return ids, fmt.Errorf("%w（会话已过期，标记失效失败：%v）", ErrIlinkLoginRequired, markErr)
				}
				return ids, fmt.Errorf("%w（会话已过期）", ErrIlinkLoginRequired)
			}
			return ids, fmt.Errorf("发送第 %d/%d 段失败：%w", i+1, len(parts), err)
		}
		ids = append(ids, id)
	}
	return ids, nil
}
```

- [ ] **Step 4: 运行确认通过**

```powershell
gofmt -w internal/biz/weixin_ilink_usecase.go internal/biz/weixin_ilink_push.go internal/biz/weixin_ilink_push_test.go
go vet ./internal/biz/
go test ./internal/biz/ -count=1
```

Expected: `ok  backend/internal/biz`（含 P1 的 `TestIlink*`）。

- [ ] **Step 5: Commit**

```powershell
git add internal/biz/weixin_ilink_usecase.go internal/biz/weixin_ilink_push.go internal/biz/weixin_ilink_push_test.go
git commit -m "feat(biz): WeixinIlinkUsecase.Push 出站推送（白名单 / token / 需重扫前置校验，-14 标记失效，不重试）"
```

---

### Task 4: `cron_tasks.delivery_peer_id` 数据层

**Files:**
- Modify: `portal/internal/data/model/cron.go`、`portal/internal/biz/cron.go`、`portal/internal/data/cron_mysql.go`
- Create: `portal/migrations/024_cron_delivery_peer.sql`
- Test: `portal/internal/data/cron_mysql_test.go`

- [ ] **Step 1: 写失败测试**

`portal/internal/data/cron_mysql_test.go` 末尾追加：

```go
func TestCronTaskRepo_DeliveryPeerID(t *testing.T) {
	ctx := context.Background()
	repo, _ := newCronTaskRepoForTest(t)
	task, err := repo.Create(ctx, &biz.CronTaskCreate{
		Name: "wx", AgentID: "a1", ScheduleKind: "cron", ScheduleExpr: "0 9 * * *", Timezone: "UTC",
		PayloadKind: "agent_turn", PayloadContent: "hi", TimeoutSec: 60,
		DeliveryMode: "channel", DeliveryChannelID: "uuid-1", DeliveryPeerID: "p1@im.wechat", Enabled: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	if task.DeliveryPeerID != "p1@im.wechat" {
		t.Fatalf("created peer = %q", task.DeliveryPeerID)
	}
	updated, err := repo.Update(ctx, task.ID, map[string]any{"delivery_peer_id": "p2@im.wechat"})
	if err != nil || updated.DeliveryPeerID != "p2@im.wechat" {
		t.Fatalf("updated = %+v, %v", updated, err)
	}
}
```

- [ ] **Step 2: 运行确认失败**

Run: `go test ./internal/data/ -run TestCronTaskRepo_DeliveryPeerID -count=1`  
Expected: FAIL（编译错误：`unknown field DeliveryPeerID`）。

- [ ] **Step 3: 实现**

`portal/internal/data/model/cron.go` 的 `CronTask` 中，在 `DeliveryChannelID` 行之后插入：

```go
	DeliveryPeerID     string     `gorm:"column:delivery_peer_id;size:128"` // weixin_ilink 原始对端 ID
```

（`model.CronTask` 已在 `data.go` 的 AutoMigrate 列表中，启动时自动加列。）

`portal/internal/biz/cron.go`：

1. `CronTaskCreate` 中，在 `DeliveryChannelID  string // 投递到渠道（wxpusher 等）` 之后插入：

```go
	DeliveryPeerID     string // weixin_ilink 渠道的原始对端 ID（无 user: 前缀）
```

2. `CronTaskMeta` 中，在 `DeliveryChannelID  string` 之后插入：

```go
	DeliveryPeerID     string
```

`portal/internal/data/cron_mysql.go`：

1. `Create` 构造 `model.CronTask` 时，在 `DeliveryChannelID:  t.DeliveryChannelID,` 之后插入：

```go
		DeliveryPeerID:     t.DeliveryPeerID,
```

2. `cronTaskModelToBiz` 中，在 `DeliveryChannelID:  m.DeliveryChannelID,` 之后插入：

```go
		DeliveryPeerID:     m.DeliveryPeerID,
```

`portal/migrations/024_cron_delivery_peer.sql`：

```sql
-- portal/migrations/024_cron_delivery_peer.sql
-- Cron 投递到 weixin_ilink 渠道时的接收人（docs/superpowers/specs/2026-10-18-weixin-ilink-channel-design.md §5.4）。
-- 启动时 GORM AutoMigrate 自动加列；本文件仅作手工修复与记录。
ALTER TABLE cron_tasks
  ADD COLUMN delivery_peer_id VARCHAR(128) NULL;
```

- [ ] **Step 4: 运行确认通过**

```powershell
gofmt -w internal/data/model/cron.go internal/biz/cron.go internal/data/cron_mysql.go internal/data/cron_mysql_test.go
go build ./...
go test ./internal/data/ -count=1
```

Expected: `ok  backend/internal/data`。

- [ ] **Step 5: Commit**

```powershell
git add internal/data/model/cron.go internal/biz/cron.go internal/data/cron_mysql.go internal/data/cron_mysql_test.go migrations/024_cron_delivery_peer.sql
git commit -m "feat(cron): cron_tasks 新增 delivery_peer_id"
```

---

### Task 5: `cron.proto` 字段与 CronService 映射

**Files:**
- Modify: `portal/api/cron/v1/cron.proto`、`portal/api/cron/v1/cron.pb.go`（生成）、`portal/openapi.yaml`（生成）、`portal/internal/cron/cron_service.go`
- Test: `portal/internal/cron/cron_service_test.go`（新）

`UpdateCronTask` 的 `updates` 是 `google.protobuf.Struct`，无需改 proto；create 与 reply 需要新字段。

- [ ] **Step 1: 改 proto 并生成**

`portal/api/cron/v1/cron.proto`：

1. `message CreateCronTaskRequest` 中，在 `bool enabled = 18;` 之后加：

```proto
  // weixin_ilink 渠道的接收人（原始微信用户 ID）；delivery_mode=channel 且渠道为 weixin_ilink 时必填
  string delivery_peer_id = 19;
```

2. `message CronTaskReply` 中，在 `string updated_at = 21;` 之后加：

```proto
  string delivery_peer_id = 22;
```

生成（在 `portal/`）：

```powershell
protoc --proto_path=./api --proto_path=./third_party --go_out=paths=source_relative:./api --go-http_out=paths=source_relative:./api --go-grpc_out=paths=source_relative:./api cron/v1/cron.proto
$root = (Resolve-Path api).Path
$protos = Get-ChildItem api -Recurse -Filter *.proto | ForEach-Object { $_.FullName.Substring($root.Length + 1).Replace('\', '/') }
protoc --proto_path=./api --proto_path=./third_party --openapi_out=fq_schema_naming=true,default_response=false:. $protos
git diff --stat -- api openapi.yaml
```

Expected: `git diff --stat` 只列出 `api/cron/v1/cron.proto`、`api/cron/v1/cron.pb.go`、`openapi.yaml`（`openapi.yaml` 两处各加 `deliveryPeerId`；protoc 会打印一条既有的 `WARN: POST /api/v1/cron/tasks/{id}/run does not declare a body.`，可忽略）。`git status` 可能还把 `cron_grpc.pb.go` / `cron_http.pb.go` 标为修改——那只是生成器写出 LF 换行（`core.autocrlf=true`），内容无变化，用下面的命令还原；若其它文件出现真实差异，同样还原：

```powershell
git checkout -- api/cron/v1/cron_grpc.pb.go api/cron/v1/cron_http.pb.go
```

- [ ] **Step 2: 写失败测试**

`portal/internal/cron/cron_service_test.go`：

```go
package cron

import (
	"context"
	"testing"

	cronv1 "backend/api/cron/v1"
	"backend/internal/biz"

	"github.com/go-kratos/kratos/v2/log"
)

type captureCronRepo struct {
	biz.CronTaskRepo
	last *biz.CronTaskCreate
}

func (r *captureCronRepo) Create(_ context.Context, t *biz.CronTaskCreate) (*biz.CronTaskMeta, error) {
	cp := *t
	r.last = &cp
	return &biz.CronTaskMeta{ID: "t1", Name: t.Name, DeliveryMode: t.DeliveryMode, DeliveryChannelID: t.DeliveryChannelID, DeliveryPeerID: t.DeliveryPeerID}, nil
}

func TestCronService_CreateMapsDeliveryPeerID(t *testing.T) {
	repo := &captureCronRepo{}
	s := NewCronService(biz.NewCronUsecase(repo, newMemRunRepo(), log.DefaultLogger), nil)

	reply, err := s.CreateCronTask(context.Background(), &cronv1.CreateCronTaskRequest{
		Name: "wx", AgentId: "a1", ScheduleKind: "cron", ScheduleExpr: "0 9 * * *",
		PayloadKind: "agent_turn", PayloadContent: "hi",
		DeliveryMode: "channel", DeliveryChannelId: "uuid-1", DeliveryPeerId: "p1@im.wechat",
	})
	if err != nil {
		t.Fatalf("CreateCronTask: %v", err)
	}
	if repo.last.DeliveryPeerID != "p1@im.wechat" {
		t.Fatalf("create input peer = %q", repo.last.DeliveryPeerID)
	}
	if reply.GetDeliveryPeerId() != "p1@im.wechat" {
		t.Fatalf("reply peer = %q", reply.GetDeliveryPeerId())
	}
}
```

- [ ] **Step 3: 运行确认失败**

Run: `go test ./internal/cron/ -run TestCronService_CreateMapsDeliveryPeerID -count=1`  
Expected: FAIL（`create input peer = ""`）。

- [ ] **Step 4: 实现**

`portal/internal/cron/cron_service.go`：

1. `cronTaskMetaToReply` 中，在 `DeliveryChannelId:  t.DeliveryChannelID,` 之后插入：

```go
		DeliveryPeerId:     t.DeliveryPeerID,
```

2. `CreateCronTask` 构造 `biz.CronTaskCreate` 时，在 `DeliveryChannelID:  req.GetDeliveryChannelId(),` 之后插入：

```go
		DeliveryPeerID:     req.GetDeliveryPeerId(),
```

- [ ] **Step 5: 运行确认通过**

```powershell
gofmt -w internal/cron/cron_service.go internal/cron/cron_service_test.go
go build ./...
go test ./internal/cron/ -count=1
```

Expected: `ok  backend/internal/cron`。

- [ ] **Step 6: Commit**

```powershell
git add api/cron/v1/cron.proto api/cron/v1/cron.pb.go openapi.yaml internal/cron/cron_service.go internal/cron/cron_service_test.go
git commit -m "feat(cron): Cron API create / reply 支持 delivery_peer_id"
```

---

### Task 6: `CronUsecase` 微信投递校验与会话反查

**Files:**
- Create: `portal/internal/biz/weixin_ilink_origin.go`、`portal/internal/biz/cron_ilink.go`
- Modify: `portal/internal/biz/cron_usecase.go`、`portal/cmd/backend/wire_gen.go`
- Test: `portal/internal/biz/cron_ilink_test.go`

- [ ] **Step 1: 写失败测试**

`portal/internal/biz/cron_ilink_test.go`（复用同包测试替身：Task 3 的 `pushFakeRepo`、P1 的 `ilinkFakeChannels`、Task 2 的 `fakeChannelPeerSessionRepo.GetBySessionID`）：

```go
package biz

import (
	"context"
	"strings"
	"testing"
	"time"

	pkgErrors "backend/internal/pkg/errors"

	"github.com/go-kratos/kratos/v2/log"
)

type cronIlinkTaskRepo struct {
	CronTaskRepo
	tasks   map[string]*CronTaskMeta
	created []*CronTaskCreate
	updated []map[string]any
}

func (r *cronIlinkTaskRepo) Create(_ context.Context, t *CronTaskCreate) (*CronTaskMeta, error) {
	cp := *t
	r.created = append(r.created, &cp)
	return &CronTaskMeta{ID: "t-new", DeliveryMode: t.DeliveryMode, DeliveryChannelID: t.DeliveryChannelID, DeliveryPeerID: t.DeliveryPeerID}, nil
}

func (r *cronIlinkTaskRepo) GetByID(_ context.Context, id string) (*CronTaskMeta, error) {
	t, ok := r.tasks[id]
	if !ok {
		return nil, pkgErrors.ErrNotFound
	}
	cp := *t
	return &cp, nil
}

func (r *cronIlinkTaskRepo) Update(_ context.Context, id string, updates map[string]any) (*CronTaskMeta, error) {
	r.updated = append(r.updated, updates)
	return r.GetByID(context.Background(), id)
}

func newCronIlinkUsecaseForTest() (*CronUsecase, *cronIlinkTaskRepo) {
	channels := &ilinkFakeChannels{byID: map[string]*ChannelMeta{
		"uuid-1":   {ID: "uuid-1", ChannelID: "wx-1", Type: ChannelTypeWeixinIlink, Enabled: true},
		"uuid-wcm": {ID: "uuid-wcm", ChannelID: "wecom-1", Type: "wecom", Enabled: true},
	}}
	roster := &pushFakeRepo{ilinkFakeRepo: &ilinkFakeRepo{}, peers: map[string]*IlinkPeer{
		"p1":      {PeerID: "p1", Allowed: true},
		"blocked": {PeerID: "blocked"},
	}}
	now := time.Now()
	peers := &fakeChannelPeerSessionRepo{rows: map[string]*ChannelPeerSession{
		peerKey("wx-1", "user:p1"):     {ChannelID: "wx-1", PeerID: "user:p1", SessionID: "s-wx", UpdatedAt: now},
		peerKey("wecom-1", "user:p2"):  {ChannelID: "wecom-1", PeerID: "user:p2", SessionID: "s-wecom", UpdatedAt: now},
		peerKey("gone-1", "user:p3"):   {ChannelID: "gone-1", PeerID: "user:p3", SessionID: "s-gone", UpdatedAt: now},
		peerKey("wx-1", "user:older"):  {ChannelID: "wx-1", PeerID: "user:older", SessionID: "s-multi", UpdatedAt: now.Add(-time.Hour)},
		peerKey("wx-1", "user:latest"): {ChannelID: "wx-1", PeerID: "user:latest", SessionID: "s-multi", UpdatedAt: now},
	}}
	tasks := &cronIlinkTaskRepo{tasks: map[string]*CronTaskMeta{
		"t-wx":   {ID: "t-wx", DeliveryMode: "channel", DeliveryChannelID: "uuid-1", DeliveryPeerID: "blocked"},
		"t-none": {ID: "t-none", DeliveryMode: "none"},
	}}
	uc := NewCronUsecase(tasks, nil, log.DefaultLogger)
	uc.SetWeixinIlink(channels, peers, roster)
	return uc, tasks
}

func TestCronCreate_IlinkDeliveryValidation(t *testing.T) {
	uc, tasks := newCronIlinkUsecaseForTest()
	ctx := context.Background()
	create := func(channelID, peer string) error {
		_, err := uc.Create(ctx, &CronTaskCreate{Name: "n", DeliveryMode: "channel", DeliveryChannelID: channelID, DeliveryPeerID: peer})
		return err
	}

	if err := create("uuid-1", ""); !isIlinkInvalidArgument(err) || !strings.Contains(err.Error(), "delivery_peer_id") {
		t.Fatalf("missing peer err = %v", err)
	}
	for _, peer := range []string{"p9", "blocked"} {
		if err := create("uuid-1", peer); !isIlinkInvalidArgument(err) || !strings.Contains(err.Error(), "目标 "+peer+" 未在白名单") {
			t.Fatalf("peer %s err = %v", peer, err)
		}
	}
	if err := create("uuid-1", strings.Repeat("字", ilinkPeerIDMaxRunes+1)); !isIlinkInvalidArgument(err) {
		t.Fatalf("long peer err = %v", err)
	}
	if len(tasks.created) != 0 {
		t.Fatalf("created = %d, want none", len(tasks.created))
	}
	if err := create("uuid-1", " p1 "); err != nil {
		t.Fatalf("allowed peer err = %v", err)
	}
	if got := tasks.created[0].DeliveryPeerID; got != "p1" {
		t.Fatalf("stored peer = %q, want trimmed", got)
	}
	if err := create("uuid-wcm", ""); err != nil {
		t.Fatalf("wecom channel err = %v", err)
	}
	if err := create("missing", ""); err != nil {
		t.Fatalf("unknown channel err = %v", err)
	}
}

func TestCronCreate_SkipsIlinkValidationWhenNotWired(t *testing.T) {
	tasks := &cronIlinkTaskRepo{}
	uc := NewCronUsecase(tasks, nil, log.DefaultLogger)
	if _, err := uc.Create(context.Background(), &CronTaskCreate{DeliveryMode: "channel", DeliveryChannelID: "uuid-1"}); err != nil {
		t.Fatalf("err = %v", err)
	}
}

func TestCronUpdate_IlinkDeliveryValidation(t *testing.T) {
	uc, tasks := newCronIlinkUsecaseForTest()
	ctx := context.Background()

	if _, err := uc.Update(ctx, "t-wx", map[string]any{"enabled": false}); err != nil {
		t.Fatalf("pause with a stale peer err = %v", err)
	}
	if _, err := uc.Update(ctx, "t-wx", map[string]any{"name": "x", "delivery_peer_id": "blocked"}); !isIlinkInvalidArgument(err) {
		t.Fatalf("disallowed peer err = %v", err)
	}
	if _, err := uc.Update(ctx, "t-none", map[string]any{"delivery_mode": "channel", "delivery_channel_id": "uuid-1"}); !isIlinkInvalidArgument(err) {
		t.Fatalf("switch to weixin without peer err = %v", err)
	}
	updates := map[string]any{"delivery_peer_id": " p1 "}
	if _, err := uc.Update(ctx, "t-wx", updates); err != nil {
		t.Fatalf("allowed peer err = %v", err)
	}
	if updates["delivery_peer_id"] != "p1" {
		t.Fatalf("updates peer = %v, want trimmed", updates["delivery_peer_id"])
	}
	if _, err := uc.Update(ctx, "missing", map[string]any{"delivery_peer_id": "p1"}); err != ErrCronTaskNotFound {
		t.Fatalf("missing task err = %v", err)
	}
	if len(tasks.updated) != 2 {
		t.Fatalf("repo updates = %d, want 2", len(tasks.updated))
	}
}

func TestCronIlinkOrigin(t *testing.T) {
	uc, _ := newCronIlinkUsecaseForTest()
	ctx := context.Background()
	cases := map[string][2]string{
		"s-wx":    {"uuid-1", "p1"},
		"s-multi": {"uuid-1", "latest"},
		"s-wecom": {"", ""},
		"s-gone":  {"", ""},
		"unknown": {"", ""},
		"":        {"", ""},
	}
	for sessionID, want := range cases {
		ch, peer, err := uc.IlinkOrigin(ctx, sessionID)
		if err != nil || ch != want[0] || peer != want[1] {
			t.Errorf("IlinkOrigin(%q) = %q, %q, %v; want %v", sessionID, ch, peer, err, want)
		}
	}
}
```

- [ ] **Step 2: 运行确认失败**

Run: `go test ./internal/biz/ -run "TestCronCreate|TestCronUpdate|TestCronIlinkOrigin" -count=1`  
Expected: FAIL（编译错误：`uc.SetWeixinIlink undefined`、`uc.IlinkOrigin undefined`）。

- [ ] **Step 3: 实现会话反查（`send_to_weixin` 也会用）**

`portal/internal/biz/weixin_ilink_origin.go`：

```go
package biz

import (
	"context"
	"errors"
	"strings"

	pkgErrors "backend/internal/pkg/errors"
)

// ilinkPeerKeyPrefix prefixes raw iLink user IDs in channel_peer_sessions.peer_id (the Gateway peer key).
const ilinkPeerKeyPrefix = "user:"

// ilinkSessionOrigin finds the weixin_ilink channel and raw peer ID a chat session came from;
// it returns (nil, "", nil) when the session did not start in a weixin_ilink channel.
func ilinkSessionOrigin(ctx context.Context, peers ChannelPeerSessionRepo, channels ChannelRepo, sessionID string) (*ChannelMeta, string, error) {
	sessionID = strings.TrimSpace(sessionID)
	if peers == nil || channels == nil || sessionID == "" {
		return nil, "", nil
	}
	row, err := peers.GetBySessionID(ctx, sessionID)
	if errors.Is(err, pkgErrors.ErrNotFound) {
		return nil, "", nil
	}
	if err != nil {
		return nil, "", err
	}
	ch, err := channels.GetByChannelID(ctx, row.ChannelID)
	if errors.Is(err, pkgErrors.ErrNotFound) {
		return nil, "", nil
	}
	if err != nil {
		return nil, "", err
	}
	peerID := strings.TrimPrefix(row.PeerID, ilinkPeerKeyPrefix)
	if ch.Type != ChannelTypeWeixinIlink || peerID == "" {
		return nil, "", nil
	}
	return ch, peerID, nil
}
```

- [ ] **Step 4: 实现校验与 `IlinkOrigin`**

`portal/internal/biz/cron_ilink.go`：

```go
package biz

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"unicode/utf8"

	pkgErrors "backend/internal/pkg/errors"

	kratosErrors "github.com/go-kratos/kratos/v2/errors"
)

var cronDeliveryKeys = []string{"delivery_mode", "delivery_channel_id", "delivery_peer_id"}

// SetWeixinIlink enables weixin_ilink delivery validation and session origin lookup.
func (uc *CronUsecase) SetWeixinIlink(channels ChannelRepo, peers ChannelPeerSessionRepo, roster WeixinIlinkRepo) {
	uc.ilinkChannels = channels
	uc.ilinkPeers = peers
	uc.ilinkRoster = roster
}

// IlinkOrigin returns the weixin_ilink channel (channels.id) and raw peer ID that a chat session
// came from; both are empty when the session did not start in a weixin_ilink channel.
func (uc *CronUsecase) IlinkOrigin(ctx context.Context, sessionID string) (channelUUID, peerID string, err error) {
	ch, peer, err := ilinkSessionOrigin(ctx, uc.ilinkPeers, uc.ilinkChannels, sessionID)
	if err != nil || ch == nil {
		return "", "", err
	}
	return ch.ID, peer, nil
}

// validateIlinkUpdate checks the effective delivery only when the update touches it, so pausing a
// task whose peer was later removed from the roster still works.
func (uc *CronUsecase) validateIlinkUpdate(ctx context.Context, id string, updates map[string]any) error {
	if v, ok := updates["delivery_peer_id"].(string); ok {
		v = strings.TrimSpace(v)
		if utf8.RuneCountInString(v) > ilinkPeerIDMaxRunes {
			return kratosErrors.BadRequest("INVALID_ARGUMENT", "delivery_peer_id is too long")
		}
		updates["delivery_peer_id"] = v
	}
	if uc.ilinkChannels == nil || uc.ilinkRoster == nil {
		return nil
	}
	touched := false
	for _, k := range cronDeliveryKeys {
		if _, ok := updates[k]; ok {
			touched = true
		}
	}
	if !touched {
		return nil
	}
	cur, err := uc.taskRepo.GetByID(ctx, id)
	if errors.Is(err, pkgErrors.ErrNotFound) {
		return ErrCronTaskNotFound
	}
	if err != nil {
		return err
	}
	mode, channelID, peerID := cur.DeliveryMode, cur.DeliveryChannelID, cur.DeliveryPeerID
	if v, ok := updates["delivery_mode"].(string); ok {
		mode = v
	}
	if v, ok := updates["delivery_channel_id"].(string); ok {
		channelID = v
	}
	if v, ok := updates["delivery_peer_id"].(string); ok {
		peerID = v
	}
	return uc.validateIlinkDelivery(ctx, mode, channelID, peerID)
}

// validateIlinkDelivery requires an allowed roster peer when delivering to a weixin_ilink channel.
// Unknown channels are left to the executor, matching the other channel types.
func (uc *CronUsecase) validateIlinkDelivery(ctx context.Context, mode, channelID, peerID string) error {
	if utf8.RuneCountInString(peerID) > ilinkPeerIDMaxRunes {
		return kratosErrors.BadRequest("INVALID_ARGUMENT", "delivery_peer_id is too long")
	}
	if uc.ilinkChannels == nil || uc.ilinkRoster == nil || mode != "channel" || channelID == "" {
		return nil
	}
	ch, err := uc.ilinkChannels.GetByID(ctx, channelID)
	if errors.Is(err, pkgErrors.ErrNotFound) {
		return nil
	}
	if err != nil {
		return err
	}
	if ch.Type != ChannelTypeWeixinIlink {
		return nil
	}
	if peerID == "" {
		return kratosErrors.BadRequest("INVALID_ARGUMENT", "投递到微信渠道必须选择接收人（delivery_peer_id）")
	}
	peer, err := uc.ilinkRoster.GetPeer(ctx, ch.ID, peerID)
	if errors.Is(err, pkgErrors.ErrNotFound) || (err == nil && !peer.Allowed) {
		return kratosErrors.BadRequest("INVALID_ARGUMENT", fmt.Sprintf("目标 %s 未在白名单", peerID))
	}
	return err
}
```

`portal/internal/biz/cron_usecase.go`：

1. import 块加 `"strings"`：

```go
import (
	"context"
	"errors"
	"strings"
	"time"

	pkgErrors "backend/internal/pkg/errors"

	kratosErrors "github.com/go-kratos/kratos/v2/errors"
	"github.com/go-kratos/kratos/v2/log"
)
```

2. `CronUsecase` 结构体改为：

```go
type CronUsecase struct {
	taskRepo CronTaskRepo
	runRepo  CronRunRepo
	// weixin_ilink 投递校验与"推回发起人"反查；未注入时跳过（见 SetWeixinIlink）
	ilinkChannels ChannelRepo
	ilinkPeers    ChannelPeerSessionRepo
	ilinkRoster   WeixinIlinkRepo
	log           *log.Helper
}
```

3. `Create` 改为：

```go
func (uc *CronUsecase) Create(ctx context.Context, t *CronTaskCreate) (*CronTaskMeta, error) {
	t.DeliveryPeerID = strings.TrimSpace(t.DeliveryPeerID)
	if err := uc.validateIlinkDelivery(ctx, t.DeliveryMode, t.DeliveryChannelID, t.DeliveryPeerID); err != nil {
		return nil, err
	}
	return uc.taskRepo.Create(ctx, t)
}
```

4. `Update` 的第一行 `t, err := uc.taskRepo.Update(ctx, id, updates)` 之前插入：

```go
	if err := uc.validateIlinkUpdate(ctx, id, updates); err != nil {
		return nil, err
	}
```

- [ ] **Step 5: 接线**

`portal/cmd/backend/wire_gen.go`（**手工编辑，不要运行 wire**）：在 `runtimeService.SetIlink(weixinIlinkUsecase)` 这一行之后插入：

```go
	cronUsecase.SetWeixinIlink(channelRepo, channelPeerSessionRepo, weixinIlinkRepo)
```

- [ ] **Step 6: 运行确认通过**

```powershell
gofmt -w internal/biz/weixin_ilink_origin.go internal/biz/cron_ilink.go internal/biz/cron_ilink_test.go internal/biz/cron_usecase.go cmd/backend/wire_gen.go
go build ./...
go vet ./internal/biz/ ./cmd/backend/
go test ./internal/biz/ ./internal/cron/ ./internal/chat/ -count=1 2>&1 | Select-String -Pattern "^(ok|FAIL|---)"
```

Expected: `biz`、`cron` 为 `ok`；`chat` 只有 Task 0 记录的已知失败。

- [ ] **Step 7: Commit**

```powershell
git add internal/biz/weixin_ilink_origin.go internal/biz/cron_ilink.go internal/biz/cron_ilink_test.go internal/biz/cron_usecase.go cmd/backend/wire_gen.go
git commit -m "feat(cron): 投递到 weixin_ilink 时校验接收人白名单，并支持按会话反查微信渠道对端"
```

---

### Task 7: Executor 投递到 `weixin_ilink`

**Files:**
- Modify: `portal/internal/cron/executor.go`、`portal/cmd/backend/wire_gen.go`
- Test: `portal/internal/cron/executor_ilink_test.go`（新）

- [ ] **Step 1: 写失败测试**

`portal/internal/cron/executor_ilink_test.go`：

```go
package cron

import (
	"context"
	"errors"
	"strings"
	"testing"

	"backend/internal/biz"
	pkgErrors "backend/internal/pkg/errors"

	"github.com/go-kratos/kratos/v2/log"
)

type cronFakeChannels struct {
	biz.ChannelRepo
	byID map[string]*biz.ChannelMeta
}

func (f *cronFakeChannels) GetByID(_ context.Context, id string) (*biz.ChannelMeta, error) {
	ch, ok := f.byID[id]
	if !ok {
		return nil, pkgErrors.ErrNotFound
	}
	return ch, nil
}

type fakeIlinkPusher struct {
	calls []string
	err   error
}

func (f *fakeIlinkPusher) Push(_ context.Context, channelUUID, peerID, text string) ([]string, error) {
	f.calls = append(f.calls, channelUUID+"|"+peerID+"|"+text)
	if f.err != nil {
		return nil, f.err
	}
	return []string{"m1"}, nil
}

func newIlinkDeliveryExecutor() (*Executor, *fakeIlinkPusher) {
	channels := &cronFakeChannels{byID: map[string]*biz.ChannelMeta{
		"uuid-1":  {ID: "uuid-1", ChannelID: "wx-1", Type: biz.ChannelTypeWeixinIlink},
		"uuid-wh": {ID: "uuid-wh", ChannelID: "wh-1", Type: "webhook"},
	}}
	e := &Executor{
		channelUC: biz.NewChannelUsecase(channels, nil, log.DefaultLogger),
		log:       log.NewHelper(log.DefaultLogger),
	}
	p := &fakeIlinkPusher{}
	e.SetIlinkPusher(p)
	return e, p
}

func TestDeliverChannel_WeixinIlink(t *testing.T) {
	e, p := newIlinkDeliveryExecutor()
	ctx := context.Background()
	task := &biz.CronTaskMeta{Name: "日报", DeliveryChannelID: "uuid-1", DeliveryPeerID: "p1@im.wechat"}

	if err := e.deliverChannel(ctx, task, "今天晴", nil); err != nil {
		t.Fatalf("deliver: %v", err)
	}
	if len(p.calls) != 1 || p.calls[0] != "uuid-1|p1@im.wechat|[定时任务 日报] 今天晴" {
		t.Fatalf("calls = %q", p.calls)
	}

	p.err = biz.ErrIlinkLoginRequired
	err := e.deliverChannel(ctx, task, "x", nil)
	if !errors.Is(err, biz.ErrIlinkLoginRequired) || !strings.Contains(err.Error(), "微信推送失败") {
		t.Fatalf("push failure err = %v", err)
	}
}

func TestDeliverChannel_WeixinIlinkMisconfigured(t *testing.T) {
	e, p := newIlinkDeliveryExecutor()
	ctx := context.Background()

	err := e.deliverChannel(ctx, &biz.CronTaskMeta{Name: "n", DeliveryChannelID: "uuid-1"}, "x", nil)
	if err == nil || !strings.Contains(err.Error(), "delivery_peer_id") || len(p.calls) != 0 {
		t.Fatalf("missing peer err = %v calls = %d", err, len(p.calls))
	}
	e.SetIlinkPusher(nil)
	if err := e.deliverChannel(ctx, &biz.CronTaskMeta{Name: "n", DeliveryChannelID: "uuid-1", DeliveryPeerID: "p1"}, "x", nil); err == nil {
		t.Fatal("want error when the pusher is not wired")
	}
	err = e.deliverChannel(ctx, &biz.CronTaskMeta{Name: "n", DeliveryChannelID: "uuid-wh"}, "x", nil)
	if err == nil || !strings.Contains(err.Error(), "weixin_ilink") {
		t.Fatalf("unsupported type err = %v", err)
	}
}
```

- [ ] **Step 2: 运行确认失败**

Run: `go test ./internal/cron/ -run TestDeliverChannel_WeixinIlink -count=1`  
Expected: FAIL（编译错误：`e.SetIlinkPusher undefined`）。

- [ ] **Step 3: 实现**

`portal/internal/cron/executor.go`：

1. `Executor` 结构体末尾改为（新增 `ilink` 字段，并在结构体之后加接口与 setter）：

```go
	servicePrincipalUserID string
	ilink                  IlinkPusher
	// payloadRunner replaces the payload dispatch when set (tests).
	payloadRunner func(ctx context.Context, task *biz.CronTaskMeta) (string, error)
}

// IlinkPusher sends text to a weixin_ilink roster peer (*biz.WeixinIlinkUsecase satisfies it).
type IlinkPusher interface {
	Push(ctx context.Context, channelUUID, peerID, text string) ([]string, error)
}

// SetIlinkPusher enables delivery to weixin_ilink channels.
func (e *Executor) SetIlinkPusher(p IlinkPusher) {
	e.ilink = p
}
```

2. `deliverChannel` 的 `switch ch.Type` 中，把

```go
	default:
		return fmt.Errorf("渠道 %s 的类型 %s 不支持投递（仅支持 wxpusher、wecom）", ch.ID, ch.Type)
	}
```

替换为

```go
	case biz.ChannelTypeWeixinIlink:
		if e.ilink == nil {
			return errors.New("微信渠道推送未启用")
		}
		if task.DeliveryPeerID == "" {
			return fmt.Errorf("微信渠道 %s 未配置接收人（delivery_peer_id）", ch.ChannelID)
		}
		if _, err := e.ilink.Push(ctx, ch.ID, task.DeliveryPeerID, content); err != nil {
			return fmt.Errorf("微信推送失败：%w", err)
		}
		return nil
	default:
		return fmt.Errorf("渠道 %s 的类型 %s 不支持投递（仅支持 wxpusher、wecom、weixin_ilink）", ch.ID, ch.Type)
	}
```

`portal/cmd/backend/wire_gen.go`：在 Task 6 插入的 `cronUsecase.SetWeixinIlink(...)` 之后插入：

```go
	executor.SetIlinkPusher(weixinIlinkUsecase)
```

- [ ] **Step 4: 运行确认通过**

```powershell
gofmt -w internal/cron/executor.go internal/cron/executor_ilink_test.go cmd/backend/wire_gen.go
go build ./...
go vet ./internal/cron/ ./cmd/backend/
go test ./internal/cron/ -count=1
```

Expected: `ok  backend/internal/cron`。

- [ ] **Step 5: Commit**

```powershell
git add internal/cron/executor.go internal/cron/executor_ilink_test.go cmd/backend/wire_gen.go
git commit -m "feat(cron): Cron 结果可投递到 weixin_ilink 渠道"
```

---

### Task 8: framework `cronjob` 工具支持 `delivery_peer_id`

**Files:**
- Modify: `framework/tool/cronjob_tool.go`
- Test: `framework/tool/cronjob_tool_test.go`

portal 经 `replace github.com/sixath/framework => ../framework` 直接编译本目录，无需发版。

- [ ] **Step 1: 写失败测试**

```powershell
Set-Location ../framework
```

`framework/tool/cronjob_tool_test.go`：在 `func TestCronjobToolCheckFnDisabled(t *testing.T) {` 之前插入：

```go
func TestCronjobToolCreateDeliveryDefaults(t *testing.T) {
	reg := NewRegistry()
	client := &mockCronClient{}
	if err := RegisterCronjobTool(reg, client, &CronjobToolConfig{Enabled: true}); err != nil {
		t.Fatal(err)
	}
	toolDef, _ := reg.Get("cronjob")
	ctx := context.WithValue(context.Background(), ContextKeyAgentID, "agent-1")
	base := map[string]any{
		"action": "create", "name": "n", "schedule_kind": "cron", "schedule_expr": "0 9 * * *",
		"payload_kind": "agent_turn", "payload_content": "p",
	}
	if _, err := toolDef.Execute(ctx, base); err != nil {
		t.Fatal(err)
	}
	withPeer := map[string]any{"delivery_mode": "channel", "delivery_channel_id": "ch-1", "delivery_peer_id": " u1@im.wechat "}
	for k, v := range base {
		withPeer[k] = v
	}
	if _, err := toolDef.Execute(ctx, withPeer); err != nil {
		t.Fatal(err)
	}
	if len(client.created) != 2 {
		t.Fatalf("created = %d", len(client.created))
	}
	if got := client.created[0].DeliveryMode; got != "" {
		t.Fatalf("omitted delivery_mode = %q, want empty for the host to decide", got)
	}
	if got := client.created[1]; got.DeliveryMode != "channel" || got.DeliveryChannelID != "ch-1" || got.DeliveryPeerID != "u1@im.wechat" {
		t.Fatalf("explicit delivery = %+v", got)
	}
	toolProps := toolDef.Parameters.(map[string]any)["properties"].(map[string]any)
	if _, ok := toolProps["delivery_peer_id"]; !ok {
		t.Fatal("schema missing delivery_peer_id")
	}
}

```

- [ ] **Step 2: 运行确认失败**

Run: `go test ./tool/ -run TestCronjobToolCreateDeliveryDefaults -count=1`  
Expected: FAIL（编译错误：`got.DeliveryPeerID undefined`）。

- [ ] **Step 3: 实现**

`framework/tool/cronjob_tool.go`：

1. `CronJobCreateInput` 改为：

```go
// CronJobCreateInput is the payload for creating a scheduled task. An empty DeliveryMode means the
// caller did not choose one; the CronClient implementation picks the default.
type CronJobCreateInput struct {
	Name               string
	ScheduleKind       string
	ScheduleExpr       string
	Timezone           string
	PayloadKind        string
	PayloadContent     string
	TimeoutSec         int
	DeliveryMode       string
	DeliveryWebhookURL string
	DeliverySessionID  string
	DeliveryChannelID  string
	DeliveryPeerID     string
	Enabled            bool
}
```

2. 工具 schema 中 `delivery_mode` 的 `description` 改为：

```go
					"description": "Where to deliver results (create/update). Omit on create to let the host choose (e.g. reply to the chat the request came from).",
```

3. schema 中 `delivery_channel_id` 属性之后插入：

```go
				"delivery_peer_id": map[string]any{
					"type":        "string",
					"description": "Recipient within the channel when delivery_mode=channel and the channel needs one (e.g. a WeChat user id).",
				},
```

4. `cronjobCreate` 中删除

```go
	if deliveryMode == "" {
		deliveryMode = "none"
	}
```

5. `cronjobCreate` 构造 `CronJobCreateInput` 时，在 `DeliveryChannelID:  stringParam(params, "delivery_channel_id"),` 之后插入：

```go
		DeliveryPeerID:     strings.TrimSpace(stringParam(params, "delivery_peer_id")),
```

- [ ] **Step 4: 运行确认通过**

```powershell
gofmt -w tool/cronjob_tool.go tool/cronjob_tool_test.go
go vet ./tool/
go test ./tool/ -count=1
Set-Location ../evals/runner
go build ./...
Set-Location ../../portal
go build ./...
```

Expected: `ok  github.com/sixath/framework/tool`；`evals/runner` 与 portal 编译通过（两者都 `replace` 到本地 framework）。portal 此时 `DeliveryMode == ""` 仍由 `portalCronClient` 兜底为 `none`，行为不变。

- [ ] **Step 5: Commit**

```powershell
Set-Location ../framework
git add tool/cronjob_tool.go tool/cronjob_tool_test.go
git commit -m "feat(framework): cronjob 工具支持 delivery_peer_id，未指定投递方式时交由宿主决定"
Set-Location ../portal
```

---

### Task 9: `portalCronClient` 推回发起人

**Files:**
- Modify: `portal/internal/chat/cronjob_wiring.go`
- Test: `portal/internal/chat/cronjob_wiring_ilink_test.go`（新）

- [ ] **Step 1: 写失败测试**

`portal/internal/chat/cronjob_wiring_ilink_test.go`：

```go
package chat

import (
	"context"
	"testing"

	"backend/internal/biz"
	pkgErrors "backend/internal/pkg/errors"

	"github.com/sixath/framework/tool"
)

type originChannels struct {
	biz.ChannelRepo
	list []*biz.ChannelMeta
}

func (f *originChannels) GetByID(_ context.Context, id string) (*biz.ChannelMeta, error) {
	for _, ch := range f.list {
		if ch.ID == id {
			return ch, nil
		}
	}
	return nil, pkgErrors.ErrNotFound
}

func (f *originChannels) GetByChannelID(_ context.Context, slug string) (*biz.ChannelMeta, error) {
	for _, ch := range f.list {
		if ch.ChannelID == slug {
			return ch, nil
		}
	}
	return nil, pkgErrors.ErrNotFound
}

type originPeers struct {
	biz.ChannelPeerSessionRepo
	bySession map[string]*biz.ChannelPeerSession
}

func (f *originPeers) GetBySessionID(_ context.Context, sessionID string) (*biz.ChannelPeerSession, error) {
	row, ok := f.bySession[sessionID]
	if !ok {
		return nil, pkgErrors.ErrNotFound
	}
	return row, nil
}

type allowAllRoster struct{ biz.WeixinIlinkRepo }

func (allowAllRoster) GetPeer(_ context.Context, _, peerID string) (*biz.IlinkPeer, error) {
	return &biz.IlinkPeer{PeerID: peerID, Allowed: true}, nil
}

type captureCronCreateRepo struct {
	biz.CronTaskRepo
	last *biz.CronTaskCreate
}

func (r *captureCronCreateRepo) Create(_ context.Context, t *biz.CronTaskCreate) (*biz.CronTaskMeta, error) {
	cp := *t
	r.last = &cp
	return &biz.CronTaskMeta{ID: "t1", Name: t.Name, DeliveryMode: t.DeliveryMode}, nil
}

func newOriginCronClient() (tool.CronClient, *captureCronCreateRepo) {
	repo := &captureCronCreateRepo{}
	uc := biz.NewCronUsecase(repo, stubCronRunRepo{}, nil)
	uc.SetWeixinIlink(
		&originChannels{list: []*biz.ChannelMeta{
			{ID: "uuid-wx", ChannelID: "wx-1", Type: biz.ChannelTypeWeixinIlink, Enabled: true},
			{ID: "uuid-bot", ChannelID: "bot-1", Type: "wecom_bot", Enabled: true},
		}},
		&originPeers{bySession: map[string]*biz.ChannelPeerSession{
			"s-wx":  {ChannelID: "wx-1", PeerID: "user:p1@im.wechat", SessionID: "s-wx"},
			"s-bot": {ChannelID: "bot-1", PeerID: "user:zhangsan", SessionID: "s-bot"},
		}},
		allowAllRoster{},
	)
	return NewPortalCronClient(uc, nil), repo
}

func createIn(mode, channelID, webhookURL string) tool.CronJobCreateInput {
	return tool.CronJobCreateInput{
		Name: "daily", ScheduleKind: "cron", ScheduleExpr: "0 9 * * *", PayloadKind: "agent_turn", PayloadContent: "x",
		DeliveryMode: mode, DeliveryChannelID: channelID, DeliveryWebhookURL: webhookURL, Enabled: true,
	}
}

func TestPortalCronClient_BackfillsWeixinOrigin(t *testing.T) {
	client, repo := newOriginCronClient()
	ctx := context.WithValue(context.Background(), tool.ContextKeySessionID, "s-wx")

	if _, err := client.Create(ctx, "agent-1", createIn("", "", "")); err != nil {
		t.Fatalf("Create: %v", err)
	}
	got := repo.last
	if got.DeliveryMode != "channel" || got.DeliveryChannelID != "uuid-wx" || got.DeliveryPeerID != "p1@im.wechat" {
		t.Fatalf("delivery = %q / %q / %q", got.DeliveryMode, got.DeliveryChannelID, got.DeliveryPeerID)
	}
}

func TestPortalCronClient_NoBackfill(t *testing.T) {
	client, repo := newOriginCronClient()
	cases := []struct {
		session, mode, channelID, webhook string
		wantMode, wantChannel             string
	}{
		{"", "", "", "", "none", ""},
		{"s-unknown", "", "", "", "none", ""},
		{"s-bot", "", "", "", "none", ""},
		{"s-wx", "none", "", "", "none", ""},
		{"s-wx", "", "", "https://example.com/hook", "none", ""},
		{"s-wx", "channel", "uuid-other", "", "channel", "uuid-other"},
	}
	for _, c := range cases {
		ctx := context.WithValue(context.Background(), tool.ContextKeySessionID, c.session)
		if _, err := client.Create(ctx, "agent-1", createIn(c.mode, c.channelID, c.webhook)); err != nil {
			t.Fatalf("%+v: Create: %v", c, err)
		}
		got := repo.last
		if got.DeliveryMode != c.wantMode || got.DeliveryChannelID != c.wantChannel || got.DeliveryPeerID != "" {
			t.Fatalf("%+v: delivery = %q / %q / %q", c, got.DeliveryMode, got.DeliveryChannelID, got.DeliveryPeerID)
		}
	}
}
```

- [ ] **Step 2: 运行确认失败**

Run: `go test ./internal/chat/ -run TestPortalCronClient -count=1`  
Expected: FAIL，`TestPortalCronClient_BackfillsWeixinOrigin` 报 `delivery = "none" / "" / ""`。

- [ ] **Step 3: 实现**

`portal/internal/chat/cronjob_wiring.go` 的 `portalCronClient.Create` 中：

1. 把

```go
	deliveryMode := in.DeliveryMode
	if deliveryMode == "" {
		deliveryMode = "none"
	}
```

替换为

```go
	deliveryMode := in.DeliveryMode
	deliveryChannelID := in.DeliveryChannelID
	deliveryPeerID := in.DeliveryPeerID
	if deliveryMode == "" && deliveryChannelID == "" && deliveryPeerID == "" && in.DeliveryWebhookURL == "" && in.DeliverySessionID == "" {
		sessionID, _ := ctx.Value(tool.ContextKeySessionID).(string)
		channelUUID, peerID, err := c.uc.IlinkOrigin(ctx, sessionID)
		if err != nil {
			return tool.CronJobSummary{}, err
		}
		if channelUUID != "" {
			deliveryMode, deliveryChannelID, deliveryPeerID = "channel", channelUUID, peerID
		}
	}
	if deliveryMode == "" {
		deliveryMode = "none"
	}
```

2. 构造 `biz.CronTaskCreate` 时，把 `DeliveryChannelID:  in.DeliveryChannelID,` 替换为：

```go
		DeliveryChannelID:  deliveryChannelID,
		DeliveryPeerID:     deliveryPeerID,
```

- [ ] **Step 4: 运行确认通过**

```powershell
gofmt -w internal/chat/cronjob_wiring.go internal/chat/cronjob_wiring_ilink_test.go
go vet ./internal/chat/
go test ./internal/chat/ -run "TestPortalCronClient|TestRequestMetadataFromCronContext" -count=1
```

Expected: `ok  backend/internal/chat`。

- [ ] **Step 5: Commit**

```powershell
git add internal/chat/cronjob_wiring.go internal/chat/cronjob_wiring_ilink_test.go
git commit -m "feat(chat): 微信会话中创建的 Cron 默认推回发起人"
```

---

### Task 10: `send_to_weixin` 渠道与对端解析（biz）

**Files:**
- Create: `portal/internal/biz/weixin_ilink_send.go`
- Test: `portal/internal/biz/weixin_ilink_send_test.go`

- [ ] **Step 1: 写失败测试**

`portal/internal/biz/weixin_ilink_send_test.go`：

```go
package biz

import (
	"context"
	"strings"
	"testing"
)

type sendFakeChannels struct {
	*ilinkFakeChannels
	byAgent map[string][]*ChannelMeta
}

func (f *sendFakeChannels) ListIlinkByAgent(_ context.Context, agentID string) ([]*ChannelMeta, error) {
	return f.byAgent[agentID], nil
}

func newSendUsecaseForTest() *WeixinIlinkUsecase {
	wx1 := &ChannelMeta{ID: "uuid-1", ChannelID: "wx-1", Type: ChannelTypeWeixinIlink, Enabled: true}
	wx2 := &ChannelMeta{ID: "uuid-2", ChannelID: "wx-2", Type: ChannelTypeWeixinIlink, Enabled: true}
	wx3 := &ChannelMeta{ID: "uuid-3", ChannelID: "wx-3", Type: ChannelTypeWeixinIlink, Enabled: true}
	channels := &sendFakeChannels{
		ilinkFakeChannels: &ilinkFakeChannels{byID: map[string]*ChannelMeta{
			"uuid-1": wx1, "uuid-2": wx2, "uuid-3": wx3,
			"uuid-bot": {ID: "uuid-bot", ChannelID: "bot-1", Type: "wecom_bot", Enabled: true},
		}},
		byAgent: map[string][]*ChannelMeta{"solo": {wx2}, "multi": {wx1, wx2}},
	}
	peers := &fakeChannelPeerSessionRepo{rows: map[string]*ChannelPeerSession{
		peerKey("wx-3", "user:p3"): {ChannelID: "wx-3", PeerID: "user:p3", SessionID: "s-wx3"},
		peerKey("bot-1", "user:u"): {ChannelID: "bot-1", PeerID: "user:u", SessionID: "s-bot"},
	}}
	uc := NewWeixinIlinkUsecaseWith(channels, &ilinkFakeRepo{}, newAuthEphemeralFake(), newQRLogin(), nil)
	uc.SetPeerSessions(peers)
	return uc
}

func TestIlinkResolveSendTarget(t *testing.T) {
	uc := newSendUsecaseForTest()
	cases := []struct {
		agent, session, slug, peer string
		wantChannel, wantPeer      string
		wantErr                    string
	}{
		{"multi", "s-wx3", "", "", "wx-3", "p3", ""},
		{"multi", "s-wx3", "", "px", "wx-3", "px", ""},
		{"multi", "s-wx3", "wx-3", "", "wx-3", "p3", ""},
		{"solo", "", "", " p1 ", "wx-2", "p1", ""},
		{"solo", "s-bot", "", "p1", "wx-2", "p1", ""},
		{"multi", "", "wx-1", "p1", "wx-1", "p1", ""},
		{"multi", "s-wx3", "wx-2", "p1", "wx-2", "p1", ""},
		{"solo", "", "", "", "", "", "peer_id"},
		{"multi", "s-wx3", "wx-2", "", "", "", "peer_id"},
		{"multi", "", "", "p1", "", "", "wx-1、wx-2"},
		{"multi", "", "wx-9", "p1", "", "", "wx-9 不可用"},
		{"nobody", "s-bot", "", "p1", "", "", "未关联"},
	}
	for _, c := range cases {
		got, err := uc.ResolveSendTarget(context.Background(), c.agent, c.session, c.slug, c.peer)
		if c.wantErr != "" {
			if err == nil || !strings.Contains(err.Error(), c.wantErr) {
				t.Errorf("%+v: err = %v, want %q", c, err, c.wantErr)
			}
			continue
		}
		if err != nil || got.Channel.ChannelID != c.wantChannel || got.PeerID != c.wantPeer {
			t.Errorf("%+v: got %+v, %v", c, got, err)
		}
	}
}
```

- [ ] **Step 2: 运行确认失败**

Run: `go test ./internal/biz/ -run TestIlinkResolveSendTarget -count=1`  
Expected: FAIL（编译错误：`uc.SetPeerSessions undefined`、`uc.ResolveSendTarget undefined`）。

- [ ] **Step 3: 实现**

`portal/internal/biz/weixin_ilink_send.go`：

```go
package biz

import (
	"context"
	"errors"
	"fmt"
	"strings"
)

// IlinkSendTarget is where send_to_weixin delivers.
type IlinkSendTarget struct {
	Channel *ChannelMeta
	PeerID  string
}

// SetPeerSessions enables looking up the weixin_ilink channel a chat session came from.
func (uc *WeixinIlinkUsecase) SetPeerSessions(peers ChannelPeerSessionRepo) {
	uc.peers = peers
}

// ChannelsForAgent lists enabled weixin_ilink channels whose default or allowed agents include agentID.
func (uc *WeixinIlinkUsecase) ChannelsForAgent(ctx context.Context, agentID string) ([]*ChannelMeta, error) {
	return uc.channels.ListIlinkByAgent(ctx, agentID)
}

// ResolveSendTarget picks the channel and peer for send_to_weixin. Without channelSlug it uses the
// session's weixin_ilink channel, else the agent's only channel; an explicit channelSlug must name the
// session's channel or one of the agent's channels. peerID defaults to the session peer, and only on
// the session's own channel.
func (uc *WeixinIlinkUsecase) ResolveSendTarget(ctx context.Context, agentID, sessionID, channelSlug, peerID string) (*IlinkSendTarget, error) {
	channelSlug = strings.TrimSpace(channelSlug)
	peerID = strings.TrimSpace(peerID)
	origin, originPeer, err := ilinkSessionOrigin(ctx, uc.peers, uc.channels, sessionID)
	if err != nil {
		return nil, err
	}
	ch := origin
	if origin == nil || (channelSlug != "" && channelSlug != origin.ChannelID) {
		list, err := uc.ChannelsForAgent(ctx, agentID)
		if err != nil {
			return nil, err
		}
		ch = nil
		for _, c := range list {
			if channelSlug != "" && c.ChannelID == channelSlug {
				ch = c
			}
		}
		if ch == nil && channelSlug == "" && len(list) == 1 {
			ch = list[0]
		}
		if ch == nil {
			return nil, ilinkChannelChoiceErr(channelSlug, list)
		}
	}
	if peerID == "" {
		if origin == nil || ch.ID != origin.ID {
			return nil, errors.New("请指定 peer_id：当前会话不是来自该微信渠道")
		}
		peerID = originPeer
	}
	return &IlinkSendTarget{Channel: ch, PeerID: peerID}, nil
}

func ilinkChannelChoiceErr(channelSlug string, list []*ChannelMeta) error {
	slugs := make([]string, 0, len(list))
	for _, c := range list {
		slugs = append(slugs, c.ChannelID)
	}
	switch {
	case len(slugs) == 0:
		return errors.New("当前 Agent 未关联启用的微信渠道")
	case channelSlug != "":
		return fmt.Errorf("微信渠道 %s 不可用，可选渠道：%s", channelSlug, strings.Join(slugs, "、"))
	default:
		return fmt.Errorf("当前 Agent 关联了多个微信渠道，请用 channel_id 指定其一：%s", strings.Join(slugs, "、"))
	}
}
```

- [ ] **Step 4: 运行确认通过**

```powershell
gofmt -w internal/biz/weixin_ilink_send.go internal/biz/weixin_ilink_send_test.go
go vet ./internal/biz/
go test ./internal/biz/ -count=1
```

Expected: `ok  backend/internal/biz`。

- [ ] **Step 5: Commit**

```powershell
git add internal/biz/weixin_ilink_send.go internal/biz/weixin_ilink_send_test.go
git commit -m "feat(biz): send_to_weixin 渠道与接收人解析"
```

---

### Task 11: `send_to_weixin` 工具与目录多渠道绑定（chat）

**Files:**
- Create: `portal/internal/chat/weixin_wiring.go`
- Modify: `portal/internal/chat/wecom_wiring.go`、`portal/internal/chat/catalog_wiring.go`、`portal/internal/chat/channel_catalog.go`
- Test: `portal/internal/chat/weixin_wiring_test.go`（新）

- [ ] **Step 1: 写失败测试**

`portal/internal/chat/weixin_wiring_test.go`：

```go
package chat

import (
	"context"
	"errors"
	"net"
	"slices"
	"strings"
	"testing"
	"time"

	"backend/internal/biz"
	"backend/internal/channel"

	"github.com/sixath/framework/tool"
)

type fakeWeixinPusher struct {
	resolveErr error
	pushErr    error
	resolved   []string
	pushed     []string
}

func (f *fakeWeixinPusher) ResolveSendTarget(_ context.Context, agentID, sessionID, channelSlug, peerID string) (*biz.IlinkSendTarget, error) {
	f.resolved = append(f.resolved, strings.Join([]string{agentID, sessionID, channelSlug, peerID}, "|"))
	if f.resolveErr != nil {
		return nil, f.resolveErr
	}
	if peerID == "" {
		peerID = "o9cq800Ngkx4@im.wechat"
	}
	return &biz.IlinkSendTarget{Channel: &biz.ChannelMeta{ID: "uuid-wx", ChannelID: "wx-1"}, PeerID: peerID}, nil
}

func (f *fakeWeixinPusher) Push(_ context.Context, channelUUID, peerID, text string) ([]string, error) {
	f.pushed = append(f.pushed, channelUUID+"|"+peerID+"|"+text)
	if f.pushErr != nil {
		return nil, f.pushErr
	}
	return []string{"m1"}, nil
}

type captureRecorder struct{ recs []channel.DeliveryRecord }

func (r *captureRecorder) Record(_ context.Context, rec channel.DeliveryRecord) error {
	r.recs = append(r.recs, rec)
	return nil
}

func registerWeixinForTest(t *testing.T, p *fakeWeixinPusher, rec channel.DeliveryRecorder) tool.Tool {
	t.Helper()
	reg := tool.NewRegistry()
	if err := RegisterSendToWeixinTool(reg, SendToWeixinOptions{AgentID: "agent-1", Pusher: p, Recorder: rec, RateLimitInterval: time.Millisecond}); err != nil {
		t.Fatalf("register: %v", err)
	}
	tdef, ok := reg.Get("send_to_weixin")
	if !ok {
		t.Fatal("send_to_weixin not registered")
	}
	return tdef
}

func TestRegisterSendToWeixinTool_NilPusher(t *testing.T) {
	reg := tool.NewRegistry()
	if err := RegisterSendToWeixinTool(reg, SendToWeixinOptions{AgentID: "agent-1"}); err != nil {
		t.Fatal(err)
	}
	if _, ok := reg.Get("send_to_weixin"); ok {
		t.Fatal("send_to_weixin registered without a pusher")
	}
}

func TestSendToWeixin_SuccessRecordsDelivery(t *testing.T) {
	p := &fakeWeixinPusher{}
	rec := &captureRecorder{}
	tdef := registerWeixinForTest(t, p, rec)
	ctx := context.WithValue(context.Background(), tool.ContextKeySessionID, "sess-wx-ok")

	out, err := tdef.Execute(ctx, map[string]any{"text": "你好"})
	if err != nil {
		t.Fatal(err)
	}
	msg, _ := out.(string)
	if !strings.Contains(msg, "已推送到微信") || !strings.Contains(msg, "wx-1") || !strings.Contains(msg, "gkx4") || !strings.Contains(msg, "delivery_id") {
		t.Fatalf("result = %q", msg)
	}
	if len(p.resolved) != 1 || p.resolved[0] != "agent-1|sess-wx-ok||" {
		t.Fatalf("resolved = %q", p.resolved)
	}
	if len(p.pushed) != 1 || p.pushed[0] != "uuid-wx|o9cq800Ngkx4@im.wechat|你好" {
		t.Fatalf("pushed = %q", p.pushed)
	}
	if len(rec.recs) != 1 || rec.recs[0].Status != channel.DeliverySent || rec.recs[0].ChannelID != "uuid-wx" || rec.recs[0].SessionID != "sess-wx-ok" {
		t.Fatalf("records = %+v", rec.recs)
	}
}

func TestSendToWeixin_FailureIsNotRetried(t *testing.T) {
	p := &fakeWeixinPusher{pushErr: &net.OpError{Op: "dial", Err: errors.New("connection reset")}}
	rec := &captureRecorder{}
	tdef := registerWeixinForTest(t, p, rec)
	ctx := context.WithValue(context.Background(), tool.ContextKeySessionID, "sess-wx-fail")

	out, _ := tdef.Execute(ctx, map[string]any{"text": "x", "peer_id": "p2", "channel_id": "wx-1"})
	msg, _ := out.(string)
	if !strings.Contains(msg, "投递失败") || !strings.Contains(msg, "connection reset") {
		t.Fatalf("result = %q", msg)
	}
	if len(p.pushed) != 1 {
		t.Fatalf("push attempts = %d, want 1", len(p.pushed))
	}
	if len(rec.recs) != 1 || rec.recs[0].Status != channel.DeliveryFailed || rec.recs[0].Attempts != 1 {
		t.Fatalf("records = %+v", rec.recs)
	}
	if p.resolved[0] != "agent-1|sess-wx-fail|wx-1|p2" {
		t.Fatalf("resolved = %q", p.resolved)
	}
}

func TestSendToWeixin_ResolveErrorAndValidation(t *testing.T) {
	p := &fakeWeixinPusher{resolveErr: errors.New("请指定 peer_id")}
	tdef := registerWeixinForTest(t, p, nil)
	ctx := context.WithValue(context.Background(), tool.ContextKeySessionID, "sess-wx-resolve")

	out, _ := tdef.Execute(ctx, map[string]any{"text": " "})
	if msg, _ := out.(string); !strings.Contains(msg, "text is required") {
		t.Fatalf("blank text result = %q", msg)
	}
	out, _ = tdef.Execute(ctx, map[string]any{"text": "x"})
	if msg, _ := out.(string); !strings.Contains(msg, "请指定 peer_id") || len(p.pushed) != 0 {
		t.Fatalf("resolve error result = %q pushed = %d", msg, len(p.pushed))
	}
}

func TestSendToWeixin_RateLimitedPerSession(t *testing.T) {
	p := &fakeWeixinPusher{}
	reg := tool.NewRegistry()
	if err := RegisterSendToWeixinTool(reg, SendToWeixinOptions{AgentID: "agent-1", Pusher: p, RateLimitInterval: time.Hour}); err != nil {
		t.Fatal(err)
	}
	tdef, _ := reg.Get("send_to_weixin")
	ctx := context.WithValue(context.Background(), tool.ContextKeySessionID, "sess-wx-rate")

	_, _ = tdef.Execute(ctx, map[string]any{"text": "1"})
	out, _ := tdef.Execute(ctx, map[string]any{"text": "2"})
	if msg, _ := out.(string); !strings.Contains(msg, "发送过于频繁") || len(p.pushed) != 1 {
		t.Fatalf("second send = %q pushed = %d", msg, len(p.pushed))
	}
}

func TestCatalogWiring_WecomAndWeixinBindings(t *testing.T) {
	reg := tool.NewRegistry()
	if err := RegisterSendToWeComTool(reg, SendToWeComOptions{ResolveWebhook: func(context.Context) (string, error) { return "http://x", nil }}); err != nil {
		t.Fatal(err)
	}
	if err := RegisterSendToWeixinTool(reg, SendToWeixinOptions{AgentID: "agent-1", Pusher: &fakeWeixinPusher{}}); err != nil {
		t.Fatal(err)
	}

	cat := BuildCatalogForAgent(context.Background(), CatalogWiringInput{
		Reg:            reg,
		WecomChannelID: "ch-wecom",
		ChannelType:    "wecom",
		Channels:       WeixinCatalogBindings([]string{"wx-1", "wx-2"}),
	})
	byName := map[string]tool.ToolCatalogEntry{}
	for _, e := range cat.Entries {
		byName[e.Name] = e
	}
	wecom, wx := byName["send_to_wecom"], byName["send_to_weixin"]
	if wecom.Bindings["channel_id"] != "ch-wecom" || wecom.Bindings["channel_type"] != "wecom" {
		t.Fatalf("send_to_wecom bindings = %v", wecom.Bindings)
	}
	if wx.Bindings["channel_id"] != "wx-1,wx-2" || wx.Bindings["channel_type"] != "weixin_ilink" {
		t.Fatalf("send_to_weixin bindings = %v", wx.Bindings)
	}
	for _, want := range []string{"微信", "weixin", "推送"} {
		if !slices.Contains(wx.SearchHints, want) {
			t.Fatalf("send_to_weixin hints missing %q: %v", want, wx.SearchHints)
		}
	}
	if WeixinCatalogBindings(nil) != nil {
		t.Fatal("no slugs must yield no binding")
	}
}

func TestWeixinPeerTail(t *testing.T) {
	if got := weixinPeerTail("o9cq800VxswKIZHziZ1yez1Ngkx4@im.wechat"); got != "gkx4" {
		t.Fatalf("tail = %q", got)
	}
	if got := weixinPeerTail("ab"); got != "ab" {
		t.Fatalf("short tail = %q", got)
	}
}
```

- [ ] **Step 2: 运行确认失败**

Run: `go test ./internal/chat/ -run "Weixin|TestCatalogWiring" -count=1`  
Expected: FAIL（编译错误：`undefined: RegisterSendToWeixinTool`、`unknown field Channels` 等）。

- [ ] **Step 3: 抽出按会话限流**

`portal/internal/chat/wecom_wiring.go`：把 `checkSendToWeComRateLimit` 整个函数替换为：

```go
func checkSendToWeComRateLimit(sessionID string, interval time.Duration) error {
	return checkSessionRateLimit(&sendToWeComLastSend, sessionID, interval)
}

// checkSessionRateLimit enforces a per-session minimum interval using lastSend (session_id → time.Time).
func checkSessionRateLimit(lastSend *sync.Map, sessionID string, interval time.Duration) error {
	if interval <= 0 {
		interval = sendToWeComDefaultInterval
	}
	now := time.Now()
	if v, ok := lastSend.Load(sessionID); ok {
		if last, ok := v.(time.Time); ok && now.Sub(last) < interval {
			return fmt.Errorf("发送过于频繁，请至少间隔 %s 后再试", interval)
		}
	}
	lastSend.Store(sessionID, now)
	return nil
}
```

- [ ] **Step 4: 目录多渠道绑定**

`portal/internal/chat/catalog_wiring.go`：

1. 把 `CatalogWiringInput` 的定义（含其注释）替换为：

```go
// ChannelBinding 把出站渠道工具绑定到目录；ChannelID 可为逗号分隔的多个渠道 slug。
type ChannelBinding struct {
	ChannelID   string
	ChannelType string
}

// CatalogWiringInput 构建 Agent 工具目录所需的运行时上下文。
type CatalogWiringInput struct {
	Reg            *tool.Registry
	DsBindings     []DatasourceBinding
	WecomChannelID string
	ChannelType    string           // "wecom" when bound
	Channels       []ChannelBinding // 其它出站渠道（如 weixin_ilink），与 WecomChannelID 并存
	SkillsIdx      *skills.Index
}
```

2. `BuildCatalogForAgent` 中，在 `if in.WecomChannelID != "" { ... }` 代码块之后插入：

```go
	for _, b := range in.Channels {
		providers = append(providers, &ChannelCatalogProvider{ChannelID: b.ChannelID, ChannelType: b.ChannelType})
	}
```

`portal/internal/chat/channel_catalog.go`：两个映射改为：

```go
var channelTypeTools = map[string][]string{
	"wecom":        {"send_to_wecom"},
	"weixin_ilink": {"send_to_weixin"},
}

var channelTypeHints = map[string][]string{
	"wecom":        {"企微", "企业微信", "推送", "webhook"},
	"weixin_ilink": {"微信", "weixin", "推送"},
}
```

- [ ] **Step 5: 注册 `send_to_weixin`**

`portal/internal/chat/weixin_wiring.go`：

```go
package chat

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"time"

	"backend/internal/biz"
	"backend/internal/channel"

	"github.com/sixath/framework/tool"
)

// sendToWeixinLastSend 按 session_id 记录 send_to_weixin 上次发送时间，进程内限流（与 send_to_wecom 分开计数）。
var sendToWeixinLastSend sync.Map

// WeixinPusher resolves send_to_weixin targets and sends (*biz.WeixinIlinkUsecase satisfies it).
type WeixinPusher interface {
	ResolveSendTarget(ctx context.Context, agentID, sessionID, channelSlug, peerID string) (*biz.IlinkSendTarget, error)
	Push(ctx context.Context, channelUUID, peerID, text string) ([]string, error)
}

// SendToWeixinOptions 构造 send_to_weixin 工具的依赖。
type SendToWeixinOptions struct {
	AgentID string
	Pusher  WeixinPusher
	// Recorder 可选：非空时每次投递（成功或失败）都会落一条 channel_deliveries 记录。
	Recorder channel.DeliveryRecorder
	// RateLimitInterval 同一会话发送的最小间隔，<=0 默认 1s。
	RateLimitInterval time.Duration
}

// RegisterSendToWeixinTool registers send_to_weixin; it is a no-op when reg or Pusher is nil.
func RegisterSendToWeixinTool(reg *tool.Registry, opts SendToWeixinOptions) error {
	if reg == nil || opts.Pusher == nil {
		return nil
	}
	return reg.Register(tool.Tool{
		Name: "send_to_weixin",
		Description: "Send a text message to a WeChat user through the agent's WeChat (weixin_ilink) bot. " +
			"In a WeChat conversation peer_id defaults to the current user; otherwise pass peer_id, and channel_id when the agent has several WeChat channels. " +
			"The recipient must be allowed in the channel roster and must have messaged the bot since its last login. " +
			"通过 Agent 关联的微信 bot 给名册内已放行的微信用户推送文字消息；在微信会话中默认发给当前对话人。",
		Toolset: tool.ToolsetCore,
		Parameters: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"text": map[string]any{
					"type":        "string",
					"description": "Message body (Markdown is flattened to plain text).",
				},
				"peer_id": map[string]any{
					"type":        "string",
					"description": "Recipient WeChat user id (e.g. xxx@im.wechat). Defaults to the current WeChat user.",
				},
				"channel_id": map[string]any{
					"type":        "string",
					"description": "weixin_ilink channel slug; only needed when the agent has several WeChat channels.",
				},
			},
			"required": []string{"text"},
		},
		Execute: buildSendToWeixinExecute(opts),
	})
}

func buildSendToWeixinExecute(opts SendToWeixinOptions) tool.ExecuteFunc {
	return func(ctx context.Context, params map[string]any) (any, error) {
		text, _ := params["text"].(string)
		if strings.TrimSpace(text) == "" {
			return "send_to_weixin: text is required", nil
		}
		peerID, _ := params["peer_id"].(string)
		channelSlug, _ := params["channel_id"].(string)
		agentID := opts.AgentID
		if agentID == "" {
			agentID, _ = ctx.Value(tool.ContextKeyAgentID).(string)
		}
		sessionID, _ := ctx.Value(tool.ContextKeySessionID).(string)
		if err := checkSessionRateLimit(&sendToWeixinLastSend, sessionID, opts.RateLimitInterval); err != nil {
			return err.Error(), nil
		}
		target, err := opts.Pusher.ResolveSendTarget(ctx, agentID, sessionID, channelSlug, peerID)
		if err != nil {
			return fmt.Sprintf("send_to_weixin: %v", err), nil
		}
		out := &weixinOutbound{pusher: opts.Pusher, channelUUID: target.Channel.ID, peerID: target.PeerID}
		// MaxAttempts=1: a failed sendmessage may still have been delivered, so never resend.
		d, err := channel.Deliver(ctx, out, channel.OutboundMessage{Content: text, MsgType: "text"}, target.Channel.ID, sessionID, channel.DeliverOptions{MaxAttempts: 1}, opts.Recorder)
		if err != nil {
			return fmt.Sprintf("send_to_weixin: 投递失败（delivery_id: %s）: %v", d.ID, err), nil
		}
		return fmt.Sprintf("已推送到微信（渠道 %s，接收人尾号 %s，delivery_id: %s）", target.Channel.ChannelID, weixinPeerTail(target.PeerID), d.ID), nil
	}
}

// weixinOutbound adapts WeixinPusher to channel.OutboundChannel so deliveries are recorded.
type weixinOutbound struct {
	pusher      WeixinPusher
	channelUUID string
	peerID      string
}

func (w *weixinOutbound) Type() string { return biz.ChannelTypeWeixinIlink }

func (w *weixinOutbound) Send(ctx context.Context, msg channel.OutboundMessage) (channel.DeliveryReceipt, error) {
	if _, err := w.pusher.Push(ctx, w.channelUUID, w.peerID, msg.Content); err != nil {
		return channel.DeliveryReceipt{Status: "failed", Attempts: 1, LastError: err.Error()}, err
	}
	return channel.DeliveryReceipt{Status: "sent", Attempts: 1}, nil
}

// weixinPeerTail returns the last 4 chars of the part before '@' (iLink IDs are opaque).
func weixinPeerTail(peerID string) string {
	local, _, _ := strings.Cut(peerID, "@")
	r := []rune(local)
	if len(r) <= 4 {
		return local
	}
	return string(r[len(r)-4:])
}

// WeixinCatalogBindings binds send_to_weixin in the tool catalog to the agent's weixin_ilink slugs.
func WeixinCatalogBindings(slugs []string) []ChannelBinding {
	if len(slugs) == 0 {
		return nil
	}
	return []ChannelBinding{{ChannelID: strings.Join(slugs, ","), ChannelType: biz.ChannelTypeWeixinIlink}}
}
```

- [ ] **Step 6: 运行确认通过**

```powershell
gofmt -w internal/chat/weixin_wiring.go internal/chat/weixin_wiring_test.go internal/chat/wecom_wiring.go internal/chat/catalog_wiring.go internal/chat/channel_catalog.go
go build ./...
go vet ./internal/chat/
go test ./internal/chat/ -run "Weixin|WeCom|TestCatalogWiring|TestChannelCatalog" -count=1
```

Expected: `ok  backend/internal/chat`（含既有 `send_to_wecom` 与目录测试）。

- [ ] **Step 7: Commit**

```powershell
git add internal/chat/weixin_wiring.go internal/chat/weixin_wiring_test.go internal/chat/wecom_wiring.go internal/chat/catalog_wiring.go internal/chat/channel_catalog.go
git commit -m "feat(chat): send_to_weixin 工具与工具目录多渠道绑定"
```

---

### Task 12: 按 Agent 注册 `send_to_weixin` 并接线

**Files:**
- Create: `portal/internal/service/weixin_bind.go`
- Modify: `portal/internal/service/chat.go`、`portal/internal/service/agent.go`、`portal/cmd/backend/wire_gen.go`
- Test: `portal/internal/service/weixin_bind_test.go`（新）

- [ ] **Step 1: 写失败测试**

`portal/internal/service/weixin_bind_test.go`：

```go
package service

import (
	"context"
	"testing"

	"backend/internal/biz"

	"github.com/sixath/framework/tool"
)

type fakeWeixinSender struct {
	byAgent map[string][]*biz.ChannelMeta
}

func (f *fakeWeixinSender) ChannelsForAgent(_ context.Context, agentID string) ([]*biz.ChannelMeta, error) {
	return f.byAgent[agentID], nil
}

func (f *fakeWeixinSender) ResolveSendTarget(context.Context, string, string, string, string) (*biz.IlinkSendTarget, error) {
	return nil, nil
}

func (f *fakeWeixinSender) Push(context.Context, string, string, string) ([]string, error) {
	return nil, nil
}

func TestRegisterWeixinToolForAgent(t *testing.T) {
	sender := &fakeWeixinSender{byAgent: map[string][]*biz.ChannelMeta{
		"a1": {{ID: "u1", ChannelID: "wx-1"}, {ID: "u2", ChannelID: "wx-2"}},
	}}

	reg := tool.NewRegistry()
	bindings := registerWeixinToolForAgent(context.Background(), sender, nil, reg, &biz.AgentMeta{ID: "a1"})
	if _, ok := reg.Get("send_to_weixin"); !ok {
		t.Fatal("send_to_weixin not registered")
	}
	if len(bindings) != 1 || bindings[0].ChannelID != "wx-1,wx-2" || bindings[0].ChannelType != biz.ChannelTypeWeixinIlink {
		t.Fatalf("bindings = %+v", bindings)
	}

	reg = tool.NewRegistry()
	if b := registerWeixinToolForAgent(context.Background(), sender, nil, reg, &biz.AgentMeta{ID: "a2"}); b != nil {
		t.Fatalf("bindings without channels = %+v", b)
	}
	if _, ok := reg.Get("send_to_weixin"); ok {
		t.Fatal("send_to_weixin registered for an agent without weixin_ilink channels")
	}
	if b := registerWeixinToolForAgent(context.Background(), nil, nil, reg, &biz.AgentMeta{ID: "a1"}); b != nil {
		t.Fatalf("bindings without sender = %+v", b)
	}
}
```

- [ ] **Step 2: 运行确认失败**

Run: `go test ./internal/service/ -run TestRegisterWeixinToolForAgent -count=1`  
Expected: FAIL，`undefined: registerWeixinToolForAgent`。

- [ ] **Step 3: 实现注册函数**

`portal/internal/service/weixin_bind.go`：

```go
package service

import (
	"context"

	"backend/internal/biz"
	"backend/internal/channel"
	"backend/internal/chat"

	"github.com/sixath/framework/tool"
)

// WeixinIlinkSender is what send_to_weixin needs from biz (*biz.WeixinIlinkUsecase satisfies it).
type WeixinIlinkSender interface {
	chat.WeixinPusher
	ChannelsForAgent(ctx context.Context, agentID string) ([]*biz.ChannelMeta, error)
}

// registerWeixinToolForAgent registers send_to_weixin when the agent has at least one enabled
// weixin_ilink channel and returns the matching catalog bindings (nil when not registered).
func registerWeixinToolForAgent(ctx context.Context, sender WeixinIlinkSender, recorder channel.DeliveryRecorder, reg *tool.Registry, agentMeta *biz.AgentMeta) []chat.ChannelBinding {
	if sender == nil || agentMeta == nil {
		return nil
	}
	channels, err := sender.ChannelsForAgent(ctx, agentMeta.ID)
	if err != nil || len(channels) == 0 {
		return nil
	}
	if err := chat.RegisterSendToWeixinTool(reg, chat.SendToWeixinOptions{AgentID: agentMeta.ID, Pusher: sender, Recorder: recorder}); err != nil {
		return nil
	}
	slugs := make([]string, 0, len(channels))
	for _, ch := range channels {
		slugs = append(slugs, ch.ChannelID)
	}
	return chat.WeixinCatalogBindings(slugs)
}
```

- [ ] **Step 4: 在三个装配点调用**

`portal/internal/service/chat.go`：

1. `ChatService` 结构体中，在 `deliveryRecorder channel.DeliveryRecorder` 之后插入：

```go
	weixinIlink      WeixinIlinkSender
```

2. 在 `SetDeliveryRecorder` 方法之后插入：

```go

// SetWeixinIlink enables the send_to_weixin tool (nil-safe).
func (s *ChatService) SetWeixinIlink(w WeixinIlinkSender) {
	if s == nil {
		return
	}
	s.weixinIlink = w
}
```

3. `SendMessage` 与 `SendMessageStream` 中**各有一处**

```go
	registerWeComToolForAgent(ctx, s.channelUC, s.deliveryRecorder, reg, agentMeta)

	wecomChannelID := resolveAgentWecomChannelID(ctx, s.channelUC, agentMeta)
	catalogInput := chat.CatalogWiringInput{
		Reg:            reg,
		DsBindings:     regResult.DsBindings,
		WecomChannelID: wecomChannelID,
		ChannelType:    "wecom",
		SkillsIdx:      skillsIdx,
	}
```

两处都改为：

```go
	registerWeComToolForAgent(ctx, s.channelUC, s.deliveryRecorder, reg, agentMeta)
	weixinBindings := registerWeixinToolForAgent(ctx, s.weixinIlink, s.deliveryRecorder, reg, agentMeta)

	wecomChannelID := resolveAgentWecomChannelID(ctx, s.channelUC, agentMeta)
	catalogInput := chat.CatalogWiringInput{
		Reg:            reg,
		DsBindings:     regResult.DsBindings,
		WecomChannelID: wecomChannelID,
		ChannelType:    "wecom",
		Channels:       weixinBindings,
		SkillsIdx:      skillsIdx,
	}
```

`portal/internal/service/agent.go`：

1. `AgentService` 结构体中，在 `handbookDirs HandbookSkillDirResolver` 之后插入 `weixinIlink  WeixinIlinkSender`；并在结构体之后（`SetRCARootResolver` 之前）插入：

```go
// SetWeixinIlink enables the send_to_weixin tool for direct agent chats (e.g. cron agent_turn).
func (s *AgentService) SetWeixinIlink(w WeixinIlinkSender) {
	if s == nil {
		return
	}
	s.weixinIlink = w
}

```

2. `Chat` 方法中把

```go
	registerWeComToolForAgent(ctx, s.channelUC, nil, reg, agentMeta)

	wecomChannelID := resolveAgentWecomChannelID(ctx, s.channelUC, agentMeta)
	catalogInput := chat.CatalogWiringInput{
		Reg:            reg,
		DsBindings:     regResult.DsBindings,
		WecomChannelID: wecomChannelID,
		ChannelType:    "wecom",
		SkillsIdx:      skillsIdx,
	}
```

改为：

```go
	registerWeComToolForAgent(ctx, s.channelUC, nil, reg, agentMeta)
	weixinBindings := registerWeixinToolForAgent(ctx, s.weixinIlink, nil, reg, agentMeta)

	wecomChannelID := resolveAgentWecomChannelID(ctx, s.channelUC, agentMeta)
	catalogInput := chat.CatalogWiringInput{
		Reg:            reg,
		DsBindings:     regResult.DsBindings,
		WecomChannelID: wecomChannelID,
		ChannelType:    "wecom",
		Channels:       weixinBindings,
		SkillsIdx:      skillsIdx,
	}
```

- [ ] **Step 5: 接线**

`portal/cmd/backend/wire_gen.go`：在 Task 7 插入的 `executor.SetIlinkPusher(weixinIlinkUsecase)` 之后插入：

```go
	weixinIlinkUsecase.SetPeerSessions(channelPeerSessionRepo)
	chatService.SetWeixinIlink(weixinIlinkUsecase)
	agentService.SetWeixinIlink(weixinIlinkUsecase)
```

完成后这一段应为：

```go
	runtimeService.SetIlink(weixinIlinkUsecase)
	cronUsecase.SetWeixinIlink(channelRepo, channelPeerSessionRepo, weixinIlinkRepo)
	executor.SetIlinkPusher(weixinIlinkUsecase)
	weixinIlinkUsecase.SetPeerSessions(channelPeerSessionRepo)
	chatService.SetWeixinIlink(weixinIlinkUsecase)
	agentService.SetWeixinIlink(weixinIlinkUsecase)
```

- [ ] **Step 6: 运行确认通过**

```powershell
gofmt -w internal/service/weixin_bind.go internal/service/weixin_bind_test.go internal/service/chat.go internal/service/agent.go cmd/backend/wire_gen.go
go build ./...
go vet ./internal/service/ ./cmd/backend/
go test ./internal/service/ -count=1
```

Expected: `ok  backend/internal/service`。

- [ ] **Step 7: Commit**

```powershell
git add internal/service/weixin_bind.go internal/service/weixin_bind_test.go internal/service/chat.go internal/service/agent.go cmd/backend/wire_gen.go
git commit -m "feat(service): Agent 关联启用的 weixin_ilink 渠道时注册 send_to_weixin 并接线"
```

---

### Task 13: Web 纯函数与 API 字段

**Files:**
- Modify: `web/src/utils/ilink.ts`、`web/src/api/client.ts`
- Test: `web/tests/ilink.test.ts`

- [ ] **Step 1: 写失败测试**

```powershell
Set-Location ../web
```

`web/tests/ilink.test.ts`：

1. import 改为：

```ts
import {
  cronIlinkPeerError,
  ilinkLoginHint,
  ilinkPeerActivation,
  ilinkPeerLabel,
  ilinkPeerOptions,
  ilinkPeerTail,
  mergeRemarkDrafts,
} from '../src/utils/ilink.ts'
```

2. 在 `describe('mergeRemarkDrafts', () => {` 之前插入：

```ts
describe('cronIlinkPeerError', () => {
  it('requires a recipient only for weixin_ilink channel delivery', () => {
    assert.match(cronIlinkPeerError({ deliveryMode: 'channel', channelType: 'weixin_ilink', peerId: ' ' }), /接收人/)
    assert.equal(cronIlinkPeerError({ deliveryMode: 'channel', channelType: 'weixin_ilink', peerId: 'p1' }), '')
    assert.equal(cronIlinkPeerError({ deliveryMode: 'channel', channelType: 'wecom', peerId: '' }), '')
    assert.equal(cronIlinkPeerError({ deliveryMode: 'none', channelType: 'weixin_ilink', peerId: '' }), '')
  })
})

describe('ilinkPeerOptions', () => {
  const peers = [
    { peer_id: 'aaaa1111@im.wechat', allowed: true, remark: '张三' },
    { peer_id: 'bbbb2222@im.wechat', allowed: false, remark: '' },
    { peer_id: 'cccc3333@im.wechat', allowed: true, remark: '' },
  ]
  it('lists allowed peers only', () => {
    assert.deepEqual(ilinkPeerOptions(peers, ''), [
      { value: 'aaaa1111@im.wechat', label: '张三（尾号 1111）' },
      { value: 'cccc3333@im.wechat', label: '尾号 3333' },
    ])
  })
  it('keeps a saved recipient that is no longer allowed, marked', () => {
    const opts = ilinkPeerOptions(peers, 'bbbb2222@im.wechat')
    assert.deepEqual(opts[0], { value: 'bbbb2222@im.wechat', label: '尾号 2222（未放行）' })
    assert.equal(ilinkPeerOptions([], 'gone9999@im.wechat')[0].label, '尾号 9999（未放行）')
  })
})

```

- [ ] **Step 2: 运行确认失败**

Run: `npm test`  
Expected: FAIL（`cronIlinkPeerError` / `ilinkPeerOptions` 未导出）。

- [ ] **Step 3: 实现**

`web/src/utils/ilink.ts`：在 `type RemarkRow = { peer_id: string; remark: string }` 之前插入：

```ts
/** Cron delivery to a weixin_ilink channel needs a recipient; '' means valid. */
export function cronIlinkPeerError(input: { deliveryMode?: string; channelType?: string; peerId: string }): string {
  if (input.deliveryMode !== 'channel' || input.channelType !== 'weixin_ilink') return ''
  return input.peerId.trim() ? '' : '投递到微信渠道必须选择接收人。'
}

type PeerOptionRow = { peer_id: string; allowed: boolean; remark?: string }

/** Allowed peers for the cron recipient select; a saved recipient that is no longer allowed stays visible and marked. */
export function ilinkPeerOptions(peers: PeerOptionRow[], current: string): Array<{ value: string; label: string }> {
  const options = peers.filter((p) => p.allowed).map((p) => ({ value: p.peer_id, label: ilinkPeerLabel(p) }))
  if (current && !options.some((o) => o.value === current)) {
    const known = peers.find((p) => p.peer_id === current)
    options.unshift({ value: current, label: `${ilinkPeerLabel(known ?? { peer_id: current })}（未放行）` })
  }
  return options
}

```

`web/src/api/client.ts`：

1. `interface CronTask` 中，在 `delivery_channel_id?: string` 之后插入：

```ts
  /** weixin_ilink recipient (raw WeChat user id) */
  delivery_peer_id?: string
```

2. `interface CreateCronTaskRequest` 中，在 `delivery_channel_id?: string` 之后插入：

```ts
  delivery_peer_id?: string
```

3. `normalizeCronTask` 中，在 `delivery_channel_id:` 两行之后插入：

```ts
    delivery_peer_id: (raw.delivery_peer_id as string | undefined) ?? (raw.deliveryPeerId as string | undefined),
```

- [ ] **Step 4: 运行确认通过**

```powershell
npm test
npx tsc
```

Expected: 测试全部通过；`tsc` 无错误。

- [ ] **Step 5: Commit**

```powershell
git add src/utils/ilink.ts src/api/client.ts tests/ilink.test.ts
git commit -m "feat(web): Cron 微信接收人校验与选项纯函数、delivery_peer_id 字段"
```

---

### Task 14: Web Cron 表单的微信接收人下拉

**Files:**
- Modify: `web/src/pages/CronTaskForm.tsx`

- [ ] **Step 1: 实现**

`web/src/pages/CronTaskForm.tsx`：

1. import 改为：

```tsx
import { agentApi, channelApi, cronApi, ilinkApi, type Agent, type Channel, type CreateCronTaskRequest, type IlinkPeer } from '../api/client'
import { getStoredOrgId } from '../api/auth'
import { cronIlinkPeerError, ilinkPeerOptions } from '../utils/ilink'
```

2. 在 `const [deliveryChannelId, setDeliveryChannelId] = useState('')` 之后插入：

```tsx
  const [deliveryPeerId, setDeliveryPeerId] = useState('')
  const [ilinkPeers, setIlinkPeers] = useState<IlinkPeer[]>([])
  const [ilinkPeersError, setIlinkPeersError] = useState('')
```

3. `applyTask` 中，在 `setDeliveryChannelId(task.delivery_channel_id || '')` 之后插入：

```tsx
    setDeliveryPeerId(task.delivery_peer_id || '')
```

4. 在 `applyTask` 函数结束（`}`）之后、第一个 `useEffect(` 之前插入：

```tsx

  const selectedChannelType = channels.find((c) => c.id === deliveryChannelId)?.type
  const isIlinkDelivery = deliveryMode === 'channel' && selectedChannelType === 'weixin_ilink'

  useEffect(() => {
    setIlinkPeersError('')
    if (!isIlinkDelivery) {
      setIlinkPeers([])
      return
    }
    let cancelled = false
    ilinkApi
      .listPeers(deliveryChannelId)
      .then((peers) => {
        if (!cancelled) setIlinkPeers(peers)
      })
      .catch((e) => {
        if (!cancelled) setIlinkPeersError((e as Error).message)
      })
    return () => {
      cancelled = true
    }
  }, [isIlinkDelivery, deliveryChannelId])
```

5. `handleSubmit` 中，在 `Payload content is required.` 那段校验之后插入：

```tsx
    const peerError = cronIlinkPeerError({ deliveryMode, channelType: selectedChannelType, peerId: deliveryPeerId })
    if (peerError) {
      setError(peerError)
      return
    }
```

6. 构造 `data` 时，在 `delivery_channel_id: deliveryChannelId || undefined,` 之后插入：

```tsx
      // '' (not undefined) so an update clears the recipient when the task stops delivering to weixin_ilink
      delivery_peer_id: isIlinkDelivery ? deliveryPeerId : '',
```

7. 把 `{deliveryMode === 'channel' && ( ... )}` 渠道选择块整体替换为：

```tsx
          {deliveryMode === 'channel' && (
            <div className="form-group">
              <label>Channel</label>
              <select
                value={deliveryChannelId}
                onChange={(e) => {
                  setDeliveryChannelId(e.target.value)
                  setDeliveryPeerId('')
                }}
              >
                <option value="">Select channel</option>
                {channels.map((channel) => (
                  <option key={channel.id} value={channel.id}>{channel.channel_id}</option>
                ))}
              </select>
            </div>
          )}

          {isIlinkDelivery && (
            <div className="form-group">
              <label>接收人 *</label>
              <select value={deliveryPeerId} onChange={(e) => setDeliveryPeerId(e.target.value)}>
                <option value="">选择接收人</option>
                {ilinkPeerOptions(ilinkPeers, deliveryPeerId).map((o) => (
                  <option key={o.value} value={o.value}>{o.label}</option>
                ))}
              </select>
              {ilinkPeersError ? (
                <div className="error">{ilinkPeersError}</div>
              ) : (
                <p className="muted" style={{ margin: '0.35rem 0 0', fontSize: '0.8rem' }}>
                  只列出名册中已放行的微信用户；对方需在 bot 最近一次扫码登录后给 bot 发过消息才能收到推送。
                </p>
              )}
            </div>
          )}
```

- [ ] **Step 2: 类型检查与测试**

```powershell
npx tsc
npm test
```

Expected: 均无错误。

- [ ] **Step 3: Commit**

```powershell
git add src/pages/CronTaskForm.tsx
git commit -m "feat(web): Cron 投递到微信渠道时选择接收人"
Set-Location ../portal
```

---

### Task 15: 全量验证与手工验收

**Files:** 无（只在发现问题时修复并单独提交）

- [ ] **Step 1: 全量 Go 测试**

在 `portal/` 目录执行：

```powershell
Remove-Item Env:GOROOT -ErrorAction SilentlyContinue
gofmt -l (git diff --name-only c2c163a -- . | Where-Object { $_ -like "*.go" -and $_ -notlike "*.pb.go" } | ForEach-Object { $_ -replace "^portal/", "" })
go build ./...
go vet ./internal/biz/ ./internal/data/ ./internal/cron/ ./internal/chat/ ./internal/service/ ./internal/server/ ./internal/runtime/ ./internal/ilink/ ./cmd/backend/
go test ./internal/biz/ ./internal/data/ ./internal/cron/ ./internal/chat/ ./internal/service/ ./internal/server/ ./internal/runtime/ ./internal/ilink/ -count=1 2>&1 | Select-String -Pattern "^(ok|FAIL|---)"
Set-Location ../framework
gofmt -l tool/cronjob_tool.go tool/cronjob_tool_test.go
go vet ./tool/
go test ./tool/ -count=1 2>&1 | Select-String -Pattern "^(ok|FAIL|---)"
Set-Location ../web
npx tsc
npm test
Set-Location ../portal
```

（`c2c163a` 是本计划开始时 `main` 的 HEAD；若基线不同，换成 Task 0 时的 `git rev-parse HEAD`。）

Expected: `gofmt -l` 无输出；与 Task 0 基线相比没有新增 `FAIL`（`internal/chat` 只允许出现 Task 0 记录的两个已知失败）；`tsc` 无错误，`npm test` 全部通过。

- [ ] **Step 2: 手工验收（需要 P2 Gateway 已部署、用户扫码配合）**

按项目 README 启动 Portal、Gateway 与 Web（MySQL 已就绪），并设置 `CRONJOB_TOOL_ENABLED=true`：

1. Channels → `weixin_ilink` 渠道已扫码、`connected`，`default_agent` 指向测试 Agent；名册中扫码人"已放行"且"已激活"。
2. Cron → 新建：Delivery Mode 选 Channel、渠道选该微信渠道 → 出现"接收人"下拉，只列已放行对端；不选接收人点保存 → 提示"投递到微信渠道必须选择接收人。"。选扫码人保存 → 点"立即运行" → 微信收到"[定时任务 xxx] …"，执行历史 `delivery_ok=true`。
3. 名册里把某人"取消放行" → 用 API 把该任务 `delivery_peer_id` 改成此人 → 返回 400"目标 … 未在白名单"；只改 `enabled` 仍成功。
4. 选一个"待激活"（无 `context_token`）的已放行对端建任务并运行 → 执行历史 `delivery_error` 为"…对方尚未与 bot 对话（或 bot 已重新登录）…"。
5. 在微信里对 bot 说"每天早上 9 点提醒我喝水"（让 Agent 调 `cronjob` 且不指定投递方式）→ Portal 中新任务 `delivery_mode=channel`、渠道为该微信渠道、接收人为本人；运行后推回本人微信。
6. Web 会话中让该 Agent "用 send_to_weixin 给 {peer_id} 发一句你好" → 微信收到，工具结果含 `delivery_id`；`channel_deliveries` 有一条 `attempts=1` 的记录。在微信会话里说"再给我发一条测试消息" → 不需要 `peer_id` 也能发回本人。
7. 重新扫码后立即运行第 2 步的任务 → `delivery_error` 为"对方尚未与 bot 对话（或 bot 已重新登录）…"；本人给 bot 发一条消息后再运行 → 成功。

把结果（通过 / 问题）记录到最终报告。发现问题时修复、补测试、单独提交。

---

## 完成标准

- Task 1–14 的测试全部通过，Task 15 全量测试无新增失败。
- 手工验收 1–7 通过（或问题已修复）。
- 未改动 `gateway/`；未提交任何与本计划无关的文件；日志与 API 未明文暴露 `bot_token` / `context_token`。
