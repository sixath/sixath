package memory

import (
	"context"
	"errors"
	"strings"
	"testing"
)

type fakePrefetchStore struct {
	userHits    []MemoryHit
	sessionHits []MemoryHit
	agentHits   []MemoryHit
	recallErr   error
	calls       []RecallQuery
}

func (f *fakePrefetchStore) Remember(context.Context, RememberInput) (MemoryHit, error) {
	return MemoryHit{}, ErrNotSupported
}

func (f *fakePrefetchStore) Recall(_ context.Context, q RecallQuery) ([]MemoryHit, error) {
	if f.recallErr != nil {
		return nil, f.recallErr
	}
	f.calls = append(f.calls, q)
	switch q.Scope {
	case ScopeUser:
		return f.userHits, nil
	case ScopeSession:
		return f.sessionHits, nil
	case ScopeAgent:
		return f.agentHits, nil
	default:
		return nil, nil
	}
}

func (f *fakePrefetchStore) Get(context.Context, GetRef) (MemoryHit, error) {
	return MemoryHit{}, ErrNotSupported
}

func (f *fakePrefetchStore) List(context.Context, ListFilter) ([]MemoryHit, error) {
	return nil, ErrNotSupported
}

func (f *fakePrefetchStore) Delete(context.Context, GetRef) error {
	return ErrNotSupported
}

func TestStorePrefetchBackend_Name(t *testing.T) {
	b := &StorePrefetchBackend{}
	if got := b.Name(); got != "memory_store_prefetch" {
		t.Fatalf("Name() = %q, want memory_store_prefetch", got)
	}
}

func TestStorePrefetchBackend_Prefetch_UserThenSessionThenAgent(t *testing.T) {
	store := &fakePrefetchStore{
		userHits:    []MemoryHit{{Content: "user pref"}},
		sessionHits: []MemoryHit{{Content: "session fact"}},
		agentHits:   []MemoryHit{{Content: "agent note"}},
	}
	b := &StorePrefetchBackend{Store: store, MaxSnippets: 3}

	parts, err := b.Prefetch(context.Background(), PrefetchQuery{
		UserID:        "u1",
		SessionID:     "s1",
		AgentID:       "a1",
		WorkspaceRoot: "/ws",
		UserMessage:   "q",
	})
	if err != nil {
		t.Fatalf("Prefetch() error = %v", err)
	}
	if len(parts) != 3 {
		t.Fatalf("Prefetch() len(parts) = %d, want 3", len(parts))
	}
	if parts[0].Label != "user" || parts[0].Content != "user pref" {
		t.Fatalf("parts[0] = %+v, want user/user pref", parts[0])
	}
	if parts[1].Label != "session" || parts[1].Content != "session fact" {
		t.Fatalf("parts[1] = %+v, want session/session fact", parts[1])
	}
	if parts[2].Label != "agent" || parts[2].Content != "agent note" {
		t.Fatalf("parts[2] = %+v, want agent/agent note", parts[2])
	}
	if len(store.calls) != 3 {
		t.Fatalf("Recall calls = %d, want 3", len(store.calls))
	}
	if store.calls[0].Scope != ScopeUser || store.calls[0].Source != SourceUnits ||
		store.calls[0].ScopeID != "u1" || store.calls[0].AgentID != "a1" ||
		store.calls[0].Query != "q" || store.calls[0].Limit != 3 {
		t.Fatalf("user RecallQuery = %+v", store.calls[0])
	}
	if store.calls[1].Scope != ScopeSession || store.calls[1].Source != SourceUnits ||
		store.calls[1].ScopeID != "s1" || store.calls[1].AgentID != "a1" ||
		store.calls[1].Query != "q" || store.calls[1].Limit != 3 {
		t.Fatalf("session RecallQuery = %+v", store.calls[1])
	}
	if store.calls[2].Scope != ScopeAgent || store.calls[2].Source != SourceFiles ||
		store.calls[2].AgentID != "a1" || store.calls[2].WorkspaceRoot != "/ws" ||
		store.calls[2].Query != "q" || store.calls[2].Limit != 3 {
		t.Fatalf("agent RecallQuery = %+v", store.calls[2])
	}
}

func TestStorePrefetchBackend_Prefetch_SkipsUserWhenNoUserID(t *testing.T) {
	store := &fakePrefetchStore{
		sessionHits: []MemoryHit{{Content: "session fact"}},
		agentHits:   []MemoryHit{{Content: "agent note"}},
	}
	b := &StorePrefetchBackend{Store: store, MaxSnippets: 3}

	parts, err := b.Prefetch(context.Background(), PrefetchQuery{
		SessionID:     "s1",
		AgentID:       "a1",
		WorkspaceRoot: "/ws",
		UserMessage:   "q",
	})
	if err != nil {
		t.Fatalf("Prefetch() error = %v", err)
	}
	if len(parts) != 2 {
		t.Fatalf("Prefetch() len(parts) = %d, want 2", len(parts))
	}
	if len(store.calls) != 2 {
		t.Fatalf("Recall calls = %d, want 2", len(store.calls))
	}
	for _, q := range store.calls {
		if q.Scope == ScopeUser {
			t.Fatalf("unexpected ScopeUser RecallQuery = %+v", q)
		}
	}
}

func TestStorePrefetchBackend_Prefetch_MergesSessionAndAgent(t *testing.T) {
	store := &fakePrefetchStore{
		sessionHits: []MemoryHit{{Content: "session fact"}},
		agentHits:   []MemoryHit{{Content: "agent note", Path: "MEMORY.md"}},
	}
	b := &StorePrefetchBackend{Store: store, MaxSnippets: 3}

	parts, err := b.Prefetch(context.Background(), PrefetchQuery{
		SessionID:     "sess-1",
		AgentID:       "agent-1",
		WorkspaceRoot: "/ws",
		UserMessage:   "deploy",
	})
	if err != nil {
		t.Fatalf("Prefetch() error = %v", err)
	}
	if len(parts) != 2 {
		t.Fatalf("Prefetch() len(parts) = %d, want 2", len(parts))
	}
	if parts[0].Label != "session" || parts[0].Content != "session fact" {
		t.Fatalf("parts[0] = %+v, want session/session fact", parts[0])
	}
	if parts[1].Label != "agent" || parts[1].Content != "agent note" {
		t.Fatalf("parts[1] = %+v, want agent/agent note", parts[1])
	}
	if len(store.calls) != 2 {
		t.Fatalf("Recall calls = %d, want 2", len(store.calls))
	}
	if store.calls[0].Scope != ScopeSession || store.calls[0].Source != SourceUnits ||
		store.calls[0].ScopeID != "sess-1" || store.calls[0].AgentID != "agent-1" ||
		store.calls[0].Query != "deploy" || store.calls[0].Limit != 3 {
		t.Fatalf("session RecallQuery = %+v", store.calls[0])
	}
	if store.calls[1].Scope != ScopeAgent || store.calls[1].Source != SourceFiles ||
		store.calls[1].AgentID != "agent-1" || store.calls[1].WorkspaceRoot != "/ws" ||
		store.calls[1].Query != "deploy" || store.calls[1].Limit != 3 {
		t.Fatalf("agent RecallQuery = %+v", store.calls[1])
	}
}

func TestStorePrefetchBackend_Prefetch_ProceduralBindings(t *testing.T) {
	store := &fakePrefetchStore{}
	b := &StorePrefetchBackend{
		Store: store,
		ProceduralBindings: []ProceduralBinding{{
			TriggerQuery: "转人工",
			ActionKind:   BindingActionSkill,
			SkillID:      "escalation",
			Mode:         BindingModeSuggest,
		}},
	}
	parts, err := b.Prefetch(context.Background(), PrefetchQuery{
		SessionID:   "s1",
		AgentID:     "zone-4100-agent",
		UserMessage: "请转人工",
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(parts) != 1 || parts[0].Label != "procedural" {
		t.Fatalf("parts=%+v", parts)
	}
	if !strings.Contains(parts[0].Content, "escalation") {
		t.Fatalf("content=%s", parts[0].Content)
	}
}

func TestStorePrefetchBackend_Prefetch_DefaultLimit(t *testing.T) {
	store := &fakePrefetchStore{}
	b := &StorePrefetchBackend{Store: store}

	_, err := b.Prefetch(context.Background(), PrefetchQuery{
		SessionID:   "sess-1",
		AgentID:     "agent-1",
		UserMessage: "q",
	})
	if err != nil {
		t.Fatalf("Prefetch() error = %v", err)
	}
	for _, q := range store.calls {
		if q.Limit != 5 {
			t.Fatalf("RecallQuery Limit = %d, want default 5", q.Limit)
		}
	}
}

func TestStorePrefetchBackend_Prefetch_StoreErrorPropagates(t *testing.T) {
	wantErr := errors.New("recall failed")
	store := &fakePrefetchStore{recallErr: wantErr}
	b := &StorePrefetchBackend{Store: store}

	_, err := b.Prefetch(context.Background(), PrefetchQuery{UserMessage: "q"})
	if !errors.Is(err, wantErr) {
		t.Fatalf("Prefetch() error = %v, want %v", err, wantErr)
	}
}

func TestStorePrefetchBackend_Prefetch_EmptyUserMessage(t *testing.T) {
	store := &fakePrefetchStore{
		sessionHits: []MemoryHit{{Content: "should not appear"}},
	}
	b := &StorePrefetchBackend{Store: store}

	parts, err := b.Prefetch(context.Background(), PrefetchQuery{UserMessage: ""})
	if err != nil {
		t.Fatalf("Prefetch() error = %v, want nil", err)
	}
	if parts != nil {
		t.Fatalf("Prefetch() parts = %v, want nil", parts)
	}
	if len(store.calls) != 0 {
		t.Fatalf("Recall calls = %d, want 0 for empty user message", len(store.calls))
	}
}

func TestStorePrefetchBackend_Prefetch_SkipsEmptyContent(t *testing.T) {
	store := &fakePrefetchStore{
		sessionHits: []MemoryHit{{Content: "  "}, {Content: "ok"}},
		agentHits:   []MemoryHit{{Content: ""}},
	}
	b := &StorePrefetchBackend{Store: store}

	parts, err := b.Prefetch(context.Background(), PrefetchQuery{UserMessage: "q"})
	if err != nil {
		t.Fatalf("Prefetch() error = %v", err)
	}
	if len(parts) != 1 || parts[0].Content != "ok" {
		t.Fatalf("Prefetch() parts = %+v, want single non-empty part", parts)
	}
}

func TestStorePrefetchBackend_Prefetch_SkipsDraftUnits(t *testing.T) {
	store := &fakePrefetchStore{
		userHits: []MemoryHit{
			{Content: "draft-secret", Metadata: map[string]any{"status": "active", "hub_status": "draft"}},
			{Content: "user-active", Metadata: map[string]any{"status": "active"}},
		},
		sessionHits: []MemoryHit{
			{Content: "session-draft", Metadata: map[string]any{"hub_status": "draft"}},
			{Content: "session-active"},
		},
		agentHits: []MemoryHit{{Content: "agent note"}},
	}
	b := &StorePrefetchBackend{Store: store, MaxSnippets: 5}

	parts, err := b.Prefetch(context.Background(), PrefetchQuery{
		UserID: "u1", SessionID: "s1", AgentID: "a1", WorkspaceRoot: "/ws", UserMessage: "q",
	})
	if err != nil {
		t.Fatalf("Prefetch() error = %v", err)
	}
	got := make([]string, 0, len(parts))
	for _, p := range parts {
		got = append(got, p.Label+":"+p.Content)
	}
	want := []string{"user:user-active", "session:session-active", "agent:agent note"}
	if len(got) != len(want) {
		t.Fatalf("parts=%v want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("parts[%d]=%q want %q (all=%v)", i, got[i], want[i], got)
		}
	}
}

func TestStorePrefetchBackend_Prefetch_DedupeAcrossScopes(t *testing.T) {
	dup := "same fact everywhere"
	store := &fakePrefetchStore{
		userHits:    []MemoryHit{{Content: dup}},
		sessionHits: []MemoryHit{{Content: "  " + dup + "  "}, {Content: "session only"}},
		agentHits:   []MemoryHit{{Content: dup}, {Content: "agent only"}},
	}
	maxTotal := 10
	b := &StorePrefetchBackend{Store: store, MaxSnippets: 5, MaxTotal: &maxTotal}

	parts, err := b.Prefetch(context.Background(), PrefetchQuery{
		UserID: "u1", SessionID: "s1", AgentID: "a1", WorkspaceRoot: "/ws", UserMessage: "q",
	})
	if err != nil {
		t.Fatalf("Prefetch() error = %v", err)
	}
	if len(parts) != 3 {
		t.Fatalf("len(parts)=%d want 3 (deduped), got %+v", len(parts), parts)
	}
	if parts[0].Label != "user" || parts[0].Content != dup {
		t.Fatalf("parts[0]=%+v want user/same fact", parts[0])
	}
	if parts[1].Label != "session" || parts[1].Content != "session only" {
		t.Fatalf("parts[1]=%+v", parts[1])
	}
	if parts[2].Label != "agent" || parts[2].Content != "agent only" {
		t.Fatalf("parts[2]=%+v", parts[2])
	}
}

func TestStorePrefetchBackend_Prefetch_MaxTotalTruncates(t *testing.T) {
	store := &fakePrefetchStore{
		userHits:    []MemoryHit{{Content: "u1"}, {Content: "u2"}},
		sessionHits: []MemoryHit{{Content: "s1"}, {Content: "s2"}},
		agentHits:   []MemoryHit{{Content: "a1"}},
	}
	maxTotal := 3
	b := &StorePrefetchBackend{Store: store, MaxSnippets: 5, MaxTotal: &maxTotal}

	parts, err := b.Prefetch(context.Background(), PrefetchQuery{
		UserID: "u1", SessionID: "s1", AgentID: "a1", WorkspaceRoot: "/ws", UserMessage: "q",
	})
	if err != nil {
		t.Fatalf("Prefetch() error = %v", err)
	}
	if len(parts) != 3 {
		t.Fatalf("len=%d want 3, %+v", len(parts), parts)
	}
	if parts[0].Content != "u1" || parts[1].Content != "u2" || parts[2].Content != "s1" {
		t.Fatalf("order/truncation wrong: %+v", parts)
	}
}

func TestStorePrefetchBackend_Prefetch_MaxTotalZeroNoTruncate(t *testing.T) {
	store := &fakePrefetchStore{
		userHits:    []MemoryHit{{Content: "u1"}, {Content: "u2"}},
		sessionHits: []MemoryHit{{Content: "s1"}, {Content: "s2"}},
		agentHits:   []MemoryHit{{Content: "a1"}},
	}
	maxTotal := 0
	b := &StorePrefetchBackend{Store: store, MaxSnippets: 5, MaxTotal: &maxTotal}

	parts, err := b.Prefetch(context.Background(), PrefetchQuery{
		UserID: "u1", SessionID: "s1", AgentID: "a1", WorkspaceRoot: "/ws", UserMessage: "q",
	})
	if err != nil {
		t.Fatalf("Prefetch() error = %v", err)
	}
	if len(parts) != 5 {
		t.Fatalf("len=%d want 5 (no truncate), %+v", len(parts), parts)
	}
}

func TestStorePrefetchBackend_Prefetch_DefaultMaxTotal(t *testing.T) {
	store := &fakePrefetchStore{
		userHits: []MemoryHit{
			{Content: "ua"}, {Content: "ub"}, {Content: "uc"}, {Content: "ud"},
		},
		sessionHits: []MemoryHit{
			{Content: "s0"}, {Content: "s1"}, {Content: "s2"}, {Content: "s3"},
		},
		agentHits: []MemoryHit{
			{Content: "a0"}, {Content: "a1"}, {Content: "a2"}, {Content: "a3"},
		},
	}
	// MaxTotal nil → default 8
	b := &StorePrefetchBackend{Store: store, MaxSnippets: 5}

	parts, err := b.Prefetch(context.Background(), PrefetchQuery{
		UserID: "u1", SessionID: "s1", AgentID: "a1", WorkspaceRoot: "/ws", UserMessage: "q",
	})
	if err != nil {
		t.Fatalf("Prefetch() error = %v", err)
	}
	if len(parts) != 8 {
		t.Fatalf("default max_total: len=%d want 8, %+v", len(parts), parts)
	}
}
