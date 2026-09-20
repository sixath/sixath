package data

import (
	"context"
	"time"

	"backend/internal/biz"
	"backend/internal/data/model"

	"github.com/go-kratos/kratos/v2/log"
	"gorm.io/gorm"
)

var _ biz.AttachmentRepo = (*chatAttachmentRepo)(nil)

type chatAttachmentRepo struct {
	db  *gorm.DB
	log *log.Helper
}

// NewChatAttachmentRepo creates AttachmentRepo backed by chat_attachments.
func NewChatAttachmentRepo(data *Data, logger log.Logger) biz.AttachmentRepo {
	return &chatAttachmentRepo{db: data.db, log: log.NewHelper(logger)}
}

func (r *chatAttachmentRepo) Create(ctx context.Context, a *biz.ChatAttachment) error {
	if a == nil || a.ID == "" || a.SessionID == "" {
		return ErrNotFound
	}
	m := &model.ChatAttachment{
		ID:           a.ID,
		SessionID:    a.SessionID,
		Kind:         a.Kind,
		Mime:         a.Mime,
		Name:         a.Name,
		Size:         a.Size,
		RelativePath: a.RelativePath,
		CreatedAt:    a.CreatedAt,
	}
	if m.CreatedAt.IsZero() {
		m.CreatedAt = time.Now()
	}
	if err := r.db.WithContext(ctx).Create(m).Error; err != nil {
		return err
	}
	a.CreatedAt = m.CreatedAt
	return nil
}

func (r *chatAttachmentRepo) Get(ctx context.Context, sessionID, id string) (*biz.ChatAttachment, error) {
	var m model.ChatAttachment
	if err := r.db.WithContext(ctx).Where("session_id = ? AND id = ?", sessionID, id).First(&m).Error; err != nil {
		if err == gorm.ErrRecordNotFound {
			return nil, ErrNotFound
		}
		return nil, err
	}
	return toBizChatAttachment(&m), nil
}

func (r *chatAttachmentRepo) Delete(ctx context.Context, sessionID, id string) error {
	res := r.db.WithContext(ctx).Where("session_id = ? AND id = ?", sessionID, id).Delete(&model.ChatAttachment{})
	if res.Error != nil {
		return res.Error
	}
	if res.RowsAffected == 0 {
		return ErrNotFound
	}
	return nil
}

func (r *chatAttachmentRepo) CountBySession(ctx context.Context, sessionID string) (int64, error) {
	var n int64
	err := r.db.WithContext(ctx).Model(&model.ChatAttachment{}).Where("session_id = ?", sessionID).Count(&n).Error
	return n, err
}

func (r *chatAttachmentRepo) ListByIDs(ctx context.Context, sessionID string, ids []string) ([]*biz.ChatAttachment, error) {
	if len(ids) == 0 {
		return nil, nil
	}
	var rows []model.ChatAttachment
	if err := r.db.WithContext(ctx).Where("session_id = ? AND id IN ?", sessionID, ids).Find(&rows).Error; err != nil {
		return nil, err
	}
	out := make([]*biz.ChatAttachment, len(rows))
	for i := range rows {
		out[i] = toBizChatAttachment(&rows[i])
	}
	return out, nil
}

func toBizChatAttachment(m *model.ChatAttachment) *biz.ChatAttachment {
	if m == nil {
		return nil
	}
	return &biz.ChatAttachment{
		ID:           m.ID,
		SessionID:    m.SessionID,
		Kind:         m.Kind,
		Mime:         m.Mime,
		Name:         m.Name,
		Size:         m.Size,
		RelativePath: m.RelativePath,
		CreatedAt:    m.CreatedAt,
	}
}
