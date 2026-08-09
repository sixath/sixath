# G4 / G4.1：失败捕获 → harness 修复

## 开关

| 变量 | 默认 | 说明 |
|------|------|------|
| `SATH_GROWTH_FAILURE_CAPTURE` | off | `1`/`true`/`yes` 时主对话注入 `FailureCaptureHook`，工具 hard/soft 失败写入 `.learnings/ERRORS.md` |
| `SATH_SKILL_MANAGE_CONFIRM_PATCH` | on（未设则为 true） | `0`/`false` 可关闭主对话 patch/edit 确认；Growth fork 始终直写 |

启动时 `chat.EnrichFailureCaptureFromEnv()`（`cmd/backend/main.go`）。

## 流水线（Skill）

1. 工具失败 → ERRORS.md（`Status: pending`，指纹短窗去重）
2. Agent 加载 `harness-fix` Skill
3. `skill_manage` create/patch → SSE `confirm_required`
4. 用户确认 → `ApplyPatchBatch`

## G4.1：声明式 Hook

- 路径：workspace `harness/hooks.yaml`
- 每轮主对话 `LoadWorkspaceHarnessHooks` → `WithReActToolHooks`（与 FailureCapture 同切片；YAML 在前）
- 仅 `action: block`（Before 拒绝 Execute）
- 写该文件走 workspace danger confirm（与 `.env` 同类）

示例：

```yaml
version: 1
rules:
  - id: block-pipe-sh
    tools: [terminal]
    match:
      param: command
      regex: "(?i)curl.*\\|.*sh"
    action: block
    reason: "piped curl|sh blocked by harness hook"
```

缺失文件 = 无 YAML hooks（零回归）。解析失败只打 warn，不阻断对话。
