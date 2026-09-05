package service

import (
	"context"
	"testing"

	chatv1 "backend/api/chat/v1"
	"backend/internal/biz"

	"github.com/go-kratos/kratos/v2/log"
)

func TestDeleteSession_DefaultChatService_noGrowthSessionEndHooks(t *testing.T) {
	repo := &fakeGrowthRepoForService{state: &biz.ChatGrowthState{
		SessionID:              "g2-sess",
		TurnsSinceMemoryReview: 1,
		ToolItersSinceReview:   1,
	}}
	uc := biz.NewGrowthUsecase(repo)
	uc.SetSessionEndMemoryReviewEnabled(true)
	uc.SetSessionEndSkillReviewEnabled(true)

	chatUC := biz.NewChatUsecase(stubSessionRepoSucceedDelete{}, nil, nil, nil, nil)
	s := NewChatService(chatUC, nil, nil, nil, uc, nil, log.DefaultLogger)

	reply, err := s.DeleteSession(biz.WithCallerUserID(context.Background(), "user-1"), &chatv1.DeleteSessionRequest{Id: "g2-sess"})
	if err != nil {
		t.Fatalf("DeleteSession: %v", err)
	}
	if reply == nil || reply.GetRet() == nil || reply.GetRet().GetCode() != 0 {
		t.Fatalf("expected ok reply, got %#v", reply)
	}
	if repo.state.PendingMemoryReview {
		t.Fatal("default ChatService must not register growth session-end hooks")
	}
	if repo.state.PendingSkillReview {
		t.Fatal("default ChatService must not register growth session-end hooks")
	}
}
