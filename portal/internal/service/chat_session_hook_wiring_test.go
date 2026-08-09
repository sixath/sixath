package service

import (
	"context"
	"errors"
	"testing"

	chatv1 "backend/api/chat/v1"
	"backend/internal/biz"

	"github.com/go-kratos/kratos/v2/log"
	"github.com/sixath/framework/agent"
)

// stubSessionRepoSucceedDelete is a minimal ChatSessionRepo for DeleteSession wiring tests.
type stubSessionRepoSucceedDelete struct{}

func (stubSessionRepoSucceedDelete) Create(context.Context, string, string, string, string) (*biz.ChatSession, error) {
	return nil, nil
}
func (stubSessionRepoSucceedDelete) GetByID(_ context.Context, id string) (*biz.ChatSession, error) {
	return &biz.ChatSession{ID: id, UserID: "user-1"}, nil
}
func (stubSessionRepoSucceedDelete) ListByAgent(context.Context, string, string, string, int32, int32, bool) ([]*biz.ChatSession, int, error) {
	return nil, 0, nil
}
func (stubSessionRepoSucceedDelete) ListAll(context.Context, string, int32, int32, bool) ([]*biz.ChatSession, int, error) {
	return nil, 0, nil
}
func (stubSessionRepoSucceedDelete) Update(context.Context, string, map[string]any) (*biz.ChatSession, error) {
	return nil, nil
}
func (stubSessionRepoSucceedDelete) Delete(context.Context, string) error { return nil }
func (stubSessionRepoSucceedDelete) Touch(context.Context, string) error  { return nil }
func (stubSessionRepoSucceedDelete) BumpRewindCount(context.Context, string) error {
	return nil
}
func (stubSessionRepoSucceedDelete) MarkReadonly(context.Context, string) error { return nil }

func TestDeleteSession_InvokesChatSessionEndHooks(t *testing.T) {
	chatUC := biz.NewChatUsecase(stubSessionRepoSucceedDelete{}, nil, nil, nil, nil)
	reg := agent.NewChatSessionHookRegistry()
	var gotSessionID string
	reg.Register(agent.ChatSessionHookFunc(func(_ context.Context, sessionID string) error {
		gotSessionID = sessionID
		return nil
	}))

	s := &ChatService{
		chatUC: chatUC,
		log:    log.NewHelper(log.DefaultLogger),
	}
	s.SetChatSessionHooks(reg)

	const wantID = "sess-hook-1"
	reply, err := s.DeleteSession(biz.WithCallerUserID(context.Background(), "user-1"), &chatv1.DeleteSessionRequest{Id: wantID})
	if err != nil {
		t.Fatalf("DeleteSession: %v", err)
	}
	if reply == nil || reply.GetRet() == nil || reply.GetRet().GetCode() != 0 {
		t.Fatalf("expected ok reply, got %#v", reply)
	}
	if gotSessionID != wantID {
		t.Fatalf("hook session id: got %q want %q", gotSessionID, wantID)
	}
}

func TestDeleteSession_HookErrorStillReturnsOK(t *testing.T) {
	chatUC := biz.NewChatUsecase(stubSessionRepoSucceedDelete{}, nil, nil, nil, nil)
	reg := agent.NewChatSessionHookRegistry()
	reg.Register(agent.ChatSessionHookFunc(func(context.Context, string) error {
		return errors.New("hook boom")
	}))

	s := &ChatService{
		chatUC: chatUC,
		log:    log.NewHelper(log.DefaultLogger),
	}
	s.SetChatSessionHooks(reg)

	reply, err := s.DeleteSession(biz.WithCallerUserID(context.Background(), "user-1"), &chatv1.DeleteSessionRequest{Id: "sess-hook-err"})
	if err != nil {
		t.Fatalf("DeleteSession should not fail on hook error: %v", err)
	}
	if reply == nil || reply.GetRet() == nil || reply.GetRet().GetCode() != 0 {
		t.Fatalf("expected Code 0, got %#v", reply)
	}
}
