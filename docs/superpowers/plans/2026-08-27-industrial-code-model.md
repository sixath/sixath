# 切片 E0：code 族强制代码模型 Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** `FamilyCode` 激活时必须用已配置的 code 模型；缺模型名或 `BuildModel` 失败则本轮可见失败。`TestEvalGolden_code_model` 在 A 脚本里能红。

**Architecture:** 改 `ResolveTurnModel` 为 `(model.Model, error)`。已配置 = `resolveCodeModelSpec` 后 `Model != ""`（Agent / 全局 / `SATH_CODE_MODEL`）。禁止把会话 `ModelConfig.Model` 填进 code 模型名。缺配 → `ErrCodeModelRequired`；构建失败 → `ErrCodeModelBuild`。`chat.go` 两处直接 `return err`。叠 env 当且仅当全局 **Model 空**，不用 `Usable()`。

**Tech Stack:** Go。根 `go.mod` 是空 module：portal 测 `cd portal`。`BuildModel` 非法 provider 可失败，不调付费 LLM。

**Spec:** `docs/superpowers/specs/2026-08-27-industrial-code-model-design.md`  
**评测网:** `docs/superpowers/specs/2026-08-25-industrial-eval-design.md`

**不做:** E1–E5；改 `FamilyCode` 关键词；默全开 MEA；声称闸读 pin；改正 Skill 索引；缺配警告仍用对话模型；改 `code_model_settings.go`；`SATH_TURN_TOOL_SURFACE` off 时对全量绑定强制 code 模型；live LLM；新建平行评测框架；自动 git commit（除非用户另行要求）。

**夹具：** `familySet([]string{FamilyCode})` / `FamilyCore`。测缺配前必须 `t.Setenv` 清空四条 `SATH_CODE_*` 且 `SetGlobalCodeModel(CodeModelSpec{})`。

---

## File Structure

| 文件 | 责任 |
|------|------|
| `portal/internal/chat/code_model.go` | 哨兵；签名；去掉会话模型名回填；叠 env 改看 `Model==""` |
| `portal/internal/chat/code_model_test.go` | `noSpecKeepsChat` 改为期望 error；其余调用改 `(m, err)`；env+仅全局 key |
| `portal/internal/chat/evalgolden_test.go` | `TestEvalGolden_code_model` |
| `portal/internal/service/chat.go` | L368、L630 处理 error |
| `docs/superpowers/specs/2026-08-25-industrial-eval-design.md` | §7 加 `code_model` |
| `docs/superpowers/specs/2026-08-27-industrial-code-model-design.md` | 状态已确认；下一份指本 plan |

**不要改：** `tool_families.go` 的 `familyKeywords`、`AppendCodeAnalysisPrompt`、`code_model_settings.go`、`framework/agent`、`scripts/industrial-eval.ps1`（已跑 `./internal/chat`）。

签名一改，`internal/chat` 测试必须全部改完才能绿；`internal/service` 在 Task 2 之前 **编译失败**（预期，先测 chat 包）。

---

### Task 1: `ResolveTurnModel` + `TestEvalGolden_code_model`

**Files:**
- Modify: `portal/internal/chat/code_model.go`
- Modify: `portal/internal/chat/code_model_test.go`
- Modify: `portal/internal/chat/evalgolden_test.go`

- [ ] **Step 1: 写金样例（先于实现；此时可能编译失败）**

`evalgolden_test.go` 增加 `"errors"`、`"backend/internal/biz"`。追加：

```go
func TestEvalGolden_code_model(t *testing.T) {
	t.Setenv("SATH_CODE_MODEL", "")
	t.Setenv("SATH_CODE_PROVIDER", "")
	t.Setenv("SATH_CODE_API_KEY", "")
	t.Setenv("SATH_CODE_BASE_URL", "")
	SetGlobalCodeModel(CodeModelSpec{})
	t.Cleanup(func() { SetGlobalCodeModel(CodeModelSpec{}) })

	chat := stubTurnModel{name: "chat"}

	got, err := ResolveTurnModel(familySet([]string{FamilyCode}), chat, biz.AgentMeta{})
	if !errors.Is(err, ErrCodeModelRequired) || got != nil {
		t.Fatalf("missing spec must error, got=%v err=%v", got, err)
	}

	got, err = ResolveTurnModel(familySet([]string{FamilyCore}), chat, biz.AgentMeta{
		ModelConfig: biz.ModelConfig{CodeModel: "gpt-code", CodeAPIKey: "k", CodeProvider: "openai", CodeBaseURL: "http://127.0.0.1:9"},
	})
	if err != nil || got != chat {
		t.Fatalf("non-code must keep chat: got=%v err=%v", got, err)
	}

	got, err = ResolveTurnModel(nil, chat, biz.AgentMeta{})
	if err != nil || got != chat {
		t.Fatalf("nil active must keep chat: got=%v err=%v", got, err)
	}

	got, err = ResolveTurnModel(familySet([]string{FamilyCode}), chat, biz.AgentMeta{
		ModelConfig: biz.ModelConfig{
			Provider: "openai", Model: "gpt-chat",
			CodeProvider: "openai", CodeModel: "gpt-code", CodeAPIKey: "sk-test", CodeBaseURL: "http://127.0.0.1:9",
		},
	})
	if err != nil || got == nil || got == chat {
		t.Fatalf("configured code model must swap: got=%v err=%v", got, err)
	}
}
```

`stubTurnModel` 已在 `code_model_test.go` 同包，evalgolden 可直接用。

- [ ] **Step 2: 实现 `code_model.go`**

在 import 加 `"errors"`、`"fmt"`。截断用 `[]rune`，**不要**加未使用的 `unicode/utf8`。

```go
const codeModelRequiredMsg = "本轮是源码分析，需要配置 code 模型后才能继续，不会用对话模型代替。\n请在 Agent 的 code_model、门户全局 code 模型，或环境变量 SATH_CODE_MODEL 中填写模型名。"

const codeModelBuildPrefix = "本轮是源码分析，code 模型创建失败，不会用对话模型代替。"

var (
	ErrCodeModelRequired = errors.New(codeModelRequiredMsg)
	ErrCodeModelBuild    = errors.New(codeModelBuildPrefix)
)

func truncateRunes(s string, n int) string {
	if n <= 0 || s == "" {
		return ""
	}
	r := []rune(s)
	if len(r) <= n {
		return s
	}
	return string(r[:n])
}

func wrapCodeModelBuild(err error) error {
	if err == nil {
		return ErrCodeModelBuild
	}
	extra := truncateRunes(err.Error(), 200)
	return fmt.Errorf("%w %s", ErrCodeModelBuild, extra)
}
```

`ResolveTurnModel` **整函数换成**：

```go
func ResolveTurnModel(active map[string]struct{}, chatModel model.Model, meta biz.AgentMeta) (model.Model, error) {
	if chatModel == nil {
		return nil, nil
	}
	if active == nil || !FamilyActive(active, FamilyCode) {
		return chatModel, nil
	}
	spec := resolveCodeModelSpec(meta)
	if strings.TrimSpace(spec.Model) == "" {
		return nil, ErrCodeModelRequired
	}
	provider := strings.TrimSpace(spec.Provider)
	if provider == "" {
		provider = strings.TrimSpace(meta.ModelConfig.Provider)
	}
	if provider == "" {
		provider = "openai"
	}
	modelName := strings.TrimSpace(spec.Model)
	apiKey := strings.TrimSpace(spec.APIKey)
	if apiKey == "" {
		apiKey = strings.TrimSpace(meta.ModelConfig.APIKey)
	}
	baseURL := strings.TrimSpace(spec.BaseURL)
	if baseURL == "" {
		baseURL = strings.TrimSpace(meta.ModelConfig.BaseURL)
	}
	m, err := BuildModel(provider, modelName, apiKey, baseURL)
	if err != nil || m == nil {
		return nil, wrapCodeModelBuild(err)
	}
	return m, nil
}
```

**禁止** `modelName = meta.ModelConfig.Model`。注释改成不再写 fail-open。

`resolveCodeModelSpec`：

```go
func resolveCodeModelSpec(meta biz.AgentMeta) CodeModelSpec {
	base := GlobalCodeModel()
	if strings.TrimSpace(base.Model) == "" {
		base = overlayCodeModel(base, CodeModelSpecFromEnv())
	}
	agent := agentCodeModelSpec(meta)
	if !agent.hasAny() {
		return base
	}
	return overlayCodeModel(base, agent)
}
```

不要改 `Usable()` 方法体（设置页仍用）。不要改 `code_model_settings.go`。

- [ ] **Step 3: 改同包旧测试签名**

`code_model_test.go` 所有 `got := ResolveTurnModel(...)` 改为 `got, err := ResolveTurnModel(...)`：

- `TestResolveTurnModel_nonCodeKeepsChat`：`err != nil` 则 Fatal；`got == chat`。
- **删除** `TestResolveTurnModel_noSpecKeepsChat`（由金样例取代），或改成 `errors.Is(err, ErrCodeModelRequired)` 且 `got==nil`。二选一，不要留「缺配保 chat」。
- `TestResolveTurnModel_codeFamilyUsesAgentSpec`：`err != nil` Fatal；`got != chat`。
- 追加：

```go
func TestResolveTurnModel_buildFail(t *testing.T) {
	t.Setenv("SATH_CODE_MODEL", "")
	t.Setenv("SATH_CODE_PROVIDER", "")
	t.Setenv("SATH_CODE_API_KEY", "")
	t.Setenv("SATH_CODE_BASE_URL", "")
	SetGlobalCodeModel(CodeModelSpec{})
	t.Cleanup(func() { SetGlobalCodeModel(CodeModelSpec{}) })
	chat := stubTurnModel{name: "chat"}
	got, err := ResolveTurnModel(familySet([]string{FamilyCode}), chat, biz.AgentMeta{
		ModelConfig: biz.ModelConfig{CodeProvider: "not-a-provider", CodeModel: "x"},
	})
	if !errors.Is(err, ErrCodeModelBuild) || got != nil {
		t.Fatalf("got=%v err=%v", got, err)
	}
}

func TestResolveCodeModelSpec_envWhenGlobalHasKeyOnly(t *testing.T) {
	t.Setenv("SATH_CODE_MODEL", "env-code")
	t.Setenv("SATH_CODE_API_KEY", "env-key")
	t.Setenv("SATH_CODE_PROVIDER", "")
	t.Setenv("SATH_CODE_BASE_URL", "")
	SetGlobalCodeModel(CodeModelSpec{APIKey: "gk"})
	t.Cleanup(func() { SetGlobalCodeModel(CodeModelSpec{}) })
	got := resolveCodeModelSpec(biz.AgentMeta{})
	if got.Model != "env-code" {
		t.Fatalf("key-only global must still overlay env model: %#v", got)
	}
}

func TestResolveTurnModel_apiKeyOnlyRequired(t *testing.T) {
	t.Setenv("SATH_CODE_MODEL", "")
	t.Setenv("SATH_CODE_PROVIDER", "")
	t.Setenv("SATH_CODE_API_KEY", "sk-only")
	t.Setenv("SATH_CODE_BASE_URL", "")
	SetGlobalCodeModel(CodeModelSpec{})
	t.Cleanup(func() { SetGlobalCodeModel(CodeModelSpec{}) })
	got, err := ResolveTurnModel(familySet([]string{FamilyCode}), stubTurnModel{name: "chat"}, biz.AgentMeta{})
	if !errors.Is(err, ErrCodeModelRequired) || got != nil {
		t.Fatalf("api key is not a model name: got=%v err=%v", got, err)
	}
}
```

`code_model_test.go` 加 `"errors"` import。

- [ ] **Step 4: 跑 chat 包测试**

Run:

```
cd E:\workspace\github\sixath\sixath\portal
go test ./internal/chat -count=1 -run "TestEvalGolden_code_model|TestResolveTurnModel_|TestResolveCodeModelSpec_"
```

Expected: PASS。`go test ./internal/service` 此时会因签名 **编译失败**，Task 2 修。

破坏：缺 Model 时 `return chatModel, nil` → `TestEvalGolden_code_model` FAIL。

---

### Task 2: `chat.go` 两处接线

**Files:**
- Modify: `portal/internal/service/chat.go`（约 L368 与 L630）

**SendMessage（约 L368）：**

```go
	m, err = chat.ResolveTurnModel(active, m, *agentMeta)
	if err != nil {
		s.log.Errorf("resolve turn model failed: session_id=%s agent_id=%s err=%v", sessionID, session.AgentID, err)
		return nil, err
	}
```

**SendMessageStream（约 L630）：**

```go
	m, err = chat.ResolveTurnModel(active, m, *agentMeta)
	if err != nil {
		s.log.Errorf("resolve turn model failed: session_id=%s agent_id=%s err=%v", sessionID, session.AgentID, err)
		return nil, "", err
	}
```

两处都要改。日志 **不要**打印 `api_key` / `CodeAPIKey`。不要 catch 后 `m = oldChat`。

- [ ] **Step 2: 编译 service**

Run:

```
cd E:\workspace\github\sixath\sixath\portal
go test ./internal/service -count=1 -timeout 60s
go test ./internal/chat -count=1 -run TestEvalGolden_code_model
```

Expected: 编译过；chat 金样例仍 PASS。service 全量若过慢，至少 `go test ./internal/service -count=1 -run TestDoesNotExist` 会编译失败以外的包——更稳妥：

```
cd E:\workspace\github\sixath\sixath\portal
go test ./internal/service -count=1 -run '^$'
```

（无匹配测试但会编译该包。）Expected: `ok` 或 `no tests to run` 且 exit 0。

---

### Task 3: 文档 + A 脚本

**Files:**
- Modify: `docs/superpowers/specs/2026-08-25-industrial-eval-design.md`
- Modify: `docs/superpowers/specs/2026-08-27-industrial-code-model-design.md`

- [ ] **Step 1: §7 表在 `obs_hit` 后追加一行（不要删 D/C/B）**

| ID | 锁什么 | 何时 |
|----|--------|------|
| `code_model` | FamilyCode 缺配不得用对话模型 | **E0** |

E0 spec 文首：`状态: 已确认（2026-08-27）`；`下一份` 保持 `docs/superpowers/plans/2026-08-27-industrial-code-model.md`。

**不要**改 `scripts/industrial-eval.ps1`（已包含 `./internal/chat`）。

- [ ] **Step 2: 全脚本**

Run: `powershell -NoProfile -File E:\workspace\github\sixath\sixath\scripts\industrial-eval.ps1`  
Expected: 全部 `TestEvalGolden_` PASS，含 `TestEvalGolden_code_model`。`c7aa` 仍 Skip。

破坏：缺配返回 chat → `TestEvalGolden_code_model` FAIL。

---

## 验收对照（规格 §6）

| ID | 任务 |
|----|------|
| `code_model` 缺配 / 闲聊 / nil active / 已配 | Task 1 金样例 |
| 构建失败 | Task 1 `TestResolveTurnModel_buildFail` |
| 仅 APIKey | Task 1 `TestResolveTurnModel_apiKeyOnlyRequired` |
| 全局仅 key + env 模型名 | Task 1 `TestResolveCodeModelSpec_envWhenGlobalHasKeyOnly` |
| `chat.go` 两处 | Task 2 |
| A 脚本 | Task 3 |
