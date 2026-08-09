# Session 上下文压缩 — 阶段一 Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Portal 启用 L2 摘要 + tool 预剪枝 + token-soft 触发，并将 Agent 历史加载改为 rune 预算驱动（修复当前 ASC 取最旧 N 条的问题）。

**Architecture:** 扩展 `conf.proto` 的 `context_compression` 节；`chat` 包提供 `BuildContextCompressionOptions` 装配 auxiliary 与 L2Runtime；`biz/data` 新增 `ListBySessionBudget`（DESC 累加 rune 后正序返回）；`ChatService` 组装 Agent 消息时使用 budget 加载并记录 `ContextOps` 日志。Framework 补全 `ReActAgent.modelOpts` 对 `MaxContextTokensSoft` / `TokenEstimateAlpha` 的传递。

**Tech Stack:** Go 1.25、Kratos、GORM MySQL、protobuf、framework/model L2 流水线

**Spec:** [2026-05-26-session-context-compression-design.md](../specs/2026-05-26-session-context-compression-design.md) §3  
**前置:** Framework `PrepareChatContextCtx` / `L2Runtime` / `WithReActContextCompression` 已实现  
**非目标（阶段一）:** compact boundary 落库、UI 变更、snipCompact、手动 compact API  
**阶段二计划:** [2026-05-26-session-context-compression-phase2.md](./2026-05-26-session-context-compression-phase2.md)

---

## File Structure

| 文件 | 职责 |
|------|------|
| `portal/internal/conf/conf.proto` | 新增 `ContextCompression` message + Bootstrap 字段 |
| `portal/internal/conf/context_compression_env.go` | **新建** — `EnrichContextCompressionFromEnv` |
| `portal/cmd/backend/main.go` | 加载后调用 enrich + `chat.SetContextCompressionSettings` |
| `portal/internal/chat/context_compression.go` | **新建** — 默认值、settings、`BuildContextCompressionOptions` |
| `portal/internal/chat/context_compression_test.go` | **新建** — options 装配单测 |
| `portal/internal/chat/agent_builder.go` | 使用 settings 中的 `l0_max_runes`（若已设） |
| `framework/agent/react_agent.go` | `ReActConfig` 增加 soft token 字段；`modelOpts` 传递 |
| `framework/agent/react_agent_test.go` | 断言 soft token 进入 CallConfig |
| `portal/internal/biz/chat.go` | `ListBudgetOpts`、`ListMessagesForAgent` |
| `portal/internal/biz/message_budget.go` | **新建** — `AccumulateMessagesByRunes` 纯函数 |
| `portal/internal/biz/message_budget_test.go` | **新建** — budget 算法单测 |
| `portal/internal/data/chat_mysql.go` | `ListBySessionBudget` SQL |
| `portal/internal/data/chat_mysql_budget_test.go` | **新建** — sqlmock 或 sqlite 单测（可选 mock repo） |
| `portal/internal/service/chat.go` | Agent 路径改用 budget 加载 + context_ops 日志 |
| `portal/internal/service/chat_context_test.go` | **新建** — 组装 helper 单测 |
| `portal/configs/config.yaml` | 注释示例节（默认 `l2_enabled: false`） |

**不改:** `ListMessages` API（UI 仍 limit=100 ASC）；阶段二再处理 compact 加载

---

## 常量（写死）

```go
// portal/internal/chat/context_compression.go
const (
	defaultSoftTokenEstimate       = 96000
	defaultL0MaxRunes              = 200_000
	defaultHistoryLoadMaxRunes     = 120_000
	defaultHistoryLoadMaxMessages  = 200
	defaultToolPrePruneRunes       = 8000
	defaultL2MaxFailures           = 3
	defaultL2CooldownSec           = 600
	defaultEstimateAlpha           = 1.5
)
```

---

### Task 1: Proto — `ContextCompression`

**Files:**
- Modify: `portal/internal/conf/conf.proto`
- Regenerate: `portal/internal/conf/conf.pb.go`（`make api` 或项目等价命令）

- [ ] **Step 1: 编辑 `conf.proto`**

在 `message Web { ... }` 之后、`message Data` 之前插入：

```protobuf
message ContextCompression {
  bool l2_enabled = 1;
  GrowthLLM auxiliary = 2;
  int32 soft_token_estimate = 3;
  int32 max_consecutive_failures = 4;
  int32 cooldown_sec = 5;
  double estimate_alpha = 6;
  int32 tool_content_pre_prune_runes = 7;
  int32 l0_max_runes = 8;
  int32 history_load_max_runes = 9;
  int32 history_load_max_messages = 10;
}
```

在 `message Bootstrap` 增加：

```protobuf
  ContextCompression context_compression = 6;
```

- [ ] **Step 2: 重新生成 protobuf**

Run（在 `portal/` 目录）:

```bash
make api
```

Expected: `conf.pb.go` 含 `ContextCompression` 与 `Bootstrap.GetContextCompression()`

- [ ] **Step 3: Commit**

```bash
git add portal/internal/conf/conf.proto portal/internal/conf/conf.pb.go
git commit -m "feat(portal): add context_compression config proto"
```

---

### Task 2: 环境变量 enrich + main 注入

**Files:**
- Create: `portal/internal/conf/context_compression_env.go`
- Modify: `portal/cmd/backend/main.go`

- [ ] **Step 1: 编写 `context_compression_env.go`**

```go
package conf

import (
	"os"
	"strconv"
	"strings"
)

// EnrichContextCompressionFromEnv 补全 auxiliary（YAML 未配 model 时）。
// SATH_CONTEXT_L2_ENABLED=true、SATH_CONTEXT_L2_MODEL、SATH_CONTEXT_L2_PROVIDER、
// SATH_CONTEXT_L2_API_KEY、SATH_CONTEXT_L2_BASE_URL
func EnrichContextCompressionFromEnv(c *ContextCompression) {
	if c == nil {
		return
	}
	if v := strings.TrimSpace(os.Getenv("SATH_CONTEXT_L2_ENABLED")); v != "" {
		c.L2Enabled = v == "1" || v == "true" || v == "yes"
	}
	modelName := strings.TrimSpace(os.Getenv("SATH_CONTEXT_L2_MODEL"))
	if modelName == "" {
		return
	}
	if c.Auxiliary == nil {
		c.Auxiliary = &GrowthLLM{}
	}
	if strings.TrimSpace(c.Auxiliary.GetModel()) == "" {
		c.Auxiliary.Model = modelName
	}
	if strings.TrimSpace(c.Auxiliary.GetProvider()) == "" {
		if p := strings.TrimSpace(os.Getenv("SATH_CONTEXT_L2_PROVIDER")); p != "" {
			c.Auxiliary.Provider = p
		}
	}
	if strings.TrimSpace(c.Auxiliary.GetApiKey()) == "" {
		if k := strings.TrimSpace(os.Getenv("SATH_CONTEXT_L2_API_KEY")); k != "" {
			c.Auxiliary.ApiKey = k
		}
	}
	if strings.TrimSpace(c.Auxiliary.GetBaseUrl()) == "" {
		if u := strings.TrimSpace(os.Getenv("SATH_CONTEXT_L2_BASE_URL")); u != "" {
			c.Auxiliary.BaseUrl = u
		}
	}
	if c.GetSoftTokenEstimate() <= 0 {
		if v := strings.TrimSpace(os.Getenv("SATH_CONTEXT_L2_SOFT_TOKENS")); v != "" {
			if n, err := strconv.Atoi(v); err == nil && n > 0 {
				c.SoftTokenEstimate = int32(n)
			}
		}
	}
}
```

- [ ] **Step 2: 修改 `main.go`（`conf.EnrichGrowthFromEnv` 之后）**

```go
	conf.EnrichContextCompressionFromEnv(bc.ContextCompression)
	chat.SetContextCompressionSettings(chat.ContextCompressionSettingsFromProto(bc.GetContextCompression()))
```

- [ ] **Step 3: Commit**

```bash
git add portal/internal/conf/context_compression_env.go portal/cmd/backend/main.go
git commit -m "feat(portal): load context_compression from config and env"
```

---

### Task 3: Framework — ReAct 传递 token-soft 阈值

**Files:**
- Modify: `framework/agent/react_agent.go`
- Modify: `framework/agent/react_agent_test.go`

- [ ] **Step 1: 扩展 `ReActConfig` 与 `ContextCompressionConfig`**

在 `ReActConfig` 增加：

```go
	MaxContextTokensSoft int
	TokenEstimateAlpha   float64
```

在 `WithReActContextCompression` 末尾增加：

```go
		c.MaxContextTokensSoft = cc.SoftTokenEstimate
		c.TokenEstimateAlpha = cc.EstimateAlpha
```

- [ ] **Step 2: 扩展 `modelOpts`**

```go
	if a.config.MaxContextTokensSoft > 0 {
		opts = append(opts, model.WithMaxContextTokensSoft(a.config.MaxContextTokensSoft))
	}
	if a.config.TokenEstimateAlpha > 0 {
		opts = append(opts, model.WithTokenEstimateAlpha(a.config.TokenEstimateAlpha))
	}
```

- [ ] **Step 3: 编写失败测试 + 实现验证**

在 `react_agent_test.go` 增加测试：fake model 记录 `ApplyOptions` 结果，或沿用现有 `TestReActAgent_Run_MetadataTraceJSONWithContextOps` 模式，传入 `WithReActContextCompression` 且 `SoftTokenEstimate: 100`，断言 trace 含 `l0_compress_tokens` 或 `l2_summarize`。

Run:

```bash
cd framework && go test ./agent/... -run ContextOps -count=1
```

Expected: PASS

- [ ] **Step 4: Commit**

```bash
git add framework/agent/react_agent.go framework/agent/react_agent_test.go
git commit -m "feat(agent): wire MaxContextTokensSoft into model CallConfig"
```

---

### Task 4: `chat` 包 — 装配 L2 options

**Files:**
- Create: `portal/internal/chat/context_compression.go`
- Create: `portal/internal/chat/context_compression_test.go`

- [ ] **Step 1: 编写 `context_compression.go`**

核心 API：

```go
type ContextCompressionSettings struct {
	L2Enabled                bool
	Auxiliary                model.ModelConfig // provider/model/key/base
	SoftTokenEstimate        int
	MaxConsecutiveFailures   int
	CooldownSec              int
	EstimateAlpha            float64
	ToolContentPrePruneRunes int
	L0MaxRunes               int
	HistoryLoadMaxRunes      int
	HistoryLoadMaxMessages   int
}

var globalContextCompression ContextCompressionSettings

func SetContextCompressionSettings(s ContextCompressionSettings) { globalContextCompression = s }
func ContextCompressionSettings() ContextCompressionSettings { return globalContextCompression }

func ContextCompressionSettingsFromProto(c *conf.ContextCompression) ContextCompressionSettings { /* 填默认值 */ }

func BuildContextCompressionOptions() []agent.ReActOption {
	s := globalContextCompression
	opts := make([]agent.ReActOption, 0, 2)
	if s.L0MaxRunes > 0 {
		opts = append(opts, agent.WithReActMaxContextRunes(s.L0MaxRunes))
	}
	if !s.L2Enabled {
		return opts
	}
	aux := s.Auxiliary
	if strings.TrimSpace(aux.Model) == "" {
		return opts
	}
	m, err := model.NewModelFromConfig(aux)
	if err != nil {
		return opts // 或 log — 生产可在 main 校验
	}
	opts = append(opts, agent.WithReActContextCompression(&agent.ContextCompressionConfig{
		L2Enabled:                true,
		AuxiliaryModel:           m,
		SoftTokenEstimate:        s.SoftTokenEstimate,
		MaxConsecutiveFailures:   s.MaxConsecutiveFailures,
		CooldownSec:              s.CooldownSec,
		EstimateAlpha:            s.EstimateAlpha,
		ToolContentPrePruneRunes: s.ToolContentPrePruneRunes,
	}))
	return opts
}
```

`GrowthLLM` → `model.ModelConfig` helper：

```go
func modelConfigFromGrowthLLM(g *conf.GrowthLLM) model.ModelConfig {
	if g == nil {
		return model.ModelConfig{}
	}
	return model.ModelConfig{
		Provider: g.GetProvider(),
		Model:    g.GetModel(),
		APIKey:   g.GetApiKey(),
		BaseURL:  g.GetBaseUrl(),
	}
}
```

- [ ] **Step 2: 单测 `context_compression_test.go`**

```go
func TestBuildContextCompressionOptions_L2Disabled(t *testing.T) {
	SetContextCompressionSettings(ContextCompressionSettings{L2Enabled: false, L0MaxRunes: 1000})
	opts := BuildContextCompressionOptions()
	// NewReActAgent + fake，断言 L2Runtime nil
}

func TestBuildContextCompressionOptions_L2Enabled(t *testing.T) {
	SetContextCompressionSettings(ContextCompressionSettings{
		L2Enabled: true,
		Auxiliary: model.ModelConfig{Provider: "fake", Model: "mini"},
		SoftTokenEstimate: 96000,
		L0MaxRunes: 200000,
	})
	// 使用 framework 已有 fake model 或 stub NewModelFromConfig 若可注入
}
```

Run:

```bash
cd portal && go test ./internal/chat/... -run ContextCompression -count=1
```

- [ ] **Step 3: Commit**

```bash
git add portal/internal/chat/context_compression.go portal/internal/chat/context_compression_test.go
git commit -m "feat(portal): BuildContextCompressionOptions for L2 wiring"
```

---

### Task 5: `ListBySessionBudget` — biz + data

**Files:**
- Create: `portal/internal/biz/message_budget.go`
- Create: `portal/internal/biz/message_budget_test.go`
- Modify: `portal/internal/biz/chat.go`
- Modify: `portal/internal/data/chat_mysql.go`

- [ ] **Step 1: 纯函数单测（TDD）**

`message_budget.go`:

```go
func messageRunes(m *ChatMessage) int {
	return utf8.RuneCountInString(m.Content)
}

// SelectMessagesWithinBudget 从 msgs（时间升序）中保留「最新且 rune 总和 ≤ maxRunes」的连续后缀。
func SelectMessagesWithinBudget(msgs []*ChatMessage, maxRunes, maxMessages int) []*ChatMessage {
	if maxMessages <= 0 {
		maxMessages = len(msgs)
	}
	if len(msgs) > maxMessages {
		msgs = msgs[len(msgs)-maxMessages:]
	}
	if maxRunes <= 0 {
		return msgs
	}
	total := 0
	start := len(msgs)
	for i := len(msgs) - 1; i >= 0; i-- {
		r := messageRunes(msgs[i])
		if start < len(msgs) && total+r > maxRunes {
			break
		}
		total += r
		start = i
	}
	return msgs[start:]
}
```

`message_budget_test.go`：3 条消息 rune 为 100/200/300，`maxRunes=350` → 返回后两条。

Run:

```bash
cd portal && go test ./internal/biz/... -run SelectMessagesWithinBudget -count=1
```

Expected: FAIL then PASS

- [ ] **Step 2: Repo 接口 + MySQL 实现**

`biz/chat.go`:

```go
type ListBudgetOpts struct {
	MaxRunes    int
	MaxMessages int
	AfterTime   *time.Time // 阶段二使用；阶段一 nil
}

func (uc *ChatUsecase) ListMessagesForAgent(ctx context.Context, sessionID string, opts ListBudgetOpts) ([]*ChatMessage, error) {
	raw, err := uc.messageRepo.ListBySessionBudget(ctx, sessionID, opts)
	if err != nil {
		return nil, err
	}
	return SelectMessagesWithinBudget(raw, opts.MaxRunes, opts.MaxMessages), nil
}
```

`ChatMessageRepo` 增加：

```go
ListBySessionBudget(ctx context.Context, sessionID string, opts ListBudgetOpts) ([]*ChatMessage, error)
```

`chat_mysql.go`:

```go
func (r *chatMessageRepo) ListBySessionBudget(ctx context.Context, sessionID string, opts biz.ListBudgetOpts) ([]*biz.ChatMessage, error) {
	maxMsg := opts.MaxMessages
	if maxMsg <= 0 {
		maxMsg = 200
	}
	q := r.db.WithContext(ctx).Where("session_id = ?", sessionID)
	if opts.AfterTime != nil {
		q = q.Where("created_at > ?", *opts.AfterTime)
	}
	var rows []model.ChatMessage
	if err := q.Order("created_at DESC").Limit(maxMsg).Find(&rows).Error; err != nil {
		return nil, err
	}
	// 反转为 ASC
	for i, j := 0, len(rows)-1; i < j; i, j = i+1, j-1 {
		rows[i], rows[j] = rows[j], rows[i]
	}
	items := make([]*biz.ChatMessage, len(rows))
	for i, m := range rows {
		items[i] = modelMessageToBiz(m)
	}
	return items, nil
}
```

**注意:** 修复 Agent 路径「ASC+Limit 取最旧消息」缺陷；`ListBySession` 保持不变供 UI。

- [ ] **Step 3: 更新 stub repos in tests**

修改 `portal/internal/biz/chat_test.go` 等 stub 实现 `ListBySessionBudget`。

Run:

```bash
cd portal && go test ./internal/biz/... ./internal/data/... -count=1
```

- [ ] **Step 4: Commit**

```bash
git add portal/internal/biz/message_budget.go portal/internal/biz/message_budget_test.go portal/internal/biz/chat.go portal/internal/data/chat_mysql.go portal/internal/biz/chat_test.go
git commit -m "feat(portal): ListBySessionBudget for agent history loading"
```

---

### Task 6: `ChatService` — 接线 Agent 路径

**Files:**
- Modify: `portal/internal/service/chat.go`
- Create: `portal/internal/service/chat_agent_history.go`（可选拆分 helper）
- Create: `portal/internal/service/chat_context_test.go`

- [ ] **Step 1: 抽取 helper**

```go
func (s *ChatService) agentHistoryMessages(ctx context.Context, sessionID string) ([]*biz.ChatMessage, error) {
	cfg := chat.ContextCompressionSettings()
	opts := biz.ListBudgetOpts{
		MaxRunes:    cfg.HistoryLoadMaxRunes,
		MaxMessages: cfg.HistoryLoadMaxMessages,
	}
	return s.chatUC.ListMessagesForAgent(ctx, sessionID, opts)
}

func (s *ChatService) buildAgentReactOptions(extra ...agent.ReActOption) []agent.ReActOption {
	opts := chat.BuildContextCompressionOptions()
	opts = append(opts, extra...)
	return opts
}

func (s *ChatService) logContextOps(sessionID string, resp *agent.Response) {
	if resp == nil || resp.Metadata == nil {
		return
	}
	tr, ok := resp.Metadata["trace"].(*agent.RunTrace)
	if !ok || tr == nil || tr.ContextOps == nil {
		return
	}
	co := tr.ContextOps
	s.log.Infof("context_ops session_id=%s l0_dropped=%d l2_used=%v l2_hash=%s invocations=%d",
		sessionID, co.L0DroppedMessages, co.L2Used, co.L2SummaryHash, len(co.Invocations))
}
```

- [ ] **Step 2: 替换 `SendMessage` / Stream 中的调用**

将：

```go
history, err := s.chatUC.ListMessages(ctx, sessionID, maxHistory*2)
```

改为：

```go
history, err := s.agentHistoryMessages(ctx, sessionID)
```

将：

```go
a := chat.BuildReActAgent(m, reg, agentMeta.SystemPrompt, maxHistory, s.growthReActOptions()...)
```

改为：

```go
a := chat.BuildReActAgent(m, reg, agentMeta.SystemPrompt, maxHistory, s.buildAgentReactOptions(s.growthReActOptions()...)...)
```

在 `a.Run` 成功后调用 `s.logContextOps(sessionID, resp)`。

- [ ] **Step 3: 单测 helper（mock chatUC）**

- [ ] **Step 4: Commit**

```bash
git add portal/internal/service/chat.go portal/internal/service/chat_agent_history.go portal/internal/service/chat_context_test.go
git commit -m "feat(portal): wire L2 and budget history into ChatService"
```

---

### Task 7: 配置示例 + 文档注释

**Files:**
- Modify: `portal/configs/config.yaml`

- [ ] **Step 1: 追加注释块（默认不启用）**

```yaml
# context_compression:
#   l2_enabled: false
#   auxiliary:
#     provider: openai
#     model: gpt-4o-mini
#     api_key: "..."
#   soft_token_estimate: 96000
#   estimate_alpha: 1.5
#   tool_content_pre_prune_runes: 8000
#   l0_max_runes: 200000
#   history_load_max_runes: 120000
#   history_load_max_messages: 200
```

- [ ] **Step 2: 更新 spec 状态为「阶段一实施中」**（可选）

- [ ] **Step 3: Commit**

```bash
git add portal/configs/config.yaml
git commit -m "docs(portal): add context_compression config example"
```

---

### Task 8: 验收 A1–A5

- [ ] **Step 1: 全量测试**

```bash
cd framework && go test ./...
cd portal && go test ./...
```

Expected: PASS

- [ ] **Step 2: A1 — l2_enabled=false 回归**

配置关闭 L2，发送短对话，确认无 regression；Agent 历史为**最新**消息（非最旧 40 条）。

- [ ] **Step 3: A2 — 启用 L2 长会话**

本地开启 `l2_enabled: true` + auxiliary，构造大 tool 输出 fixture（或 integration test with fake auxiliary），断言 `logContextOps` 输出 `l2_used=true` 或 trace 含 `l2_summarize`。

- [ ] **Step 4: A5 — pre-prune trace**

`tool_content_pre_prune_runes: 8000`，断言 `l2_pre_prune_tool` 出现在 invocations。

- [ ] **Step 5: Commit（若有测试 fixture）**

```bash
git add ...
git commit -m "test(portal): context compression phase1 acceptance"
```

---

## Spec Coverage（自检）

| Spec §3 要求 | Task |
|--------------|------|
| conf.proto ContextCompression | Task 1 |
| BuildContextCompressionOptions | Task 4 |
| ListBySessionBudget | Task 5 |
| 触发阈值 defaults | Task 2, 4 |
| ContextOps 日志 | Task 6 |
| 验收 A1–A5 | Task 8 |
| MaxContextTokensSoft 传递 | Task 3 |

**Gap 说明:** `ListMessages` UI API 仍 limit=100 ASC（pre-existing）；阶段二 compact 加载另案处理。

---

## Execution Handoff

**Plan complete.** 阶段二见 [2026-05-26-session-context-compression-phase2.md](./2026-05-26-session-context-compression-phase2.md)。

**Two execution options:**

1. **Subagent-Driven (recommended)** — 每 Task 派生子 agent，Task 间人工/主 agent 审查  
2. **Inline Execution** — 本会话按 Task 1→8 顺序执行，每 Task 完成后 checkpoint

**Which approach?**
