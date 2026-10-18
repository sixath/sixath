package data

import (
	"context"
	"errors"
	"time"

	"backend/internal/biz"
	"backend/internal/data/model"

	"github.com/go-kratos/kratos/v2/log"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

var _ biz.WeixinIlinkRepo = (*weixinIlinkRepo)(nil)

type weixinIlinkRepo struct {
	db  *gorm.DB
	log *log.Helper
}

// NewWeixinIlinkRepo creates the GORM weixin_ilink repository.
func NewWeixinIlinkRepo(data *Data, logger log.Logger) biz.WeixinIlinkRepo {
	if data == nil || data.db == nil {
		panic("NewWeixinIlinkRepo: Data.db is nil, database config required")
	}
	return &weixinIlinkRepo{db: data.db, log: log.NewHelper(logger)}
}

var ilinkPeerKey = []clause.Column{{Name: "channel_uuid"}, {Name: "peer_id"}}

// ilinkGeneration row-locks the channel so a concurrent ConfirmLogin cannot slip between check and write.
func ilinkGeneration(tx *gorm.DB, channelUUID string) (int, error) {
	var ch model.Channel
	err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).
		Select("id", "ilink_generation").Where("id = ?", channelUUID).First(&ch).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return 0, ErrNotFound
	}
	if err != nil {
		return 0, err
	}
	return ch.IlinkGeneration, nil
}

func (r *weixinIlinkRepo) ConfirmLogin(ctx context.Context, channelUUID string, in biz.IlinkLoginResult, now time.Time) (*biz.ChannelMeta, error) {
	var out model.Channel
	err := r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		gen, err := ilinkGeneration(tx, channelUUID)
		if err != nil {
			return err
		}
		if err := tx.Model(&model.Channel{}).Where("id = ?", channelUUID).Updates(map[string]any{
			"ilink_bot_token":          in.BotToken,
			"ilink_bot_id":             in.BotID,
			"ilink_owner_user_id":      in.OwnerUserID,
			"ilink_base_url":           in.BaseURL,
			"ilink_generation":         gen + 1,
			"ilink_session_expired_at": nil,
			"updated_at":               now,
		}).Error; err != nil {
			return err
		}
		if err := tx.Where("channel_uuid = ?", channelUUID).Delete(&model.ChannelIlinkState{}).Error; err != nil {
			return err
		}
		if err := tx.Model(&model.ChannelIlinkPeer{}).Where("channel_uuid = ?", channelUUID).Updates(map[string]any{
			"context_token":    "",
			"context_token_at": nil,
			"updated_at":       now,
		}).Error; err != nil {
			return err
		}
		if in.OwnerUserID != "" {
			owner := model.ChannelIlinkPeer{ChannelUUID: channelUUID, PeerID: in.OwnerUserID, Allowed: true, CreatedAt: now, UpdatedAt: now}
			if err := tx.Clauses(clause.OnConflict{
				Columns:   ilinkPeerKey,
				DoUpdates: clause.Assignments(map[string]any{"allowed": true, "updated_at": now}),
			}).Create(&owner).Error; err != nil {
				return err
			}
		}
		return tx.Where("id = ?", channelUUID).First(&out).Error
	})
	if err != nil {
		return nil, err
	}
	return channelModelToBiz(&out), nil
}

func (r *weixinIlinkRepo) MarkSessionExpired(ctx context.Context, channelUUID string, generation int, now time.Time) (bool, error) {
	applied := false
	err := r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		res := tx.Model(&model.Channel{}).
			Where("id = ? AND ilink_generation = ?", channelUUID, generation).
			Updates(map[string]any{"ilink_session_expired_at": now, "updated_at": now})
		if res.Error != nil {
			return res.Error
		}
		if res.RowsAffected == 0 {
			return nil
		}
		applied = true
		return tx.Where("channel_uuid = ?", channelUUID).Delete(&model.ChannelIlinkState{}).Error
	})
	return applied, err
}

func (r *weixinIlinkRepo) UpsertPeerReport(ctx context.Context, channelUUID string, generation int, in biz.IlinkPeerReport) (*biz.IlinkPeer, error) {
	var out model.ChannelIlinkPeer
	err := r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		gen, err := ilinkGeneration(tx, channelUUID)
		if err != nil {
			return err
		}
		if gen != generation {
			return biz.ErrIlinkGenerationMismatch
		}
		at := in.At
		row := model.ChannelIlinkPeer{
			ChannelUUID:   channelUUID,
			PeerID:        in.PeerID,
			LastInboundAt: &at,
			LastPreview:   in.Preview,
			CreatedAt:     at,
			UpdatedAt:     at,
		}
		updates := map[string]any{"last_inbound_at": at, "last_preview": in.Preview, "updated_at": at}
		if in.ContextToken != "" {
			row.ContextToken = in.ContextToken
			row.ContextTokenAt = &at
			updates["context_token"] = in.ContextToken
			updates["context_token_at"] = at
		}
		if err := tx.Clauses(clause.OnConflict{Columns: ilinkPeerKey, DoUpdates: clause.Assignments(updates)}).Create(&row).Error; err != nil {
			return err
		}
		return tx.Where("channel_uuid = ? AND peer_id = ?", channelUUID, in.PeerID).First(&out).Error
	})
	if err != nil {
		return nil, err
	}
	return ilinkPeerModelToBiz(&out), nil
}

func (r *weixinIlinkRepo) ListPeers(ctx context.Context, channelUUID string) ([]*biz.IlinkPeer, error) {
	var rows []model.ChannelIlinkPeer
	if err := r.db.WithContext(ctx).Where("channel_uuid = ?", channelUUID).
		Order("updated_at DESC").Order("peer_id").Find(&rows).Error; err != nil {
		return nil, err
	}
	out := make([]*biz.IlinkPeer, 0, len(rows))
	for i := range rows {
		out = append(out, ilinkPeerModelToBiz(&rows[i]))
	}
	return out, nil
}

func (r *weixinIlinkRepo) UpdatePeer(ctx context.Context, channelUUID, peerID string, patch biz.IlinkPeerPatch) (*biz.IlinkPeer, error) {
	var out model.ChannelIlinkPeer
	err := r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		where := tx.Where("channel_uuid = ? AND peer_id = ?", channelUUID, peerID)
		if err := where.First(&out).Error; err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return ErrNotFound
			}
			return err
		}
		updates := map[string]any{"updated_at": time.Now()}
		if patch.Allowed != nil {
			updates["allowed"] = *patch.Allowed
		}
		if patch.Remark != nil {
			updates["remark"] = *patch.Remark
		}
		if err := tx.Model(&model.ChannelIlinkPeer{}).Where("channel_uuid = ? AND peer_id = ?", channelUUID, peerID).Updates(updates).Error; err != nil {
			return err
		}
		return tx.Where("channel_uuid = ? AND peer_id = ?", channelUUID, peerID).First(&out).Error
	})
	if err != nil {
		return nil, err
	}
	return ilinkPeerModelToBiz(&out), nil
}

func (r *weixinIlinkRepo) DeletePeer(ctx context.Context, channelUUID, peerID string) error {
	res := r.db.WithContext(ctx).Where("channel_uuid = ? AND peer_id = ?", channelUUID, peerID).Delete(&model.ChannelIlinkPeer{})
	if res.Error != nil {
		return res.Error
	}
	if res.RowsAffected == 0 {
		return ErrNotFound
	}
	return nil
}

func (r *weixinIlinkRepo) ListAllowedPeerIDs(ctx context.Context, channelUUID string) ([]string, error) {
	var ids []string
	if err := r.db.WithContext(ctx).Model(&model.ChannelIlinkPeer{}).
		Where("channel_uuid = ? AND allowed = ?", channelUUID, true).
		Order("peer_id").Pluck("peer_id", &ids).Error; err != nil {
		return nil, err
	}
	return ids, nil
}

func (r *weixinIlinkRepo) GetCursor(ctx context.Context, channelUUID string) (*biz.IlinkCursor, error) {
	var row model.ChannelIlinkState
	err := r.db.WithContext(ctx).Where("channel_uuid = ?", channelUUID).First(&row).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return &biz.IlinkCursor{}, nil
	}
	if err != nil {
		return nil, err
	}
	return &biz.IlinkCursor{Generation: row.Generation, GetUpdatesBuf: row.GetUpdatesBuf}, nil
}

func (r *weixinIlinkRepo) PutCursor(ctx context.Context, channelUUID string, cur biz.IlinkCursor, now time.Time) error {
	return r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		gen, err := ilinkGeneration(tx, channelUUID)
		if err != nil {
			return err
		}
		if gen != cur.Generation {
			return biz.ErrIlinkGenerationMismatch
		}
		row := model.ChannelIlinkState{ChannelUUID: channelUUID, Generation: cur.Generation, GetUpdatesBuf: cur.GetUpdatesBuf, UpdatedAt: now}
		return tx.Clauses(clause.OnConflict{
			Columns:   []clause.Column{{Name: "channel_uuid"}},
			DoUpdates: clause.AssignmentColumns([]string{"generation", "get_updates_buf", "updated_at"}),
		}).Create(&row).Error
	})
}

func ilinkPeerModelToBiz(m *model.ChannelIlinkPeer) *biz.IlinkPeer {
	return &biz.IlinkPeer{
		ChannelUUID:    m.ChannelUUID,
		PeerID:         m.PeerID,
		Allowed:        m.Allowed,
		Remark:         m.Remark,
		ContextToken:   m.ContextToken,
		ContextTokenAt: m.ContextTokenAt,
		LastInboundAt:  m.LastInboundAt,
		LastPreview:    m.LastPreview,
		CreatedAt:      m.CreatedAt,
		UpdatedAt:      m.UpdatedAt,
	}
}
