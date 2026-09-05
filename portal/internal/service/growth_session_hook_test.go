package service

import (
	"context"
	"testing"
	"time"

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

func TestNotifyGrowthAssistantTurn_doesNotCallTrySessionEnd(t *testing.T) {
	repo := &fakeGrowthRepoForService{state: &biz.ChatGrowthState{
		SessionID:              "g2-asst",
		TurnsSinceMemoryReview: 1,
		ToolItersSinceReview:   1,
	}}
	uc := biz.NewGrowthUsecase(repo)
	uc.SetSessionEndMemoryReviewEnabled(true)
	uc.SetSessionEndSkillReviewEnabled(true)

	s := &ChatService{
		growthUC: uc,
		log:      log.NewHelper(log.DefaultLogger),
	}
	s.notifyGrowthAssistantTurn("g2-asst")

	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if repo.state != nil && repo.state.TurnsSinceMemoryReview == 2 {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	if repo.state == nil || repo.state.TurnsSinceMemoryReview != 2 {
		t.Fatalf("expected OnAssistantTurn to bump turns to 2, got %#v", repo.state)
	}
	if repo.state.PendingMemoryReview {
		t.Fatal("assistant path must not call TrySessionEndMemoryReview")
	}
	if repo.state.PendingSkillReview {
		t.Fatal("assistant path must not call TrySessionEndSkillReview")
	}
}
