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

- [ ] `TestCatalogSearchGo_omitsPlainTextCredentialRedirect`
- [ ] 先跑必须红

```go
func TestCatalogSearchGo_omitsPlainTextCredentialRedirect(t *testing.T) {
	b, err := os.ReadFile("catalog_search.go")
	if err != nil {
		t.Fatal(err)
	}
	src := string(b)
	for _, needle := range []string{
		"MatchCredentialSolicitation",
		"FormatCredentialSolicitationRedirect",
	} {
		if strings.Contains(src, needle) {
			t.Errorf("catalog_search.go must not define %s", needle)
		}
	}
}
```

Run: `cd framework; $env:GOMAXPROCS='1'; go test ./tool -count=1 -run TestCatalogSearchGo_omitsPlainTextCredentialRedirect`

Expected: FAIL（源码仍含这两个函数名）

---

### Task 2: 删货架函数

- [ ] 从 `catalog_search.go` 删 `MatchCredentialSolicitation`、`FormatCredentialSolicitationRedirect`、`formatBindingsBrief`、`deniesCredentialSolicitation`、`isSkillsFamilyTool`、`DefaultAskUserGuardConfig`
- [ ] 从 `catalog_search_test.go` 删全部 `TestMatchCredentialSolicitation_*`
- [ ] 保留 `TestMatchAskUserIntent_*` 与 `looksLikeCredentialSolicitation`
- [ ] `cd framework && go test ./tool ./harness -count=1`
- [ ] **Commit** `fix(tool): drop unused plain-text credential solicitation redirect`

---

### Task 3: 回归

- [ ] 不要 merge/push，除非用户明确要求。
