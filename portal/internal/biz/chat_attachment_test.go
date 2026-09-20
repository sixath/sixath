package biz

import (
	"context"
	"errors"
	"testing"
	"time"

	pkgErrors "backend/internal/pkg/errors"
)

type fakeAttachmentRepo struct {
	bySession map[string]map[string]*ChatAttachment // sessionID -> id -> att
}

func newFakeAttachmentRepo() *fakeAttachmentRepo {
	return &fakeAttachmentRepo{bySession: map[string]map[string]*ChatAttachment{}}
}

func (f *fakeAttachmentRepo) Create(_ context.Context, a *ChatAttachment) error {
	if a == nil || a.ID == "" || a.SessionID == "" {
		return errors.New("invalid attachment")
	}
	m, ok := f.bySession[a.SessionID]
	if !ok {
		m = map[string]*ChatAttachment{}
		f.bySession[a.SessionID] = m
	}
	cp := *a
	m[a.ID] = &cp
	return nil
}

func (f *fakeAttachmentRepo) Get(_ context.Context, sessionID, id string) (*ChatAttachment, error) {
	m := f.bySession[sessionID]
	if m == nil {
		return nil, pkgErrors.ErrNotFound
	}
	a, ok := m[id]
	if !ok {
		return nil, pkgErrors.ErrNotFound
	}
	cp := *a
	return &cp, nil
}

func (f *fakeAttachmentRepo) Delete(_ context.Context, sessionID, id string) error {
	m := f.bySession[sessionID]
	if m == nil {
		return pkgErrors.ErrNotFound
	}
	if _, ok := m[id]; !ok {
		return pkgErrors.ErrNotFound
	}
	delete(m, id)
	return nil
}

func (f *fakeAttachmentRepo) CountBySession(_ context.Context, sessionID string) (int64, error) {
	return int64(len(f.bySession[sessionID])), nil
}

func (f *fakeAttachmentRepo) ListByIDs(_ context.Context, sessionID string, ids []string) ([]*ChatAttachment, error) {
	m := f.bySession[sessionID]
	out := make([]*ChatAttachment, 0, len(ids))
	for _, id := range ids {
		if a, ok := m[id]; ok {
			cp := *a
			out = append(out, &cp)
		}
	}
	return out, nil
}

func attachmentTestUsecase(t *testing.T) (*ChatUsecase, *fakeAttachmentRepo) {
	t.Helper()
	sessions := &fakeChatSessionRepo{
		sessions: map[string]*ChatSession{
			"sess-1": {ID: "sess-1", UserID: "user-a", AgentID: "agent-1", Title: "t"},
		},
	}
	atts := newFakeAttachmentRepo()
	uc := NewChatUsecase(sessions, nil, nil, nil, nil, atts)
	return uc, atts
}

func TestAttachmentUsecase_CreateGetCountListDelete(t *testing.T) {
	uc, _ := attachmentTestUsecase(t)
	ctx := WithCallerUserID(context.Background(), "user-a")
	now := time.Date(2026, 9, 20, 12, 0, 0, 0, time.UTC)

	a := &ChatAttachment{
		ID:           "att-1",
		SessionID:    "sess-1",
		Kind:         "image",
		Mime:         "image/png",
		Name:         "shot.png",
		Size:         128,
		RelativePath: "sessions/sess-1/uploads/att-1_shot.png",
		CreatedAt:    now,
	}
	if err := uc.CreateAttachment(ctx, a); err != nil {
		t.Fatalf("CreateAttachment: %v", err)
	}

	got, err := uc.GetAttachment(ctx, "sess-1", "att-1")
	if err != nil {
		t.Fatalf("GetAttachment: %v", err)
	}
	if got.Name != "shot.png" || got.Kind != "image" || got.RelativePath != a.RelativePath {
		t.Fatalf("GetAttachment = %+v", got)
	}

	n, err := uc.CountAttachmentsBySession(ctx, "sess-1")
	if err != nil {
		t.Fatalf("CountAttachmentsBySession: %v", err)
	}
	if n != 1 {
		t.Fatalf("Count = %d, want 1", n)
	}

	list, err := uc.ListAttachmentsByIDs(ctx, "sess-1", []string{"att-1", "missing"})
	if err != nil {
		t.Fatalf("ListAttachmentsByIDs: %v", err)
	}
	if len(list) != 1 || list[0].ID != "att-1" {
		t.Fatalf("List = %+v, want single att-1", list)
	}

	if err := uc.DeleteAttachment(ctx, "sess-1", "att-1"); err != nil {
		t.Fatalf("DeleteAttachment: %v", err)
	}
	if _, err := uc.GetAttachment(ctx, "sess-1", "att-1"); !errors.Is(err, ErrAttachmentNotFound) {
		t.Fatalf("Get after delete err = %v, want ErrAttachmentNotFound", err)
	}
	n, err = uc.CountAttachmentsBySession(ctx, "sess-1")
	if err != nil {
		t.Fatalf("Count after delete: %v", err)
	}
	if n != 0 {
		t.Fatalf("Count after delete = %d, want 0", n)
	}
}

func TestAttachmentUsecase_RequiresSessionOwnership(t *testing.T) {
	uc, _ := attachmentTestUsecase(t)
	ctx := WithCallerUserID(context.Background(), "user-b")
	a := &ChatAttachment{
		ID: "att-1", SessionID: "sess-1", Kind: "text", Mime: "text/plain",
		Name: "a.txt", Size: 1, RelativePath: "sessions/sess-1/uploads/att-1_a.txt",
	}
	if err := uc.CreateAttachment(ctx, a); !isReason(err, "SESSION_NOT_FOUND") {
		t.Fatalf("CreateAttachment by other user err = %v, want SESSION_NOT_FOUND", err)
	}
	if _, err := uc.GetAttachment(ctx, "sess-1", "att-1"); !isReason(err, "SESSION_NOT_FOUND") {
		t.Fatalf("GetAttachment by other user err = %v, want SESSION_NOT_FOUND", err)
	}
}
