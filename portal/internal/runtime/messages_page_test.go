package runtime

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"backend/internal/biz"
)

func seedMessages(t *testing.T, chat *fakeChat, sessionID string, n int) {
	t.Helper()
	for i := 0; i < n; i++ {
		chat.messages[sessionID] = append(chat.messages[sessionID], &biz.ChatMessage{
			ID:        fmt.Sprintf("msg-%02d", i),
			SessionID: sessionID,
			Role:      "user",
			Content:   fmt.Sprintf("c%02d", i),
			CreatedAt: time.Now().UTC(),
		})
	}
}

func TestRuntimeSessions_MessagesPageLimitAndCursor(t *testing.T) {
	chat := newFakeChat()
	sess, err := chat.CreateSession(biz.WithCallerUserID(context.Background(), "user-1"), "agent-p", "t", "")
	if err != nil {
		t.Fatal(err)
	}
	seedMessages(t, chat, sess.ID, 5)
	srv := testRuntimeServer(t, newTestService(chat, nil, nil))

	req := runtimeReq(http.MethodGet, "/runtime/v1/sessions/"+sess.ID+"/messages?limit=2", "", "user-1", true)
	rec := httptest.NewRecorder()
	srv.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}
	var body struct {
		Items []struct {
			ID string `json:"id"`
		} `json:"items"`
		NextCursor string `json:"next_cursor"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode: %v body=%s", err, rec.Body.String())
	}
	if len(body.Items) != 2 {
		t.Fatalf("items=%d want 2 (limit honoured)", len(body.Items))
	}
	if body.NextCursor == "" {
		t.Fatal("expected next_cursor when more messages exist")
	}
}

func TestRuntimeSessions_MessagesPageAcceptsValidCursor(t *testing.T) {
	chat := newFakeChat()
	sess, err := chat.CreateSession(biz.WithCallerUserID(context.Background(), "user-1"), "agent-p2", "t", "")
	if err != nil {
		t.Fatal(err)
	}
	seedMessages(t, chat, sess.ID, 3)
	srv := testRuntimeServer(t, newTestService(chat, nil, nil))

	cursor := biz.MessageCursor{CreatedAt: time.Now().UTC(), ID: "msg-01"}.Encode()
	req := runtimeReq(http.MethodGet,
		"/runtime/v1/sessions/"+sess.ID+"/messages?limit=1&before="+cursor, "", "user-1", true)
	rec := httptest.NewRecorder()
	srv.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}
}

func TestRuntimeSessions_MessagesPageRejectsInvalidCursor(t *testing.T) {
	chat := newFakeChat()
	sess, err := chat.CreateSession(biz.WithCallerUserID(context.Background(), "user-1"), "agent-p3", "t", "")
	if err != nil {
		t.Fatal(err)
	}
	srv := testRuntimeServer(t, newTestService(chat, nil, nil))

	req := runtimeReq(http.MethodGet,
		"/runtime/v1/sessions/"+sess.ID+"/messages?before=!!!not-a-cursor!!!", "", "user-1", true)
	rec := httptest.NewRecorder()
	srv.ServeHTTP(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status=%d want 400; body=%s", rec.Code, rec.Body.String())
	}
}

// 旧客户端只读 items：分页改造不得改变 items 的 JSON 形状。
func TestRuntimeSessions_MessagesShapeStaysBackwardCompatible(t *testing.T) {
	chat := newFakeChat()
	sess, err := chat.CreateSession(biz.WithCallerUserID(context.Background(), "user-1"), "agent-p4", "t", "")
	if err != nil {
		t.Fatal(err)
	}
	seedMessages(t, chat, sess.ID, 1)
	srv := testRuntimeServer(t, newTestService(chat, nil, nil))

	req := runtimeReq(http.MethodGet, "/runtime/v1/sessions/"+sess.ID+"/messages", "", "user-1", true)
	rec := httptest.NewRecorder()
	srv.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}
	var body map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if _, ok := body["items"].([]any); !ok {
		t.Fatalf("items must stay a JSON array: %#v", body["items"])
	}
	if _, ok := body["ret"].(map[string]any); !ok {
		t.Fatalf("ret must stay a JSON object: %#v", body["ret"])
	}
	if _, ok := body["next_cursor"]; ok {
		t.Fatal("next_cursor should be omitted when there is nothing earlier (omitempty)")
	}
}
