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
		ID:               id,
		ChannelID:        ch.ChannelID,
		Type:             ch.Type,
		DefaultAgent:     ch.DefaultAgent,
		AllowedAgents:    model.StringSlice(ch.AllowedAgents),
		Enabled:          ch.Enabled,
		WebhookPath:      ch.WebhookPath,
		WebhookSecret:    ch.WebhookSecret,
		IPWhitelist:      model.StringSlice(ch.IPWhitelist),
		AppToken:         ch.AppToken,
		DefaultUids:      model.StringSlice(ch.DefaultUids),
		WebhookURL:       ch.WebhookURL,
		BotID:            ch.BotID,
		BotSecret:        ch.BotSecret,
		BotNames:         model.StringSlice(ch.BotNames),
		WSURL:            ch.WSURL,
		CorpID:           ch.CorpID,
		CorpSecret:       ch.CorpSecret,
		DefaultReplyMode: ch.DefaultReplyMode,
	}
	// GORM skips zero-value fields with DB defaults on Create; wrap Create+enabled
	// correction in one transaction so a failed second step cannot leave enabled=true.
	err := r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := tx.Create(m).Error; err != nil {
			return err
		}
		if !ch.Enabled {
			if err := tx.Model(&model.Channel{}).Where("id = ?", id).Update("enabled", false).Error; err != nil {
				return err
			}
			m.Enabled = false
		}
		return nil
	})
	if err != nil {
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

// ListGatewayChannels returns webhook/wecom_bot channels including disabled, with plaintext secrets.
func (r *channelRepo) ListGatewayChannels(ctx context.Context) ([]*biz.ChannelMeta, error) {
	var list []model.Channel
	if err := r.db.WithContext(ctx).
		Where("type IN ?", []string{"webhook", "wecom_bot"}).
		Order("created_at DESC").
		Find(&list).Error; err != nil {
		return nil, err
	}
	out := make([]*biz.ChannelMeta, len(list))
	for i := range list {
		out[i] = channelModelToBiz(&list[i])
	}
	return out, nil
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
		"bot_id": true, "bot_secret": true, "bot_names": true, "ws_url": true,
		"corp_id": true, "corp_secret": true, "default_reply_mode": true,
	}
	upd := make(map[string]interface{})
	for k, v := range updates {
		if !allowed[k] {
			continue
		}
		if k == "ip_whitelist" || k == "default_uids" || k == "allowed_agents" || k == "bot_names" {
			if sl, ok := coerceUpdateStringSlice(v); ok {
				upd[k] = sl
			}
			continue
		}
		upd[k] = v
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

func coerceUpdateStringSlice(v any) (model.StringSlice, bool) {
	switch x := v.(type) {
	case []string:
		return model.StringSlice(x), true
	case model.StringSlice:
		return x, true
	case []any:
		out := make([]string, 0, len(x))
		for _, item := range x {
			s, ok := item.(string)
			if !ok {
				return nil, false
			}
			out = append(out, s)
		}
		return model.StringSlice(out), true
	default:
		return nil, false
	}
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
	botNames := []string(m.BotNames)
	if botNames == nil {
		botNames = []string{}
	}
	return &biz.ChannelMeta{
		ID:               m.ID,
		ChannelID:        m.ChannelID,
		Type:             m.Type,
		DefaultAgent:     m.DefaultAgent,
		AllowedAgents:    allowedAgents,
		Enabled:          m.Enabled,
		WebhookPath:      m.WebhookPath,
		WebhookSecret:    m.WebhookSecret,
		IPWhitelist:      ipList,
		AppToken:         m.AppToken,
		DefaultUids:      uids,
		WebhookURL:       m.WebhookURL,
		BotID:            m.BotID,
		BotSecret:        m.BotSecret,
		BotNames:         botNames,
		WSURL:            m.WSURL,
		CorpID:           m.CorpID,
		CorpSecret:       m.CorpSecret,
		DefaultReplyMode: m.DefaultReplyMode,
		CreatedAt:        m.CreatedAt,
		UpdatedAt:        m.UpdatedAt,
	}
}
