package service

import (
	"context"
	"fmt"
	"os"
	"path"
	"path/filepath"
	"strings"

	"backend/internal/biz"
	"backend/internal/chat"

	kratosErrors "github.com/go-kratos/kratos/v2/errors"
	"github.com/google/uuid"
	"github.com/sixath/framework/model"
)

var (
	ErrAttachmentUnsupported = kratosErrors.BadRequest("ATTACHMENT_UNSUPPORTED", "unsupported attachment type")
	ErrAttachmentTooLarge    = kratosErrors.BadRequest("ATTACHMENT_TOO_LARGE", "attachment exceeds size limit")
	ErrAttachmentQuota       = kratosErrors.BadRequest("ATTACHMENT_QUOTA", "session attachment limit reached")
	ErrAttachmentWorkspace   = kratosErrors.BadRequest("ATTACHMENT_WORKSPACE_REQUIRED", "请先配置 Agent workspace")
	ErrAttachmentInUse       = kratosErrors.Conflict("ATTACHMENT_IN_USE", "attachment is referenced by a message")
	ErrAttachmentBadFile     = kratosErrors.BadRequest("ATTACHMENT_INVALID_FILE", "invalid attachment file")
	ErrEmptySendContent      = kratosErrors.BadRequest("EMPTY_MESSAGE", "content and attachment_ids cannot both be empty")
	ErrAttachmentIDsInvalid  = kratosErrors.BadRequest("ATTACHMENT_IDS_INVALID", "attachment not found or file missing")
)

// AttachmentUploadReply is the JSON body for POST …/attachments.
type AttachmentUploadReply struct {
	ID           string `json:"id"`
	Kind         string `json:"kind"`
	Mime         string `json:"mime"`
	Name         string `json:"name"`
	Size         int64  `json:"size"`
	RelativePath string `json:"relative_path"`
}

// AttachmentFile is the raw bytes payload for GET …/attachments/{id}.
type AttachmentFile struct {
	Mime        string
	Name        string
	Kind        string // image|text — drives Content-Disposition
	Data        []byte
	Disposition string // inline (image) or attachment (text)
}

// UploadAttachment validates, writes to workspace uploads, and persists metadata.
// clientMIME is used only as a fallback when the filename has no extension;
// the stored mime is always server-derived via chat.DefaultMIMEForKind.
func (s *ChatService) UploadAttachment(ctx context.Context, sessionID, filename, clientMIME string, data []byte) (*AttachmentUploadReply, error) {
	sessionID = strings.TrimSpace(sessionID)
	if s == nil || s.chatUC == nil || s.agentUC == nil || sessionID == "" {
		return nil, biz.ErrSessionNotFound
	}
	session, err := s.chatUC.GetSession(ctx, sessionID)
	if err != nil {
		return nil, err
	}
	agentMeta, err := s.agentUC.GetForSession(ctx, session.AgentID)
	if err != nil {
		return nil, err
	}
	workspace := strings.TrimSpace(agentMeta.Workspace)
	if workspace == "" {
		return nil, ErrAttachmentWorkspace
	}

	safeName := chat.SafeFilename(filename)
	if safeName == "" {
		return nil, ErrAttachmentBadFile
	}
	kind, ok := chat.ClassifyAttachment(safeName, clientMIME)
	if !ok {
		return nil, ErrAttachmentUnsupported
	}
	size := int64(len(data))
	switch kind {
	case chat.KindImage:
		if size > chat.MaxImageAttachmentBytes {
			return nil, ErrAttachmentTooLarge
		}
	case chat.KindText:
		if size > chat.MaxTextAttachmentBytes {
			return nil, ErrAttachmentTooLarge
		}
	default:
		return nil, ErrAttachmentUnsupported
	}

	n, err := s.chatUC.CountAttachmentsBySession(ctx, sessionID)
	if err != nil {
		return nil, err
	}
	if n >= int64(chat.MaxAttachmentsPerSession) {
		return nil, ErrAttachmentQuota
	}

	// Always server-derived — never persist client Content-Type (MIME XSS).
	resolvedMIME := chat.DefaultMIMEForKind(kind, safeName)

	attID := uuid.NewString()
	rel := path.Join("sessions", sessionID, "uploads", attID+"_"+safeName)
	abs := filepath.Join(workspace, filepath.FromSlash(rel))
	if err := os.MkdirAll(filepath.Dir(abs), 0o755); err != nil {
		return nil, fmt.Errorf("mkdir uploads: %w", err)
	}
	if err := os.WriteFile(abs, data, 0o644); err != nil {
		return nil, fmt.Errorf("write attachment: %w", err)
	}

	row := &biz.ChatAttachment{
		ID:           attID,
		SessionID:    sessionID,
		Kind:         string(kind),
		Mime:         resolvedMIME,
		Name:         safeName,
		Size:         size,
		RelativePath: rel,
	}
	if err := s.chatUC.CreateAttachment(ctx, row); err != nil {
		_ = os.Remove(abs)
		return nil, err
	}

	return &AttachmentUploadReply{
		ID:           attID,
		Kind:         string(kind),
		Mime:         resolvedMIME,
		Name:         safeName,
		Size:         size,
		RelativePath: rel,
	}, nil
}

// GetAttachmentFile returns attachment bytes for download/preview.
func (s *ChatService) GetAttachmentFile(ctx context.Context, sessionID, attID string) (*AttachmentFile, error) {
	sessionID = strings.TrimSpace(sessionID)
	attID = strings.TrimSpace(attID)
	if s == nil || s.chatUC == nil || s.agentUC == nil || sessionID == "" || attID == "" {
		return nil, biz.ErrAttachmentNotFound
	}
	session, err := s.chatUC.GetSession(ctx, sessionID)
	if err != nil {
		return nil, err
	}
	att, err := s.chatUC.GetAttachment(ctx, sessionID, attID)
	if err != nil {
		return nil, err
	}
	agentMeta, err := s.agentUC.GetForSession(ctx, session.AgentID)
	if err != nil {
		return nil, err
	}
	workspace := strings.TrimSpace(agentMeta.Workspace)
	if workspace == "" {
		return nil, ErrAttachmentWorkspace
	}
	abs := filepath.Join(workspace, filepath.FromSlash(att.RelativePath))
	if !pathUnderWorkspace(workspace, abs) {
		return nil, biz.ErrAttachmentNotFound
	}
	st, err := os.Stat(abs)
	if err != nil {
		return nil, biz.ErrAttachmentNotFound
	}
	if err := validateAttachmentFileSize(att, st.Size()); err != nil {
		return nil, err
	}
	data, err := os.ReadFile(abs)
	if err != nil {
		return nil, biz.ErrAttachmentNotFound
	}
	// Always derive Content-Type from kind+name — ignore DB mime (I4).
	mime := chat.DefaultMIMEForKind(chat.AttachmentKind(att.Kind), att.Name)
	disposition := "inline"
	if att.Kind == string(chat.KindText) {
		disposition = "attachment"
	}
	return &AttachmentFile{
		Mime:        mime,
		Name:        att.Name,
		Kind:        att.Kind,
		Data:        data,
		Disposition: disposition,
	}, nil
}

// validateAttachmentFileSize rejects files over kind caps or abnormally larger than recorded Size.
func validateAttachmentFileSize(att *biz.ChatAttachment, fileSize int64) error {
	if att == nil || fileSize < 0 {
		return ErrAttachmentBadFile
	}
	maxByKind := int64(chat.MaxImageAttachmentBytes)
	if att.Kind == string(chat.KindText) {
		maxByKind = chat.MaxTextAttachmentBytes
	}
	if fileSize > maxByKind {
		return ErrAttachmentTooLarge
	}
	// Recorded size sanity: on-disk must not exceed att.Size*2 + small slack
	// (guards against swapped/tampered files without trusting DB mime).
	const sizeSlack = 64 << 10 // 64KiB
	if att.Size > 0 {
		maxExpected := att.Size*2 + sizeSlack
		if fileSize > maxExpected {
			return ErrAttachmentBadFile
		}
	}
	return nil
}

// DeleteAttachment removes an unbound attachment file + row. Returns 409 when referenced.
func (s *ChatService) DeleteAttachment(ctx context.Context, sessionID, attID string) error {
	sessionID = strings.TrimSpace(sessionID)
	attID = strings.TrimSpace(attID)
	if s == nil || s.chatUC == nil || s.agentUC == nil || sessionID == "" || attID == "" {
		return biz.ErrAttachmentNotFound
	}
	session, err := s.chatUC.GetSession(ctx, sessionID)
	if err != nil {
		return err
	}
	att, err := s.chatUC.GetAttachment(ctx, sessionID, attID)
	if err != nil {
		return err
	}

	referenced, err := s.chatUC.AttachmentReferencedInMessages(ctx, sessionID, attID)
	if err != nil {
		return err
	}
	if referenced {
		return ErrAttachmentInUse
	}

	agentMeta, err := s.agentUC.GetForSession(ctx, session.AgentID)
	if err != nil {
		return err
	}
	workspace := strings.TrimSpace(agentMeta.Workspace)
	if workspace != "" && strings.TrimSpace(att.RelativePath) != "" {
		abs := filepath.Join(workspace, filepath.FromSlash(att.RelativePath))
		if pathUnderWorkspace(workspace, abs) {
			_ = os.Remove(abs)
		}
	}
	return s.chatUC.DeleteAttachment(ctx, sessionID, attID)
}

// pathUnderWorkspace reports whether target resolves inside workspace (no .. escape).
func pathUnderWorkspace(workspace, target string) bool {
	root, err := filepath.Abs(workspace)
	if err != nil {
		return false
	}
	abs, err := filepath.Abs(target)
	if err != nil {
		return false
	}
	rel, err := filepath.Rel(root, abs)
	if err != nil {
		return false
	}
	if rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return false
	}
	return true
}

type resolvedTurnAttachments struct {
	Metas         []chat.AttachmentMeta
	MetadataItems []map[string]any
}

// validateSendMessagePayload enforces content|attachment_ids non-empty for normal
// turns. HITL (input_response / confirm_response) is exempt and ignores attachments.
func validateSendMessagePayload(content string, attachmentIDs []string, hitl bool) error {
	if hitl {
		return nil
	}
	if strings.TrimSpace(content) == "" && len(chat.UniqueNonEmpty(attachmentIDs)) == 0 {
		return ErrEmptySendContent
	}
	return nil
}

// resolveTurnAttachments loads attachment rows + verifies files on disk.
// Already-bound ids are reusable (no reference check). Missing id/file → 400.
func (s *ChatService) resolveTurnAttachments(ctx context.Context, sessionID, workspace string, ids []string) (*resolvedTurnAttachments, error) {
	ids = chat.UniqueNonEmpty(ids)
	if len(ids) == 0 {
		return &resolvedTurnAttachments{}, nil
	}
	workspace = strings.TrimSpace(workspace)
	if workspace == "" {
		return nil, ErrAttachmentWorkspace
	}
	if s == nil || s.chatUC == nil {
		return nil, ErrAttachmentIDsInvalid
	}
	rows, err := s.chatUC.ListAttachmentsByIDs(ctx, sessionID, ids)
	if err != nil {
		return nil, err
	}
	byID := make(map[string]*biz.ChatAttachment, len(rows))
	for _, r := range rows {
		if r != nil {
			byID[r.ID] = r
		}
	}
	out := &resolvedTurnAttachments{
		Metas:         make([]chat.AttachmentMeta, 0, len(ids)),
		MetadataItems: make([]map[string]any, 0, len(ids)),
	}
	for _, id := range ids {
		att, ok := byID[id]
		if !ok {
			return nil, ErrAttachmentIDsInvalid
		}
		abs := filepath.Join(workspace, filepath.FromSlash(att.RelativePath))
		if !pathUnderWorkspace(workspace, abs) {
			return nil, ErrAttachmentIDsInvalid
		}
		st, err := os.Stat(abs)
		if err != nil || st.IsDir() {
			return nil, ErrAttachmentIDsInvalid
		}
		if err := validateAttachmentFileSize(att, st.Size()); err != nil {
			return nil, err
		}
		kind := chat.AttachmentKind(att.Kind)
		mime := chat.DefaultMIMEForKind(kind, att.Name)
		out.Metas = append(out.Metas, chat.AttachmentMeta{
			Kind:         kind,
			RelativePath: att.RelativePath,
			Mime:         mime,
		})
		out.MetadataItems = append(out.MetadataItems, map[string]any{
			"id":            att.ID,
			"kind":          att.Kind,
			"mime":          mime,
			"name":          att.Name,
			"size":          att.Size,
			"relative_path": att.RelativePath,
		})
	}
	return out, nil
}

func workspaceAttachmentReader(workspace string) func(rel string) ([]byte, error) {
	return func(rel string) ([]byte, error) {
		abs := filepath.Join(workspace, filepath.FromSlash(rel))
		if !pathUnderWorkspace(workspace, abs) {
			return nil, os.ErrNotExist
		}
		return os.ReadFile(abs)
	}
}

func buildCurrentUserModelMessage(baseContent, persistContent string, metas []chat.AttachmentMeta, workspace string) model.Message {
	modelContent := chat.BuildUserContentWithAttachments(baseContent, metas, chat.ContentForModelWithImageParts)
	parts := chat.BuildImageParts(metas, workspaceAttachmentReader(workspace))
	if chat.CountImageMetas(metas) > 0 && len(parts) == 0 {
		// No usable image Parts — keep footnotes so paths are still visible.
		modelContent = persistContent
	}
	// openAIChatMessage serializes Parts only (ignores Content) when len(Parts) > 0.
	// Prepend text so MultiContent includes user text + footnotes (§5.2).
	if modelContent != "" && len(parts) > 0 {
		parts = append([]model.ContentPart{{Type: model.ContentTypeText, Text: modelContent}}, parts...)
	}
	return model.Message{Role: "user", Content: modelContent, Parts: parts}
}

func historyToModelMessages(history []*biz.ChatMessage) []model.Message {
	messages := make([]model.Message, 0, len(history))
	for _, h := range history {
		if h == nil || h.Role == "system" {
			continue
		}
		if strings.TrimSpace(h.Content) == "" {
			continue
		}
		messages = append(messages, model.Message{Role: h.Role, Content: h.Content})
	}
	return messages
}
