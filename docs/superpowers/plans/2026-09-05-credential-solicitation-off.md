# S39 Credential Solicitation Off Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** 删除无调用者的纯文本凭据回拉；保留 ask_user 的 `MatchAskUserIntent`。

**Architecture:** 先锁 `catalog_search.go` 不含货架函数名，再删函数与只服务它们的单测。

**Tech Stack:** Go（`framework/tool`）

**规格:** [`2026-09-05-credential-solicitation-off-design.md`](../specs/2026-09-05-credential-solicitation-off-design.md)

**分支:** 从 `feature/s38-portal-setting-off` 切 `feature/s39-credential-solicitation-off`。不要在 `main` 上改。PowerShell 无 HEREDOC。不要 `--no-verify`。不要提交 `_neo4j_q/`。

---

## File map

| 动作 | 路径 |
|------|------|
| 测 | `framework/tool/credential_solicitation_off_test.go` |
| 改 | `framework/tool/catalog_search.go`、`catalog_search_test.go` |

禁止：改 `MatchAskUserIntent`；改 MaybeSpill；合 assembler。

---

### Task 1: 失败锁定测试

- [x] `TestCatalogSearchGo_omitsPlainTextCredentialRedirect`
- [x] 先跑必须红

---

### Task 2: 删货架函数

- [x] 从 `catalog_search.go` 删 `MatchCredentialSolicitation`、`FormatCredentialSolicitationRedirect`、`formatBindingsBrief`、`deniesCredentialSolicitation`、`isSkillsFamilyTool`、`DefaultAskUserGuardConfig`
- [x] 从 `catalog_search_test.go` 删全部 `TestMatchCredentialSolicitation_*`
- [x] 保留 `TestMatchAskUserIntent_*` 与 `looksLikeCredentialSolicitation`
- [x] `cd framework && go test ./tool ./harness -count=1`
- [x] **Commit** `fix(tool): drop unused plain-text credential solicitation redirect`

---

### Task 3: 回归

- [x] 不要 merge/push，除非用户明确要求。
