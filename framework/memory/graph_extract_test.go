package memory

import (
	"context"
	"errors"
	"fmt"
	"testing"

	"github.com/sixath/framework/model"
)

type stubGraphExtractor struct {
	ex  GraphExtract
	err error
}

func (s stubGraphExtractor) Extract(context.Context, TurnInput) (GraphExtract, error) {
	return s.ex, s.err
}

func TestGraphPipeline_Disabled(t *testing.T) {
	g := newFakeGraphStore()
	p := &GraphPipeline{
		Graph: g, Enabled: false,
		Extractor: stubGraphExtractor{ex: GraphExtract{
			Entities: []EntityDraft{{Name: "Alice", Scope: ScopeSession, Confidence: 0.9}},
		}},
	}
	n, err := p.AddGraphFromTurn(context.Background(), TurnInput{SessionID: "s1", UserMessage: "hi"})
	if err != nil || n != 0 {
		t.Fatalf("n=%d err=%v", n, err)
	}
}

func TestGraphPipeline_WritesRelation(t *testing.T) {
	g := newFakeGraphStore()
	p := &GraphPipeline{
		Graph: g, Enabled: true, MinRelationConfidence: 0.7,
		Extractor: stubGraphExtractor{ex: GraphExtract{
			Entities: []EntityDraft{
				{Name: "Alice", Type: "person", Scope: ScopeSession, Confidence: 0.9},
				{Name: "Acme", Type: "org", Scope: ScopeSession, Confidence: 0.9},
			},
			Relations: []RelationDraft{
				{Subject: "Alice", Predicate: "works_at", Object: "Acme", Scope: ScopeSession, Confidence: 0.95},
			},
		}},
	}
	n, err := p.AddGraphFromTurn(context.Background(), TurnInput{
		SessionID: "s1", UserMessage: "Alice works at Acme", AssistantMessage: "Noted.",
	})
	if err != nil {
		t.Fatal(err)
	}
	if n != 1 {
		t.Fatalf("want 1 relation, got %d", n)
	}
	alice := StableEntityID(ScopeSession, "s1", "Alice")
	hits, err := g.Expand(context.Background(), GraphExpandQuery{
		SeedEntityIDs: []string{alice}, Hops: 1, Scope: ScopeSession, ScopeID: "s1", Limit: 5,
	})
	if err != nil || len(hits) != 1 {
		t.Fatalf("expand: %#v err=%v", hits, err)
	}
}

func TestGraphPipeline_DropsLowConfidence(t *testing.T) {
	g := newFakeGraphStore()
	p := &GraphPipeline{
		Graph: g, Enabled: true, MinRelationConfidence: 0.7,
		Extractor: stubGraphExtractor{ex: GraphExtract{
			Entities: []EntityDraft{{Name: "X", Scope: ScopeSession, Confidence: 0.9}},
			Relations: []RelationDraft{
				{Subject: "X", Predicate: "maybe", Object: "Y", Scope: ScopeSession, Confidence: 0.2},
			},
		}},
	}
	st, err := p.AddGraphFromTurnWithStats(context.Background(), TurnInput{SessionID: "s1", UserMessage: "x"})
	if err != nil {
		t.Fatal(err)
	}
	if st.WrittenRels != 0 {
		t.Fatalf("want 0 relations, got %d", st.WrittenRels)
	}
	if st.Result != GraphResultSuccess || st.Drops[GraphDropLowConfidence] < 1 {
		t.Fatalf("stats=%+v", st)
	}
}

func TestGraphPipeline_StatsCountsCandidates(t *testing.T) {
	g := newFakeGraphStore()
	p := &GraphPipeline{
		Graph: g, Enabled: true, MinRelationConfidence: 0.5, MaxEntities: 32,
		Extractor: stubGraphExtractor{ex: GraphExtract{
			Entities: []EntityDraft{
				{Name: "api-gateway", Type: "service", Scope: ScopeSession, Confidence: 0.9},
				{Name: "union-access", Type: "service", Scope: ScopeSession, Confidence: 0.9},
			},
			Relations: []RelationDraft{
				{Subject: "api-gateway", Predicate: "calls", Object: "union-access", Scope: ScopeSession, Confidence: 0.85},
			},
		}},
	}
	st, err := p.AddGraphFromTurnWithStats(context.Background(), TurnInput{
		SessionID: "s1", UserMessage: "topology", AssistantMessage: "ok",
	})
	if err != nil || st.WrittenRels != 1 || st.WrittenEntities < 2 {
		t.Fatalf("stats=%+v err=%v", st, err)
	}
	if st.CandidateEntities != 2 || st.CandidateRels != 1 {
		t.Fatalf("candidates entities=%d rels=%d", st.CandidateEntities, st.CandidateRels)
	}
}

func TestGraphPipeline_DropsUserWithoutID(t *testing.T) {
	g := newFakeGraphStore()
	p := &GraphPipeline{
		Graph: g, Enabled: true,
		Extractor: stubGraphExtractor{ex: GraphExtract{
			Entities: []EntityDraft{{Name: "Alice", Scope: ScopeUser, Confidence: 0.9}},
			Relations: []RelationDraft{
				{Subject: "Alice", Predicate: "likes", Object: "Tea", Scope: ScopeUser, Confidence: 0.9},
			},
		}},
	}
	n, err := p.AddGraphFromTurn(context.Background(), TurnInput{SessionID: "s1", UserMessage: "hi"})
	if err != nil || n != 0 {
		t.Fatalf("n=%d err=%v", n, err)
	}
}

func TestLLMGraphExtractor_ParseJSON(t *testing.T) {
	m := &stubChatModel{text: `{"entities":[{"name":"Alice","type":"person","scope":"session","confidence":0.9}],"relations":[]}`}
	ex := &LLMGraphExtractor{Model: m}
	got, err := ex.Extract(context.Background(), TurnInput{UserMessage: "Alice is here", AssistantMessage: "ok"})
	if err != nil {
		t.Fatal(err)
	}
	if len(got.Entities) != 1 || got.Entities[0].Name != "Alice" {
		t.Fatalf("got %#v", got)
	}
}

func TestLLMGraphExtractor_SalvagesTruncatedJSON(t *testing.T) {
	// Mid-object cut, same shape as max_tokens truncation.
	truncated := `{"entities":[{"name":"union-servo","type":"system","scope":"session","confidence":0.9},{"name":"area-servo","type":"system","scope":"session","confidence":0.9}],"relations":[{"subject":"union-servo","predicate":"calls","object":"area-servo","scope":"session","confidence":0.8},{"subject":"union-servo","predicate":"uses"`
	ex := &LLMGraphExtractor{Model: stubChatModel{text: truncated}}
	got, err := ex.Extract(context.Background(), TurnInput{UserMessage: "register", AssistantMessage: "flow"})
	if err != nil {
		t.Fatal(err)
	}
	if len(got.Entities) != 2 {
		t.Fatalf("entities=%#v", got.Entities)
	}
	if len(got.Relations) != 1 || got.Relations[0].Predicate != "calls" {
		t.Fatalf("relations=%#v", got.Relations)
	}
}

func TestLLMGraphExtractor_ParseFailIsErrGraphExtractParse(t *testing.T) {
	ex := &LLMGraphExtractor{Model: stubChatModel{text: "not-json"}}
	_, err := ex.Extract(context.Background(), TurnInput{UserMessage: "u", AssistantMessage: "a"})
	if !errors.Is(err, ErrGraphExtractParse) {
		t.Fatalf("err=%v", err)
	}
}

func TestGraphPipeline_ParseFailResult(t *testing.T) {
	g := newFakeGraphStore()
	p := &GraphPipeline{
		Graph: g, Enabled: true,
		Extractor: stubGraphExtractor{err: fmt.Errorf("%w: unexpected end of JSON input", ErrGraphExtractParse)},
	}
	st, err := p.AddGraphFromTurnWithStats(context.Background(), TurnInput{SessionID: "s1", UserMessage: "hi"})
	if err == nil || st.Result != GraphResultParseFail {
		t.Fatalf("st=%+v err=%v", st, err)
	}
}

type recordingGraphChatModel struct {
	text string
	cfg  *model.CallConfig
}

func (s *recordingGraphChatModel) Generate(ctx context.Context, prompt string, opts ...model.Option) (*model.Generation, error) {
	return s.Chat(ctx, []model.Message{{Role: "user", Content: prompt}}, opts...)
}
func (s *recordingGraphChatModel) Chat(ctx context.Context, messages []model.Message, opts ...model.Option) (*model.Generation, error) {
	s.cfg = model.ApplyOptions(opts...)
	return &model.Generation{Text: s.text}, nil
}
func (s *recordingGraphChatModel) Embed(ctx context.Context, texts []string, opts ...model.Option) ([]model.Embedding, error) {
	return nil, nil
}

func TestLLMGraphExtractor_RequestsLargerMaxTokens(t *testing.T) {
	m := &recordingGraphChatModel{text: `{"entities":[],"relations":[]}`}
	ex := &LLMGraphExtractor{Model: m}
	if _, err := ex.Extract(context.Background(), TurnInput{UserMessage: "x", AssistantMessage: "y"}); err != nil {
		t.Fatal(err)
	}
	if m.cfg == nil || m.cfg.MaxTokens < maxGraphExtractOutTokens {
		t.Fatalf("max_tokens=%v want >= %d", m.cfg, maxGraphExtractOutTokens)
	}
}
