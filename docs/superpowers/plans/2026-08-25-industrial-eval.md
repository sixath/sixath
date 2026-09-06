# 切片 A：黄金会话评测网 Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** 用 `TestEvalGolden_*` 把 bf26 / e9d4 / c304 / 8555 钉成 CI 可红的闸门回归；c7aa Skip 占位。

**Architecture:** 不新建子包。`package chat` 与 `package agent` 各加一个 `evalgolden_test.go`，直接调用现网纯函数。脚本只包两行 `go test -run TestEvalGolden_`。不改生产闸门逻辑。

**Tech Stack:** Go 测试；PowerShell 脚本。根 `go.mod` 是空 module，必须 `cd portal` / `cd framework`。

**Spec:** `docs/superpowers/specs/2026-08-25-industrial-eval-design.md`  
**父规格:** `docs/superpowers/specs/2026-08-25-industrial-agent-program-design.md`

**不做:** live LLM；改工具 JSON；B/C/D/E 产品代码；自动 git commit（除非用户另行要求）。

---

## File Structure

| 文件 | 责任 |
|------|------|
| `portal/internal/chat/evalgolden_test.go` | `TestEvalGolden_bf26`、`TestEvalGolden_8555` |
| `framework/agent/evalgolden_test.go` | `TestEvalGolden_e9d4`、`TestEvalGolden_c304`、`TestEvalGolden_c7aa` |
| `scripts/industrial-eval.ps1` | 跑上面两个 `-run TestEvalGolden_` |

复用（不要复制实现）：`bf26Q`（`task_lock_test.go`）、`helperSource()`（`code_quote_gate_test.go`）、`familySet`、`credentialSolicitationRedirect`。

---

### Task 1: portal 金样例 bf26 + 8555

**Files:**
- Create: `portal/internal/chat/evalgolden_test.go`

- [ ] **Step 1: 写测试**

```go
package chat

import (
	"context"
	"testing"

	"github.com/sixath/framework/agent"
	"github.com/sixath/framework/model"
)

func TestEvalGolden_bf26(t *testing.T) {
	lock := BuildTurnTaskLock(bf26Q, []model.Message{
		{Role: "user", Content: "这条流水4_a8uva8m5tpsl 正常吗"},
		{Role: "assistant", Content: "流水 4_a8uva8m5tpsl 正常。uid=104551174 ugid=796"},
		{Role: "user", Content: bf26Q},
	})
	if lock.Q != bf26Q || lock.Delivery != "" {
		t.Fatalf("%+v", lock)
	}
}

func TestEvalGolden_8555(t *testing.T) {
	gate := TurnIntentGate{ActiveFamilies: familySet([]string{FamilyCore, FamilyCode})}
	res := gate.Evaluate(context.Background(), agent.PostModelPolicyInput{
		Req:           &agent.Request{Messages: []model.Message{{Role: "user", Content: "会发生什么"}}},
		AssistantText: "加载手册",
		ToolStep: model.ToolStep{Used: true, ToolCalls: []model.ToolCall{
			{ID: "1", Name: "skill_view", Arguments: map[string]any{"name": "demo"}},
		}},
	})
	if res.Decision != agent.PostModelRetry || res.Reason != "family_dropped_all" {
		t.Fatalf("got %v %q", res.Decision, res.Reason)
	}
}
```

- [ ] **Step 2: 跑测试（现网已有逻辑，预期 PASS）**

Run: `cd E:\workspace\github\sixath\sixath\portal; go test ./internal/chat -count=1 -run TestEvalGolden_`  
Expected: PASS（2 tests）

这是回归网不是新闸。不要为「先红」去改生产代码。

---

### Task 2: framework 金样例 e9d4 + c304 + c7aa

**Files:**
- Create: `framework/agent/evalgolden_test.go`

- [ ] **Step 1: 写测试**

```go
package agent

import (
	"context"
	"testing"

	"github.com/sixath/framework/tool"
)

func TestEvalGolden_e9d4(t *testing.T) {
	cat := tool.ToolCatalog{Entries: []tool.ToolCatalogEntry{{
		Name: "execute_read", Available: true,
		Bindings:    map[string]string{"datasource_id": "prod_mysql", "type": "mysql"},
		SearchHints: []string{"mysql", "数据库", "host", "password"},
	}}}
	ctx := context.WithValue(context.Background(), tool.ContextKeyToolCatalog, cat)
	trace := &RunTrace{ToolCalls: []ToolCallRecord{{
		ToolName: "es_log_query", Allowed: true, Result: map[string]any{"ok": true},
	}}}
	ask := "请提供 MySQL 的 Host、Port、用户名、密码"
	if _, _, ok := credentialSolicitationRedirect(ctx, ask, 0, trace, "GetGameInfo 失败原因"); ok {
		t.Fatal("already used bound evidence this turn; must not redirect")
	}
}

func TestEvalGolden_c304(t *testing.T) {
	src := helperSource()
	got := EvaluateScenarioPathGate("区域已有用户（errcode=1105）会不会写本地映射？",
		"区域已有用户时会把 UID 写入本地映射表。", src)
	if got.Allow {
		t.Fatalf("1105 write prose must fail, got %#v", got)
	}
}

func TestEvalGolden_c7aa(t *testing.T) {
	// E2：去掉 Skip，在本测试断言入边 / inbound_empty。
	t.Skip("E2 inbound")
}
```

`helperSource` 已在同包 `code_quote_gate_test.go`。

- [ ] **Step 2: 跑测试**

Run: `cd E:\workspace\github\sixath\sixath\framework; go test ./agent -count=1 -run TestEvalGolden_ -v`  
Expected: PASS；verbose 含 `TestEvalGolden_c7aa` SKIP

---

### Task 3: 脚本

**Files:**
- Create: `scripts/industrial-eval.ps1`

- [ ] **Step 1: 写脚本**

仓库根 = `scripts/` 的上一级：

```powershell
$ErrorActionPreference = "Stop"
$root = Resolve-Path (Join-Path $PSScriptRoot "..")
Set-Location (Join-Path $root "portal")
go test ./internal/chat -count=1 -run TestEvalGolden_ -v
if ($LASTEXITCODE -ne 0) { exit $LASTEXITCODE }
Set-Location (Join-Path $root "framework")
go test ./agent -count=1 -run TestEvalGolden_ -v
exit $LASTEXITCODE
```

- [ ] **Step 2: 跑脚本**

Run: `powershell -File E:\workspace\github\sixath\sixath\scripts\industrial-eval.ps1`  
Expected: exit 0；两包 TestEvalGolden_ 绿

---

### Task 4: 核对破坏会红（手工，不提交破坏）

- [ ] **Step 1:** 临时把 `BuildTurnTaskLock` 改成始终 `lock.Q = d`（忽略继承），确认 `TestEvalGolden_bf26` FAIL，然后**还原**。不要提交这次破坏。不要只改测试里的 `if` 条件。

---

## Execution

不要自动 `git commit`。实现按 Task 1→3。完成后告诉用户可跑 `scripts/industrial-eval.ps1`。
