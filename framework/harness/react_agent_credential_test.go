package harness

import (
	"context"
	"strings"
	"testing"

	"github.com/sixath/framework/tool"
)

func TestCredentialSolicitationRedirect_skipsWhenEvidenceExists(t *testing.T) {
	cat := tool.ToolCatalog{Entries: []tool.ToolCatalogEntry{{
		Name: "execute_read", Available: true,
		Bindings:    map[string]string{"datasource_id": "prod_mysql", "type": "mysql"},
		SearchHints: []string{"mysql", "数据库", "host", "password"},
	}}}
	ctx := context.WithValue(context.Background(), tool.ContextKeyToolCatalog, cat)
	trace := &RunTrace{ToolCalls: []ToolCallRecord{{
		ToolName: "es_log_query", Allowed: true, Result: map[string]any{"ok": true},
	}}}
	ask := "请提供 MySQL 的 Host、Port、用户名、密码"
	if _, _, ok := credentialSolicitationRedirect(ctx, ask, 0, trace, "GetGameInfo 失败原因"); ok {
		t.Fatal("already used bound evidence this turn; must not redirect")
	}
}

func TestCredentialSolicitationRedirect_trueAskWithoutEvidenceStillFires(t *testing.T) {
	cat := tool.ToolCatalog{Entries: []tool.ToolCatalogEntry{{
		Name: "execute_read", Available: true,
		Bindings:    map[string]string{"datasource_id": "prod_mysql", "type": "mysql"},
		SearchHints: []string{"mysql", "数据库", "host", "password"},
	}}}
	ctx := context.WithValue(context.Background(), tool.ContextKeyToolCatalog, cat)
	prompt, match, ok := credentialSolicitationRedirect(ctx, "请提供 MySQL 的 Host、Port、用户名、密码", 0, &RunTrace{}, "查流水失败原因")
	if !ok || match.Name != "execute_read" {
		t.Fatalf("ok=%v match=%#v", ok, match)
	}
	if !strings.Contains(prompt, "查流水失败原因") {
		t.Fatalf("prompt must include G: %s", prompt)
	}
	if !strings.Contains(prompt, "禁止调用 skills_list") {
		t.Fatalf("prompt must forbid skills_list: %s", prompt)
	}
}
