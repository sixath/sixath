package service

import (
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"backend/internal/biz"
	pkgErrors "backend/internal/pkg/errors"

	kratosErrors "github.com/go-kratos/kratos/v2/errors"
	"github.com/go-kratos/kratos/v2/log"
)

type attachFakeRepo struct {
	bySession map[string]map[string]*biz.ChatAttachment
}

func newAttachFakeRepo() *attachFakeRepo {
	return &attachFakeRepo{bySession: map[string]map[string]*biz.ChatAttachment{}}
}

func (f *attachFakeRepo) Create(_ context.Context, a *biz.ChatAttachment) error {
	m, ok := f.bySession[a.SessionID]
	if !ok {
		m = map[string]*biz.ChatAttachment{}
		f.bySession[a.SessionID] = m
	}
	cp := *a
	m[a.ID] = &cp
	return nil
}

func (f *attachFakeRepo) Get(_ context.Context, sessionID, id string) (*biz.ChatAttachment, error) {
	m := f.bySession[sessionID]
	if m == nil || m[id] == nil {
		return nil, pkgErrors.ErrNotFound
	}
	cp := *m[id]
	return &cp, nil
}

func (f *attachFakeRepo) Delete(_ context.Context, sessionID, id string) error {
	m := f.bySession[sessionID]
	if m == nil || m[id] == nil {
		return pkgErrors.ErrNotFound
	}
	delete(m, id)
	return nil
}

func (f *attachFakeRepo) CountBySession(_ context.Context, sessionID string) (int64, error) {
	return int64(len(f.bySession[sessionID])), nil
}

func (f *attachFakeRepo) ListByIDs(_ context.Context, sessionID string, ids []string) ([]*biz.ChatAttachment, error) {
	m := f.bySession[sessionID]
	var out []*biz.ChatAttachment
	for _, id := range ids {
		if a, ok := m[id]; ok {
			cp := *a
			out = append(out, &cp)
		}
	}
	return out, nil
}

func newAttachmentChatService(t *testing.T, workspace string, msgs map[string]*biz.ChatMessage) *ChatService {
	t.Helper()
	const (
		agentID   = "agent-att"
		sessionID = "sess-att"
		userID    = "owner"
	)
	sess := &biz.ChatSession{ID: sessionID, AgentID: agentID, UserID: userID}
	if msgs == nil {
		msgs = map[string]*biz.ChatMessage{}
	}
	atts := newAttachFakeRepo()
	chatUC := biz.NewChatUsecase(&rewindSessRepo{sess: sess}, &rewindMsgRepo{msgs: msgs}, nil, nil, nil, atts)
	agentUC := biz.NewAgentUsecase(&hybridAgentRepo{agent: &biz.AgentMeta{
		ID: agentID, Name: "a", Workspace: workspace,
	}}, nil, nil, nil, t.TempDir(), log.NewStdLogger(nil))
	return NewChatService(chatUC, agentUC, nil, nil, nil, log.NewStdLogger(nil))
}

func TestUploadAttachment_RejectsPDF(t *testing.T) {
	ws := t.TempDir()
	s := newAttachmentChatService(t, ws, nil)
	ctx := biz.WithCallerUserID(context.Background(), "owner")

	_, err := s.UploadAttachment(ctx, "sess-att", "report.pdf", "application/pdf", []byte("%PDF-1.4"))
	if err == nil {
		t.Fatal("expected pdf upload to fail")
	}
	se := kratosErrors.FromError(err)
	if se.Code != 400 {
		t.Fatalf("code=%d want 400; err=%v", se.Code, err)
	}
}

func TestUploadAttachment_RequiresWorkspace(t *testing.T) {
	s := newAttachmentChatService(t, "  ", nil)
	ctx := biz.WithCallerUserID(context.Background(), "owner")

	_, err := s.UploadAttachment(ctx, "sess-att", "a.png", "image/png", []byte{0x89, 0x50, 0x4e, 0x47})
	if err == nil {
		t.Fatal("expected empty workspace to fail")
	}
	se := kratosErrors.FromError(err)
	if se.Code != 400 {
		t.Fatalf("code=%d want 400; err=%v", se.Code, err)
	}
	if !strings.Contains(se.Message, "请先配置 Agent workspace") {
		t.Fatalf("message=%q, want workspace hint", se.Message)
	}
}

func TestDeleteAttachment_ReferencedReturns409(t *testing.T) {
	ws := t.TempDir()
	attID := "att-bound"
	rel := "sessions/sess-att/uploads/" + attID + "_shot.png"
	abs := filepath.Join(ws, filepath.FromSlash(rel))
	if err := os.MkdirAll(filepath.Dir(abs), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(abs, []byte("png"), 0o644); err != nil {
		t.Fatal(err)
	}

	// Inactive (rewound) message still holds metadata.attachments — must block DELETE.
	msgs := map[string]*biz.ChatMessage{
		"m1": {
			ID: "m1", SessionID: "sess-att", Role: "user", Content: "see", Active: false,
			Metadata: map[string]any{
				"attachments": []any{
					map[string]any{"id": attID, "kind": "image", "name": "shot.png"},
				},
			},
		},
	}
	s := newAttachmentChatService(t, ws, msgs)
	ctx := biz.WithCallerUserID(context.Background(), "owner")

	// seed attachment row
	if err := s.chatUC.CreateAttachment(ctx, &biz.ChatAttachment{
		ID: attID, SessionID: "sess-att", Kind: "image", Mime: "image/png",
		Name: "shot.png", Size: 3, RelativePath: rel,
	}); err != nil {
		t.Fatalf("seed CreateAttachment: %v", err)
	}

	err := s.DeleteAttachment(ctx, "sess-att", attID)
	if err == nil {
		t.Fatal("expected 409 when attachment referenced")
	}
	se := kratosErrors.FromError(err)
	if se.Code != 409 {
		t.Fatalf("code=%d want 409; err=%v", se.Code, err)
	}
	if _, statErr := os.Stat(abs); statErr != nil {
		t.Fatalf("file should remain after 409: %v", statErr)
	}
}

func TestUploadAttachment_HappyPathPNG(t *testing.T) {
	ws := t.TempDir()
	s := newAttachmentChatService(t, ws, nil)
	ctx := biz.WithCallerUserID(context.Background(), "owner")

	png := []byte{0x89, 0x50, 0x4e, 0x47, 0x0d, 0x0a, 0x1a, 0x0a}
	out, err := s.UploadAttachment(ctx, "sess-att", "shot.png", "image/png", png)
	if err != nil {
		t.Fatalf("UploadAttachment: %v", err)
	}
	if out.ID == "" || out.Kind != "image" || out.Name != "shot.png" || out.Size != int64(len(png)) {
		t.Fatalf("reply = %+v", out)
	}
	if out.Mime != "image/png" {
		t.Fatalf("mime = %q, want image/png", out.Mime)
	}
	if !strings.HasPrefix(out.RelativePath, "sessions/sess-att/uploads/") {
		t.Fatalf("relative_path = %q", out.RelativePath)
	}
	abs := filepath.Join(ws, filepath.FromSlash(out.RelativePath))
	got, err := os.ReadFile(abs)
	if err != nil {
		t.Fatalf("read uploaded file: %v", err)
	}
	if !bytes.Equal(got, png) {
		t.Fatalf("file bytes mismatch")
	}
}

func TestUploadAttachment_IgnoresClientMIME(t *testing.T) {
	ws := t.TempDir()
	s := newAttachmentChatService(t, ws, nil)
	ctx := biz.WithCallerUserID(context.Background(), "owner")

	png := []byte{0x89, 0x50, 0x4e, 0x47}
	// Client claims text/html — must still store image/png from extension.
	out, err := s.UploadAttachment(ctx, "sess-att", "shot.png", "text/html", png)
	if err != nil {
		t.Fatalf("UploadAttachment: %v", err)
	}
	if out.Mime != "image/png" {
		t.Fatalf("mime = %q, want server-derived image/png", out.Mime)
	}

	file, err := s.GetAttachmentFile(ctx, "sess-att", out.ID)
	if err != nil {
		t.Fatalf("GetAttachmentFile: %v", err)
	}
	if file.Mime != "image/png" {
		t.Fatalf("GET mime = %q", file.Mime)
	}
	if file.Disposition != "inline" {
		t.Fatalf("image disposition = %q, want inline", file.Disposition)
	}
}

func TestUploadAttachment_TextDispositionAttachment(t *testing.T) {
	ws := t.TempDir()
	s := newAttachmentChatService(t, ws, nil)
	ctx := biz.WithCallerUserID(context.Background(), "owner")

	out, err := s.UploadAttachment(ctx, "sess-att", "err.log", "application/octet-stream", []byte("boom"))
	if err != nil {
		t.Fatalf("UploadAttachment: %v", err)
	}
	if out.Mime != "text/plain" {
		t.Fatalf("mime = %q, want text/plain", out.Mime)
	}
	file, err := s.GetAttachmentFile(ctx, "sess-att", out.ID)
	if err != nil {
		t.Fatalf("GetAttachmentFile: %v", err)
	}
	if file.Disposition != "attachment" {
		t.Fatalf("text disposition = %q, want attachment", file.Disposition)
	}
}

func TestGetAttachmentFile_IgnoresDBMIME(t *testing.T) {
	ws := t.TempDir()
	s := newAttachmentChatService(t, ws, nil)
	ctx := biz.WithCallerUserID(context.Background(), "owner")

	rel := "sessions/sess-att/uploads/att-dbmime_shot.png"
	abs := filepath.Join(ws, filepath.FromSlash(rel))
	if err := os.MkdirAll(filepath.Dir(abs), 0o755); err != nil {
		t.Fatal(err)
	}
	png := []byte{0x89, 0x50, 0x4e, 0x47}
	if err := os.WriteFile(abs, png, 0o644); err != nil {
		t.Fatal(err)
	}
	if err := s.chatUC.CreateAttachment(ctx, &biz.ChatAttachment{
		ID: "att-dbmime", SessionID: "sess-att", Kind: "image",
		Mime: "text/html", // poisoned DB mime — GET must ignore
		Name: "shot.png", Size: int64(len(png)), RelativePath: rel,
	}); err != nil {
		t.Fatal(err)
	}
	file, err := s.GetAttachmentFile(ctx, "sess-att", "att-dbmime")
	if err != nil {
		t.Fatalf("GetAttachmentFile: %v", err)
	}
	if file.Mime != "image/png" {
		t.Fatalf("GET mime = %q, want DefaultMIMEForKind image/png", file.Mime)
	}
}

func TestGetAttachmentFile_RejectsOversizedOnDisk(t *testing.T) {
	ws := t.TempDir()
	s := newAttachmentChatService(t, ws, nil)
	ctx := biz.WithCallerUserID(context.Background(), "owner")

	rel := "sessions/sess-att/uploads/att-big_a.txt"
	abs := filepath.Join(ws, filepath.FromSlash(rel))
	if err := os.MkdirAll(filepath.Dir(abs), 0o755); err != nil {
		t.Fatal(err)
	}
	// Recorded size 4; on-disk much larger than Size*2+slack → BadFile
	big := bytes.Repeat([]byte("x"), 200*1024)
	if err := os.WriteFile(abs, big, 0o644); err != nil {
		t.Fatal(err)
	}
	if err := s.chatUC.CreateAttachment(ctx, &biz.ChatAttachment{
		ID: "att-big", SessionID: "sess-att", Kind: "text", Mime: "text/plain",
		Name: "a.txt", Size: 4, RelativePath: rel,
	}); err != nil {
		t.Fatal(err)
	}
	_, err := s.GetAttachmentFile(ctx, "sess-att", "att-big")
	if err == nil {
		t.Fatal("expected oversized-vs-recorded to fail")
	}
	if !errors.Is(err, ErrAttachmentBadFile) && kratosErrors.FromError(err).Reason != "ATTACHMENT_INVALID_FILE" {
		t.Fatalf("err = %v, want BadFile", err)
	}
}

func TestGetAttachmentFile_RejectsPathEscape(t *testing.T) {
	ws := t.TempDir()
	s := newAttachmentChatService(t, ws, nil)
	ctx := biz.WithCallerUserID(context.Background(), "owner")

	// Seed a row whose relative_path points outside workspace.
	outside := filepath.Join(t.TempDir(), "secret.txt")
	if err := os.WriteFile(outside, []byte("secret"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := s.chatUC.CreateAttachment(ctx, &biz.ChatAttachment{
		ID: "att-escape", SessionID: "sess-att", Kind: "text", Mime: "text/plain",
		Name: "secret.txt", Size: 6, RelativePath: filepath.ToSlash(outside),
	}); err != nil {
		t.Fatalf("seed: %v", err)
	}
	_, err := s.GetAttachmentFile(ctx, "sess-att", "att-escape")
	if err == nil {
		t.Fatal("expected path escape to fail")
	}
}

func TestPathUnderWorkspace(t *testing.T) {
	root := t.TempDir()
	inside := filepath.Join(root, "sessions", "s1", "uploads", "a.png")
	if !pathUnderWorkspace(root, inside) {
		t.Fatal("inside path should be allowed")
	}
	if pathUnderWorkspace(root, filepath.Join(root, "..", "other")) {
		t.Fatal("escaped path should be rejected")
	}
}
