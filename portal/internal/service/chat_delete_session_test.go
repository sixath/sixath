package service

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	chatv1 "backend/api/chat/v1"
	"backend/internal/biz"

	"github.com/go-kratos/kratos/v2/log"
)

type deleteSessionSessRepo struct {
	sess *biz.ChatSession
}

func (r *deleteSessionSessRepo) Create(context.Context, string, string, string, string) (*biz.ChatSession, error) {
	return nil, nil
}
func (r *deleteSessionSessRepo) GetByID(context.Context, string) (*biz.ChatSession, error) {
	if r.sess == nil {
		return nil, biz.ErrSessionNotFound
	}
	cp := *r.sess
	return &cp, nil
}
func (r *deleteSessionSessRepo) ListByAgent(context.Context, string, string, string, int32, int32, bool) ([]*biz.ChatSession, int, error) {
	return nil, 0, nil
}
func (r *deleteSessionSessRepo) ListAll(context.Context, string, int32, int32, bool) ([]*biz.ChatSession, int, error) {
	return nil, 0, nil
}
func (r *deleteSessionSessRepo) Update(context.Context, string, map[string]any) (*biz.ChatSession, error) {
	return nil, nil
}
func (r *deleteSessionSessRepo) Delete(_ context.Context, id string) error {
	if r.sess == nil || r.sess.ID != id {
		return biz.ErrSessionNotFound
	}
	r.sess = nil
	return nil
}
func (r *deleteSessionSessRepo) Touch(context.Context, string) error { return nil }
func (r *deleteSessionSessRepo) BumpRewindCount(context.Context, string) error {
	return nil
}
func (r *deleteSessionSessRepo) MarkReadonly(context.Context, string) error { return nil }
func (r *deleteSessionSessRepo) SetModelOverride(context.Context, string, string, string) error {
	return nil
}

func TestDeleteSession_RemovesSessionUploadsDir(t *testing.T) {
	const (
		sessionID = "sess-svc-del"
		agentID   = "agent-del"
		userID    = "user-del"
	)
	ws := t.TempDir()
	uploads := filepath.Join(ws, "sessions", sessionID, "uploads")
	if err := os.MkdirAll(uploads, 0o755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	if err := os.WriteFile(filepath.Join(uploads, "att-1_shot.png"), []byte("png"), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	sessRepo := &deleteSessionSessRepo{
		sess: &biz.ChatSession{ID: sessionID, AgentID: agentID, UserID: userID},
	}
	chatUC := biz.NewChatUsecase(sessRepo, nil, &hybridAgentRepo{
		agent: &biz.AgentMeta{ID: agentID, Workspace: ws},
	}, nil, nil, nil)
	s := NewChatService(chatUC, biz.NewAgentUsecase(&hybridAgentRepo{
		agent: &biz.AgentMeta{ID: agentID, Workspace: ws},
	}, nil, nil, nil, t.TempDir(), log.NewStdLogger(nil)), nil, nil, nil, log.NewStdLogger(nil))

	ctx := biz.WithCallerUserID(context.Background(), userID)
	reply, err := s.DeleteSession(ctx, &chatv1.DeleteSessionRequest{Id: sessionID})
	if err != nil {
		t.Fatalf("DeleteSession: %v", err)
	}
	if reply == nil || reply.GetRet() == nil || reply.GetRet().GetCode() != 0 {
		t.Fatalf("expected ok reply, got %#v", reply)
	}
	if sessRepo.sess != nil {
		t.Fatal("expected session deleted in repo")
	}
	if _, err := os.Stat(filepath.Join(ws, "sessions", sessionID)); !os.IsNotExist(err) {
		t.Fatalf("session uploads dir should be removed: stat err=%v", err)
	}
}
