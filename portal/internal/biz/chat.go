package biz

import (
	"context"
	"encoding/base64"
	"errors"
	"log"
	"strings"
	"time"

	pkgErrors "backend/internal/pkg/errors"

	kratosErrors "github.com/go-kratos/kratos/v2/errors"
)

// ChatSession 会话实体
type ChatSession struct {
	ID              string
	AgentID         string
	UserID          string
	ParentSessionID string
	Title           string
	Preview         string
	AgentName       string
	RewindCount     int
	Readonly        bool
	ModelProviderID string
	Model           string
	CreatedAt       time.Time
	UpdatedAt       time.Time
}

// SearchHit 跨 Agent 会话搜索命中
type SearchHit struct {
	SessionID       string
	RootSessionID   string
	AgentID         string
	AgentName       string
	Title           string
	Preview         string
	MatchedSnippets []string
	UpdatedAt       time.Time
}

// ChatMessage 消息实体
type ChatMessage struct {
	ID        string
	SessionID string
	Role      string // user, assistant, system
	Content   string
	Metadata  map[string]any // optional JSON (timeline, sources, …)
	Active    bool
	CreatedAt time.Time
}

// ChatAttachment 会话附件元数据（落盘路径 + 表行）
type ChatAttachment struct {
	ID           string
	SessionID    string
	Kind         string // image|text
	Mime         string
	Name         string
	Size         int64
	RelativePath string
	CreatedAt    time.Time
}

// ChatSessionRepo 会话存储接口
type ChatSessionRepo interface {
	Create(ctx context.Context, userID, agentID, title, parentSessionID string) (*ChatSession, error)
	GetByID(ctx context.Context, id string) (*ChatSession, error)
	ListByAgent(ctx context.Context, userID, agentID string, q string, page, pageSize int32, includePreview bool) ([]*ChatSession, int, error)
	ListAll(ctx context.Context, userID string, page, pageSize int32, includePreview bool) ([]*ChatSession, int, error)
	Update(ctx context.Context, id string, updates map[string]any) (*ChatSession, error)
	Delete(ctx context.Context, id string) error
	Touch(ctx context.Context, id string) error // 更新 updated_at
	// BumpRewindCount increments chat_sessions.rewind_count by 1.
	BumpRewindCount(ctx context.Context, sessionID string) error
	// MarkReadonly sets readonly=true (archive after L2 fork).
	MarkReadonly(ctx context.Context, sessionID string) error
	// SetModelOverride writes session model overlay. Both empty clears to Agent default.
	SetModelOverride(ctx context.Context, sessionID, providerID, model string) error
}

var ErrSessionNotFound = kratosErrors.NotFound("SESSION_NOT_FOUND", "session not found")
var ErrInvalidParentSession = kratosErrors.BadRequest("INVALID_PARENT_SESSION", "parent session must belong to the same agent")
var ErrAttachmentNotFound = kratosErrors.NotFound("ATTACHMENT_NOT_FOUND", "attachment not found")

// AttachmentRepo 会话附件元数据存储
type AttachmentRepo interface {
	Create(ctx context.Context, a *ChatAttachment) error
	Get(ctx context.Context, sessionID, id string) (*ChatAttachment, error)
	Delete(ctx context.Context, sessionID, id string) error
	CountBySession(ctx context.Context, sessionID string) (int64, error)
	ListByIDs(ctx context.Context, sessionID string, ids []string) ([]*ChatAttachment, error)
}

// 消息分页尺寸。默认 50：兼顾首屏渲染与长会话翻页次数；上限 200 防止单次拉爆。
const (
	DefaultMessagePageSize = 50
	MaxMessagePageSize     = 200
)

// NormalizeMessagePageSize 归一化页大小：<=0 → 默认值；>上限 → 上限。
func NormalizeMessagePageSize(limit int) int {
	if limit <= 0 {
		return DefaultMessagePageSize
	}
	if limit > MaxMessagePageSize {
		return MaxMessagePageSize
	}
	return limit
}

// MessageCursor 为消息分页游标：(created_at, id) 复合键。
// 仅用 created_at 会在同一时间戳（批量写入 / 同秒）时分页丢消息或重复。
type MessageCursor struct {
	CreatedAt time.Time
	ID        string
}

// Encode 把游标编码为可直接放进 URL 的不透明字符串；空游标（取最新一页）返回空串。
func (c MessageCursor) Encode() string {
	if c.ID == "" {
		return ""
	}
	raw := c.CreatedAt.UTC().Format(time.RFC3339Nano) + "|" + c.ID
	return base64.RawURLEncoding.EncodeToString([]byte(raw))
}

// DecodeMessageCursor 解析 Encode 产出的游标；空串表示"最新一页"。
func DecodeMessageCursor(raw string) (MessageCursor, error) {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" {
		return MessageCursor{}, nil
	}
	decoded, err := base64.RawURLEncoding.DecodeString(trimmed)
	if err != nil {
		return MessageCursor{}, errors.New("invalid message cursor")
	}
	parts := strings.SplitN(string(decoded), "|", 2)
	if len(parts) != 2 || parts[1] == "" {
		return MessageCursor{}, errors.New("invalid message cursor")
	}
	createdAt, err := time.Parse(time.RFC3339Nano, parts[0])
	if err != nil {
		return MessageCursor{}, errors.New("invalid message cursor")
	}
	return MessageCursor{CreatedAt: createdAt, ID: parts[1]}, nil
}

// ChatMessageRepo 消息存储接口
type ChatMessageRepo interface {
	Create(ctx context.Context, sessionID, role, content string, metadata map[string]any) (*ChatMessage, error)
	ListBySession(ctx context.Context, sessionID string, limit int) ([]*ChatMessage, error)
	// ListBySessionBefore 返回 before 游标之前（更早）的一页消息，按时间升序返回。
	// before 为零值表示取最新一页；第二个返回值是继续向前翻页的游标（空串表示没有更早的消息）。
	ListBySessionBefore(ctx context.Context, sessionID string, before MessageCursor, limit int) ([]*ChatMessage, string, error)
	LastUserOrAssistantBySessions(ctx context.Context, sessionIDs []string) (map[string]string, error)
	DeleteBySession(ctx context.Context, sessionID string) error
	// GetByID returns a message by primary key (including inactive).
	GetByID(ctx context.Context, messageID string) (*ChatMessage, error)
	// SoftDeactivateAfter sets active=false for the anchor message and all later
	// messages in the session (created_at > afterCreatedAt, or id == includeMessageID).
	// Remaining transcript ends at the message before the anchor.
	SoftDeactivateAfter(ctx context.Context, sessionID string, afterCreatedAt time.Time, includeMessageID string) (deactivatedIDs []string, err error)
	// ListActiveOrdered returns all active messages for a session, ordered by
	// created_at ASC, id ASC, with no row limit.
	ListActiveOrdered(ctx context.Context, sessionID string) ([]*ChatMessage, error)
	// ListBySessionIncludingInactive returns every message for the session
	// (active and inactive), ordered by created_at ASC, id ASC, with no row limit.
	// Used for attachment DELETE reference checks (spec: any message metadata).
	ListBySessionIncludingInactive(ctx context.Context, sessionID string) ([]*ChatMessage, error)
	// InsertClone copies src into destSessionID with a new ID, preserving CreatedAt
	// and stamping metadata.forked_from_message_id.
	InsertClone(ctx context.Context, destSessionID string, src *ChatMessage) (*ChatMessage, error)
}

// ChatUsecase 对话用例
type ChatUsecase struct {
	sessionRepo    ChatSessionRepo
	messageRepo    ChatMessageRepo
	attachmentRepo AttachmentRepo
	agentRepo      AgentRepo
	resources      ResourceRepo
	access         *AccessChecker
	sessionSearch  SessionSearchBackend
}

// NewChatUsecase creates ChatUsecase
func NewChatUsecase(sessionRepo ChatSessionRepo, messageRepo ChatMessageRepo, agentRepo AgentRepo, resources ResourceRepo, access *AccessChecker, attachmentRepo AttachmentRepo) *ChatUsecase {
	return &ChatUsecase{
		sessionRepo:    sessionRepo,
		messageRepo:    messageRepo,
		attachmentRepo: attachmentRepo,
		agentRepo:      agentRepo,
		resources:      resources,
		access:         access,
	}
}

// SetSessionSearchBackend installs the cross-session search adapter assembled
// by the chat wiring package.
func (uc *ChatUsecase) SetSessionSearchBackend(backend SessionSearchBackend) {
	uc.sessionSearch = backend
}

// CreateSession 新建会话
func (uc *ChatUsecase) CreateSession(ctx context.Context, agentID, title, parentSessionID string) (*ChatSession, error) {
	caller, err := requireCaller(ctx)
	if err != nil {
		return nil, err
	}
	if agentID == "" {
		return nil, ErrAgentNotFound
	}
	if err := uc.requireAgentUse(ctx, caller, agentID); err != nil {
		return nil, err
	}
	if _, err := uc.agentRepo.GetByID(ctx, agentID); err != nil {
		if errors.Is(err, pkgErrors.ErrNotFound) {
			return nil, ErrAgentNotFound
		}
		return nil, err
	}
	if parentSessionID != "" {
		parent, err := uc.GetSession(ctx, parentSessionID)
		if err != nil {
			return nil, err
		}
		if parent.AgentID != agentID {
			return nil, ErrInvalidParentSession
		}
	}
	if title == "" {
		title = "新对话"
	}
	return uc.sessionRepo.Create(ctx, caller, agentID, title, parentSessionID)
}

// GetSession 获取会话
func (uc *ChatUsecase) GetSession(ctx context.Context, id string) (*ChatSession, error) {
	caller, err := requireCaller(ctx)
	if err != nil {
		return nil, err
	}
	s, err := uc.sessionRepo.GetByID(ctx, id)
	if err != nil && errors.Is(err, pkgErrors.ErrNotFound) {
		return nil, ErrSessionNotFound
	}
	if err != nil {
		return nil, err
	}
	if s.UserID != caller {
		return nil, ErrSessionNotFound
	}
	return s, err
}

// SetModelOverride writes or clears the session model overlay after GetSession ACL.
func (uc *ChatUsecase) SetModelOverride(ctx context.Context, sessionID, providerID, model string) error {
	if _, err := uc.GetSession(ctx, sessionID); err != nil {
		return err
	}
	return uc.sessionRepo.SetModelOverride(ctx, sessionID, providerID, model)
}

// ListSessions 获取 Agent 的会话列表
func (uc *ChatUsecase) ListSessions(ctx context.Context, agentID string, q string, page, pageSize int32, includePreview bool) ([]*ChatSession, int, error) {
	caller, err := requireCaller(ctx)
	if err != nil {
		return nil, 0, err
	}
	return uc.sessionRepo.ListByAgent(ctx, caller, agentID, q, page, pageSize, includePreview)
}

// ListAllSessions 跨 Agent 分页列出会话
func (uc *ChatUsecase) ListAllSessions(ctx context.Context, page, pageSize int32, includePreview bool) ([]*ChatSession, int, error) {
	caller, err := requireCaller(ctx)
	if err != nil {
		return nil, 0, err
	}
	return uc.sessionRepo.ListAll(ctx, caller, page, pageSize, includePreview)
}

// UpdateSession 更新会话
func (uc *ChatUsecase) UpdateSession(ctx context.Context, id string, title string) (*ChatSession, error) {
	if _, err := uc.GetSession(ctx, id); err != nil {
		return nil, err
	}
	return uc.sessionRepo.Update(ctx, id, map[string]any{"title": title})
}

// DeleteSession 删除会话（级联删消息/附件行，并 best-effort 清理 workspace/sessions/<id>）
func (uc *ChatUsecase) DeleteSession(ctx context.Context, id string) error {
	session, err := uc.GetSession(ctx, id)
	if err != nil {
		return err
	}
	workspace := uc.agentWorkspaceForSession(ctx, session.AgentID)
	if err := uc.sessionRepo.Delete(ctx, id); err != nil {
		return err
	}
	if workspace != "" {
		if err := RemoveSessionWorkspaceDir(workspace, id); err != nil {
			log.Printf("chat: DeleteSession remove session workspace dir: session_id=%s err=%v", id, err)
		}
	}
	return nil
}

func (uc *ChatUsecase) agentWorkspaceForSession(ctx context.Context, agentID string) string {
	if uc == nil || uc.agentRepo == nil || strings.TrimSpace(agentID) == "" {
		return ""
	}
	agent, err := uc.agentRepo.GetByID(ctx, agentID)
	if err != nil || agent == nil {
		return ""
	}
	return strings.TrimSpace(agent.Workspace)
}

// ListMessages 获取会话消息
func (uc *ChatUsecase) ListMessages(ctx context.Context, sessionID string, limit int) ([]*ChatMessage, error) {
	if _, err := uc.GetSession(ctx, sessionID); err != nil {
		return nil, err
	}
	return uc.messageRepo.ListBySession(ctx, sessionID, limit)
}

// ListMessagePage 游标分页获取会话消息：before 为空取最新一页，否则取更早的一页。
// 返回 (消息, 继续向前翻页的游标)；游标为空表示已到会话开头。
func (uc *ChatUsecase) ListMessagePage(ctx context.Context, sessionID string, before MessageCursor, limit int) ([]*ChatMessage, string, error) {
	if _, err := uc.GetSession(ctx, sessionID); err != nil {
		return nil, "", err
	}
	return uc.messageRepo.ListBySessionBefore(ctx, sessionID, before, NormalizeMessagePageSize(limit))
}

// CreateMessage 创建消息（无 metadata）
func (uc *ChatUsecase) CreateMessage(ctx context.Context, sessionID, role, content string) (*ChatMessage, error) {
	if _, err := uc.GetSession(ctx, sessionID); err != nil {
		return nil, err
	}
	return uc.messageRepo.Create(ctx, sessionID, role, content, nil)
}

// CreateMessageWithMetadata 创建消息并写入 metadata JSON（如 timeline）
func (uc *ChatUsecase) CreateMessageWithMetadata(ctx context.Context, sessionID, role, content string, metadata map[string]any) (*ChatMessage, error) {
	if _, err := uc.GetSession(ctx, sessionID); err != nil {
		return nil, err
	}
	return uc.messageRepo.Create(ctx, sessionID, role, content, metadata)
}

// GetMessageByID returns a message by id (including inactive). Session ownership
// is checked by the caller via GetSession before Rewind.
func (uc *ChatUsecase) GetMessageByID(ctx context.Context, messageID string) (*ChatMessage, error) {
	if uc == nil || uc.messageRepo == nil || messageID == "" {
		return nil, ErrSessionNotFound
	}
	msg, err := uc.messageRepo.GetByID(ctx, messageID)
	if err != nil {
		if errors.Is(err, pkgErrors.ErrNotFound) {
			return nil, ErrSessionNotFound
		}
		return nil, err
	}
	return msg, nil
}

// CreateAttachment persists attachment metadata after session ACL check.
func (uc *ChatUsecase) CreateAttachment(ctx context.Context, a *ChatAttachment) error {
	if a == nil || a.SessionID == "" {
		return ErrAttachmentNotFound
	}
	if _, err := uc.GetSession(ctx, a.SessionID); err != nil {
		return err
	}
	if uc.attachmentRepo == nil {
		return ErrAttachmentNotFound
	}
	return uc.attachmentRepo.Create(ctx, a)
}

// GetAttachment returns attachment metadata for a session the caller owns.
func (uc *ChatUsecase) GetAttachment(ctx context.Context, sessionID, id string) (*ChatAttachment, error) {
	if _, err := uc.GetSession(ctx, sessionID); err != nil {
		return nil, err
	}
	if uc.attachmentRepo == nil || id == "" {
		return nil, ErrAttachmentNotFound
	}
	a, err := uc.attachmentRepo.Get(ctx, sessionID, id)
	if err != nil {
		if errors.Is(err, pkgErrors.ErrNotFound) {
			return nil, ErrAttachmentNotFound
		}
		return nil, err
	}
	return a, nil
}

// DeleteAttachment removes attachment metadata for a session the caller owns.
func (uc *ChatUsecase) DeleteAttachment(ctx context.Context, sessionID, id string) error {
	if _, err := uc.GetSession(ctx, sessionID); err != nil {
		return err
	}
	if uc.attachmentRepo == nil || id == "" {
		return ErrAttachmentNotFound
	}
	if err := uc.attachmentRepo.Delete(ctx, sessionID, id); err != nil {
		if errors.Is(err, pkgErrors.ErrNotFound) {
			return ErrAttachmentNotFound
		}
		return err
	}
	return nil
}

// CountAttachmentsBySession returns attachment row count for a session.
func (uc *ChatUsecase) CountAttachmentsBySession(ctx context.Context, sessionID string) (int64, error) {
	if _, err := uc.GetSession(ctx, sessionID); err != nil {
		return 0, err
	}
	if uc.attachmentRepo == nil {
		return 0, nil
	}
	return uc.attachmentRepo.CountBySession(ctx, sessionID)
}

// ListAttachmentsByIDs returns attachments that exist for the session (missing ids omitted).
func (uc *ChatUsecase) ListAttachmentsByIDs(ctx context.Context, sessionID string, ids []string) ([]*ChatAttachment, error) {
	if _, err := uc.GetSession(ctx, sessionID); err != nil {
		return nil, err
	}
	if uc.attachmentRepo == nil || len(ids) == 0 {
		return nil, nil
	}
	return uc.attachmentRepo.ListByIDs(ctx, sessionID, ids)
}

// AttachmentReferencedInMessages reports whether any session message (including
// inactive/rewound) references attID in metadata.attachments.
func (uc *ChatUsecase) AttachmentReferencedInMessages(ctx context.Context, sessionID, attID string) (bool, error) {
	if _, err := uc.GetSession(ctx, sessionID); err != nil {
		return false, err
	}
	attID = strings.TrimSpace(attID)
	if attID == "" || uc.messageRepo == nil {
		return false, nil
	}
	msgs, err := uc.messageRepo.ListBySessionIncludingInactive(ctx, sessionID)
	if err != nil {
		return false, err
	}
	return MessageReferencesAttachment(msgs, attID), nil
}

// SoftDeactivateAfter soft-hides the anchor and later messages (Rewind).
func (uc *ChatUsecase) SoftDeactivateAfter(ctx context.Context, sessionID string, afterCreatedAt time.Time, includeMessageID string) ([]string, error) {
	if uc == nil || uc.messageRepo == nil {
		return nil, nil
	}
	return uc.messageRepo.SoftDeactivateAfter(ctx, sessionID, afterCreatedAt, includeMessageID)
}

// BumpRewindCount increments session rewind_count.
func (uc *ChatUsecase) BumpRewindCount(ctx context.Context, sessionID string) error {
	if uc == nil || uc.sessionRepo == nil {
		return nil
	}
	return uc.sessionRepo.BumpRewindCount(ctx, sessionID)
}

// MarkSessionReadonly marks a session as archive-only (no new messages).
func (uc *ChatUsecase) MarkSessionReadonly(ctx context.Context, sessionID string) error {
	if uc == nil || uc.sessionRepo == nil {
		return nil
	}
	return uc.sessionRepo.MarkReadonly(ctx, sessionID)
}

var ErrSessionReadonly = kratosErrors.BadRequest("SESSION_READONLY", "session is readonly after archive compact")

func (uc *ChatUsecase) requireAgentView(ctx context.Context, caller, agentID string) error {
	resource, err := uc.resources.GetByPayload(ctx, ResourceTypeAgent, agentID)
	if err != nil {
		return ErrAgentNotFound
	}
	canView, err := uc.access.Can(ctx, caller, resource.ID, PermView, "")
	if err != nil {
		return err
	}
	if !canView {
		return ErrAgentNotFound
	}
	return nil
}

func (uc *ChatUsecase) requireAgentUse(ctx context.Context, caller, agentID string) error {
	if err := uc.requireAgentView(ctx, caller, agentID); err != nil {
		return err
	}
	resource, err := uc.resources.GetByPayload(ctx, ResourceTypeAgent, agentID)
	if err != nil {
		return ErrAgentNotFound
	}
	canUse, err := uc.access.Can(ctx, caller, resource.ID, PermUse, "")
	if err != nil {
		return err
	}
	if !canUse {
		return ErrForbiddenPerm
	}
	return nil
}
