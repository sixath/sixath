package memory

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"
)

type stubExtractor struct {
	facts []CandidateFact
	err   error
}

func (s stubExtractor) Extract(context.Context, TurnInput) ([]CandidateFact, error) {
	return s.facts, s.err
}

func TestPipeline_AddFromTurn_Disabled(t *testing.T) {
	store := NewFacade(FacadeConfig{Session: NewSessionMemory()})
	p := &Pipeline{
		Store: store, Enabled: false, MaxFacts: 5,
		Extractor: stubExtractor{facts: []CandidateFact{{Content: "x", Scope: ScopeSession}}},
	}
	n, err := p.AddFromTurn(context.Background(), TurnInput{
		SessionID: "s1", UserMessage: "hi", AssistantMessage: "yo",
	})
	if err != nil || n != 0 {
		t.Fatalf("n=%d err=%v", n, err)
	}
}

func TestPipeline_AddFromTurn_EmptyMessages(t *testing.T) {
	store := NewFacade(FacadeConfig{Session: NewSessionMemory()})
	p := &Pipeline{
		Store: store, Enabled: true,
		Extractor: stubExtractor{facts: []CandidateFact{{Content: "x", Scope: ScopeSession}}},
	}
	n, err := p.AddFromTurn(context.Background(), TurnInput{SessionID: "s1"})
	if err != nil || n != 0 {
		t.Fatalf("n=%d err=%v", n, err)
	}
}

func TestPipeline_AddFromTurn_WritesSessionAndDropsUserWithoutID(t *testing.T) {
	store := NewFacade(FacadeConfig{Session: NewSessionMemory()})
	p := &Pipeline{
		Store: store, Enabled: true, MaxFacts: 5,
		Extractor: stubExtractor{facts: []CandidateFact{
			{Content: "likes tea", Scope: ScopeSession},
			{Content: "timezone UTC", Scope: ScopeUser},
		}},
	}
	n, err := p.AddFromTurn(context.Background(), TurnInput{
		SessionID: "sess-1", UserMessage: "I like tea", AssistantMessage: "Noted.",
	})
	if err != nil || n != 1 {
		t.Fatalf("n=%d err=%v want 1", n, err)
	}
	hits, err := store.Recall(context.Background(), RecallQuery{
		Scope: ScopeSession, ScopeID: "sess-1", Query: "tea",
	})
	if err != nil || len(hits) != 1 {
		t.Fatalf("hits=%+v err=%v", hits, err)
	}
	if hits[0].Metadata["source"] != "turn_extract" {
		t.Fatalf("metadata=%+v", hits[0].Metadata)
	}
	if hits[0].Metadata["kind"] != "fact" {
		t.Fatalf("kind want fact, metadata=%+v", hits[0].Metadata)
	}
}

func TestPipeline_AddFromTurn_UserScopeWithID(t *testing.T) {
	store := NewFacade(FacadeConfig{Session: NewSessionMemory()})
	p := &Pipeline{
		Store: store, Enabled: true,
		Extractor: stubExtractor{facts: []CandidateFact{{Content: "prefers dark", Scope: ScopeUser}}},
	}
	n, err := p.AddFromTurn(context.Background(), TurnInput{
		UserID: "u1", SessionID: "s1", UserMessage: "dark mode", AssistantMessage: "ok",
	})
	if err != nil || n != 1 {
		t.Fatalf("n=%d err=%v", n, err)
	}
	hits, err := store.Recall(context.Background(), RecallQuery{
		Scope: ScopeUser, ScopeID: "u1", Query: "dark",
	})
	if err != nil || len(hits) != 1 {
		t.Fatalf("hits=%+v err=%v", hits, err)
	}
	if hits[0].Metadata["source_session_id"] != "s1" {
		t.Fatalf("metadata=%+v", hits[0].Metadata)
	}
}

func TestPipeline_AddFromTurn_HashDedupe(t *testing.T) {
	store := NewFacade(FacadeConfig{Session: NewSessionMemory()})
	p := &Pipeline{
		Store: store, Enabled: true,
		Extractor: stubExtractor{facts: []CandidateFact{{Content: "same fact", Scope: ScopeSession}}},
	}
	in := TurnInput{SessionID: "s1", UserMessage: "a", AssistantMessage: "b"}
	n1, err := p.AddFromTurn(context.Background(), in)
	if err != nil || n1 != 1 {
		t.Fatalf("first n=%d err=%v", n1, err)
	}
	n2, err := p.AddFromTurn(context.Background(), in)
	if err != nil || n2 != 0 {
		t.Fatalf("second n=%d err=%v want 0", n2, err)
	}
}

func TestPipeline_AddFromTurn_SemanticIgnoreCountsZero(t *testing.T) {
	stub := &StubSemanticConflictResolver{Decision: ConflictIgnore}
	store := NewFacade(FacadeConfig{
		Session:           NewSessionMemory(),
		SemanticConflicts: stub,
	})
	ctx := context.Background()
	if _, err := store.Remember(ctx, RememberInput{
		Scope: ScopeSession, ScopeID: "s1", Action: ActionAdd, Content: "favorite color is blue",
	}); err != nil {
		t.Fatal(err)
	}
	// Candidate must be a substring of seed so SessionMemory Recall returns peers.
	p := &Pipeline{
		Store: store, Enabled: true,
		Extractor: stubExtractor{facts: []CandidateFact{
			{Content: "favorite color", Scope: ScopeSession},
		}},
	}
	n, err := p.AddFromTurn(ctx, TurnInput{
		SessionID: "s1", UserMessage: "color", AssistantMessage: "ok",
	})
	if err != nil || n != 0 {
		t.Fatalf("n=%d err=%v want 0 (Ignore must not count as write)", n, err)
	}
	if stub.Calls < 1 {
		t.Fatalf("stub.Calls = %d, want resolver invoked via turn_extract", stub.Calls)
	}
}

func TestPipeline_AddFromTurn_DropsLongAndInvalidScope(t *testing.T) {
	store := NewFacade(FacadeConfig{Session: NewSessionMemory()})
	long := strings.Repeat("x", 2049)
	p := &Pipeline{
		Store: store, Enabled: true,
		Extractor: stubExtractor{facts: []CandidateFact{
			{Content: long, Scope: ScopeSession},
			{Content: "agent leak", Scope: ScopeAgent},
			{Content: "  ", Scope: ScopeSession},
			{Content: "ok", Scope: ScopeSession},
		}},
	}
	st, err := p.AddFromTurnWithStats(context.Background(), TurnInput{
		SessionID: "s1", UserMessage: "u", AssistantMessage: "a",
	})
	if err != nil || st.Written != 1 {
		t.Fatalf("written=%d err=%v", st.Written, err)
	}
	if st.Result != ExtractResultSuccess {
		t.Fatalf("result=%s", st.Result)
	}
	if st.Candidates != 4 {
		t.Fatalf("candidates=%d", st.Candidates)
	}
	if st.Drops[DropTooLong] != 1 || st.Drops[DropInvalidScope] != 1 || st.Drops[DropEmpty] != 1 {
		t.Fatalf("drops=%v", st.Drops)
	}
	// Duration may be 0 on very fast paths (Windows timer resolution).
	if st.Duration < 0 {
		t.Fatalf("duration=%v", st.Duration)
	}
}

func TestPipeline_AddFromTurn_ExtractorError(t *testing.T) {
	store := NewFacade(FacadeConfig{Session: NewSessionMemory()})
	want := errors.New("llm boom")
	p := &Pipeline{
		Store: store, Enabled: true,
		Extractor: stubExtractor{err: want},
	}
	st, err := p.AddFromTurnWithStats(context.Background(), TurnInput{
		SessionID: "s1", UserMessage: "u", AssistantMessage: "a",
	})
	if !errors.Is(err, want) {
		t.Fatalf("err=%v want %v", err, want)
	}
	if st.Result != ExtractResultModelFail {
		t.Fatalf("result=%s", st.Result)
	}
}

func TestPipeline_AddFromTurn_ParseFail(t *testing.T) {
	store := NewFacade(FacadeConfig{Session: NewSessionMemory()})
	p := &Pipeline{
		Store: store, Enabled: true,
		Extractor: stubExtractor{err: fmt.Errorf("%w: junk", ErrExtractParse)},
	}
	st, err := p.AddFromTurnWithStats(context.Background(), TurnInput{
		SessionID: "s1", UserMessage: "u", AssistantMessage: "a",
	})
	if !errors.Is(err, ErrExtractParse) {
		t.Fatalf("err=%v", err)
	}
	if st.Result != ExtractResultParseFail || !st.ParseFail {
		t.Fatalf("stats=%+v", st)
	}
}

func TestPipeline_AddFromTurn_HashDedupeStats(t *testing.T) {
	store := NewFacade(FacadeConfig{Session: NewSessionMemory()})
	p := &Pipeline{
		Store: store, Enabled: true,
		Extractor: stubExtractor{facts: []CandidateFact{{Content: "same fact", Scope: ScopeSession}}},
	}
	in := TurnInput{SessionID: "s1", UserMessage: "a", AssistantMessage: "b"}
	if _, err := p.AddFromTurn(context.Background(), in); err != nil {
		t.Fatal(err)
	}
	st, err := p.AddFromTurnWithStats(context.Background(), in)
	if err != nil || st.Written != 0 {
		t.Fatalf("written=%d err=%v", st.Written, err)
	}
	if st.Drops[DropHashDedupe] != 1 {
		t.Fatalf("drops=%v", st.Drops)
	}
}
