package service

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"backend/internal/biz"
	"backend/internal/chat"

	kratosErrors "github.com/go-kratos/kratos/v2/errors"
	"github.com/sixath/framework/model"
)

func TestSendRejectsEmptyContentAndAttachments(t *testing.T) {
	if err := validateSendMessagePayload("", nil, false); err != ErrEmptySendContent {
		t.Fatalf("empty both: %v", err)
	}
	if err := validateSendMessagePayload("  ", nil, false); err != ErrEmptySendContent {
		t.Fatalf("whitespace content: %v", err)
	}
	if err := validateSendMessagePayload("", []string{"  "}, false); err != ErrEmptySendContent {
		t.Fatalf("blank ids only: %v", err)
	}
	if err := validateSendMessagePayload("", []string{"att-1"}, false); err != nil {
		t.Fatalf("attachments only should pass: %v", err)
	}
	if err := validateSendMessagePayload("hi", nil, false); err != nil {
		t.Fatalf("content only should pass: %v", err)
	}
	// HITL exempt
	if err := validateSendMessagePayload("", nil, true); err != nil {
		t.Fatalf("HITL empty should pass: %v", err)
	}
}

func TestAlreadyBoundAttachmentIDReusable(t *testing.T) {
	ws := t.TempDir()
	attID := "att-bound-reuse"
	rel := "sessions/sess-att/uploads/" + attID + "_shot.png"
	abs := filepath.Join(ws, filepath.FromSlash(rel))
	if err := os.MkdirAll(filepath.Dir(abs), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(abs, []byte{0x89, 0x50, 0x4e, 0x47}, 0o644); err != nil {
		t.Fatal(err)
	}

	// Message already references attID — resolve must still succeed (reusable).
	msgs := map[string]*biz.ChatMessage{
		"m1": {
			ID: "m1", SessionID: "sess-att", Role: "user", Content: "see", Active: true,
			Metadata: map[string]any{
				"attachments": []any{
					map[string]any{"id": attID, "kind": "image", "name": "shot.png"},
				},
			},
		},
	}
	s := newAttachmentChatService(t, ws, msgs)
	ctx := biz.WithCallerUserID(context.Background(), "owner")
	if err := s.chatUC.CreateAttachment(ctx, &biz.ChatAttachment{
		ID: attID, SessionID: "sess-att", Kind: "image", Mime: "image/png",
		Name: "shot.png", Size: 4, RelativePath: rel,
	}); err != nil {
		t.Fatalf("seed: %v", err)
	}

	got, err := s.resolveTurnAttachments(ctx, "sess-att", ws, []string{attID, attID, ""})
	if err != nil {
		t.Fatalf("resolve reusable bound id: %v", err)
	}
	if len(got.Metas) != 1 || got.Metas[0].RelativePath != rel {
		t.Fatalf("metas=%+v", got.Metas)
	}
	if len(got.MetadataItems) != 1 {
		t.Fatalf("metadata items=%d", len(got.MetadataItems))
	}
	if id, _ := got.MetadataItems[0]["id"].(string); id != attID {
		t.Fatalf("metadata id=%v", got.MetadataItems[0]["id"])
	}

	// Missing id → 400, no write side-effect required at this layer.
	_, err = s.resolveTurnAttachments(ctx, "sess-att", ws, []string{attID, "missing"})
	if err == nil {
		t.Fatal("expected missing id error")
	}
	se := kratosErrors.FromError(err)
	if se.Code != 400 {
		t.Fatalf("code=%d want 400; err=%v", se.Code, err)
	}
}

func TestResolveTurnAttachments_RejectsOversizedFile(t *testing.T) {
	ws := t.TempDir()
	s := newAttachmentChatService(t, ws, nil)
	ctx := biz.WithCallerUserID(context.Background(), "owner")
	attID := "att-big"
	rel := "sessions/sess-att/uploads/" + attID + "_big.png"
	abs := filepath.Join(ws, filepath.FromSlash(rel))
	if err := os.MkdirAll(filepath.Dir(abs), 0o755); err != nil {
		t.Fatal(err)
	}
	// On-disk larger than image cap (10MB) and recorded Size.
	big := make([]byte, chat.MaxImageAttachmentBytes+1)
	if err := os.WriteFile(abs, big, 0o644); err != nil {
		t.Fatal(err)
	}
	if err := s.chatUC.CreateAttachment(ctx, &biz.ChatAttachment{
		ID: attID, SessionID: "sess-att", Kind: "image", Mime: "image/png",
		Name: "big.png", Size: 4, RelativePath: rel,
	}); err != nil {
		t.Fatal(err)
	}
	_, err := s.resolveTurnAttachments(ctx, "sess-att", ws, []string{attID})
	if err != ErrAttachmentTooLarge {
		t.Fatalf("err=%v want ErrAttachmentTooLarge", err)
	}
}


func TestBuildCurrentUserModelMessage_OmitsImageFootnoteWhenParts(t *testing.T) {
	ws := t.TempDir()
	rel := "sessions/s1/uploads/att_a.png"
	abs := filepath.Join(ws, filepath.FromSlash(rel))
	if err := os.MkdirAll(filepath.Dir(abs), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(abs, []byte{0x89, 0x50}, 0o644); err != nil {
		t.Fatal(err)
	}
	metas := []chat.AttachmentMeta{
		{Kind: chat.KindImage, RelativePath: rel, Mime: "image/png"},
		{Kind: chat.KindText, RelativePath: "sessions/s1/uploads/a.log"},
	}
	persist := chat.BuildUserContentWithAttachments("hi", metas, chat.ContentForPersist)
	msg := buildCurrentUserModelMessage("hi", persist, metas, ws)
	wantContent := chat.BuildUserContentWithAttachments("hi", metas, chat.ContentForModelWithImageParts)
	if wantContent != msg.Content {
		t.Fatalf("model content should omit image footnote: %q", msg.Content)
	}
	// MultiContent-ready: text first, then image (§5.2).
	if len(msg.Parts) != 2 {
		t.Fatalf("parts=%d want 2 (text+image)", len(msg.Parts))
	}
	if msg.Parts[0].Type != model.ContentTypeText {
		t.Fatalf("Parts[0] type=%q want text", msg.Parts[0].Type)
	}
	if msg.Parts[0].Text != wantContent {
		t.Fatalf("Parts[0].Text=%q want %q", msg.Parts[0].Text, wantContent)
	}
	if msg.Parts[1].Type != model.ContentTypeImageURL || !strings.HasPrefix(msg.Parts[1].URL, "data:image/png") {
		t.Fatalf("Parts[1]=%+v", msg.Parts[1])
	}
}

func TestBuildCurrentUserModelMessage_TextThenImageParts(t *testing.T) {
	ws := t.TempDir()
	rel := "sessions/s1/uploads/att_hello.png"
	abs := filepath.Join(ws, filepath.FromSlash(rel))
	if err := os.MkdirAll(filepath.Dir(abs), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(abs, []byte{0x89, 0x50, 0x4e, 0x47}, 0o644); err != nil {
		t.Fatal(err)
	}
	metas := []chat.AttachmentMeta{
		{Kind: chat.KindImage, RelativePath: rel, Mime: "image/png"},
	}
	persist := chat.BuildUserContentWithAttachments("hello", metas, chat.ContentForPersist)
	msg := buildCurrentUserModelMessage("hello", persist, metas, ws)
	if msg.Content != "hello" {
		t.Fatalf("Content=%q want hello", msg.Content)
	}
	if len(msg.Parts) < 2 {
		t.Fatalf("Parts=%d want >=2 (text then image)", len(msg.Parts))
	}
	if msg.Parts[0].Type != model.ContentTypeText || msg.Parts[0].Text != "hello" {
		t.Fatalf("Parts[0]=%+v want text hello", msg.Parts[0])
	}
	if msg.Parts[1].Type != model.ContentTypeImageURL || !strings.HasPrefix(msg.Parts[1].URL, "data:image/png;base64,") {
		t.Fatalf("Parts[1]=%+v", msg.Parts[1])
	}
}
