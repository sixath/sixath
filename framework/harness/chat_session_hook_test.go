package harness

import (
	"context"
	"errors"
	"testing"
)

func TestChatSessionHookRegistry_OnEndOrder(t *testing.T) {
	var order []string
	r := NewChatSessionHookRegistry()
	r.Register(ChatSessionHookFunc(func(ctx context.Context, sessionID string) error {
		order = append(order, "a:"+sessionID)
		return nil
	}))
	r.Register(ChatSessionHookFunc(func(ctx context.Context, sessionID string) error {
		order = append(order, "b:"+sessionID)
		return nil
	}))
	if err := r.OnChatSessionEnd(context.Background(), "s1"); err != nil {
		t.Fatal(err)
	}
	if len(order) != 2 || order[0] != "a:s1" || order[1] != "b:s1" {
		t.Fatalf("%v", order)
	}
}

func TestChatSessionHookRegistry_HookErrorDoesNotStopOthers(t *testing.T) {
	var saw bool
	r := NewChatSessionHookRegistry()
	r.Register(ChatSessionHookFunc(func(context.Context, string) error {
		return errors.New("boom")
	}))
	r.Register(ChatSessionHookFunc(func(context.Context, string) error {
		saw = true
		return nil
	}))
	err := r.OnChatSessionEnd(context.Background(), "s")
	if err == nil {
		t.Fatal("expected joined error")
	}
	if !saw {
		t.Fatal("later hooks must still run")
	}
}
