package service

import (
	"context"
	"errors"
	"testing"

	"github.com/go-kratos/kratos/v2/log"
	"github.com/sixath/framework/tool"
)

type fakeRCARootResolver struct {
	roots []tool.RCARoot
	err   error
	got   string
}

func (f *fakeRCARootResolver) RCARootsForAgent(_ context.Context, agentID string) ([]tool.RCARoot, error) {
	f.got = agentID
	return f.roots, f.err
}

func TestResolveRCARoots(t *testing.T) {
	logger := log.NewHelper(log.DefaultLogger)
	if got := resolveRCARoots(context.Background(), nil, "a", logger); got != nil {
		t.Fatalf("nil resolver: %v", got)
	}
	f := &fakeRCARootResolver{roots: []tool.RCARoot{{Name: "cg/a", Path: "/c/cg/a"}}}
	if got := resolveRCARoots(context.Background(), f, "agent-1", logger); len(got) != 1 || f.got != "agent-1" {
		t.Fatalf("got %v, agent %q", got, f.got)
	}
	f = &fakeRCARootResolver{roots: []tool.RCARoot{}}
	if got := resolveRCARoots(context.Background(), f, "agent-1", logger); got == nil || len(got) != 0 {
		t.Fatalf("bindings without usable roots must stay non-nil empty, got %#v", got)
	}
	f = &fakeRCARootResolver{}
	if got := resolveRCARoots(context.Background(), f, "agent-1", logger); got != nil {
		t.Fatalf("no bindings must stay nil, got %#v", got)
	}
	f = &fakeRCARootResolver{err: errors.New("db down")}
	if got := resolveRCARoots(context.Background(), f, "agent-1", logger); got != nil {
		t.Fatalf("error must fall back to nil, got %v", got)
	}
	if got := resolveRCARoots(context.Background(), f, "", logger); got != nil {
		t.Fatalf("empty agent id: %v", got)
	}
}
