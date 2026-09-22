# Investigation Gates Off Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** 部署默认关闭 HTTP 接地、每轮工具面收窄、任务锁/改题回拉；闸源码保留，总开关 `on` 即恢复现网。

**Architecture:** 在 `ChatConfig` 增加 `investigation_gates`（缺省/非法=`off`）。`chat.ApplyInvestigationGates` 在 Portal 启动时设置三层 process override。单层 env 已设置时覆盖对应层。`AppendTaskLock` 保持纯函数；`chat.go`/`agent.go` 在 `!TaskLockEnabled()` 时跳过 append 与 metadata。不删除 `http_grounding.go` / `turn_intent_gate.go` / `task_lock.go`。规格：[`2026-09-04-investigation-gates-off-design.md`](../specs/2026-09-04-investigation-gates-off-design.md)。

**Tech Stack:** Go（`portal/internal/conf`、`portal/internal/chat`、`portal/internal/service`、`portal/cmd/backend`）。

---

## File map

| Path | Responsibility |
|------|----------------|
| `portal/internal/conf/chat_config.go` | `InvestigationGates` 字段、YAML/env 加载、非法值归一为 `off` |
| `portal/internal/conf/chat_config_test.go` | 缺省/garbage/`on`/`off`、总开关 env |
| `portal/internal/chat/investigation_gates.go` | `ApplyInvestigationGates`、intent-gate/task-lock override、`MaybeApplyTaskLock` |
| `portal/internal/chat/investigation_gates_test.go` | 总开关 off 时三层关闭；单层 env 覆盖；忽略旧 YAML surface；MaybeApply 跳过 |
| `portal/internal/chat/turn_intent_gate.go` | `NewTurnIntentGate` 走 `TurnIntentGateEnabled()` |
| `portal/internal/service/chat.go` | 同步+SSE：跳过任务锁 |
| `portal/internal/service/agent.go` | 跳过任务锁 |
| `portal/cmd/backend/main.go` | 启动时 Apply；非法值 log warn |
| `portal/configs/config.yaml` | `chat.investigation_gates: off` |
| `portal/configs/config.docker.yaml` | 同上，给容器部署看见开关 |
| `portal/docs/turn-tool-surface.md` | 写上总开关与优先级 |

测试一律在 **`portal/` 模块根**执行（`cd portal`）。不要改：空击措辞闸、凭据闸、code claim、证据闸、SQL heal、spill、技能自动匹配。不要把 `_neo4j_q/` 当夹具。

规格 §6.2 写「`AppendTaskLock` 原样返回」；§4 允许二选一。本计划选调用点包装：`AppendTaskLock` 纯函数仍追加（现网 `task_lock_test` 不改），`MaybeApplyTaskLock` / `MaybeMergeTaskLockMetadata` 在 `!TaskLockEnabled()` 时跳过，从而 system prompt 无【本轮任务锁】。

---

### Task 1: ChatConfig 解析总开关

**Files:**
- Modify: `portal/internal/conf/chat_config.go`
- Modify: `portal/internal/conf/chat_config_test.go`

- [ ] **Step 1: Write failing tests**

在 `chat_config_test.go` 追加（清空 `SATH_INVESTIGATION_GATES` / `SATH_TURN_TOOL_SURFACE`）：

```go
func TestLoadChatFromConfigPath_investigationGatesDefaultOff(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.yaml")
	if err := os.WriteFile(path, []byte("server:\n  http:\n    addr: \":8000\"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	t.Setenv("SATH_INVESTIGATION_GATES", "")
	cfg, err := LoadChatFromConfigPath(dir)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.InvestigationGatesNormalized() != "off" {
		t.Fatalf("got %q, want off", cfg.InvestigationGatesNormalized())
	}
}

func TestLoadChatFromConfigPath_investigationGatesGarbageOff(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.yaml")
	if err := os.WriteFile(path, []byte("chat:\n  investigation_gates: garbage\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	t.Setenv("SATH_INVESTIGATION_GATES", "")
	cfg, err := LoadChatFromConfigPath(dir)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.InvestigationGatesNormalized() != "off" {
		t.Fatalf("got %q", cfg.InvestigationGatesNormalized())
	}
}

func TestLoadChatFromConfigPath_investigationGatesOn(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.yaml")
	if err := os.WriteFile(path, []byte("chat:\n  investigation_gates: on\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	t.Setenv("SATH_INVESTIGATION_GATES", "")
	cfg, err := LoadChatFromConfigPath(dir)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.InvestigationGatesNormalized() != "on" {
		t.Fatalf("got %q", cfg.InvestigationGatesNormalized())
	}
}

func TestLoadChatFromConfigPath_investigationGatesEnvOverrides(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.yaml")
	if err := os.WriteFile(path, []byte("chat:\n  investigation_gates: on\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	t.Setenv("SATH_INVESTIGATION_GATES", "off")
	cfg, err := LoadChatFromConfigPath(dir)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.InvestigationGatesNormalized() != "off" {
		t.Fatal("env must override yaml")
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./internal/conf -count=1 -run TestLoadChatFromConfigPath_investigationGates`
Expected: FAIL（没有 `InvestigationGatesNormalized`）

- [ ] **Step 3: Minimal implementation**

在 `ChatConfig` 增加：

```go
InvestigationGates string `yaml:"investigation_gates,omitempty"`
```

`LoadChatFromConfigPath` 从 YAML 拷贝该字段。`EnrichChatFromEnv`：若 `SATH_INVESTIGATION_GATES` 非空则写入该字段。

```go
func (c *ChatConfig) InvestigationGatesNormalized() string {
	if c == nil {
		return "off"
	}
	switch strings.ToLower(strings.TrimSpace(c.InvestigationGates)) {
	case "on", "1", "true", "yes":
		return "on"
	default:
		return "off"
	}
}

func (c *ChatConfig) InvestigationGatesOn() bool {
	return c.InvestigationGatesNormalized() == "on"
}

func (c *ChatConfig) InvestigationGatesInvalid() bool {
	if c == nil {
		return false
	}
	raw := strings.TrimSpace(c.InvestigationGates)
	if raw == "" {
		return false // 缺省：静默 off，避免旧配置每轮启动刷 warn
	}
	switch strings.ToLower(raw) {
	case "on", "off", "1", "0", "true", "false", "yes", "no":
		return false
	default:
		return true
	}
}
```

`LoadChatFromConfigPath` 在 `raw.Chat != nil` 时拷贝 `out.InvestigationGates = raw.Chat.InvestigationGates`（与 `PublicInboundEnabled` 一样每次覆盖）。

`EnrichChatFromEnv`：`SATH_INVESTIGATION_GATES` 非空则原样写入 `c.InvestigationGates`（不要在 conf 里把 garbage 改写成 `off`，留给 `Normalized` + main warn）。继续保留现有 `SATH_TURN_TOOL_SURFACE` → `TurnToolSurfaceEnabled` 行为（已有单测）。

非法值（含 `garbage`、空）一律 Normalized=`off`。不要在 conf 里打 log（warn 放 main）。

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./internal/conf -count=1 -run TestLoadChatFromConfigPath`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add portal/internal/conf/chat_config.go portal/internal/conf/chat_config_test.go
git commit -m "feat(portal): parse chat.investigation_gates default off"
```

---

### Task 2: Apply 三层 override（surface / intent gate / task lock）

**Files:**
- Create: `portal/internal/chat/investigation_gates.go`
- Create: `portal/internal/chat/investigation_gates_test.go`
- Modify: `portal/internal/chat/turn_intent_gate.go`（`NewTurnIntentGate`）
- 不要改：`portal/internal/chat/turn_intent_gate_test.go`（`NoopWhenDisabled` 仍只靠 env）

- [ ] **Step 1: Write failing tests**

`investigation_gates_test.go`：

```go
package chat

import (
	"testing"

	"backend/internal/conf"
)

func resetInvestigationOverrides(t *testing.T) {
	t.Helper()
	t.Setenv("SATH_TURN_TOOL_SURFACE", "")
	t.Setenv("SATH_TURN_INTENT_GATE", "")
	t.Setenv("SATH_TASK_LOCK", "")
	resetTurnToolSurfaceOverride()
	resetTurnIntentGateOverride()
	resetTaskLockOverride()
	t.Cleanup(resetTurnToolSurfaceOverride)
	t.Cleanup(resetTurnIntentGateOverride)
	t.Cleanup(resetTaskLockOverride)
}

func TestApplyInvestigationGates_OffDisablesAll(t *testing.T) {
	resetInvestigationOverrides(t)

	ApplyInvestigationGates(&conf.ChatConfig{InvestigationGates: "off"})
	if ToolSurfaceEnabled() {
		t.Fatal("surface")
	}
	if NewTurnIntentGate() != nil {
		t.Fatal("intent gate")
	}
	if TaskLockEnabled() {
		t.Fatal("task lock")
	}
}

func TestApplyInvestigationGates_OffIgnoresYAMLSurface(t *testing.T) {
	resetInvestigationOverrides(t)

	on := true
	ApplyInvestigationGates(&conf.ChatConfig{
		InvestigationGates:     "off",
		TurnToolSurfaceEnabled: &on,
	})
	if ToolSurfaceEnabled() {
		t.Fatal("yaml turn_tool_surface_enabled must not reopen B when master off")
	}
}

func TestApplyInvestigationGates_IntentEnvOverridesOff(t *testing.T) {
	resetInvestigationOverrides(t)
	t.Setenv("SATH_TURN_INTENT_GATE", "1")

	ApplyInvestigationGates(&conf.ChatConfig{InvestigationGates: "off"})
	if NewTurnIntentGate() == nil {
		t.Fatal("SATH_TURN_INTENT_GATE=1 must install gate")
	}
}

func TestApplyInvestigationGates_OnEnablesAll(t *testing.T) {
	resetInvestigationOverrides(t)

	ApplyInvestigationGates(&conf.ChatConfig{InvestigationGates: "on"})
	if !ToolSurfaceEnabled() || NewTurnIntentGate() == nil || !TaskLockEnabled() {
		t.Fatal("master on")
	}
}
```

`AppendTaskLock` 现有测试不要改：纯函数始终追加【本轮任务锁】。

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./internal/chat -count=1 -run TestApplyInvestigationGates`
Expected: FAIL（没有 `ApplyInvestigationGates`）

- [ ] **Step 3: Minimal implementation**

`resetTurnToolSurfaceOverride` 已在 `tool_families.go`，不要再声明一份。

`turn_intent_gate.go`：把 `NewTurnIntentGate` 改成读 `TurnIntentGateEnabled()`（env 优先，再 override，缺省 true，与现网「未设 env 则装闸」一致）。

```go
var turnIntentGateOverride *bool

func SetTurnIntentGateEnabled(enabled bool) {
	v := enabled
	turnIntentGateOverride = &v
}

func resetTurnIntentGateOverride() { turnIntentGateOverride = nil }

func TurnIntentGateEnabled() bool {
	v := strings.TrimSpace(strings.ToLower(os.Getenv(turnIntentGateEnv)))
	if v != "" {
		return !(v == "0" || v == "false" || v == "off" || v == "no")
	}
	if turnIntentGateOverride != nil {
		return *turnIntentGateOverride
	}
	return true
}

func NewTurnIntentGate() agent.PostModelPolicy {
	if !TurnIntentGateEnabled() {
		return nil
	}
	return TurnIntentGate{}
}
```

`investigation_gates.go`：

```go
package chat

import (
	"os"
	"strings"

	"backend/internal/conf"
)

const taskLockEnv = "SATH_TASK_LOCK"

var taskLockOverride *bool

func SetTaskLockEnabled(enabled bool) {
	v := enabled
	taskLockOverride = &v
}

func resetTaskLockOverride() { taskLockOverride = nil }

func TaskLockEnabled() bool {
	v := strings.TrimSpace(strings.ToLower(os.Getenv(taskLockEnv)))
	if v != "" {
		return !(v == "0" || v == "false" || v == "off" || v == "no")
	}
	if taskLockOverride != nil {
		return *taskLockOverride
	}
	return true
}

func envSet(key string) bool {
	return strings.TrimSpace(os.Getenv(key)) != ""
}

// ApplyInvestigationGates sets process overrides. Caller must invoke at process start.
// 单层 env 已设置时不要 Set*：ToolSurfaceEnabled / TurnIntentGateEnabled / TaskLockEnabled 会先读 env。
// 禁止在 env 已设置时再套用 YAML `turn_tool_surface_enabled`（EnrichChatFromEnv 会把该 env 写进指针，套用指针等于让 YAML 与 env 缠在一起）。
func ApplyInvestigationGates(cfg *conf.ChatConfig) {
	on := cfg != nil && cfg.InvestigationGatesOn()
	if !envSet(turnToolSurfaceEnv) {
		if on {
			if cfg != nil && cfg.TurnToolSurfaceEnabled != nil {
				SetTurnToolSurfaceEnabled(*cfg.TurnToolSurfaceEnabled)
			} else {
				SetTurnToolSurfaceEnabled(true)
			}
		} else {
			SetTurnToolSurfaceEnabled(false)
		}
	}
	if !envSet(turnIntentGateEnv) {
		SetTurnIntentGateEnabled(on)
	}
	if !envSet(taskLockEnv) {
		SetTaskLockEnabled(on)
	}
}
```

`turnToolSurfaceEnv` 已在 `tool_families.go` 同包。`turnIntentGateEnv` 在 `turn_intent_gate.go` 同包。

- [ ] **Step 4: Run tests**

Run: `go test ./internal/chat -count=1 -run "TestApplyInvestigationGates|TestTurnIntentGateOption_NoopWhenDisabled|TestPrepareTurnToolSurface_Disabled"`
Expected: PASS

再跑：`go test ./internal/chat -count=1`
Expected: PASS（现网 on 路径单测不改断言）

- [ ] **Step 5: Commit**

```bash
git add portal/internal/chat/investigation_gates.go portal/internal/chat/investigation_gates_test.go portal/internal/chat/turn_intent_gate.go
git commit -m "feat(portal): apply investigation_gates overrides for surface, intent gate, task lock"
```

---

### Task 3: 调用点跳过任务锁 + 启动接线

**Files:**
- Modify: `portal/internal/chat/investigation_gates.go`（追加 MaybeApply*）
- Modify: `portal/internal/chat/investigation_gates_test.go`（追加 MaybeApply 测试；import `"strings"`）
- Modify: `portal/internal/service/chat.go`（约 473–507、847–901）
- Modify: `portal/internal/service/agent.go`（约 374–400）
- Modify: `portal/cmd/backend/main.go`（约 164–174）
- Modify: `portal/configs/config.yaml`
- Modify: `portal/configs/config.docker.yaml`（同样写上 `investigation_gates: off`，缺字段虽已是 off）

- [ ] **Step 1: Write a small helper test in chat package**（追加到 `investigation_gates_test.go`，避免为 HTTP handler 起全套 ChatService）

```go
func TestMaybeApplyTaskLock_SkipWhenDisabled(t *testing.T) {
	resetInvestigationOverrides(t)
	SetTaskLockEnabled(false)
	lock := TurnTaskLock{Q: "查流水"}
	got := MaybeApplyTaskLock("base", lock)
	if strings.Contains(got, "【本轮任务锁】") {
		t.Fatalf("got %s", got)
	}
	md := MaybeMergeTaskLockMetadata(nil, lock)
	if _, ok := md[MetadataKeyTaskLock]; ok {
		t.Fatal("metadata")
	}
}

func TestMaybeApplyTaskLock_AppliesWhenEnabled(t *testing.T) {
	resetInvestigationOverrides(t)
	SetTaskLockEnabled(true)
	got := MaybeApplyTaskLock("base", TurnTaskLock{Q: "查流水"})
	if !strings.Contains(got, "【本轮任务锁】") {
		t.Fatal(got)
	}
}
```

- [ ] **Step 2: Run to verify fail**

Run: `go test ./internal/chat -count=1 -run TestMaybeApplyTaskLock`
Expected: FAIL

- [ ] **Step 3: Implement helpers and wire call sites**

```go
func MaybeApplyTaskLock(prompt string, lock TurnTaskLock) string {
	if !TaskLockEnabled() {
		return prompt
	}
	return AppendTaskLock(prompt, lock)
}

func MaybeMergeTaskLockMetadata(md map[string]any, lock TurnTaskLock) map[string]any {
	if !TaskLockEnabled() {
		return md
	}
	return MergeTaskLockMetadata(md, lock)
}
```

`chat.go` 同步路径（约 473–507）与 SSE 路径（约 847–901）、`agent.go`（约 374–400）：把 `chat.AppendTaskLock` / `chat.MergeTaskLockMetadata` 换成 `chat.MaybeApplyTaskLock` / `chat.MaybeMergeTaskLockMetadata`。`buildTurnTaskLockFromHistory` / `BuildTurnTaskLock` 仍照常算 lock 对象（纯计算，无副作用）。

`main.go`：**替换**现有 `if chatCfg.TurnToolSurfaceEnabled != nil { chat.SetTurnToolSurfaceEnabled(...) }` 整段，不要先套 YAML surface 再 Apply（否则总开关 off 会被旧字段重新打开）。改为：

```go
if chatCfg.InvestigationGatesInvalid() {
    log.NewHelper(logger).Warnf("chat.investigation_gates=%q invalid; treating as off", chatCfg.InvestigationGates)
}
chat.ApplyInvestigationGates(chatCfg)
if !chatCfg.InvestigationGatesOn() {
    log.NewHelper(logger).Info("investigation gates off (HTTP grounding, turn tool surface, task lock)")
}
```

`config.yaml` 与 `config.docker.yaml` 的 `chat:` 下各增加：

```yaml
  investigation_gates: off   # on 恢复工具面/HTTP 接地/任务锁
```

- [ ] **Step 4: Run tests**

Run: `go test ./internal/chat ./internal/service ./internal/conf -count=1`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add portal/internal/chat/investigation_gates.go portal/internal/chat/investigation_gates_test.go portal/internal/service/chat.go portal/internal/service/agent.go portal/cmd/backend/main.go portal/configs/config.yaml portal/configs/config.docker.yaml
git commit -m "feat(portal): default-off task lock at send paths and wire investigation_gates at startup"
```

---

### Task 4: 文档

**Files:**
- Modify: `portal/docs/turn-tool-surface.md`

- [ ] **Step 1: 在文档顶部增加总开关**

替换「关闭装配收窄」那段为：

```markdown
- 总开关：`chat.investigation_gates`（默认 `off`）或 `SATH_INVESTIGATION_GATES`。`off` 时关闭工具面收窄、TurnIntentGate（含 HTTP 接地）、任务锁。非法值当 `off`。
- 单层覆盖（仅当该 env 已设置）：`SATH_TURN_TOOL_SURFACE`、`SATH_TURN_INTENT_GATE`、`SATH_TASK_LOCK`。
- `chat.turn_tool_surface_enabled` 只在总开关 `on` 时生效；总开关 `off` 时忽略，避免旧 YAML 把工具面重新打开。
- 规格：`docs/superpowers/specs/2026-09-04-investigation-gates-off-design.md`
```

保留后面的族说明。不要改技能自动匹配文档。

- [ ] **Step 2: 无测试**

文档-only。

- [ ] **Step 3: Commit**

```bash
git add portal/docs/turn-tool-surface.md
git commit -m "docs(portal): document investigation_gates master switch"
```

---

## Self-review

- 规格 A+B+C 都有装配路径；闸文件不删。
- `on` 单测仍直接构造 Gate / `t.Setenv(..., "1")`，不依赖进程默认。
- 空击/凭据/code claim 未列入 File map。
- 每步有命令和期望 FAIL/PASS。
