package data

import (
	"context"
	"strings"
	"time"

	"backend/internal/biz"
	"backend/internal/data/model"

	"github.com/go-kratos/kratos/v2/log"
	"github.com/google/uuid"
	"gorm.io/gorm"
)

var _ biz.ChatSessionRepo = (*chatSessionRepo)(nil)
var _ biz.ChatMessageRepo = (*chatMessageRepo)(nil)

type chatSessionRepo struct {
	db  *gorm.DB
	log *log.Helper
}

type chatMessageRepo struct {
	db  *gorm.DB
	log *log.Helper
}

// NewChatSessionRepo creates ChatSessionRepo
func NewChatSessionRepo(data *Data, logger log.Logger) biz.ChatSessionRepo {
	return &chatSessionRepo{db: data.db, log: log.NewHelper(logger)}
}

// NewChatMessageRepo creates ChatMessageRepo
func NewChatMessageRepo(data *Data, logger log.Logger) biz.ChatMessageRepo {
	return &chatMessageRepo{db: data.db, log: log.NewHelper(logger)}
}

func (r *chatSessionRepo) Create(ctx context.Context, userID, agentID, title, parentSessionID string) (*biz.ChatSession, error) {
	id := uuid.New().String()
	s := &model.ChatSession{
		ID:              id,
		AgentID:         agentID,
		UserID:          userID,
		ParentSessionID: parentSessionID,
		Title:           title,
	}
	if err := r.db.WithContext(ctx).Create(s).Error; err != nil {
		return nil, err
	}
	return sessionModelToBiz(s, "", ""), nil
}

func (r *chatSessionRepo) GetByID(ctx context.Context, id string) (*biz.ChatSession, error) {
	var s model.ChatSession
	if err := r.db.WithContext(ctx).Where("id = ?", id).First(&s).Error; err != nil {
		if err == gorm.ErrRecordNotFound {
			return nil, ErrNotFound
		}
		return nil, err
	}
	return sessionModelToBiz(&s, "", ""), nil
}

func sessionModelToBiz(s *model.ChatSession, agentName, preview string) *biz.ChatSession {
	if s == nil {
		return nil
	}
	return &biz.ChatSession{
		ID:              s.ID,
		AgentID:         s.AgentID,
		UserID:          s.UserID,
		ParentSessionID: s.ParentSessionID,
		Title:           s.Title,
		AgentName:       agentName,
		Preview:         preview,
		RewindCount:     s.RewindCount,
		Readonly:        s.Readonly,
		CreatedAt:       s.CreatedAt,
		UpdatedAt:       s.UpdatedAt,
	}
}

func normalizePage(page, pageSize int32) (int32, int32) {
	if page < 1 {
		page = 1
	}
	if pageSize < 1 || pageSize > 100 {
		pageSize = 10
	}
	return page, pageSize
}

func (r *chatSessionRepo) ListByAgent(ctx context.Context, userID, agentID string, q string, page, pageSize int32, includePreview bool) ([]*biz.ChatSession, int, error) {
	page, pageSize = normalizePage(page, pageSize)
	base := r.db.WithContext(ctx).Model(&model.ChatSession{}).Where("user_id = ? AND agent_id = ?", userID, agentID)
	if strings.TrimSpace(q) != "" {
		like := "%" + strings.TrimSpace(q) + "%"
		base = base.Where("title LIKE ?", like)
	}
	var total int64
	if err := base.Count(&total).Error; err != nil {
		return nil, 0, err
	}
	offset := int((page - 1) * pageSize)
	var rows []model.ChatSession
	if err := base.Order("updated_at DESC").Offset(offset).Limit(int(pageSize)).Find(&rows).Error; err != nil {
		return nil, 0, err
	}
	items, err := r.buildSessionsWithPreview(ctx, rows, includePreview)
	if err != nil {
		return nil, 0, err
	}
	return items, int(total), nil
}

func (r *chatSessionRepo) ListAll(ctx context.Context, userID string, page, pageSize int32, includePreview bool) ([]*biz.ChatSession, int, error) {
	page, pageSize = normalizePage(page, pageSize)
	var total int64
	if err := r.db.WithContext(ctx).Model(&model.ChatSession{}).Where("user_id = ?", userID).Count(&total).Error; err != nil {
		return nil, 0, err
	}
	offset := int((page - 1) * pageSize)

	type row struct {
		model.ChatSession
		AgentName string `gorm:"column:agent_name"`
	}
	var rows []row
	if err := r.db.WithContext(ctx).
		Table("chat_sessions cs").
		Select("cs.*, a.name AS agent_name").
		Joins("JOIN agents a ON a.id = cs.agent_id").
		Where("cs.user_id = ?", userID).
		Order("cs.updated_at DESC").
		Offset(offset).
		Limit(int(pageSize)).
		Scan(&rows).Error; err != nil {
		return nil, 0, err
	}

	items := make([]*biz.ChatSession, len(rows))
	sessionIDs := make([]string, len(rows))
	for i, s := range rows {
		sessionIDs[i] = s.ID
		items[i] = sessionModelToBiz(&s.ChatSession, s.AgentName, "")
	}
	if includePreview && len(sessionIDs) > 0 {
		previews, err := r.messageRepo().LastUserOrAssistantBySessions(ctx, sessionIDs)
		if err != nil {
			return nil, 0, err
		}
		for _, item := range items {
			if p, ok := previews[item.ID]; ok {
				item.Preview = biz.TruncatePreview(p)
			}
		}
	}
	return items, int(total), nil
}

func (r *chatSessionRepo) buildSessionsWithPreview(ctx context.Context, rows []model.ChatSession, includePreview bool) ([]*biz.ChatSession, error) {
	items := make([]*biz.ChatSession, len(rows))
	sessionIDs := make([]string, len(rows))
	for i, s := range rows {
		sessionIDs[i] = s.ID
		items[i] = sessionModelToBiz(&s, "", "")
	}
	if includePreview && len(sessionIDs) > 0 {
		previews, err := r.messageRepo().LastUserOrAssistantBySessions(ctx, sessionIDs)
		if err != nil {
			return nil, err
		}
		for _, item := range items {
			if p, ok := previews[item.ID]; ok {
				item.Preview = biz.TruncatePreview(p)
			}
		}
	}
	return items, nil
}

func (r *chatSessionRepo) Update(ctx context.Context, id string, updates map[string]any) (*biz.ChatSession, error) {
	var s model.ChatSession
	if err := r.db.WithContext(ctx).Where("id = ?", id).First(&s).Error; err != nil {
		if err == gorm.ErrRecordNotFound {
			return nil, ErrNotFound
		}
		return nil, err
	}
	upd := make(map[string]interface{})
	for k, v := range updates {
		switch k {
		case "title":
			upd["title"] = v.(string)
		case "parent_session_id":
			upd["parent_session_id"] = v.(string)
		}
	}
	if len(upd) > 0 {
		if err := r.db.WithContext(ctx).Model(&s).Updates(upd).Error; err != nil {
			return nil, err
		}
	}
	return r.GetByID(ctx, id)
}

func (r *chatSessionRepo) Touch(ctx context.Context, id string) error {
	return r.db.WithContext(ctx).Model(&model.ChatSession{}).Where("id = ?", id).Update("updated_at", time.Now()).Error
}

func (r *chatSessionRepo) BumpRewindCount(ctx context.Context, sessionID string) error {
	res := r.db.WithContext(ctx).Model(&model.ChatSession{}).
		Where("id = ?", sessionID).
		Updates(map[string]any{
			"rewind_count": gorm.Expr("rewind_count + 1"),
			"updated_at":   time.Now(),
		})
	if res.Error != nil {
		return res.Error
	}
	if res.RowsAffected == 0 {
		return ErrNotFound
	}
	return nil
}

func (r *chatSessionRepo) MarkReadonly(ctx context.Context, sessionID string) error {
	res := r.db.WithContext(ctx).Model(&model.ChatSession{}).
		Where("id = ?", sessionID).
		Updates(map[string]any{
			"readonly":   true,
			"updated_at": time.Now(),
		})
	if res.Error != nil {
		return res.Error
	}
	if res.RowsAffected == 0 {
		return ErrNotFound
	}
	return nil
}

func (r *chatSessionRepo) Delete(ctx context.Context, id string) error {
	// 级联删除消息
	r.db.WithContext(ctx).Where("session_id = ?", id).Delete(&model.ChatMessage{})
	res := r.db.WithContext(ctx).Where("id = ?", id).Delete(&model.ChatSession{})
	if res.Error != nil {
		return res.Error
	}
	if res.RowsAffected == 0 {
		return ErrNotFound
	}
	return nil
}

func (r *chatMessageRepo) Create(ctx context.Context, sessionID, role, content string, metadata map[string]any) (*biz.ChatMessage, error) {
	id := uuid.New().String()
	m := &model.ChatMessage{
		ID:        id,
		SessionID: sessionID,
		Role:      role,
		Content:   content,
		Active:    true,
	}
	if len(metadata) > 0 {
		m.Metadata = model.JSONMap(metadata)
	}
	if err := r.db.WithContext(ctx).Create(m).Error; err != nil {
		return nil, err
	}
	// 更新会话 updated_at
	r.db.WithContext(ctx).Model(&model.ChatSession{}).Where("id = ?", sessionID).Update("updated_at", time.Now())
	return toBizChatMessage(m), nil
}

func toBizChatMessage(m *model.ChatMessage) *biz.ChatMessage {
	out := &biz.ChatMessage{
		ID:        m.ID,
		SessionID: m.SessionID,
		Role:      m.Role,
		Content:   m.Content,
		Active:    m.Active,
		CreatedAt: m.CreatedAt,
	}
	if len(m.Metadata) > 0 {
		out.Metadata = map[string]any(m.Metadata)
	}
	return out
}

func (r *chatMessageRepo) ListBySession(ctx context.Context, sessionID string, limit int) ([]*biz.ChatMessage, error) {
	if limit <= 0 {
		limit = 100
	}
	var rows []model.ChatMessage
	if err := r.db.WithContext(ctx).
		Where("session_id = ? AND active = ?", sessionID, true).
		Order("created_at ASC").
		Limit(limit).
		Find(&rows).Error; err != nil {
		return nil, err
	}
	items := make([]*biz.ChatMessage, len(rows))
	for i := range rows {
		items[i] = toBizChatMessage(&rows[i])
	}
	return items, nil
}

func (r *chatMessageRepo) GetByID(ctx context.Context, messageID string) (*biz.ChatMessage, error) {
	var m model.ChatMessage
	if err := r.db.WithContext(ctx).Where("id = ?", messageID).First(&m).Error; err != nil {
		if err == gorm.ErrRecordNotFound {
			return nil, ErrNotFound
		}
		return nil, err
	}
	return toBizChatMessage(&m), nil
}

// SoftDeactivateAfter sets active=false for the anchor message and all later
// messages (created_at > afterCreatedAt). Remaining transcript ends before the anchor.
func (r *chatMessageRepo) SoftDeactivateAfter(ctx context.Context, sessionID string, afterCreatedAt time.Time, includeMessageID string) ([]string, error) {
	var rows []model.ChatMessage
	q := r.db.WithContext(ctx).
		Where("session_id = ? AND active = ?", sessionID, true).
		Where("created_at > ? OR id = ?", afterCreatedAt, includeMessageID)
	if err := q.Find(&rows).Error; err != nil {
		return nil, err
	}
	if len(rows) == 0 {
		return nil, nil
	}
	ids := make([]string, 0, len(rows))
	for _, row := range rows {
		ids = append(ids, row.ID)
	}
	if err := r.db.WithContext(ctx).Model(&model.ChatMessage{}).
		Where("id IN ?", ids).
		Update("active", false).Error; err != nil {
		return nil, err
	}
	return ids, nil
}

func (r *chatSessionRepo) messageRepo() *chatMessageRepo {
	return &chatMessageRepo{db: r.db, log: r.log}
}

func (r *chatMessageRepo) LastUserOrAssistantBySessions(ctx context.Context, sessionIDs []string) (map[string]string, error) {
	out := make(map[string]string, len(sessionIDs))
	if len(sessionIDs) == 0 {
		return out, nil
	}
	// One round-trip for the page: latest user/assistant message per session.
	type previewRow struct {
		SessionID string `gorm:"column:session_id"`
		Content   string `gorm:"column:content"`
	}
	var rows []previewRow
	err := r.db.WithContext(ctx).Raw(`
SELECT m.session_id, m.content
FROM chat_messages m
INNER JOIN (
	SELECT session_id, MAX(created_at) AS max_created
	FROM chat_messages
	WHERE session_id IN ? AND role IN ('user', 'assistant') AND active = 1
	GROUP BY session_id
) latest ON latest.session_id = m.session_id AND latest.max_created = m.created_at
WHERE m.session_id IN ? AND m.role IN ('user', 'assistant') AND m.active = 1
`, sessionIDs, sessionIDs).Scan(&rows).Error
	if err != nil {
		return nil, err
	}
	for _, row := range rows {
		if _, exists := out[row.SessionID]; exists {
			continue
		}
		out[row.SessionID] = row.Content
	}
	return out, nil
}

func (r *chatMessageRepo) DeleteBySession(ctx context.Context, sessionID string) error {
	return r.db.WithContext(ctx).Where("session_id = ?", sessionID).Delete(&model.ChatMessage{}).Error
}
