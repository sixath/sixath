package runtime

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"testing"

	"backend/internal/biz"
)

func TestRuntimeSessions_ResultFile(t *testing.T) {
	root := t.TempDir()
	chat := newFakeChat()
	ctx := biz.WithCallerUserID(context.Background(), "user-1")
	sess, err := chat.CreateSession(ctx, "agent-rf", "t", "")
	if err != nil {
		t.Fatal(err)
	}
	relDir := filepath.Join(root, "tmp", "results", sess.ID)
	if err := os.MkdirAll(relDir, 0o755); err != nil {
		t.Fatal(err)
	}
	name := "mapping.jsonl"
	if err := os.WriteFile(filepath.Join(relDir, name), []byte("{\"line\":\"4103_abc vmid=1, gid=2\"}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	agents := &fakeAgentReader{byID: map[string]*biz.AgentMeta{
		"agent-rf": {ID: "agent-rf", Workspace: root},
	}}
	sessions := &fakeSessions{byID: chat.sessions}
	srv := testRuntimeServer(t, newTestServiceFull(chat, nil, sessions, nil, agents))

	rel := "tmp/results/" + sess.ID + "/" + name
	req := runtimeReq(http.MethodGet, "/runtime/v1/sessions/"+sess.ID+"/result-files?path="+url.QueryEscape(rel), "", "user-1", true)
	rec := httptest.NewRecorder()
	srv.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}
	var body struct {
		Total int              `json:"total"`
		Items []map[string]any `json:"items"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if body.Total != 1 || body.Items[0]["line"] != "4103_abc vmid=1, gid=2" {
		t.Fatalf("body=%+v", body)
	}

	bad := runtimeReq(http.MethodGet, "/runtime/v1/sessions/"+sess.ID+"/result-files?path="+url.QueryEscape("tmp/results/other/x.jsonl"), "", "user-1", true)
	rec2 := httptest.NewRecorder()
	srv.ServeHTTP(rec2, bad)
	if rec2.Code == http.StatusOK {
		t.Fatalf("traversal/other session must fail, status=%d body=%s", rec2.Code, rec2.Body.String())
	}
}
