package biz

import (
	"context"
	"errors"
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
}

var ErrSessionNotFound = kratosErrors.NotFound("SESSION_NOT_FOUND", "session not found")
var ErrInvalidParentSession = kratosErrors.BadRequest("INVALID_PARENT_SESSION", "parent session must belong to the same agent")

// ChatMessageRepo 消息存储接口
type ChatMessageRepo interface {
	Create(ctx context.Context, sessionID, role, content string, metadata map[string]any) (*ChatMessage, error)
	ListBySession(ctx context.Context, sessionID string, limit int) ([]*ChatMessage, error)
	LastUserOrAssistantBySessions(ctx context.Context, sessionIDs []string) (map[string]string, error)
	DeleteBySession(ctx context.Context, sessionID string) error
	// GetByID returns a message by primary key (including inactive).
	GetByID(ctx context.Context, messageID string) (*ChatMessage, error)
	// SoftDeactivateAfter sets active=false for the anchor message and all later
	// messages in the session (created_at > afterCreatedAt, or id == includeMessageID).
	// Remaining transcript ends at the message before the anchor.
	SoftDeactivateAfter(ctx context.Context, sessionID string, afterCreatedAt time.Time, includeMessageID string) (deactivatedIDs []string, err error)
}

// ChatUsecase 对话用例
type ChatUsecase struct {
	sessionRepo   ChatSessionRepo
	messageRepo   ChatMessageRepo
	agentRepo     AgentRepo
	resources     ResourceRepo
	access        *AccessChecker
	sessionSearch SessionSearchBackend
}

// NewChatUsecase creates ChatUsecase
func NewChatUsecase(sessionRepo ChatSessionRepo, messageRepo ChatMessageRepo, agentRepo AgentRepo, resources ResourceRepo, access *AccessChecker) *ChatUsecase {
	return &ChatUsecase{
		sessionRepo: sessionRepo,
		messageRepo: messageRepo,
		agentRepo:   agentRepo,
		resources:   resources,
		access:      access,
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

// DeleteSession 删除会话
func (uc *ChatUsecase) DeleteSession(ctx context.Context, id string) error {
	if _, err := uc.GetSession(ctx, id); err != nil {
		return err
	}
	return uc.sessionRepo.Delete(ctx, id)
}

// ListMessages 获取会话消息
func (uc *ChatUsecase) ListMessages(ctx context.Context, sessionID string, limit int) ([]*ChatMessage, error) {
	if _, err := uc.GetSession(ctx, sessionID); err != nil {
		return nil, err
	}
	return uc.messageRepo.ListBySession(ctx, sessionID, limit)
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
