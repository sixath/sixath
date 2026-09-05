package context

import (
	stdctx "context"
	"github.com/sixath/framework/model"
	"strings"
	"testing"
)

type stubAuxModel struct {
	summary string
}

func (s stubAuxModel) Generate(ctx stdctx.Context, prompt string, opts ...model.Option) (*model.Generation, error) {
	return &model.Generation{Text: "g"}, nil
}

func (s stubAuxModel) Chat(ctx stdctx.Context, messages []model.Message, opts ...model.Option) (*model.Generation, error) {
	txt := s.summary
	if txt == "" {
		txt = "COMPRESSED_SUMMARY"
	}
	return &model.Generation{Text: txt}, nil
}

func (s stubAuxModel) Embed(ctx stdctx.Context, texts []string, opts ...model.Option) ([]model.Embedding, error) {
	return nil, nil
}

func TestPrepareCtx_L2ReplacesMiddle(t *testing.T) {
	long := strings.Repeat("测", 3000)
	msgs := []model.Message{
		{Role: "system", Content: "sys"},
		{Role: "user", Content: long},
		{Role: "assistant", Content: "ack"},
		{Role: "user", Content: "final question"},
	}
	var sawL2 bool
	sink := func(kind string, detail map[string]any) {
		if kind == "l2_summarize" {
			sawL2 = true
		}
	}
	r := NewL2Runtime(stubAuxModel{}, 200, 3, 600, 2.0, 0)
	cfg := &PipelineConfig{L2: r, Trace: sink}
	out := PrepareCtx(stdctx.Background(), msgs, cfg)
	if !sawL2 {
		t.Fatal("expected l2_summarize trace")
	}
	found := false
	for _, m := range out {
		if m.Metadata != nil && m.Metadata[model.MetadataKeySixathOrigin] == model.OriginL2Handoff {
			found = true
			if !strings.Contains(m.Content, "COMPRESSED_SUMMARY") {
				t.Fatalf("unexpected l2 content: %q", m.Content)
			}
		}
	}
	if !found {
		t.Fatalf("expected l2_handoff message in %#v", out)
	}
}

func TestL2Runtime_CooldownAfterFailures(t *testing.T) {
	badChat := errorAux{}
	r := NewL2Runtime(badChat, 50, 2, 3600, 2.0, 0)
	long := strings.Repeat("x", 400)
	msgs := []model.Message{{Role: "system", Content: "s"}, {Role: "user", Content: long}, {Role: "user", Content: "z"}}
	var cools int
	sink := func(kind string, detail map[string]any) {
		if kind == "l2_cooldown_enter" {
			cools++
		}
	}
	_ = r.MaybeSummarize(stdctx.Background(), msgs, sink)
	_ = r.MaybeSummarize(stdctx.Background(), msgs, sink)
	if cools < 1 {
		t.Fatalf("expected cooldown enter, got %d", cools)
	}
	var skips int
	sink2 := func(kind string, detail map[string]any) {
		if kind == "l2_cooldown_skip" {
			skips++
		}
	}
	_ = r.MaybeSummarize(stdctx.Background(), msgs, sink2)
	if skips < 1 {
		t.Fatalf("expected cooldown skip")
	}
}

type errorAux struct{}

func (errorAux) Generate(ctx stdctx.Context, prompt string, opts ...model.Option) (*model.Generation, error) {
	return nil, stdctx.Canceled
}

func (errorAux) Chat(ctx stdctx.Context, messages []model.Message, opts ...model.Option) (*model.Generation, error) {
	return nil, stdctx.Canceled
}

func (errorAux) Embed(ctx stdctx.Context, texts []string, opts ...model.Option) ([]model.Embedding, error) {
	return nil, nil
}
