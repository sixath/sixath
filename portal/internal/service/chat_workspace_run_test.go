package service

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	chatv1 "backend/api/chat/v1"
	"backend/internal/biz"

	kratosErrors "github.com/go-kratos/kratos/v2/errors"
	"github.com/go-kratos/kratos/v2/log"
)

func TestSendMessage_RejectsWholeRepoWorkspace(t *testing.T) {
	root := t.TempDir()
	ws := filepath.Join(root, "repo")
	if err := os.Mkdir(ws, 0o755); err != nil {
		t.Fatal(err)
	}
	const (
		agentID   = "agent-chat-whole-repo"
		sessionID = "sess-whole-repo"
	)
	sess := &biz.ChatSession{ID: sessionID, AgentID: agentID, UserID: "owner"}
	chatUC := biz.NewChatUsecase(&rewindSessRepo{sess: sess}, nil, nil, nil, nil)
	agentUC := biz.NewAgentUsecase(&hybridAgentRepo{agent: &biz.AgentMeta{
		ID: agentID, Name: "a", Workspace: ws,
	}}, nil, nil, t.TempDir(), log.NewStdLogger(nil))
	s := NewChatService(chatUC, agentUC, nil, nil, nil, nil, log.NewStdLogger(nil))
	s.SetCodeRoots([]string{root})

	_, err := s.SendMessage(biz.WithCallerUserID(context.Background(), "owner"), &chatv1.SendMessageRequest{
		SessionId: sessionID,
		Content:   "hi",
	})
	if err == nil {
		t.Fatal("expected whole-repo send to fail")
	}
	if kratosErrors.FromError(err).Reason != "WORKSPACE_WHOLE_REPO_RETIRED" {
		t.Fatalf("reason = %q, err=%v", kratosErrors.FromError(err).Reason, err)
	}
}

func TestSendMessageStream_RejectsWholeRepoWorkspace(t *testing.T) {
	root := t.TempDir()
	ws := filepath.Join(root, "repo")
	if err := os.Mkdir(ws, 0o755); err != nil {
		t.Fatal(err)
	}
	const (
		agentID   = "agent-stream-whole-repo"
		sessionID = "sess-stream-whole-repo"
	)
	sess := &biz.ChatSession{ID: sessionID, AgentID: agentID, UserID: "owner"}
	chatUC := biz.NewChatUsecase(&rewindSessRepo{sess: sess}, nil, nil, nil, nil)
	agentUC := biz.NewAgentUsecase(&hybridAgentRepo{agent: &biz.AgentMeta{
		ID: agentID, Name: "a", Workspace: ws,
	}}, nil, nil, t.TempDir(), log.NewStdLogger(nil))
	s := NewChatService(chatUC, agentUC, nil, nil, nil, nil, log.NewStdLogger(nil))
	s.SetCodeRoots([]string{root})

	_, _, err := s.SendMessageStream(biz.WithCallerUserID(context.Background(), "owner"), &chatv1.SendMessageRequest{
		SessionId: sessionID,
		Content:   "hi",
	})
	if err == nil {
		t.Fatal("expected whole-repo stream to fail")
	}
	if kratosErrors.FromError(err).Reason != "WORKSPACE_WHOLE_REPO_RETIRED" {
		t.Fatalf("reason = %q, err=%v", kratosErrors.FromError(err).Reason, err)
	}
}
