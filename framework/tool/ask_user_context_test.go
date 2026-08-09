package tool

import (
	"context"
	"testing"
)

func TestSecretFromContext(t *testing.T) {
	store := NewInMemoryAskUserFulfillmentStore()
	ctx := WithSecretProvider(context.Background(), store)
	ctx = context.WithValue(ctx, ContextKeySessionID, "sess_1")
	if err := store.PutSecret(ctx, "sess_1", "ssh_password", "x", 0); err != nil {
		t.Fatal(err)
	}
	v, ok := SecretFromContext(ctx, "ssh_password")
	if !ok || v != "x" {
		t.Fatalf("got %q ok=%v", v, ok)
	}
}

func TestSecretFromContext_Missing(t *testing.T) {
	v, ok := SecretFromContext(context.Background(), "ssh_password")
	if ok || v != "" {
		t.Fatalf("expected missing secret, got %q ok=%v", v, ok)
	}
}
