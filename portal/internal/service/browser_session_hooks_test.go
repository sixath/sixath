package service

import (
	"context"
	"testing"

	chatv1 "backend/api/chat/v1"
	"backend/internal/biz"
	"backend/internal/chat"

	"github.com/go-kratos/kratos/v2/log"
	"github.com/sixath/framework/tool/browser"
)

func TestDeleteSession_ClosesBrowserBackend(t *testing.T) {
	prevStore := chat.BrowserSessionStore()
	t.Cleanup(func() { chat.SetBrowserSessionStore(prevStore) })

	store := browser.NewSessionStore()
	chat.SetBrowserSessionStore(store)

	fb := browser.NewFakeBackend()
	const sessionID = "browser-sess-end"
	if _, err := store.GetOrCreate(sessionID, func() (browser.Backend, error) {
		return fb, nil
	}); err != nil {
		t.Fatalf("GetOrCreate: %v", err)
	}

	chatUC := biz.NewChatUsecase(stubSessionRepoSucceedDelete{}, nil, nil, nil, nil)
	s := NewChatService(chatUC, nil, nil, nil, nil, log.DefaultLogger)

	reply, err := s.DeleteSession(biz.WithCallerUserID(context.Background(), "user-1"), &chatv1.DeleteSessionRequest{Id: sessionID})
	if err != nil {
		t.Fatalf("DeleteSession: %v", err)
	}
	if reply == nil || reply.GetRet() == nil || reply.GetRet().GetCode() != 0 {
		t.Fatalf("expected ok reply, got %#v", reply)
	}
	if !fb.Closed() {
		t.Fatal("expected Fake backend Closed after DeleteSession")
	}
}
