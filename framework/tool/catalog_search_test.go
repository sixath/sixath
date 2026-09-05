package tool

import "testing"

func TestSearchCatalog_HitsExecuteRead(t *testing.T) {
	cat := ToolCatalog{Entries: []ToolCatalogEntry{{
		Name: "execute_read", Toolset: ToolsetFile, Available: true,
		SearchHints: []string{"mysql", "查询", "数据库"},
		Description: "Run read-only SQL",
	}}}
	results := SearchCatalog(cat, "mysql group by status", 5)
	if len(results) == 0 || results[0].Name != "execute_read" {
		t.Fatalf("expected execute_read top hit, got %v", results)
	}
}

func TestMatchAskUserIntent_BlocksCredentialAsk(t *testing.T) {
	cat := ToolCatalog{Entries: []ToolCatalogEntry{{
		Name: "execute_read", Available: true,
		Bindings:    map[string]string{"datasource_id": "prod_mysql", "type": "mysql"},
		SearchHints: []string{"mysql", "数据库", "host", "password"},
	}}}
	match, ok := MatchAskUserIntent(cat, AskUserGuardConfig{MinScore: 2.0}, "请提供 MySQL host 和 password", "text")
	if !ok || match.Name != "execute_read" {
		t.Fatal("expected block with execute_read redirect")
	}
}

func TestMatchAskUserIntent_DoesNotRedirectToHTTPRequest(t *testing.T) {
	cat := ToolCatalog{Entries: []ToolCatalogEntry{
		{Name: "http_request", Available: true, Toolset: ToolsetWeb,
			Description: "Send an HTTP request with method, url, headers and optional body, and return status, headers and body."},
		{Name: "execute_read", Available: true, Toolset: ToolsetFile,
			Bindings:    map[string]string{"datasource_id": "archive_mysql", "type": "mysql", "db_name": "archive"},
			SearchHints: []string{"mysql", "数据库", "SQL"},
			Description: "Run read-only SQL against bound datasource"},
		{Name: "send_to_wecom", Available: true, Toolset: ToolsetCore,
			Bindings:    map[string]string{"channel_id": "ch-ops", "channel_type": "wecom"},
			SearchHints: []string{"企微", "webhook", "推送"},
			Description: "Push a message to the bound WeCom group webhook"},
	}}
	prompt := "请提供以下信息（直接粘贴即可）：\n\n1. **MySQL 连接信息** — host / port / database / user / password\n2. **企业微信机器人 Webhook URL** — https://qyapi.weixin.qq.com/cgi-bin/webhook/send?key=xxx"

	match, ok := MatchAskUserIntent(cat, AskUserGuardConfig{MinScore: 2.0}, prompt, "text")
	if !ok {
		t.Fatal("expected credential ask_user to be blocked")
	}
	if match.Name == "http_request" {
		t.Fatal("must not redirect credential ask to http_request")
	}
	if match.Name != "execute_read" && match.Name != "send_to_wecom" {
		t.Fatalf("unexpected redirect tool %q", match.Name)
	}
	if len(match.Bindings) == 0 {
		t.Fatalf("redirect tool should have bindings, got %#v", match)
	}
}

func TestMatchAskUserIntent_ExemptConfirm(t *testing.T) {
	cat := ToolCatalog{Entries: []ToolCatalogEntry{{
		Name: "execute_read", Available: true, SearchHints: []string{"mysql"},
	}}}
	_, ok := MatchAskUserIntent(cat, AskUserGuardConfig{ExemptKinds: []string{"confirm"}}, "是否继续？", "confirm")
	if ok {
		t.Fatal("confirm kind should be exempt")
	}
}
