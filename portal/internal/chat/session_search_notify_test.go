package chat

import (
	"context"
	"testing"
	"time"

	"backend/internal/biz"

	"github.com/sixath/framework/config"
	"github.com/sixath/framework/sessionsearch"
)

type stubSessionRepo struct {
	session *biz.ChatSession
}

func (s *stubSessionRepo) Create(context.Context, string, string, string, string) (*biz.ChatSession, error) {
	return nil, nil
}
func (s *stubSessionRepo) GetByID(context.Context, string) (*biz.ChatSession, error) {
	return s.session, nil
}
func (s *stubSessionRepo) ListByAgent(context.Context, string, string, string, int32, int32, bool) ([]*biz.ChatSession, int, error) {
	return nil, 0, nil
}
func (s *stubSessionRepo) ListAll(context.Context, string, int32, int32, bool) ([]*biz.ChatSession, int, error) {
	return nil, 0, nil
}
func (s *stubSessionRepo) Update(context.Context, string, map[string]any) (*biz.ChatSession, error) {
	return nil, nil
}
func (s *stubSessionRepo) Delete(context.Context, string) error { return nil }
func (s *stubSessionRepo) Touch(context.Context, string) error  { return nil }
func (s *stubSessionRepo) BumpRewindCount(context.Context, string) error {
	return nil
}
func (s *stubSessionRepo) MarkReadonly(context.Context, string) error { return nil }

type stubMessageRepo struct{}

func (stubMessageRepo) Create(context.Context, string, string, string, map[string]any) (*biz.ChatMessage, error) {
	return nil, nil
}
func (stubMessageRepo) ListBySession(context.Context, string, int) ([]*biz.ChatMessage, error) {
	return nil, nil
}
func (stubMessageRepo) LastUserOrAssistantBySessions(context.Context, []string) (map[string]string, error) {
	return nil, nil
}
func (stubMessageRepo) DeleteBySession(context.Context, string) error { return nil }
func (stubMessageRepo) GetByID(context.Context, string) (*biz.ChatMessage, error) {
	return nil, nil
}
func (stubMessageRepo) SoftDeactivateAfter(context.Context, string, time.Time, string) ([]string, error) {
	return nil, nil
}

func TestNotifySessionMessageIndexed_WithDetachedCaller(t *testing.T) {
	dir := t.TempDir()
	prev := DefaultSessionSearchConfig
	DefaultSessionSearchConfig.Enabled = true
	DefaultSessionSearchConfig.StoreDir = dir
	t.Cleanup(func() { DefaultSessionSearchConfig = prev })

	agentID := "agent-notify-test"
	sessionID := "sess-notify-1"
	phrase := "UNIQUE_FTS_PHRASE_NOTIFY_TEST"
	sess := &biz.ChatSession{
		ID:        sessionID,
		AgentID:   agentID,
		UserID:    "bootstrap",
		Title:     "t",
		UpdatedAt: time.Now(),
	}
	uc := biz.NewChatUsecase(&stubSessionRepo{session: sess}, stubMessageRepo{}, nil, nil, nil)
	msg := &biz.ChatMessage{
		ID:        "msg-1",
		SessionID: sessionID,
		Role:      "user",
		Content:   phrase,
		CreatedAt: time.Now(),
	}
	ctx := biz.WithCallerUserID(context.Background(), "bootstrap")
	NotifySessionMessageIndexed(ctx, uc, sessionID, msg)

	cfg := config.Config{SessionSearch: DefaultSessionSearchConfig}
	t.Cleanup(sessionsearch.ResetManagerCacheForTest)
	var hits []sessionsearch.SessionHit
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		mgr, err := sessionsearch.GetSessionSearchManager(cfg, agentID)
		if err != nil || mgr == nil {
			time.Sleep(50 * time.Millisecond)
			continue
		}
		hits, err = mgr.Search(context.Background(), sessionsearch.SearchOpts{
			AgentID: agentID,
			Query:   phrase,
			Limit:   5,
		})
		if err == nil && len(hits) > 0 {
			return
		}
		time.Sleep(50 * time.Millisecond)
	}
	t.Fatalf("expected FTS hit for %q, got %d", phrase, len(hits))
}

func TestNotifySessionMessageIndexed_WithoutCallerSkips(t *testing.T) {
	dir := t.TempDir()
	prev := DefaultSessionSearchConfig
	DefaultSessionSearchConfig.Enabled = true
	DefaultSessionSearchConfig.StoreDir = dir
	t.Cleanup(func() { DefaultSessionSearchConfig = prev })

	agentID := "agent-notify-skip"
	sessionID := "sess-notify-skip"
	sess := &biz.ChatSession{
		ID: sessionID, AgentID: agentID, UserID: "bootstrap", UpdatedAt: time.Now(),
	}
	uc := biz.NewChatUsecase(&stubSessionRepo{session: sess}, stubMessageRepo{}, nil, nil, nil)
	msg := &biz.ChatMessage{
		ID: "msg-skip", SessionID: sessionID, Role: "user",
		Content: "SHOULD_NOT_INDEX", CreatedAt: time.Now(),
	}
	// no caller on ctx → GetSession fails → no index row
	NotifySessionMessageIndexed(context.Background(), uc, sessionID, msg)
	time.Sleep(200 * time.Millisecond)

	cfg := config.Config{SessionSearch: DefaultSessionSearchConfig}
	t.Cleanup(sessionsearch.ResetManagerCacheForTest)
	mgr, err := sessionsearch.GetSessionSearchManager(cfg, agentID)
	if err != nil || mgr == nil {
		return
	}
	hits, _ := mgr.Search(context.Background(), sessionsearch.SearchOpts{
		AgentID: agentID, Query: "SHOULD_NOT_INDEX", Limit: 5,
	})
	if len(hits) != 0 {
		t.Fatalf("expected no hits without caller, got %d", len(hits))
	}
}
