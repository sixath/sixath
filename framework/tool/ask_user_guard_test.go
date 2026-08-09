package tool

import (
	"context"
	"strings"
	"testing"
)

func TestAskUser_GuardBlocksCredentialRequest(t *testing.T) {
	reg := NewRegistry()
	cat := ToolCatalog{Entries: []ToolCatalogEntry{{
		Name: "execute_read", Available: true,
		Bindings:    map[string]string{"datasource_id": "prod_mysql", "type": "mysql"},
		SearchHints: []string{"mysql", "password", "host"},
		Description: "Run read-only SQL",
	}}}
	cfg := &AskUserConfig{
		PendingStore: NewInMemoryAskUserPendingStore(),
		TokenGen:     &fakeTokenGen{next: "tok_guard"},
		GuardConfig:  &AskUserGuardConfig{MinScore: 2.0},
	}
	if err := RegisterAskUserTool(reg, cfg); err != nil {
		t.Fatal(err)
	}
	ctx := context.WithValue(context.Background(), ContextKeySessionID, "s1")
	ctx = context.WithValue(ctx, ContextKeyToolCatalog, cat)
	tl, _ := reg.Get("ask_user")
	out, err := tl.Execute(ctx, map[string]any{"prompt": "请提供 MySQL 密码和 host"})
	if err != nil {
		t.Fatal(err)
	}
	msg, ok := out.(string)
	if !ok {
		t.Fatalf("expected blocked string, got %#v", out)
	}
	if !strings.Contains(msg, "execute_read") {
		t.Fatalf("expected execute_read in blocked message, got %q", msg)
	}
	if strings.Contains(msg, "pending") {
		t.Fatalf("blocked message should not mention pending: %q", msg)
	}
}

func TestAskUser_GuardBlocksCombinedMysqlWebhookWithoutHTTPRequest(t *testing.T) {
	reg := NewRegistry()
	cat := ToolCatalog{Entries: []ToolCatalogEntry{
		{Name: "http_request", Available: true, Toolset: ToolsetWeb,
			Description: "Send an HTTP request with method, url, headers and optional body"},
		{Name: "execute_read", Available: true,
			Bindings:    map[string]string{"datasource_id": "archive_mysql", "type": "mysql"},
			SearchHints: []string{"mysql", "数据库", "password", "host"},
			Description: "Run read-only SQL"},
		{Name: "send_to_wecom", Available: true,
			Bindings:    map[string]string{"channel_id": "ch-ops", "channel_type": "wecom"},
			SearchHints: []string{"企微", "webhook"},
			Description: "Push to WeCom"},
	}}
	cfg := &AskUserConfig{
		PendingStore: NewInMemoryAskUserPendingStore(),
		TokenGen:     &fakeTokenGen{next: "tok_guard2"},
		GuardConfig:  &AskUserGuardConfig{MinScore: 2.0},
	}
	if err := RegisterAskUserTool(reg, cfg); err != nil {
		t.Fatal(err)
	}
	ctx := context.WithValue(context.Background(), ContextKeySessionID, "s1")
	ctx = context.WithValue(ctx, ContextKeyToolCatalog, cat)
	tl, _ := reg.Get("ask_user")
	prompt := "请提供 MySQL host、port、database、user、password 和企业微信 Webhook URL"
	out, err := tl.Execute(ctx, map[string]any{"prompt": prompt, "field": "db_webhook_info"})
	if err != nil {
		t.Fatal(err)
	}
	msg, ok := out.(string)
	if !ok {
		t.Fatalf("expected blocked string, got %#v", out)
	}
	if strings.Contains(msg, "http_request") {
		t.Fatalf("must not redirect to http_request, got %q", msg)
	}
	if !strings.Contains(msg, "execute_read") && !strings.Contains(msg, "send_to_wecom") {
		t.Fatalf("expected bound tool redirect, got %q", msg)
	}
}

func TestAskUser_GuardAllowsConfirm(t *testing.T) {
	reg := NewRegistry()
	cat := ToolCatalog{Entries: []ToolCatalogEntry{{
		Name: "execute_read", Available: true,
		SearchHints: []string{"mysql", "password", "host"},
	}}}
	pendingStore := NewInMemoryAskUserPendingStore()
	cfg := &AskUserConfig{
		PendingStore: pendingStore,
		TokenGen:     &fakeTokenGen{next: "tok_confirm"},
		GuardConfig: &AskUserGuardConfig{
			MinScore:    2.0,
			ExemptKinds: []string{"confirm", "select"},
		},
	}
	if err := RegisterAskUserTool(reg, cfg); err != nil {
		t.Fatal(err)
	}
	ctx := context.WithValue(context.Background(), ContextKeySessionID, "s1")
	ctx = context.WithValue(ctx, ContextKeyToolCatalog, cat)
	tl, _ := reg.Get("ask_user")
	out, err := tl.Execute(ctx, map[string]any{
		"prompt": "是否继续执行 MySQL 查询？",
		"kind":   "confirm",
		"field":  "go_ahead",
	})
	if err != nil {
		t.Fatal(err)
	}
	m, ok := out.(map[string]any)
	if !ok || m["status"] != "pending" {
		t.Fatalf("confirm kind should not be blocked, got %#v", out)
	}
	if m["token"] != "tok_confirm" {
		t.Fatalf("expected pending token, got %#v", m)
	}
}
