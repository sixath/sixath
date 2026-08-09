package chat

import (
	"context"
	"errors"
	"testing"

	"backend/internal/biz"
)

type hybridAgentGetter struct {
	metas map[string]*biz.AgentMeta
	err   error
}

func (g hybridAgentGetter) Get(_ context.Context, id string) (*biz.AgentMeta, error) {
	if g.err != nil {
		return nil, g.err
	}
	return g.metas[id], nil
}

func agentWithHybrid(v *bool) *biz.AgentMeta {
	return &biz.AgentMeta{RuntimeTools: biz.RuntimeToolsConfig{HybridRecall: v}}
}

func TestHybridRecallGate_FailOpen(t *testing.T) {
	ctx := context.Background()
	off := false

	t.Run("nil getter", func(t *testing.T) {
		prev := globalMemoryAgentGetter
		t.Cleanup(func() { globalMemoryAgentGetter = prev })
		SetMemoryAgentGetter(nil)
		if !hybridRecallGate(nil)(ctx, "a1") {
			t.Fatal("nil getter must fail open")
		}
	})

	t.Run("blank agent id", func(t *testing.T) {
		g := hybridAgentGetter{metas: map[string]*biz.AgentMeta{"": agentWithHybrid(&off)}}
		if !hybridRecallGate(g)(ctx, "   ") {
			t.Fatal("blank agentID must fail open")
		}
	})

	t.Run("lookup error", func(t *testing.T) {
		g := hybridAgentGetter{err: errors.New("boom")}
		if !hybridRecallGate(g)(ctx, "a1") {
			t.Fatal("lookup error must fail open")
		}
	})

	t.Run("unknown agent", func(t *testing.T) {
		g := hybridAgentGetter{metas: map[string]*biz.AgentMeta{}}
		if !hybridRecallGate(g)(ctx, "missing") {
			t.Fatal("missing agent must fail open")
		}
	})

	t.Run("unset field", func(t *testing.T) {
		g := hybridAgentGetter{metas: map[string]*biz.AgentMeta{"a1": agentWithHybrid(nil)}}
		if !hybridRecallGate(g)(ctx, "a1") {
			t.Fatal("unset HybridRecall must fail open")
		}
	})
}

func TestHybridRecallGate_ExplicitFlag(t *testing.T) {
	ctx := context.Background()
	on, off := true, false
	g := hybridAgentGetter{metas: map[string]*biz.AgentMeta{
		"on":  agentWithHybrid(&on),
		"off": agentWithHybrid(&off),
	}}
	gate := hybridRecallGate(g)

	if !gate(ctx, "on") {
		t.Fatal("explicit true must allow hybrid recall")
	}
	if gate(ctx, "off") {
		t.Fatal("explicit false must block hybrid recall")
	}
}

func TestHybridRecallGate_FallsBackToLiveGlobalGetter(t *testing.T) {
	prev := globalMemoryAgentGetter
	t.Cleanup(func() { globalMemoryAgentGetter = prev })

	SetMemoryAgentGetter(nil)
	gate := hybridRecallGate(nil)

	off := false
	SetMemoryAgentGetter(hybridAgentGetter{metas: map[string]*biz.AgentMeta{
		"off": agentWithHybrid(&off),
	}})
	if gate(context.Background(), "off") {
		t.Fatal("gate built with nil agents must observe later SetMemoryAgentGetter")
	}
}

func TestDefaultMemoryStoreOptions_HybridRecallAlwaysSet(t *testing.T) {
	prevGetter := globalMemoryAgentGetter
	prevExtract := storedExtractionYAML
	t.Cleanup(func() {
		globalMemoryAgentGetter = prevGetter
		storedExtractionYAML = prevExtract
	})

	SetMemoryExtractionConfig(nil)

	SetMemoryAgentGetter(nil)
	if DefaultMemoryStoreOptions().HybridRecall == nil {
		t.Fatal("expected HybridRecall gate even without an AgentGetter")
	}

	off := false
	SetMemoryAgentGetter(hybridAgentGetter{metas: map[string]*biz.AgentMeta{"off": agentWithHybrid(&off)}})
	opts := DefaultMemoryStoreOptions()
	if opts.HybridRecall == nil {
		t.Fatal("expected HybridRecall gate with an AgentGetter")
	}
	if opts.HybridRecall(context.Background(), "off") {
		t.Fatal("gate must consult the configured AgentGetter")
	}
}
