# S4 收口：RCA 只走 `workspace/code`

**日期**: 2026-09-05  
**状态**: 已确认（父规格 §7.4 / §11 P2 后续任务；2026-09-05 实施）  
**范围**: 关掉 P2 的 RCA 独立 `roots` waiver。不改 ReAct、PromptBuilder、包名。  
**父规格**: [`2026-09-05-agent-model-workspace-harness-design.md`](./2026-09-05-agent-model-workspace-harness-design.md)  
**前置**: [S3](./2026-09-05-harness-workspace-rename-design.md)

**一句话**: `rca_code` / `rca_symbol` 的检索根只能是该 Agent 的 `workspace/code`；工具配置里的 `roots` 不再当平行宇宙。

---

## 1. 背景

父规格把双根定为癌变温床：可写 workspace 与 `rca_*` 配置 roots 各算一套真相。P2 验收允许 **waiver**（无挂载时仍用工具 `roots`）并要求开后续任务。S4 就是该任务。

现网 `MergeRCARoots`：有 `workspace/code` 时只用它；否则退回 `rca.roots`。

---

## 2. 已锁定决策

| 项 | 选择 |
|----|------|
| `rca_code` / `rca_symbol` | 只认 `workspace.ResolveCodeMount`；无挂载 → **不注册**（现网空 roots 已 skip） |
| 工具 YAML `rca.roots` | 运行时忽略；字段可留在存储/导出，不强制清库 |
| `jaeger_trace` / `es_log_query` | **不改**（不是代码仓双根） |
| 整仓 workspace | **仍不强制迁移**（P2 另一条 waiver） |
| 浏览 HTTP / `code_roots` 白名单 | 留下，只服务「把目录挂到 `workspace/code`」 |

---

## 3. 行为

```text
registerRCATool(rca_code|rca_symbol):
  roots = MergeRCARoots(workspace, configured)  // configured 忽略
  roots 空 → skip + warn
  否则 Register*(reg, roots) 且 roots == [code mount]
```

`MergeRCARoots(workspace, _)`：

- 有 `code/` 目录或指向目录的 symlink → `[]string{abs}`
- 否则 → `nil`（不再返回 configured）

---

## 4. 非目标

- 不自动给旧 Agent `LinkCode`
- 不删 `framework/tool` 的 rca 实现
- 不改 PromptBuilder / harness 循环
- 不把 jaeger/ES 绑到 workspace

---

## 5. 成功标准

1. 无 `workspace/code` 时，即使 `rca.roots` 有值，也不注册 `rca_grep` / `rca_symbol`。
2. 有 `workspace/code` 时，忽略配置 roots，只用该目录。
3. jaeger / es_log 仍按现网注册。
4. `cd portal && go test ./internal/chat -count=1` 绿（skip 预存 SQLITE_BUSY 若碰到）。
5. Tool 表单不再把 `roots` 写成「无挂载后备」。
