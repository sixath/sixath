package context

import (
	stdctx "context"
	"testing"

	"github.com/sixath/framework/model"
)

func TestTraceSink_EmitsOnPrepare(t *testing.T) {
	var kinds []string
	sink := TraceSink(func(k string, _ map[string]any) { kinds = append(kinds, k) })
	cfg := &PipelineConfig{Trace: sink, MaxContextRunes: 1}
	msgs := []model.Message{
		{Role: "system", Content: "sys"},
		{Role: "user", Content: "old"},
		{Role: "assistant", Content: "a"},
		{Role: "user", Content: "new"},
	}
	_ = PrepareCtx(stdctx.Background(), msgs, cfg)
	if len(kinds) < 1 {
		t.Fatalf("expected at least one trace event, got %v", kinds)
	}
}
