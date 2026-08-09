package biz

import (
	"context"
	"errors"

	pkgErrors "backend/internal/pkg/errors"

	kratosErrors "github.com/go-kratos/kratos/v2/errors"
	"github.com/go-kratos/kratos/v2/log"
)

var (
	ErrChannelNotFound      = kratosErrors.NotFound("CHANNEL_NOT_FOUND", "channel not found")
	ErrChannelDuplicateID   = kratosErrors.Conflict("CHANNEL_DUPLICATE_ID", "channel_id already exists")
	ErrChannelBoundToAgents = kratosErrors.Conflict("CHANNEL_BOUND_TO_AGENTS", "channel is bound to agents")
)

// ChannelUsecase 渠道用例
type ChannelUsecase struct {
	repo      ChannelRepo
	agentRepo AgentRepo
	log       *log.Helper
}

// NewChannelUsecase creates a ChannelUsecase
func NewChannelUsecase(repo ChannelRepo, agentRepo AgentRepo, logger log.Logger) *ChannelUsecase {
	return &ChannelUsecase{repo: repo, agentRepo: agentRepo, log: log.NewHelper(logger)}
}

// Create 创建渠道
func (uc *ChannelUsecase) Create(ctx context.Context, ch *ChannelCreate) (*ChannelMeta, error) {
	meta, err := uc.repo.Create(ctx, ch)
	if err != nil && errors.Is(err, pkgErrors.ErrDuplicateName) {
		return nil, ErrChannelDuplicateID
	}
	return meta, err
}

// Get 获取渠道
func (uc *ChannelUsecase) Get(ctx context.Context, id string) (*ChannelMeta, error) {
	ch, err := uc.repo.GetByID(ctx, id)
	if err != nil && errors.Is(err, pkgErrors.ErrNotFound) {
		return nil, ErrChannelNotFound
	}
	return ch, err
}

// GetByChannelID 按 channel_id 获取
func (uc *ChannelUsecase) GetByChannelID(ctx context.Context, channelID string) (*ChannelMeta, error) {
	ch, err := uc.repo.GetByChannelID(ctx, channelID)
	if err != nil && errors.Is(err, pkgErrors.ErrNotFound) {
		return nil, ErrChannelNotFound
	}
	return ch, err
}

// GetWecomByDefaultAgent 查找 default_agent 指向该 Agent 的已启用 wecom 渠道。
func (uc *ChannelUsecase) GetWecomByDefaultAgent(ctx context.Context, agentID string) (*ChannelMeta, error) {
	ch, err := uc.repo.GetWecomByDefaultAgent(ctx, agentID)
	if err != nil && errors.Is(err, pkgErrors.ErrNotFound) {
		return nil, ErrChannelNotFound
	}
	return ch, err
}

// List 列表
func (uc *ChannelUsecase) List(ctx context.Context, page, pageSize int32, typ string, enabled *bool) ([]*ChannelMeta, int, error) {
	return uc.repo.List(ctx, page, pageSize, typ, enabled)
}

// Update 更新
func (uc *ChannelUsecase) Update(ctx context.Context, id string, updates map[string]any) (*ChannelMeta, error) {
	ch, err := uc.repo.Update(ctx, id, updates)
	if err != nil && errors.Is(err, pkgErrors.ErrNotFound) {
		return nil, ErrChannelNotFound
	}
	return ch, err
}

// Delete 删除
func (uc *ChannelUsecase) Delete(ctx context.Context, id string) error {
	if uc.agentRepo != nil {
		n, err := uc.agentRepo.CountByWecomChannelID(ctx, id)
		if err != nil {
			return err
		}
		if n > 0 {
			return ErrChannelBoundToAgents
		}
	}
	err := uc.repo.Delete(ctx, id)
	if err != nil && errors.Is(err, pkgErrors.ErrNotFound) {
		return ErrChannelNotFound
	}
	return err
}
