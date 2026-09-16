package service

import (
	"context"
	"errors"
	"strings"
	"testing"

	"backend/internal/biz"

	kratosErrors "github.com/go-kratos/kratos/v2/errors"
	"github.com/go-kratos/kratos/v2/log"
)

func TestForkToMessage_InvalidArgs(t *testing.T) {
	s := &ChatService{chatUC: biz.NewChatUsecase(&rewindSessRepo{sess: &biz.ChatSession{ID: "s1", UserID: "u1", AgentID: "a1"}}, &rewindMsgRepo{msgs: map[string]*biz.ChatMessage{}}, nil, nil, nil), log: log.NewHelper(log.DefaultLogger)}
	ctx := biz.WithCallerUserID(context.Background(), "u1")
	_, err := s.ForkToMessage(ctx, "", "m1")
	if err == nil {
		t.Fatal("expected invalid argument")
	}
	if ke := kratosErrors.FromError(err); ke.Reason != "INVALID_ARGUMENT" {
		t.Fatalf("reason=%s want INVALID_ARGUMENT err=%v", ke.Reason, err)
	}
}

func TestForkToMessage_WrongSessionMessage(t *testing.T) {
	s := &ChatService{
		chatUC: biz.NewChatUsecase(
			&rewindSessRepo{sess: &biz.ChatSession{ID: "s1", UserID: "u1", AgentID: "a1"}},
			&rewindMsgRepo{msgs: map[string]*biz.ChatMessage{
				"m1": {ID: "m1", SessionID: "other", Role: "user", Content: "x", Active: true},
			}},
			nil, nil, nil,
		),
		log: log.NewHelper(log.DefaultLogger),
	}
	ctx := biz.WithCallerUserID(context.Background(), "u1")
	_, err := s.ForkToMessage(ctx, "s1", "m1")
	if !errors.Is(err, ErrForkMessageNotFound) {
		t.Fatalf("err=%v want ErrForkMessageNotFound", err)
	}
	if ke := kratosErrors.FromError(err); ke.Reason != "FORK_MESSAGE_NOT_FOUND" {
		t.Fatalf("reason=%s want FORK_MESSAGE_NOT_FOUND", ke.Reason)
	}
}

func TestForkToMessage_Inactive(t *testing.T) {
	s := &ChatService{
		chatUC: biz.NewChatUsecase(
			&rewindSessRepo{sess: &biz.ChatSession{ID: "s1", UserID: "u1", AgentID: "a1"}},
			&rewindMsgRepo{msgs: map[string]*biz.ChatMessage{
				"m1": {ID: "m1", SessionID: "s1", Role: "user", Content: "x", Active: false},
			}},
			nil, nil, nil,
		),
		log: log.NewHelper(log.DefaultLogger),
	}
	ctx := biz.WithCallerUserID(context.Background(), "u1")
	_, err := s.ForkToMessage(ctx, "s1", "m1")
	if !errors.Is(err, ErrForkInactive) {
		t.Fatalf("err=%v want ErrForkInactive", err)
	}
	if ke := kratosErrors.FromError(err); ke.Reason != "FORK_INACTIVE" {
		t.Fatalf("reason=%s want FORK_INACTIVE", ke.Reason)
	}
}

func TestForkToMessage_NilDB(t *testing.T) {
	s := &ChatService{
		chatUC: biz.NewChatUsecase(
			&rewindSessRepo{sess: &biz.ChatSession{ID: "s1", UserID: "u1", AgentID: "a1"}},
			&rewindMsgRepo{msgs: map[string]*biz.ChatMessage{
				"m1": {ID: "m1", SessionID: "s1", Role: "user", Content: "hi", Active: true},
			}},
			nil, nil, nil,
		),
		log: log.NewHelper(log.DefaultLogger),
	}
	ctx := biz.WithCallerUserID(context.Background(), "u1")
	_, err := s.ForkToMessage(ctx, "s1", "m1")
	if err == nil || !strings.Contains(err.Error(), "database unavailable") {
		t.Fatalf("err=%v want database unavailable", err)
	}
}
