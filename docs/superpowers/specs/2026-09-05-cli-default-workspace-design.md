# S10 收口：CLI 空 workspace 落到默认可写根

**日期**: 2026-09-05  
**状态**: 已确认（S7 leftover；父规格 §0「每个 Agent 必须有」；2026-09-05 实施）  
**范围**: `framework/workspace` 默认根 + `sath serve` / `demo` / `init` 模板。不改 Portal、不改 templates 库「空则跳过」语义。  
**父规格**: [`2026-09-05-agent-model-workspace-harness-design.md`](./2026-09-05-agent-model-workspace-harness-design.md)  
**前置**: [S7](./2026-09-05-cli-rca-code-mount-design.md)、[S9](./2026-09-05-chat-agent-workspace-design.md)

**一句话**: CLI 不再允许「能对话但没有可写根」；空配置落到 `{cwd}/.sath/workspace` 并 `MkdirAll`，对齐 Portal 的默认可写目录，而不是拒跑。

---

## 1. 背景

父规格 §0：每个 Agent **必须有**可写 workspace，平台给默认目录。Portal Create/Update 空字符串 → `{data_root}/agents/{id}`。

S7–S9 把非空 `config.Workspace` 接到 ReAct/ChatAgent，但锁定 **CLI 空 workspace 仍可对话**。结果：`sath serve` 不写 `workspace:` 时没有文件器官根、不读 `MEMORY.md`。

拒跑会弄坏现有无 workspace 的 yaml。本切片用「给默认根」关 waiver，与 Portal 同一故事。

---

## 2. 已锁定决策

| 项 | 选择 |
|----|------|
| 空 / 空白 | `{cwd}/.sath/workspace` + `MkdirAll` + `Abs` |
| 已配置 | `MkdirAll` + `Abs`（不改用户选的路径） |
| 调用点 | `sath serve`、`sath demo`、`sath init` 生成的 `main.go` |
| `config.Load` / templates | **不**自动展开（单测与库调用仍可空） |
| 拒跑 | **不做** |
| Portal / 别名 / Insights | **不改** |

---

## 3. 行为

```text
workspace.EnsureCLIRoot(ws):
  TrimSpace(ws) 空 → {Getwd()}/.sath/workspace
  MkdirAll(ws)
  return Abs(ws)

sath serve / demo:
  cfg.Workspace = EnsureCLIRoot(cfg.Workspace)
  再装配 handler（已有 S7–S9 接线）
```

---

## 4. 非目标

- 不把 CLI 空 workspace 改成错误退出
- 不改 Portal `{data_root}/agents/{id}`
- 不在 `config.Load` / `FromEnv` 里偷偷 MkdirAll（避免测到污染 cwd）
- 不扫示例 yaml
- 不删 `framework/agent` 别名、不删 Insights 路由
- 不改 `NewChatStreamHandler`

---

## 5. 成功标准

1. `EnsureCLIRoot("")` 在临时 cwd 下创建 `.sath/workspace` 并返回其绝对路径。
2. `EnsureCLIRoot(已有目录)` 返回该目录的 Abs，不改到 `.sath`。
3. `sath serve` 在装配 handler 前调用 `EnsureCLIRoot`。
4. `cd framework && go test ./workspace ./cli ./config ./templates ./harness -count=1` 绿。
