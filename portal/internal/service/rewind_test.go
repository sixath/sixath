package service

import (
	"context"
	"testing"
	"time"

	"backend/internal/biz"

	"github.com/go-kratos/kratos/v2/log"
	"github.com/sixath/framework/agent"
)

type rewindMsgRepo struct {
	msgs map[string]*biz.ChatMessage
}

func (r *rewindMsgRepo) Create(context.Context, string, string, string, map[string]any) (*biz.ChatMessage, error) {
	return nil, nil
}
func (r *rewindMsgRepo) ListBySession(_ context.Context, sessionID string, _ int) ([]*biz.ChatMessage, error) {
	var out []*biz.ChatMessage
	for _, m := range r.msgs {
		if m.SessionID == sessionID && m.Active {
			out = append(out, m)
		}
	}
	return out, nil
}
func (r *rewindMsgRepo) LastUserOrAssistantBySessions(context.Context, []string) (map[string]string, error) {
	return nil, nil
}
func (r *rewindMsgRepo) DeleteBySession(context.Context, string) error { return nil }
func (r *rewindMsgRepo) GetByID(_ context.Context, id string) (*biz.ChatMessage, error) {
	m, ok := r.msgs[id]
	if !ok {
		return nil, biz.ErrSessionNotFound
	}
	return m, nil
}
func (r *rewindMsgRepo) SoftDeactivateAfter(_ context.Context, sessionID string, after time.Time, include string) ([]string, error) {
	var ids []string
	for id, m := range r.msgs {
		if m.SessionID != sessionID || !m.Active {
			continue
		}
		if m.CreatedAt.After(after) || id == include {
			m.Active = false
			ids = append(ids, id)
		}
	}
	return ids, nil
}

type rewindSessRepo struct {
	sess *biz.ChatSession
}

func (r *rewindSessRepo) Create(context.Context, string, string, string, string) (*biz.ChatSession, error) {
	return nil, nil
}
func (r *rewindSessRepo) GetByID(context.Context, string) (*biz.ChatSession, error) { return r.sess, nil }
func (r *rewindSessRepo) ListByAgent(context.Context, string, string, string, int32, int32, bool) ([]*biz.ChatSession, int, error) {
	return nil, 0, nil
}
func (r *rewindSessRepo) ListAll(context.Context, string, int32, int32, bool) ([]*biz.ChatSession, int, error) {
	return nil, 0, nil
}
func (r *rewindSessRepo) Update(context.Context, string, map[string]any) (*biz.ChatSession, error) {
	return r.sess, nil
}
func (r *rewindSessRepo) Delete(context.Context, string) error { return nil }
func (r *rewindSessRepo) Touch(context.Context, string) error  { return nil }
func (r *rewindSessRepo) BumpRewindCount(context.Context, string) error {
	r.sess.RewindCount++
	return nil
}
func (r *rewindSessRepo) MarkReadonly(context.Context, string) error {
	r.sess.Readonly = true
	return nil
}

type rewindTraceStore struct {
	deactivated []string
}

func (r *rewindTraceStore) Upsert(context.Context, *agent.TurnTrace) error { return nil }
func (r *rewindTraceStore) GetByRequest(context.Context, string, string) (*agent.TurnTrace, error) {
	return nil, nil
}
func (r *rewindTraceStore) ListBySession(context.Context, string, int) ([]agent.TurnTrace, error) {
	return nil, nil
}
func (r *rewindTraceStore) DeactivateAfter(_ context.Context, _ string, _ time.Time) ([]string, error) {
	r.deactivated = []string{"req-late"}
	return r.deactivated, nil
}
func (r *rewindTraceStore) ListByAgent(context.Context, string, time.Time, time.Time, int) ([]agent.TurnTrace, error) {
	return nil, nil
}

func TestRewindToMessage_SoftHidesAndBumps(t *testing.T) {
	base := time.Date(2026, 8, 6, 15, 0, 0, 0, time.UTC)
	sess := &biz.ChatSession{ID: "s1", UserID: "user-1", AgentID: "a1"}
	msgs := map[string]*biz.ChatMessage{
		"m0": {ID: "m0", SessionID: "s1", Role: "user", Content: "early", Active: true, CreatedAt: base},
		"m1": {ID: "m1", SessionID: "s1", Role: "user", Content: "anchor", Active: true, CreatedAt: base.Add(time.Second)},
		"m2": {ID: "m2", SessionID: "s1", Role: "assistant", Content: "late", Active: true, CreatedAt: base.Add(2 * time.Second)},
	}
	sessRepo := &rewindSessRepo{sess: sess}
	msgRepo := &rewindMsgRepo{msgs: msgs}
	uc := biz.NewChatUsecase(sessRepo, msgRepo, nil, nil, nil)
	traceStore := &rewindTraceStore{}
	s := &ChatService{
		chatUC:         uc,
		turnTraceStore: traceStore,
		log:            log.NewHelper(log.DefaultLogger),
	}
	ctx := biz.WithCallerUserID(context.Background(), "user-1")
	out, err := s.RewindToMessage(ctx, "s1", "m1")
	if err != nil {
		t.Fatalf("RewindToMessage: %v", err)
	}
	if out.RewindCount != 1 {
		t.Fatalf("RewindCount=%d", out.RewindCount)
	}
	if len(out.DeactivatedMessages) < 2 {
		t.Fatalf("deactivated msgs=%v", out.DeactivatedMessages)
	}
	if len(out.DeactivatedTraceReqs) != 1 || out.DeactivatedTraceReqs[0] != "req-late" {
		t.Fatalf("traces=%v", out.DeactivatedTraceReqs)
	}
	list, _ := msgRepo.ListBySession(ctx, "s1", 100)
	if len(list) != 1 || list[0].ID != "m0" {
		t.Fatalf("list after rewind=%+v", list)
	}
}
