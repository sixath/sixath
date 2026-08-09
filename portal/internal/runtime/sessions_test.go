package runtime

import (
	"context"
	"encoding/json"
	stderrors "errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"backend/internal/biz"
	pkgErrors "backend/internal/pkg/errors"
	"backend/internal/service"

	kratosErrors "github.com/go-kratos/kratos/v2/errors"
	khttp "github.com/go-kratos/kratos/v2/transport/http"
)

const testRuntimeToken = "dev-runtime-token"

type fakeChat struct {
	sessions map[string]*biz.ChatSession
	messages map[string][]*biz.ChatMessage
	byAgent  map[string][]*biz.ChatSession
}

func newFakeChat() *fakeChat {
	return &fakeChat{
		sessions: map[string]*biz.ChatSession{},
		messages: map[string][]*biz.ChatMessage{},
		byAgent:  map[string][]*biz.ChatSession{},
	}
}

func (f *fakeChat) CreateSession(ctx context.Context, agentID, title, parentSessionID string) (*biz.ChatSession, error) {
	caller, ok := biz.CallerUserID(ctx)
	if !ok || caller == "" {
		return nil, kratosErrors.Unauthorized("UNAUTHORIZED", "caller identity is required")
	}
	if title == "" {
		title = "新对话"
	}
	now := time.Now().UTC()
	s := &biz.ChatSession{
		ID:              "sess-" + agentID,
		AgentID:         agentID,
		UserID:          caller,
		ParentSessionID: parentSessionID,
		Title:           title,
		CreatedAt:       now,
		UpdatedAt:       now,
	}
	f.sessions[s.ID] = s
	f.byAgent[agentID] = append(f.byAgent[agentID], s)
	return s, nil
}

func (f *fakeChat) GetSession(ctx context.Context, id string) (*biz.ChatSession, error) {
	caller, ok := biz.CallerUserID(ctx)
	if !ok || caller == "" {
		return nil, kratosErrors.Unauthorized("UNAUTHORIZED", "caller identity is required")
	}
	s, ok := f.sessions[id]
	if !ok || s.UserID != caller {
		return nil, biz.ErrSessionNotFound
	}
	return s, nil
}

func (f *fakeChat) ListSessions(ctx context.Context, agentID string, _ string, _, _ int32, _ bool) ([]*biz.ChatSession, int, error) {
	caller, ok := biz.CallerUserID(ctx)
	if !ok || caller == "" {
		return nil, 0, kratosErrors.Unauthorized("UNAUTHORIZED", "caller identity is required")
	}
	var out []*biz.ChatSession
	for _, s := range f.byAgent[agentID] {
		if s.UserID == caller {
			out = append(out, s)
		}
	}
	return out, len(out), nil
}

func (f *fakeChat) ListAllSessions(ctx context.Context, _, _ int32, _ bool) ([]*biz.ChatSession, int, error) {
	caller, ok := biz.CallerUserID(ctx)
	if !ok || caller == "" {
		return nil, 0, kratosErrors.Unauthorized("UNAUTHORIZED", "caller identity is required")
	}
	var out []*biz.ChatSession
	for _, s := range f.sessions {
		if s.UserID == caller {
			out = append(out, s)
		}
	}
	return out, len(out), nil
}

func (f *fakeChat) UpdateSession(ctx context.Context, id string, title string) (*biz.ChatSession, error) {
	s, err := f.GetSession(ctx, id)
	if err != nil {
		return nil, err
	}
	s.Title = title
	s.UpdatedAt = time.Now().UTC()
	return s, nil
}

func (f *fakeChat) DeleteSession(ctx context.Context, id string) error {
	if _, err := f.GetSession(ctx, id); err != nil {
		return err
	}
	delete(f.sessions, id)
	return nil
}

func (f *fakeChat) ListMessages(ctx context.Context, sessionID string, _ int) ([]*biz.ChatMessage, error) {
	if _, err := f.GetSession(ctx, sessionID); err != nil {
		return nil, err
	}
	return f.messages[sessionID], nil
}

func (f *fakeChat) SearchSessions(ctx context.Context, _, _ string, _ int) ([]biz.SearchHit, string, error) {
	if _, ok := biz.CallerUserID(ctx); !ok {
		return nil, "", kratosErrors.Unauthorized("UNAUTHORIZED", "caller identity is required")
	}
	return nil, "ok", nil
}

type fakeSessions struct {
	byID map[string]*biz.ChatSession
	err  error
}

func (f *fakeSessions) GetByID(_ context.Context, id string) (*biz.ChatSession, error) {
	if f.err != nil {
		return nil, f.err
	}
	s, ok := f.byID[id]
	if !ok {
		return nil, pkgErrors.ErrNotFound
	}
	return s, nil
}

type fakePeer struct {
	lastChannel, lastPeer, lastAgent string
	result                           *biz.ChannelPeerResolveResult
	err                              error
}

func (f *fakePeer) Resolve(_ context.Context, channelID, peerID, agentID string) (*biz.ChannelPeerResolveResult, error) {
	f.lastChannel, f.lastPeer, f.lastAgent = channelID, peerID, agentID
	if f.err != nil {
		return nil, f.err
	}
	if f.result != nil {
		return f.result, nil
	}
	return &biz.ChannelPeerResolveResult{
		SessionID: "resolved-sess",
		AgentID:   agentID,
		Created:   true,
	}, nil
}

type fakeRewind struct{}

func (fakeRewind) RewindToMessage(_ context.Context, sessionID, _ string) (*service.RewindResult, error) {
	return &service.RewindResult{SessionID: sessionID, RewindCount: 1}, nil
}

func testRuntimeServer(t *testing.T, svc *Service) *khttp.Server {
	t.Helper()
	Configure(testRuntimeToken)
	srv := khttp.NewServer()
	RegisterRoutes(srv, svc)
	return srv
}

func newTestService(chat *fakeChat, peer *fakePeer, sessions *fakeSessions) *Service {
	if chat == nil {
		chat = newFakeChat()
	}
	if peer == nil {
		peer = &fakePeer{}
	}
	if sessions == nil {
		sessions = &fakeSessions{byID: chat.sessions}
	}
	return &Service{chat: chat, peer: peer, sessions: sessions, rewinder: fakeRewind{}}
}

func runtimeReq(method, path, body string, userID string, withToken bool) *http.Request {
	var rdr io.Reader
	if body != "" {
		rdr = strings.NewReader(body)
	}
	req := httptest.NewRequest(method, path, rdr)
	if withToken {
		req.Header.Set("Authorization", "Bearer "+testRuntimeToken)
	}
	if userID != "" {
		req.Header.Set(HeaderUserID, userID)
	}
	if body != "" {
		req.Header.Set("Content-Type", "application/json")
	}
	return req
}

func TestRuntimeSessions_NoTokenUnauthorized(t *testing.T) {
	srv := testRuntimeServer(t, newTestService(nil, nil, nil))
	req := runtimeReq(http.MethodGet, "/runtime/v1/sessions", "", "user-1", false)
	rec := httptest.NewRecorder()
	srv.ServeHTTP(rec, req)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401; body=%s", rec.Code, rec.Body.String())
	}
}

func TestRuntimeSessions_Resolve(t *testing.T) {
	peer := &fakePeer{}
	srv := testRuntimeServer(t, newTestService(nil, peer, nil))
	req := runtimeReq(http.MethodPost, "/runtime/v1/sessions/resolve",
		`{"channel_id":"ch1","peer_id":"p1","agent_id":"agent-1"}`, "", true)
	rec := httptest.NewRecorder()
	srv.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body=%s", rec.Code, rec.Body.String())
	}
	if peer.lastChannel != "ch1" || peer.lastPeer != "p1" || peer.lastAgent != "agent-1" {
		t.Fatalf("resolve args = (%q,%q,%q)", peer.lastChannel, peer.lastPeer, peer.lastAgent)
	}
	var body struct {
		SessionID string `json:"session_id"`
		AgentID   string `json:"agent_id"`
		UserID    string `json:"user_id"`
		Created   bool   `json:"created"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode: %v body=%s", err, rec.Body.String())
	}
	wantUser := biz.PeerUserID("ch1", "p1")
	if body.SessionID != "resolved-sess" || body.AgentID != "agent-1" || !body.Created || body.UserID != wantUser {
		t.Fatalf("unexpected resolve body: %+v want user_id=%q", body, wantUser)
	}
}

func TestRuntimeSessions_CreateAndGet(t *testing.T) {
	chat := newFakeChat()
	srv := testRuntimeServer(t, newTestService(chat, nil, nil))

	createReq := runtimeReq(http.MethodPost, "/runtime/v1/sessions",
		`{"agent_id":"agent-9","title":"hello"}`, "user-1", true)
	createRec := httptest.NewRecorder()
	srv.ServeHTTP(createRec, createReq)
	if createRec.Code != http.StatusOK {
		t.Fatalf("create status = %d, body=%s", createRec.Code, createRec.Body.String())
	}
	var created struct {
		ID      string `json:"id"`
		AgentID string `json:"agent_id"`
		Title   string `json:"title"`
	}
	if err := json.Unmarshal(createRec.Body.Bytes(), &created); err != nil {
		t.Fatalf("decode create: %v", err)
	}
	if created.ID == "" || created.AgentID != "agent-9" || created.Title != "hello" {
		t.Fatalf("created = %+v", created)
	}

	getReq := runtimeReq(http.MethodGet, "/runtime/v1/sessions/"+created.ID, "", "user-1", true)
	getRec := httptest.NewRecorder()
	srv.ServeHTTP(getRec, getReq)
	if getRec.Code != http.StatusOK {
		t.Fatalf("get status = %d, body=%s", getRec.Code, getRec.Body.String())
	}
	var got struct {
		ID string `json:"id"`
	}
	if err := json.Unmarshal(getRec.Body.Bytes(), &got); err != nil {
		t.Fatalf("decode get: %v", err)
	}
	if got.ID != created.ID {
		t.Fatalf("get id = %q, want %q", got.ID, created.ID)
	}
}

func TestRuntimeSessions_ListByAgent(t *testing.T) {
	chat := newFakeChat()
	srv := testRuntimeServer(t, newTestService(chat, nil, nil))
	_, _ = chat.CreateSession(biz.WithCallerUserID(context.Background(), "user-1"), "agent-a", "t1", "")

	req := runtimeReq(http.MethodGet, "/runtime/v1/agents/agent-a/sessions", "", "user-1", true)
	rec := httptest.NewRecorder()
	srv.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body=%s", rec.Code, rec.Body.String())
	}
	var body struct {
		Items []struct {
			ID string `json:"id"`
		} `json:"items"`
		Total int `json:"total"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode: %v body=%s", err, rec.Body.String())
	}
	if body.Total != 1 || len(body.Items) != 1 {
		t.Fatalf("list body = %+v", body)
	}
}

func TestRuntimeSessions_Messages(t *testing.T) {
	chat := newFakeChat()
	sess, err := chat.CreateSession(biz.WithCallerUserID(context.Background(), "user-1"), "agent-m", "t", "")
	if err != nil {
		t.Fatal(err)
	}
	chat.messages[sess.ID] = []*biz.ChatMessage{{
		ID:        "msg-1",
		SessionID: sess.ID,
		Role:      "user",
		Content:   "hi",
		CreatedAt: time.Now().UTC(),
	}}
	srv := testRuntimeServer(t, newTestService(chat, nil, nil))

	req := runtimeReq(http.MethodGet, "/runtime/v1/sessions/"+sess.ID+"/messages", "", "user-1", true)
	rec := httptest.NewRecorder()
	srv.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body=%s", rec.Code, rec.Body.String())
	}
	var body struct {
		Items []struct {
			ID      string `json:"id"`
			Content string `json:"content"`
		} `json:"items"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode: %v body=%s", err, rec.Body.String())
	}
	if len(body.Items) != 1 || body.Items[0].Content != "hi" {
		t.Fatalf("messages = %+v", body.Items)
	}
}

func TestRuntimeSessions_UserMismatchForbidden(t *testing.T) {
	chat := newFakeChat()
	sess, err := chat.CreateSession(biz.WithCallerUserID(context.Background(), "owner"), "agent-x", "t", "")
	if err != nil {
		t.Fatal(err)
	}
	sessions := &fakeSessions{byID: chat.sessions}
	srv := testRuntimeServer(t, newTestService(chat, nil, sessions))

	req := runtimeReq(http.MethodGet, "/runtime/v1/sessions/"+sess.ID, "", "intruder", true)
	rec := httptest.NewRecorder()
	srv.ServeHTTP(rec, req)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("status = %d, want 403; body=%s", rec.Code, rec.Body.String())
	}
}

func TestRequireSessionOwner_MapsNotFoundOnly(t *testing.T) {
	ctx := biz.WithCallerUserID(context.Background(), "user-1")
	svc := &Service{sessions: &fakeSessions{byID: map[string]*biz.ChatSession{}}}
	if err := svc.requireSessionOwner(ctx, "missing"); !kratosErrors.IsNotFound(err) {
		t.Fatalf("missing session err = %v, want NotFound", err)
	}

	boom := stderrors.New("db down")
	svc.sessions = &fakeSessions{err: boom}
	if err := svc.requireSessionOwner(ctx, "any"); !stderrors.Is(err, boom) {
		t.Fatalf("repo error = %v, want passthrough %v", err, boom)
	}
}

func TestRequireSessionOwner_NilSessionsInternal(t *testing.T) {
	ctx := biz.WithCallerUserID(context.Background(), "user-1")
	svc := &Service{sessions: nil}
	err := svc.requireSessionOwner(ctx, "sess-1")
	if err == nil {
		t.Fatal("expected internal error when sessions repo nil")
	}
	if got := int(kratosErrors.FromError(err).Code); got != http.StatusInternalServerError {
		t.Fatalf("code = %d, want 500", got)
	}
}
