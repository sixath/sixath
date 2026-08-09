package tool

import (
	"context"
	"testing"
	"time"
)

func TestInMemoryAskUserPendingStore_SaveGetDelete(t *testing.T) {
	store := NewInMemoryAskUserPendingStore()
	ctx := context.Background()
	p := PendingInputRequest{
		RequestID: "req_1",
		Token:     "tok_1",
		SessionID: "sess_a",
		Field:     "ssh_password",
		Kind:      "password",
		CreatedAt: time.Now(),
	}
	if err := store.SavePending(ctx, "sess_a", p); err != nil {
		t.Fatal(err)
	}
	got, err := store.GetPending(ctx, "sess_a", "tok_1")
	if err != nil || got == nil || got.RequestID != "req_1" {
		t.Fatalf("GetPending: got=%#v err=%v", got, err)
	}
	if err := store.DeletePending(ctx, "sess_a", "tok_1"); err != nil {
		t.Fatal(err)
	}
	if got2, _ := store.GetPending(ctx, "sess_a", "tok_1"); got2 != nil {
		t.Fatalf("expected deleted, got %#v", got2)
	}
}

func TestInMemoryAskUserFulfillmentStore_PutGetDelete(t *testing.T) {
	store := NewInMemoryAskUserFulfillmentStore()
	ctx := context.Background()
	if err := store.PutSecret(ctx, "sess_a", "ssh_password", "secret123", time.Minute); err != nil {
		t.Fatal(err)
	}
	v, err := store.GetSecret(ctx, "sess_a", "ssh_password")
	if err != nil || v != "secret123" {
		t.Fatalf("GetSecret: v=%q err=%v", v, err)
	}
	if err := store.DeleteSecret(ctx, "sess_a", "ssh_password"); err != nil {
		t.Fatal(err)
	}
	if _, err := store.GetSecret(ctx, "sess_a", "ssh_password"); err == nil {
		t.Fatal("expected error after delete")
	}
}
