# G4.1：持久化 Harness Hooks Implementation Plan

> **For agentic workers:** Execute directly. Skip git commits unless asked.

**Goal:** 从 workspace `harness/hooks.yaml` 加载声明式 Before-block Hook，使同类危险调用在 harness 层硬挡；写盘走现有 workspace danger confirm。

**Architecture:** 解析 YAML → `DeclarativeBlockHook`（ToolHook.Before）→ Portal 按 workspace 注入 `WithReActToolHooks`。与 FailureCaptureHook 并存（先加载 YAML，再挂 Capture）。

**非目标：** After 钩子、参数改写、热重载监听、DB 存规则、主循环 fork。

---

## 文件

| 文件 | 职责 |
|------|------|
| Create `framework/agent/harness_hooks.go` | YAML 结构、Load、block Hook |
| Create `framework/agent/harness_hooks_test.go` | 解析 + block 行为 |
| Modify `portal/.../growth_chat.go` + `chat.go` | 按 workspace 注入 |
| Modify `file_tools.go` danger patterns | `harness/hooks.yaml` 须确认 |
| Modify gap G4.1 + `harness-fix` Skill | 文档 |

## Schema（v1）

```yaml
version: 1
rules:
  - id: block-pipe-sh
    tools: [terminal]
    match:
      param: command
      regex: "(?i)curl.*\\|.*sh"
    action: block
    reason: "piped shell download blocked by harness hook"
```

- `tools` 空 = 匹配所有工具名
- `match` 省略 = 对该 tools 一律 block（慎用）
- 仅支持 `action: block`

## 验收

- [x] 无文件 / 坏 YAML：不注入，不崩
- [x] 匹配规则 → Before block，不 Execute
- [x] 写 `harness/hooks.yaml` 走 danger confirm
- [x] FailureCapture 与 YAML hooks 可同时生效
