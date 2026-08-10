package data

import (
	"context"

	"backend/internal/biz"
	"backend/internal/data/model"

	"github.com/go-kratos/kratos/v2/log"
	"github.com/google/uuid"
	"gorm.io/gorm"
)

var _ biz.ChannelRepo = (*channelRepo)(nil)

type channelRepo struct {
	db  *gorm.DB
	log *log.Helper
}

// NewChannelRepo creates a new ChannelRepo (MySQL/GORM impl)
func NewChannelRepo(data *Data, logger log.Logger) biz.ChannelRepo {
	if data == nil || data.db == nil {
		panic("NewChannelRepo: Data.db is nil")
	}
	return &channelRepo{db: data.db, log: log.NewHelper(logger)}
}

func (r *channelRepo) Create(ctx context.Context, ch *biz.ChannelCreate) (*biz.ChannelMeta, error) {
	id := uuid.New().String()
	m := &model.Channel{
		ID:            id,
		ChannelID:     ch.ChannelID,
		Type:          ch.Type,
		DefaultAgent:  ch.DefaultAgent,
		AllowedAgents: model.StringSlice(ch.AllowedAgents),
		Enabled:       ch.Enabled,
		WebhookPath:   ch.WebhookPath,
		WebhookSecret: ch.WebhookSecret,
		IPWhitelist:   model.StringSlice(ch.IPWhitelist),
		AppToken:      ch.AppToken,
		DefaultUids:   model.StringSlice(ch.DefaultUids),
		WebhookURL:    ch.WebhookURL,
	}
	if err := r.db.WithContext(ctx).Create(m).Error; err != nil {
		if isDuplicateKey(err) {
			return nil, ErrDuplicateName
		}
		return nil, err
	}
	return channelModelToBiz(m), nil
}

func (r *channelRepo) GetByID(ctx context.Context, id string) (*biz.ChannelMeta, error) {
	var m model.Channel
	if err := r.db.WithContext(ctx).Where("id = ?", id).First(&m).Error; err != nil {
		if err == gorm.ErrRecordNotFound {
			return nil, ErrNotFound
		}
		return nil, err
	}
	return channelModelToBiz(&m), nil
}

func (r *channelRepo) GetByChannelID(ctx context.Context, channelID string) (*biz.ChannelMeta, error) {
	var m model.Channel
	if err := r.db.WithContext(ctx).Where("channel_id = ?", channelID).First(&m).Error; err != nil {
		if err == gorm.ErrRecordNotFound {
			return nil, ErrNotFound
		}
		return nil, err
	}
	return channelModelToBiz(&m), nil
}

func (r *channelRepo) GetWecomByDefaultAgent(ctx context.Context, agentID string) (*biz.ChannelMeta, error) {
	if agentID == "" {
		return nil, ErrNotFound
	}
	var m model.Channel
	err := r.db.WithContext(ctx).
		Where("type = ? AND default_agent = ? AND enabled = ?", "wecom", agentID, true).
		Order("updated_at DESC").
		First(&m).Error
	if err != nil {
		if err == gorm.ErrRecordNotFound {
			return nil, ErrNotFound
		}
		return nil, err
	}
	return channelModelToBiz(&m), nil
}

func (r *channelRepo) List(ctx context.Context, page, pageSize int32, typ string, enabled *bool) ([]*biz.ChannelMeta, int, error) {
	q := r.db.WithContext(ctx).Model(&model.Channel{})
	if typ != "" {
		q = q.Where("type = ?", typ)
	}
	if enabled != nil {
		q = q.Where("enabled = ?", *enabled)
	}
	var total int64
	if err := q.Count(&total).Error; err != nil {
		return nil, 0, err
	}
	if page < 1 {
		page = 1
	}
	if pageSize < 1 || pageSize > 100 {
		pageSize = 20
	}
	offset := (page - 1) * pageSize
	var list []model.Channel
	if err := q.Order("created_at DESC").Offset(int(offset)).Limit(int(pageSize)).Find(&list).Error; err != nil {
		return nil, 0, err
	}
	out := make([]*biz.ChannelMeta, len(list))
	for i := range list {
		out[i] = channelModelToBiz(&list[i])
	}
	return out, int(total), nil
}

func (r *channelRepo) Update(ctx context.Context, id string, updates map[string]any) (*biz.ChannelMeta, error) {
	var m model.Channel
	if err := r.db.WithContext(ctx).Where("id = ?", id).First(&m).Error; err != nil {
		if err == gorm.ErrRecordNotFound {
			return nil, ErrNotFound
		}
		return nil, err
	}
	allowed := map[string]bool{
		"channel_id": true, "type": true, "default_agent": true, "allowed_agents": true, "enabled": true,
		"webhook_path": true, "webhook_secret": true, "ip_whitelist": true,
		"app_token": true, "default_uids": true,
		"webhook_url": true,
	}
	upd := make(map[string]interface{})
	for k, v := range updates {
		if allowed[k] {
			if k == "ip_whitelist" || k == "default_uids" || k == "allowed_agents" {
				if sl, ok := v.([]string); ok {
					upd[k] = model.StringSlice(sl)
				}
			} else {
				upd[k] = v
			}
		}
	}
	if len(upd) > 0 {
		if err := r.db.WithContext(ctx).Model(&m).Updates(upd).Error; err != nil {
			return nil, err
		}
	}
	var updated model.Channel
	if err := r.db.WithContext(ctx).Where("id = ?", id).First(&updated).Error; err != nil {
		return nil, err
	}
	return channelModelToBiz(&updated), nil
}

func (r *channelRepo) Delete(ctx context.Context, id string) error {
	res := r.db.WithContext(ctx).Where("id = ?", id).Delete(&model.Channel{})
	if res.Error != nil {
		return res.Error
	}
	if res.RowsAffected == 0 {
		return ErrNotFound
	}
	return nil
}

func channelModelToBiz(m *model.Channel) *biz.ChannelMeta {
	ipList := []string(m.IPWhitelist)
	if ipList == nil {
		ipList = []string{}
	}
	uids := []string(m.DefaultUids)
	if uids == nil {
		uids = []string{}
	}
	allowedAgents := []string(m.AllowedAgents)
	if allowedAgents == nil {
		allowedAgents = []string{}
	}
	return &biz.ChannelMeta{
		ID:            m.ID,
		ChannelID:     m.ChannelID,
		Type:          m.Type,
		DefaultAgent:  m.DefaultAgent,
		AllowedAgents: allowedAgents,
		Enabled:       m.Enabled,
		WebhookPath:   m.WebhookPath,
		WebhookSecret: m.WebhookSecret,
		IPWhitelist:   ipList,
		AppToken:      m.AppToken,
		DefaultUids:   uids,
		WebhookURL:    m.WebhookURL,
		CreatedAt:     m.CreatedAt,
		UpdatedAt:     m.UpdatedAt,
	}
}
