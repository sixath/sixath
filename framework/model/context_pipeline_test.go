package model

import (
	"strings"
	"testing"
)

func TestPrepareChatContext_EmitsL0CompressWhenMessagesDropped(t *testing.T) {
	var kinds []string
	var removed []int
	sink := func(kind string, detail map[string]any) {
		kinds = append(kinds, kind)
		if n, ok := detail["messages_removed"].(int); ok {
			removed = append(removed, n)
		}
	}
	msgs := []Message{
		{Role: "system", Content: "sys"},
		{Role: "user", Content: "old"},
		{Role: "assistant", Content: "a"},
		{Role: "user", Content: "new"},
	}
	cfg := &CallConfig{MaxContextRunes: 1, ContextTrace: sink}
	out := PrepareChatContext(msgs, cfg)
	if len(out) >= len(msgs) {
		t.Fatalf("expected compression to drop messages, got in=%d out=%d", len(msgs), len(out))
	}
	if len(kinds) < 1 || kinds[0] != "l0_compress" {
		t.Fatalf("expected l0_compress event first, got kinds=%v", kinds)
	}
	if len(removed) < 1 || removed[0] < 1 {
		t.Fatalf("expected messages_removed >= 1, got removed=%v", removed)
	}
}

func TestPrepareChatContext_EmitsStripOrphanWhenLeadingToolsRemoved(t *testing.T) {
	var kinds []string
	msgs := []Message{
		{Role: "system", Content: "s"},
		{Role: "tool", Content: `{}`, Metadata: map[string]any{"tool_call_id": "x"}},
		{Role: "user", Content: "hi"},
	}
	sink := func(kind string, detail map[string]any) { kinds = append(kinds, kind) }
	cfg := &CallConfig{ContextTrace: sink}
	_ = PrepareChatContext(msgs, cfg)
	found := false
	for _, k := range kinds {
		if k == "strip_orphan_tools" {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("expected strip_orphan_tools in kinds=%v", kinds)
	}
}

func TestPrepareChatContext_NilCallCfgOnlyStrips(t *testing.T) {
	msgs := []Message{{Role: "user", Content: "hi"}}
	out := PrepareChatContext(msgs, nil)
	if len(out) != 1 {
		t.Fatalf("expected 1 message, got %d", len(out))
	}
}

func TestPrepareChatContext_EmitsL0CompressTokensWhenSoftThresholdExceeded(t *testing.T) {
	var kinds []string
	msgs := []Message{
		{Role: "system", Content: "sys"},
		{Role: "user", Content: strings.Repeat("测", 500)},
		{Role: "assistant", Content: "a"},
		{Role: "user", Content: strings.Repeat("文", 500)},
	}
	sink := func(kind string, detail map[string]any) { kinds = append(kinds, kind) }
	cfg := &CallConfig{
		MaxContextTokensSoft: 200,
		TokenEstimateAlpha:   2.0,
		ContextTrace:         sink,
	}
	out := PrepareChatContext(msgs, cfg)
	if len(out) >= len(msgs) {
		t.Fatalf("expected token-soft compression to drop messages, got in=%d out=%d", len(msgs), len(out))
	}
	found := false
	for _, k := range kinds {
		if k == "l0_compress_tokens" {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("expected l0_compress_tokens in kinds=%v", kinds)
	}
}
