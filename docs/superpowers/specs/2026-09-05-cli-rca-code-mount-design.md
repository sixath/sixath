# S7 收口：CLI RCA 只走 `workspace/code`

**日期**: 2026-09-05  
**状态**: 已确认（父规格 §4 双根；S4 非目标；2026-09-05 实施）  
**范围**: `framework/templates` 装配与 `config.Workspace`。不改 Portal `MergeRCARoots`、不改 jaeger/ES。  
**父规格**: [`2026-09-05-agent-model-workspace-harness-design.md`](./2026-09-05-agent-model-workspace-harness-design.md)  
**前置**: [S4](./2026-09-05-rca-code-mount-only-design.md)、[S6](./2026-09-05-whole-repo-update-reject-design.md)

**一句话**: CLI / templates 与 Portal 同一条故事：`rca_grep` 只认 `workspace/code`；配置里的 `rca.repos.roots` 不再当平行宇宙。

---

## 1. 背景

S4 关掉了 Portal 工具 YAML `rca.roots`，并写明 **不改** `framework/templates` 的 `rca.repos.roots`。Portal 整仓写入口已由 S5/S6 收口。CLI handler 仍用 `cfg.RCA.Repos.Roots` 注册代码检索，且 `NewSkillsAwareChatHandlerFromConfig` 不把 workspace 交给 ReAct。

---

## 2. 已锁定决策

| 项 | 选择 |
|----|------|
| `config.Workspace` | 新增 YAML `workspace`；`AGENT_WORKSPACE` 可覆盖 |
| `rca_grep` / `rca_glob` / `rca_read` | `workspace.ResolveCodeMount(cfg.Workspace)`；无挂载 → **不注册** |
| `rca.repos.roots` | 运行时忽略；字段可留在 yaml |
| `jaeger_trace` / `es_log_query` | **不改** |
| ReAct | 非空 `cfg.Workspace` → `WithReActWorkspace` |
| 空 workspace | CLI 仍可对话；只是没有文件器官根、不注册 rca 代码工具 |

---

## 3. 行为

```text
registerRCATools:
  code = ResolveCodeMount(cfg.Workspace)
  code 非空 → RegisterRCACodeTools([code])
  否则 skip rca_grep（即使 Repos.Roots 有值）
  jaeger / es 仍按现网
```

---

## 4. 非目标

- 不改 Portal `MergeRCARoots` / Agent CRUD
- 不扫示例 yaml 批量加 `workspace:`
- 不强制 CLI 空 workspace 拒跑
- 不改 `dataquery` / MCP handler（本切片只动 RCA 接线与 skills handler 的 workspace 选项）

---

## 5. 成功标准

1. 仅配置 `rca.repos.roots`、无 `workspace/code` → 不注册 `rca_grep`。
2. 有 `workspace/code` 时注册，且忽略 roots。
3. jaeger / es 行为与现网一致。
4. `cd framework && go test ./config ./templates ./workspace -count=1` 绿。
