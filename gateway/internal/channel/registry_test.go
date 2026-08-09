package channel

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLoad_ReadsChannelsYAML(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "channels.yaml")
	content := `
channels:
  - id: demo-webhook
    type: webhook
    default_agent: "00000000-0000-0000-0000-000000000001"
    webhook_secret: "dev-webhook-secret"
    ip_whitelist: ["127.0.0.1"]
    enabled: true
    default_reply_mode: async
  - id: other
    type: webhook
    default_agent: "agent-2"
    webhook_secret: "sec-2"
    ip_whitelist: []
    enabled: false
    default_reply_mode: sync
`
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}

	reg, err := Load(path)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}

	ch, err := reg.Get("demo-webhook")
	if err != nil {
		t.Fatalf("Get demo-webhook: %v", err)
	}
	if ch.ID != "demo-webhook" {
		t.Fatalf("ID=%q", ch.ID)
	}
	if ch.Type != "webhook" {
		t.Fatalf("Type=%q", ch.Type)
	}
	if ch.DefaultAgent != "00000000-0000-0000-0000-000000000001" {
		t.Fatalf("DefaultAgent=%q", ch.DefaultAgent)
	}
	if ch.WebhookSecret != "dev-webhook-secret" {
		t.Fatalf("WebhookSecret=%q", ch.WebhookSecret)
	}
	if len(ch.IPWhitelist) != 1 || ch.IPWhitelist[0] != "127.0.0.1" {
		t.Fatalf("IPWhitelist=%v", ch.IPWhitelist)
	}
	if !ch.Enabled {
		t.Fatal("Enabled want true")
	}
	if ch.DefaultReplyMode != "async" {
		t.Fatalf("DefaultReplyMode=%q", ch.DefaultReplyMode)
	}

	other, err := reg.Get("other")
	if err != nil {
		t.Fatalf("Get other: %v", err)
	}
	if other.Enabled {
		t.Fatal("other.Enabled want false")
	}
	if other.DefaultReplyMode != "sync" {
		t.Fatalf("other.DefaultReplyMode=%q", other.DefaultReplyMode)
	}
}

func TestGet_UnknownID(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "channels.yaml")
	content := `
channels:
  - id: demo-webhook
    type: webhook
    default_agent: "agent-1"
    webhook_secret: "secret"
    enabled: true
    default_reply_mode: async
`
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	reg, err := Load(path)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	_, err = reg.Get("missing")
	if err == nil {
		t.Fatal("expected error for unknown channel id")
	}
}
