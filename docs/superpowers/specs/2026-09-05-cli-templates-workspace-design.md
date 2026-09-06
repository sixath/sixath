# S8 收口：CLI templates ReAct 都接 `workspace`

**日期**: 2026-09-05  
**状态**: 已确认（S7 leftover；2026-09-05 实施）  
**范围**: `framework/templates` 的 dataquery / MCP ReAct 装配。不改 Portal、不改 ChatAgent、不改 RCA 注册。  
**父规格**: [`2026-09-05-agent-model-workspace-harness-design.md`](./2026-09-05-agent-model-workspace-harness-design.md)  
**前置**: [S7](./2026-09-05-cli-rca-code-mount-design.md)

**一句话**: `config.Workspace` 不只给 skills handler；dataquery 与 MCP 的 ReAct 也要同一条可写根。

---

## 1. 背景

S7 给 `config.Config` 加了 `Workspace`，并让 `NewSkillsAwareChatHandlerFromConfig` 在非空时 `WithReActWorkspace`。S7 明确 **不改** `dataquery` / MCP handler。

现网：

- `dataquery.NewDataQueryHandler`：`NewReActAgent` **没有** workspace。CLI `serve` 的 `/data/chat` 经 `NewDataQueryHandlerFromConfig`，即使 yaml 写了 `workspace:` 也不进 PromptBuilder / 文件器官。
- `templates.NewMCPAgentHandler`：专用 `templates.Config` **没有** Workspace 字段。
- `NewChatAgentHandlerFromConfig`：ChatAgent，无工具面；本切片不改。

---

## 2. 已锁定决策

| 项 | 选择 |
|----|------|
| dataquery | `DataQueryConfig.Workspace`；`FromConfig` 从 `cfg.Workspace` 拷入；非空 → `WithReActWorkspace` |
| MCP | `templates.Config.Workspace`；非空 → `WithReActWorkspace` |
| 空 / 空白 | 不传选项；CLI 仍可跑（S7 锁定） |
| helper | `appendWorkspaceOpt`，skills / dataquery / MCP 共用，避免三处复制 |
| RCA | **不**在 dataquery 注册 `registerRCATools` |
| ChatAgent | **不改** |
| Portal / jaeger / ES | **不改** |

---

## 3. 行为

```text
appendWorkspaceOpt(opts, ws):
  TrimSpace(ws) 非空 → append WithReActWorkspace
  否则原样返回

NewDataQueryHandler / NewMCPAgentHandler / skills handler:
  reactOpts = appendWorkspaceOpt(reactOpts, workspace)
```

可观测信号：workspace 根下有 `MEMORY.md` 时，模型看到的 system 含 `## MEMORY.md` 与文件正文（PromptBuilder）。

---

## 4. 非目标

- 不把 CLI 空 workspace 改成拒跑
- 不给 ChatAgent 加 workspace
- 不扫示例 yaml / `examples/mcp_agent`
- 不改 Portal、`MergeRCARoots`、jaeger/ES
- 不在 dataquery 再做一遍 RCA

---

## 5. 成功标准

1. `NewDataQueryHandler` 带非空 `Workspace` 且根下有 `MEMORY.md` → 发给模型的 system 含该文件。
2. `NewMCPAgentHandler` 同上。
3. 空白 workspace 不注入（无 `## MEMORY.md`）。
4. `cd framework && go test ./templates ./config -count=1` 绿。
