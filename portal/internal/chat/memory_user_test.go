package chat

import (
	"context"
	"testing"

	"backend/internal/biz"
)

func TestResolveMemoryUserID_PrefersSession(t *testing.T) {
	ctx := biz.WithCallerUserID(context.Background(), "caller")
	got := ResolveMemoryUserID(ctx, &biz.ChatSession{UserID: "owner"})
	if got != "owner" {
		t.Fatalf("got %q", got)
	}
}

func TestResolveMemoryUserID_FallsBackToCaller(t *testing.T) {
	ctx := biz.WithCallerUserID(context.Background(), "caller")
	got := ResolveMemoryUserID(ctx, &biz.ChatSession{UserID: ""})
	if got != "caller" {
		t.Fatalf("got %q", got)
	}
}

func TestResolveMemoryUserID_Empty(t *testing.T) {
	got := ResolveMemoryUserID(context.Background(), nil)
	if got != "" {
		t.Fatalf("got %q", got)
	}
}
