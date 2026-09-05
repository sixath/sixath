# S39 收口：删除无调用者的纯文本凭据回拉

**日期**: 2026-09-05  
**状态**: 已确认（S38 之后用户继续；选 S24 leftover：循环已拆、函数还在货架上）  
**范围**: `MatchCredentialSolicitation` / `FormatCredentialSolicitationRedirect` 及其只服务它们的辅助函数与单测。不改 `ask_user` 守卫、不改 `MaybeSpill`。不合 assembler。  
**父规格**: [`2026-09-05-agent-model-workspace-harness-design.md`](./2026-09-05-agent-model-workspace-harness-design.md)  
**前置**: [S24](./2026-09-05-remaining-default-path-off-design.md)；[S38](./2026-09-05-portal-setting-off-design.md)

**一句话**: ReAct 已不再拦终答「索密码」；这两个函数只被自己的单测调用。删掉，以免假装循环还能回拉。

---

## 1. 背景

S24 锁定：凭据回拉**移出 ReAct 循环**；`tool.MatchCredentialSolicitation` **保留**。磁盘（`rg`，排除 `_neo4j_q`）：

| leftover | 现网 |
|----------|------|
| `credentialSolicitationRedirect` | `react_agent.go` **已无**（S24 锁定测试） |
| `MatchCredentialSolicitation` / `FormatCredentialSolicitationRedirect` | 只活在 `catalog_search.go` 与对应单测 |
| `MatchAskUserIntent` / `looksLikeCredentialSolicitation` | **仍**给 `ask_user` 工具守卫（`RegisterAskUserTool`） |
| Channel `auto_route_*` / `MaybeSpill` / `growth.llm` | **还在干活，不是洞** |

函数留下等于「循环还能回压索凭」，和默认路径无关。

---

## 2. 已锁定决策

| 项 | 选择 |
|----|------|
| `MatchCredentialSolicitation` | **删除** |
| `FormatCredentialSolicitationRedirect` / `formatBindingsBrief` | **删除** |
| `deniesCredentialSolicitation` / `isSkillsFamilyTool` / `DefaultAskUserGuardConfig` | **删除**（仅被上述货架函数调用） |
| `MatchAskUserIntent` / `looksLikeCredentialSolicitation` / `fallbackBoundCredentialTool` | **保留**（ask_user 守卫） |
| `TestMatchAskUserIntent_*` / `ask_user_guard_test.go` | **保留** |
| Channel / `MaybeSpill` / assembler | **不改 / 不合** |

---

## 3. 行为

```text
MatchCredentialSolicitation / FormatCredentialSolicitationRedirect → 不存在
ReAct 终答仍不拦纯文本索凭（S24 已拆）
ask_user 工具仍可按 GuardConfig 拦截「请提供连接」并指向已绑定工具
```

---

## 4. 非目标

- 不改 `ask_user` 接线 / `MatchAskUserIntent`
- 不改 Channel / Gateway `auto_route_*`
- 不改 `MaybeSpill`
- 不合 assembler

---

## 5. 成功标准

1. `framework/tool/catalog_search.go` 不含 `MatchCredentialSolicitation` / `FormatCredentialSolicitationRedirect`。
2. 现网 `*.go`（排除 `_neo4j_q`）不含这两个标识符。
3. `cd framework && go test ./tool ./harness -count=1` 绿。
4. `TestMatchAskUserIntent_BlocksCredentialAsk` 仍通过。
