package model

import (
	"context"
	"testing"
)

func TestWithTraceSink_SetsCallConfigContextTrace(t *testing.T) {
	var kinds []string
	sink := TraceSink(func(k string, _ map[string]any) { kinds = append(kinds, k) })
	cfg := ApplyOptions(WithTraceSink(sink), WithMaxContextRunes(1))
	if cfg.ContextTrace == nil {
		t.Fatal("expected ContextTrace set via WithTraceSink")
	}
	msgs := []Message{
		{Role: "system", Content: "sys"},
		{Role: "user", Content: "old"},
		{Role: "assistant", Content: "a"},
		{Role: "user", Content: "new"},
	}
	_ = PrepareChatContextCtx(context.Background(), msgs, cfg)
	if len(kinds) < 1 {
		t.Fatalf("expected at least one trace event, got %v", kinds)
	}
}
