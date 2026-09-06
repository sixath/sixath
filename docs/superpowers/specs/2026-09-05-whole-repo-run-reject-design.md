# S5 收口：Run 拒绝整仓 workspace

**日期**: 2026-09-05  
**状态**: 已确认（父规格 §7.4 / §11 P2 后续任务；2026-09-05 实施）  
**范围**: 关掉 P2「已有整仓仍能打开」waiver。不改 ReAct、PromptBuilder、包名、RCA 注册。  
**父规格**: [`2026-09-05-agent-model-workspace-harness-design.md`](./2026-09-05-agent-model-workspace-harness-design.md)  
**前置**: [S4](./2026-09-05-rca-code-mount-only-design.md)

**一句话**: 对话与技能执行入口不再把整仓路径当可写 workspace；错误码与新建拒绝相同。

---

## 1. 背景

父规格把「整仓当 workspace」定为退役变体。P2 **仅拦新建**：Create 走 `WORKSPACE_WHOLE_REPO_RETIRED`；Run 只拒空字符串（`RequireWorkspaceRoot`）。已有整仓行仍能 Chat。

S4 关掉了 RCA 独立 `roots` waiver，并明确整仓仍不强制迁移。S5 关掉剩下这条 Run waiver：不迁移数据，只是跑不起来，直到改成默认可写根（可选 `workspace/code`）。

现网锚点：

- Create：`chat.WorkspaceUnderCodeRoots` → `biz.ErrWorkspaceWholeRepoRetired`
- Run：`biz.RequireWorkspaceRoot`（空字符串）
- Update / Get / List：**不拦**（P2 锁定，本切片保持）
- 浏览 HTTP / `code_roots` 白名单：留下，只服务挂 `workspace/code`

---

## 2. 已锁定决策

| 项 | 选择 |
|----|------|
| Chat / Stream / 快捷 Chat | 空 root → `WORKSPACE_REQUIRED`；整仓 → `WORKSPACE_WHOLE_REPO_RETIRED` |
| ExecuteSkill | 同上（cron 走该入口） |
| Update | **不拦**（用户靠编辑把路径改离 code root） |
| Get / List / 建会话 | **不拦** |
| 自动 `LinkCode` / 改库里的 workspace | **不做** |
| `code_roots` 未配置 | 与 Create 相同：检测不到整仓，不误杀 |
| CLI `rca.repos.roots` | 非目标（S4 已声明） |

---

## 3. 行为

```text
requireRunWorkspace(workspace, codeRoots):
  TrimSpace 空 → ErrWorkspaceRequired
  WorkspaceUnderCodeRoots → ErrWorkspaceWholeRepoRetired
  否则 ok
```

接线：`AgentService.Chat`、`AgentService.ExecuteSkill`、`ChatService.SendMessage`、`ChatService.SendMessageStream` 在现有 `RequireWorkspaceRoot` 处换成该检查。`ChatService` 注入与 Agent 相同的 `codeRoots`。

Web 编辑态退役提示改为：当前路径不能再跑对话，请改成默认可写根并可选挂 `code/`。

---

## 4. 非目标

- 不自动给旧 Agent `LinkCode` 或改 workspace 列
- 不拦 Update 写成整仓（本切片不扩大 Create 以外的写入口）
- 不改 harness 循环 / PromptBuilder / RCA `MergeRCARoots`
- 不删 `code_roots` 浏览 API

---

## 5. 成功标准

1. 快捷 Chat：workspace 落在任一 `code_roots` 下 → `WORKSPACE_WHOLE_REPO_RETIRED`；默认可写根仍可通过该检查。
2. `SendMessage` / Stream 同样拒绝整仓，且发生在写 user 消息、跑模型之前。
3. `ExecuteSkill` 同样拒绝。
4. Update 整仓路径仍然成功（回归不扩拦）。
5. `cd portal && go test ./internal/service -count=1` 绿（skip 预存 SQLITE_BUSY 若碰到）。
