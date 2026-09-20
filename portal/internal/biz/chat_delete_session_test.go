package biz

import (
	"context"
	"os"
	"path/filepath"
	"runtime"
	"testing"

	pkgErrors "backend/internal/pkg/errors"
)

type deleteSessionAgentRepo struct {
	workspace string
}

func (r *deleteSessionAgentRepo) Create(context.Context, string, string, string, string, string, ModelConfig, bool, string, string, RuntimeToolsConfig, []string) (*AgentMeta, error) {
	return nil, pkgErrors.ErrNotFound
}
func (r *deleteSessionAgentRepo) CountByWecomChannelID(context.Context, string) (int, error) {
	return 0, nil
}
func (r *deleteSessionAgentRepo) GetByID(_ context.Context, id string) (*AgentMeta, error) {
	if id != "agent-1" {
		return nil, pkgErrors.ErrNotFound
	}
	return &AgentMeta{ID: id, Workspace: r.workspace}, nil
}
func (r *deleteSessionAgentRepo) GetByName(context.Context, string) (*AgentMeta, error) {
	return nil, pkgErrors.ErrNotFound
}
func (r *deleteSessionAgentRepo) List(context.Context, int32, int32) ([]*AgentMeta, int, error) {
	return nil, 0, nil
}
func (r *deleteSessionAgentRepo) ListByIDs(context.Context, []string, int32, int32) ([]*AgentMeta, int, error) {
	return nil, 0, nil
}
func (r *deleteSessionAgentRepo) Update(context.Context, string, map[string]any) (*AgentMeta, error) {
	return nil, pkgErrors.ErrNotFound
}
func (r *deleteSessionAgentRepo) Delete(context.Context, string) error { return nil }
func (r *deleteSessionAgentRepo) BindTools(context.Context, string, []string) error {
	return nil
}
func (r *deleteSessionAgentRepo) UnbindTools(context.Context, string, []string) error {
	return nil
}

type deleteSessionRepo struct {
	sessions    map[string]*ChatSession
	attachments *fakeAttachmentRepo
}

func (r *deleteSessionRepo) Create(context.Context, string, string, string, string) (*ChatSession, error) {
	return nil, nil
}
func (r *deleteSessionRepo) GetByID(_ context.Context, id string) (*ChatSession, error) {
	s, ok := r.sessions[id]
	if !ok {
		return nil, pkgErrors.ErrNotFound
	}
	cp := *s
	return &cp, nil
}
func (r *deleteSessionRepo) ListByAgent(context.Context, string, string, string, int32, int32, bool) ([]*ChatSession, int, error) {
	return nil, 0, nil
}
func (r *deleteSessionRepo) ListAll(context.Context, string, int32, int32, bool) ([]*ChatSession, int, error) {
	return nil, 0, nil
}
func (r *deleteSessionRepo) Update(context.Context, string, map[string]any) (*ChatSession, error) {
	return nil, nil
}
func (r *deleteSessionRepo) Delete(_ context.Context, id string) error {
	if _, ok := r.sessions[id]; !ok {
		return pkgErrors.ErrNotFound
	}
	delete(r.sessions, id)
	if r.attachments != nil {
		delete(r.attachments.bySession, id)
	}
	return nil
}
func (r *deleteSessionRepo) Touch(context.Context, string) error { return nil }
func (r *deleteSessionRepo) BumpRewindCount(context.Context, string) error {
	return nil
}
func (r *deleteSessionRepo) MarkReadonly(context.Context, string) error { return nil }
func (r *deleteSessionRepo) SetModelOverride(context.Context, string, string, string) error {
	return nil
}

func TestDeleteSession_RemovesUploadsDirAndAttachmentRows(t *testing.T) {
	const sessionID = "sess-del-1"
	ws := t.TempDir()
	uploads := filepath.Join(ws, "sessions", sessionID, "uploads")
	if err := os.MkdirAll(uploads, 0o755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	if err := os.WriteFile(filepath.Join(uploads, "att-1_a.txt"), []byte("hi"), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	atts := newFakeAttachmentRepo()
	if err := atts.Create(context.Background(), &ChatAttachment{
		ID: "att-1", SessionID: sessionID, Kind: "text", Mime: "text/plain",
		Name: "a.txt", Size: 2, RelativePath: "sessions/" + sessionID + "/uploads/att-1_a.txt",
	}); err != nil {
		t.Fatalf("Create attachment: %v", err)
	}

	sessions := &deleteSessionRepo{
		sessions: map[string]*ChatSession{
			sessionID: {ID: sessionID, UserID: "user-a", AgentID: "agent-1", Title: "t"},
		},
		attachments: atts,
	}
	uc := NewChatUsecase(sessions, nil, &deleteSessionAgentRepo{workspace: ws}, nil, nil, atts)
	ctx := WithCallerUserID(context.Background(), "user-a")

	if err := uc.DeleteSession(ctx, sessionID); err != nil {
		t.Fatalf("DeleteSession: %v", err)
	}
	if _, ok := sessions.sessions[sessionID]; ok {
		t.Fatal("session row should be deleted")
	}
	if len(atts.bySession[sessionID]) != 0 {
		t.Fatalf("attachment rows should be deleted, got %d", len(atts.bySession[sessionID]))
	}
	if _, err := os.Stat(filepath.Join(ws, "sessions", sessionID)); !os.IsNotExist(err) {
		t.Fatalf("uploads dir should be removed: stat err=%v", err)
	}
}

func TestDeleteSession_SucceedsWhenRemoveAllFails(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("read-only dir RemoveAll behavior differs on Windows")
	}
	const sessionID = "sess-del-ro"
	ws := t.TempDir()
	sessionDir := filepath.Join(ws, "sessions", sessionID)
	if err := os.MkdirAll(sessionDir, 0o555); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}

	sessions := &deleteSessionRepo{
		sessions: map[string]*ChatSession{
			sessionID: {ID: sessionID, UserID: "user-a", AgentID: "agent-1", Title: "t"},
		},
	}
	uc := NewChatUsecase(sessions, nil, &deleteSessionAgentRepo{workspace: ws}, nil, nil, nil)
	ctx := WithCallerUserID(context.Background(), "user-a")

	if err := uc.DeleteSession(ctx, sessionID); err != nil {
		t.Fatalf("DeleteSession should succeed despite RemoveAll failure: %v", err)
	}
	if _, ok := sessions.sessions[sessionID]; ok {
		t.Fatal("session row should still be deleted")
	}
}
