package chat

import (
	"context"
	"errors"
	"os"
	"strings"
	"testing"
	"time"

	agent "github.com/sixath/framework/harness"
	"github.com/sixath/framework/sessionsearch"
)

type fakeTurnTraceStore struct {
	upserts int
	last    *agent.TurnTrace
	err     error
}

func (f *fakeTurnTraceStore) Upsert(ctx context.Context, t *agent.TurnTrace) error {
	f.upserts++
	f.last = t
	return f.err
}

func (f *fakeTurnTraceStore) GetByRequest(ctx context.Context, sessionID, requestID string) (*agent.TurnTrace, error) {
	return nil, nil
}

func (f *fakeTurnTraceStore) ListBySession(ctx context.Context, sessionID string, limit int) ([]agent.TurnTrace, error) {
	return nil, nil
}
func (f *fakeTurnTraceStore) DeactivateAfter(context.Context, string, time.Time) ([]string, error) {
	return nil, nil
}
func (f *fakeTurnTraceStore) ListByAgent(context.Context, string, time.Time, time.Time, int) ([]agent.TurnTrace, error) {
	return nil, nil
}

type fakeSessionSearchManager struct {
	indexed []indexedCall
	err     error
}

type indexedCall struct {
	Sess sessionsearch.SessionMeta
	Doc  sessionsearch.MessageDoc
}

func (f *fakeSessionSearchManager) IndexMessage(ctx context.Context, sess sessionsearch.SessionMeta, msg sessionsearch.MessageDoc) error {
	f.indexed = append(f.indexed, indexedCall{Sess: sess, Doc: msg})
	return f.err
}

func (f *fakeSessionSearchManager) RemoveSession(context.Context, string, string) error { return nil }
func (f *fakeSessionSearchManager) RemoveMessages(context.Context, []string) error      { return nil }
func (f *fakeSessionSearchManager) RemoveTraceProjections(context.Context, string, string) error {
	return nil
}
func (f *fakeSessionSearchManager) Search(context.Context, sessionsearch.SearchOpts) ([]sessionsearch.SessionHit, error) {
	return nil, nil
}
func (f *fakeSessionSearchManager) SearchAnchored(context.Context, sessionsearch.SearchOpts, sessionsearch.AnchorOpts) ([]sessionsearch.AnchoredHit, error) {
	return nil, nil
}
func (f *fakeSessionSearchManager) GetMessagesAround(context.Context, string, string, string, int) ([]sessionsearch.MessageDoc, error) {
	return nil, nil
}
func (f *fakeSessionSearchManager) ListRecent(context.Context, string, string, int) ([]sessionsearch.SessionHit, error) {
	return nil, nil
}
func (f *fakeSessionSearchManager) EnsureSynced(context.Context, string, sessionsearch.SyncSource) error {
	return nil
}

func TestPersistTurnTraceIfEnabled_CallsUpsert(t *testing.T) {
	t.Setenv("SATH_TRACE_PERSIST", "")
	store := &fakeTurnTraceStore{}
	tr := &agent.RunTrace{
		RequestID: "r1",
		ToolCalls: []agent.ToolCallRecord{{ToolCallID: "c1", ToolName: "echo"}},
	}
	PersistTurnTraceIfEnabled(context.Background(), store, agent.TurnTraceMeta{
		SessionID: "s1",
		AgentID:   "a1",
		RequestID: "r1",
	}, tr, nil)
	if store.upserts != 1 {
		t.Fatalf("upserts=%d want 1", store.upserts)
	}
	if store.last == nil || store.last.SessionID != "s1" || store.last.RequestID != "r1" {
		t.Fatalf("unexpected last=%+v", store.last)
	}
}

func TestPersistTurnTraceIfEnabled_UpsertErrorDoesNotPanic(t *testing.T) {
	t.Setenv("SATH_TRACE_PERSIST", "true")
	store := &fakeTurnTraceStore{err: errors.New("db down")}
	mgr := &fakeSessionSearchManager{}
	tr := &agent.RunTrace{RequestID: "r1", ToolCalls: []agent.ToolCallRecord{{ToolCallID: "c1", ToolName: "echo"}}}
	// Helper returns void; must not panic or propagate. Upsert failure skips index.
	PersistTurnTraceIfEnabled(context.Background(), store, agent.TurnTraceMeta{
		SessionID: "s1", AgentID: "a1", RequestID: "r1",
	}, tr, &TurnTraceIndexOpts{Manager: mgr, SessMeta: sessionsearch.SessionMeta{ID: "s1", AgentID: "a1"}})
	if store.upserts != 1 {
		t.Fatalf("upserts=%d want 1", store.upserts)
	}
	if len(mgr.indexed) != 0 {
		t.Fatalf("indexed=%d want 0 after upsert failure", len(mgr.indexed))
	}
}

func TestPersistTurnTraceIfEnabled_DisabledByEnv(t *testing.T) {
	for _, v := range []string{"false", "0"} {
		t.Run(v, func(t *testing.T) {
			t.Setenv("SATH_TRACE_PERSIST", v)
			store := &fakeTurnTraceStore{}
			PersistTurnTraceIfEnabled(context.Background(), store, agent.TurnTraceMeta{
				SessionID: "s", AgentID: "a", RequestID: "r",
			}, &agent.RunTrace{RequestID: "r"}, nil)
			if store.upserts != 0 {
				t.Fatalf("upserts=%d want 0 when SATH_TRACE_PERSIST=%q", store.upserts, v)
			}
		})
	}
}

func TestPersistTurnTraceIfEnabled_NilStoreOrTrace(t *testing.T) {
	_ = os.Unsetenv("SATH_TRACE_PERSIST")
	PersistTurnTraceIfEnabled(context.Background(), nil, agent.TurnTraceMeta{}, &agent.RunTrace{}, nil)
	store := &fakeTurnTraceStore{}
	PersistTurnTraceIfEnabled(context.Background(), store, agent.TurnTraceMeta{}, nil, nil)
	if store.upserts != 0 {
		t.Fatalf("upserts=%d want 0", store.upserts)
	}
}

func TestPersistTurnTraceIfEnabled_IndexesToolProjections(t *testing.T) {
	t.Setenv("SATH_TRACE_PERSIST", "true")
	store := &fakeTurnTraceStore{}
	mgr := &fakeSessionSearchManager{}
	tr := &agent.RunTrace{
		RequestID: "req-1",
		ToolCalls: []agent.ToolCallRecord{
			{ToolCallID: "tc-a", ToolName: "execute_read", Arguments: map[string]any{"q": "x"}, Result: "ok"},
			{ToolCallID: "", ToolName: "http_get", Error: "timeout"},
		},
	}
	meta := agent.TurnTraceMeta{SessionID: "sess-1", AgentID: "agent-1", RequestID: "req-1"}
	opts := &TurnTraceIndexOpts{
		Manager:  mgr,
		SessMeta: sessionsearch.SessionMeta{ID: "sess-1", AgentID: "agent-1", Title: "t", ParentSessionID: "p"},
	}
	PersistTurnTraceIfEnabled(context.Background(), store, meta, tr, opts)
	if store.upserts != 1 {
		t.Fatalf("upserts=%d want 1", store.upserts)
	}
	if len(mgr.indexed) != 2 {
		t.Fatalf("indexed=%d want 2", len(mgr.indexed))
	}
	d0 := mgr.indexed[0].Doc
	if d0.ID != "trace:req-1:tc-a" || d0.Role != "tool" || d0.ToolName != "execute_read" || d0.SessionID != "sess-1" {
		t.Fatalf("doc0=%+v", d0)
	}
	if !strings.Contains(d0.Content, "tool=execute_read") || !strings.Contains(d0.Content, "args=") || !strings.Contains(d0.Content, "result=") {
		t.Fatalf("content0=%q", d0.Content)
	}
	d1 := mgr.indexed[1].Doc
	if d1.ID != "trace:req-1:1" { // empty ToolCallID → index fallback
		t.Fatalf("doc1.id=%q want trace:req-1:1", d1.ID)
	}
	if d1.ToolName != "http_get" || !strings.Contains(d1.Content, "tool=http_get") || !strings.Contains(d1.Content, "err=timeout") {
		t.Fatalf("doc1=%+v", d1)
	}
	if mgr.indexed[0].Sess.Title != "t" || mgr.indexed[0].Sess.ParentSessionID != "p" {
		t.Fatalf("sessMeta=%+v", mgr.indexed[0].Sess)
	}
}

func TestPersistTurnTraceIfEnabled_RePersistSameIDs(t *testing.T) {
	t.Setenv("SATH_TRACE_PERSIST", "true")
	store := &fakeTurnTraceStore{}
	mgr := &fakeSessionSearchManager{}
	tr := &agent.RunTrace{
		RequestID: "req-same",
		ToolCalls: []agent.ToolCallRecord{
			{ToolCallID: "c1", ToolName: "echo"},
			{ToolCallID: "", ToolName: "other"},
		},
	}
	meta := agent.TurnTraceMeta{SessionID: "s", AgentID: "a", RequestID: "req-same"}
	opts := &TurnTraceIndexOpts{
		Manager:  mgr,
		SessMeta: sessionsearch.SessionMeta{ID: "s", AgentID: "a"},
	}
	PersistTurnTraceIfEnabled(context.Background(), store, meta, tr, opts)
	PersistTurnTraceIfEnabled(context.Background(), store, meta, tr, opts)
	if store.upserts != 2 {
		t.Fatalf("upserts=%d want 2", store.upserts)
	}
	if len(mgr.indexed) != 4 {
		t.Fatalf("indexed=%d want 4", len(mgr.indexed))
	}
	want := []string{"trace:req-same:c1", "trace:req-same:1", "trace:req-same:c1", "trace:req-same:1"}
	for i, id := range want {
		if mgr.indexed[i].Doc.ID != id {
			t.Fatalf("indexed[%d].ID=%q want %q", i, mgr.indexed[i].Doc.ID, id)
		}
	}
}

func TestPersistTurnTraceIfEnabled_IndexErrorDoesNotFailPersist(t *testing.T) {
	t.Setenv("SATH_TRACE_PERSIST", "true")
	store := &fakeTurnTraceStore{}
	mgr := &fakeSessionSearchManager{err: errors.New("fts down")}
	tr := &agent.RunTrace{
		RequestID: "r1",
		ToolCalls: []agent.ToolCallRecord{{ToolCallID: "c1", ToolName: "echo"}},
	}
	PersistTurnTraceIfEnabled(context.Background(), store, agent.TurnTraceMeta{
		SessionID: "s1", AgentID: "a1", RequestID: "r1",
	}, tr, &TurnTraceIndexOpts{Manager: mgr, SessMeta: sessionsearch.SessionMeta{ID: "s1", AgentID: "a1"}})
	if store.upserts != 1 {
		t.Fatalf("upserts=%d want 1", store.upserts)
	}
	if len(mgr.indexed) != 1 {
		t.Fatalf("indexed=%d want 1 (attempted despite error return)", len(mgr.indexed))
	}
}

func TestFormatToolFTSContent(t *testing.T) {
	got := formatToolFTSContent(agent.TurnToolCall{
		ToolName:      "kubectl",
		Error:         "boom",
		Arguments:     map[string]any{"cmd": "get"},
		ResultPreview: "pods",
	})
	for _, part := range []string{"tool=kubectl", "err=boom", "args=", "result=pods"} {
		if !strings.Contains(got, part) {
			t.Fatalf("missing %q in %q", part, got)
		}
	}
}

func TestToolProjectionDocID(t *testing.T) {
	if got := toolProjectionDocID("r", "tc", 0); got != "trace:r:tc" {
		t.Fatalf("got %q", got)
	}
	if got := toolProjectionDocID("r", "", 3); got != "trace:r:3" {
		t.Fatalf("empty id fallback got %q", got)
	}
}

func TestRunTraceFromMetadata(t *testing.T) {
	tr := &agent.RunTrace{RequestID: "x"}
	if got := RunTraceFromMetadata(map[string]any{"trace": tr}); got != tr {
		t.Fatalf("got %v want %v", got, tr)
	}
	if RunTraceFromMetadata(nil) != nil {
		t.Fatal("nil md")
	}
	if RunTraceFromMetadata(map[string]any{"trace": "bad"}) != nil {
		t.Fatal("wrong type")
	}
}
