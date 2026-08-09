package tool

import (
	"context"
	"testing"
	"time"
)

func TestAskUser_PendingThenFulfill(t *testing.T) {
	pendingStore := NewInMemoryAskUserPendingStore()
	fulfillStore := NewInMemoryAskUserFulfillmentStore()
	reg := NewRegistry()
	cfg := &AskUserConfig{
		PendingStore:     pendingStore,
		FulfillmentStore: fulfillStore,
		TokenGen:         &fakeTokenGen{next: "tok_abc"},
		TTLSeconds:       600,
	}
	if err := RegisterAskUserTool(reg, cfg); err != nil {
		t.Fatal(err)
	}
	tl, ok := reg.Get("ask_user")
	if !ok {
		t.Fatal("ask_user not registered")
	}
	ctx := context.WithValue(context.Background(), ContextKeySessionID, "sess_1")
	ctx = WithSecretProvider(ctx, fulfillStore)

	res, err := tl.Execute(ctx, map[string]any{
		"prompt": "Enter SSH password",
		"kind":   "password",
		"field":  "ssh_password",
	})
	if err != nil {
		t.Fatal(err)
	}
	m, ok := res.(map[string]any)
	if !ok || m["status"] != "pending" || m["token"] != "tok_abc" {
		t.Fatalf("pending: %#v", res)
	}

	res2, err := tl.Execute(ctx, map[string]any{
		"response_token": "tok_abc",
		"value":          "hunter2",
	})
	if err != nil {
		t.Fatal(err)
	}
	m2, ok := res2.(map[string]any)
	if !ok || m2["status"] != "fulfilled" || m2["value_redacted"] != true {
		t.Fatalf("fulfilled: %#v", res2)
	}
	if _, has := m2["value"]; has {
		t.Fatalf("password must not appear in tool result: %#v", m2)
	}
	secret, err := fulfillStore.GetSecret(ctx, "sess_1", "ssh_password")
	if err != nil || secret != "hunter2" {
		t.Fatalf("secret store: %q err=%v", secret, err)
	}
}

func TestAskUser_Cancelled(t *testing.T) {
	pendingStore := NewInMemoryAskUserPendingStore()
	reg := NewRegistry()
	cfg := &AskUserConfig{
		PendingStore: pendingStore,
		TokenGen:     &fakeTokenGen{next: "tok_cancel"},
		TTLSeconds:   600,
	}
	if err := RegisterAskUserTool(reg, cfg); err != nil {
		t.Fatal(err)
	}
	tl, _ := reg.Get("ask_user")
	ctx := context.WithValue(context.Background(), ContextKeySessionID, "sess_1")

	_, err := tl.Execute(ctx, map[string]any{
		"prompt": "Confirm?",
		"kind":   "confirm",
		"field":  "go_ahead",
	})
	if err != nil {
		t.Fatal(err)
	}
	res, err := tl.Execute(ctx, map[string]any{
		"response_token": "tok_cancel",
		"cancelled":      true,
	})
	if err != nil {
		t.Fatal(err)
	}
	m, _ := res.(map[string]any)
	if m["status"] != "cancelled" {
		t.Fatalf("got %#v", res)
	}
}

func TestAskUser_ExpiredToken(t *testing.T) {
	pendingStore := NewInMemoryAskUserPendingStore()
	reg := NewRegistry()
	cfg := &AskUserConfig{
		PendingStore: pendingStore,
		TokenGen:     &fakeTokenGen{next: "tok_missing"},
		TTLSeconds:   600,
	}
	if err := RegisterAskUserTool(reg, cfg); err != nil {
		t.Fatal(err)
	}
	tl, _ := reg.Get("ask_user")
	ctx := context.WithValue(context.Background(), ContextKeySessionID, "sess_1")

	res, err := tl.Execute(ctx, map[string]any{
		"response_token": "unknown_token",
	})
	if err != nil {
		t.Fatal(err)
	}
	m, _ := res.(map[string]any)
	if m["status"] != "expired" {
		t.Fatalf("got %#v", res)
	}
}

func TestAskUser_TextFulfilledIncludesValue(t *testing.T) {
	pendingStore := NewInMemoryAskUserPendingStore()
	reg := NewRegistry()
	cfg := &AskUserConfig{
		PendingStore: pendingStore,
		TokenGen:     &fakeTokenGen{next: "tok_text"},
		TTLSeconds:   600,
	}
	if err := RegisterAskUserTool(reg, cfg); err != nil {
		t.Fatal(err)
	}
	tl, _ := reg.Get("ask_user")
	ctx := context.WithValue(context.Background(), ContextKeySessionID, "sess_1")

	_, err := tl.Execute(ctx, map[string]any{
		"prompt": "Username?",
		"kind":   "text",
		"field":  "username",
	})
	if err != nil {
		t.Fatal(err)
	}
	res, err := tl.Execute(ctx, map[string]any{
		"response_token": "tok_text",
		"value":          "admin",
	})
	if err != nil {
		t.Fatal(err)
	}
	m, _ := res.(map[string]any)
	if m["status"] != "fulfilled" || m["value"] != "admin" {
		t.Fatalf("got %#v", res)
	}
}

func TestAskUser_SameFieldInvalidatesOldToken(t *testing.T) {
	pendingStore := NewInMemoryAskUserPendingStore()
	reg := NewRegistry()
	tokenGen := &fakeTokenGen{}
	cfg := &AskUserConfig{
		PendingStore: pendingStore,
		TokenGen:     tokenGen,
		TTLSeconds:   600,
	}
	if err := RegisterAskUserTool(reg, cfg); err != nil {
		t.Fatal(err)
	}
	tl, _ := reg.Get("ask_user")
	ctx := context.WithValue(context.Background(), ContextKeySessionID, "sess_1")

	tokenGen.next = "tok_old"
	_, err := tl.Execute(ctx, map[string]any{"prompt": "p", "field": "username"})
	if err != nil {
		t.Fatal(err)
	}
	tokenGen.next = "tok_new"
	_, err = tl.Execute(ctx, map[string]any{"prompt": "p2", "field": "username"})
	if err != nil {
		t.Fatal(err)
	}
	if got, _ := pendingStore.GetPending(ctx, "sess_1", "tok_old"); got != nil {
		t.Fatal("old token should be invalidated")
	}
	if got, _ := pendingStore.GetPending(ctx, "sess_1", "tok_new"); got == nil {
		t.Fatal("new token should exist")
	}
}

func TestAskUser_ProposeRequiresSession(t *testing.T) {
	reg := NewRegistry()
	cfg := &AskUserConfig{
		PendingStore: NewInMemoryAskUserPendingStore(),
		TokenGen:     &fakeTokenGen{next: "t"},
	}
	if err := RegisterAskUserTool(reg, cfg); err != nil {
		t.Fatal(err)
	}
	tl, _ := reg.Get("ask_user")
	_, err := tl.Execute(context.Background(), map[string]any{"prompt": "hi"})
	if err == nil {
		t.Fatal("expected session error")
	}
}

func TestAskUser_PendingExpiresByTTL(t *testing.T) {
	pendingStore := NewInMemoryAskUserPendingStore()
	reg := NewRegistry()
	cfg := &AskUserConfig{
		PendingStore: pendingStore,
		TokenGen:     &fakeTokenGen{next: "tok_ttl"},
		TTLSeconds:   1,
	}
	if err := RegisterAskUserTool(reg, cfg); err != nil {
		t.Fatal(err)
	}
	tl, _ := reg.Get("ask_user")
	ctx := context.WithValue(context.Background(), ContextKeySessionID, "sess_1")

	_, err := tl.Execute(ctx, map[string]any{"prompt": "p", "field": "x"})
	if err != nil {
		t.Fatal(err)
	}
	// Backdate pending
	got, _ := pendingStore.GetPending(ctx, "sess_1", "tok_ttl")
	got.CreatedAt = time.Now().Add(-2 * time.Second)
	_ = pendingStore.SavePending(ctx, "sess_1", *got)

	res, err := tl.Execute(ctx, map[string]any{
		"response_token": "tok_ttl",
		"value":          "v",
	})
	if err != nil {
		t.Fatal(err)
	}
	m, _ := res.(map[string]any)
	if m["status"] != "expired" {
		t.Fatalf("got %#v", res)
	}
}

func TestAskUser_PendingCapturesToolCallIDAndReasoningFromContext(t *testing.T) {
	pendingStore := NewInMemoryAskUserPendingStore()
	reg := NewRegistry()
	cfg := &AskUserConfig{
		PendingStore: pendingStore,
		TokenGen:     &fakeTokenGen{next: "tok_rc"},
		TTLSeconds:   600,
	}
	if err := RegisterAskUserTool(reg, cfg); err != nil {
		t.Fatal(err)
	}
	tl, _ := reg.Get("ask_user")
	ctx := context.WithValue(context.Background(), ContextKeySessionID, "sess_rc")
	ctx = context.WithValue(ctx, ContextKeyToolCallID, "call_jaeger_1")
	ctx = context.WithValue(ctx, ContextKeyReasoningContent, "need trace_id from user")

	res, err := tl.Execute(ctx, map[string]any{
		"prompt": "Provide Jaeger trace_id",
		"kind":   "text",
		"field":  "jaeger_query",
	})
	if err != nil {
		t.Fatal(err)
	}
	m, ok := res.(map[string]any)
	if !ok || m["status"] != "pending" || m["token"] != "tok_rc" {
		t.Fatalf("pending: %#v", res)
	}
	got, err := pendingStore.GetPending(ctx, "sess_rc", "tok_rc")
	if err != nil || got == nil {
		t.Fatalf("GetPending: %#v err=%v", got, err)
	}
	if got.ToolCallID != "call_jaeger_1" {
		t.Fatalf("ToolCallID: got %q", got.ToolCallID)
	}
	if got.ReasoningContent != "need trace_id from user" {
		t.Fatalf("ReasoningContent: got %q", got.ReasoningContent)
	}
}
