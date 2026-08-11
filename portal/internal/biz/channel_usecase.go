package biz

import (
	"context"
	"errors"
	"strings"

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

func normalizeAllowedAgents(defaultAgent string, allowed []string) ([]string, error) {
	defaultAgent = strings.TrimSpace(defaultAgent)
	out := uniqueTrimmedStrings(allowed)
	if len(out) == 0 {
		return out, nil
	}
	if defaultAgent == "" {
		return nil, kratosErrors.BadRequest("INVALID_ARGUMENT", "default_agent required when allowed_agents is set")
	}
	if !stringSliceContains(out, defaultAgent) {
		return nil, kratosErrors.BadRequest("INVALID_ARGUMENT", "default_agent must be in allowed_agents")
	}
	return out, nil
}

func uniqueTrimmedStrings(ss []string) []string {
	seen := make(map[string]struct{}, len(ss))
	out := make([]string, 0, len(ss))
	for _, s := range ss {
		s = strings.TrimSpace(s)
		if s == "" {
			continue
		}
		if _, ok := seen[s]; ok {
			continue
		}
		seen[s] = struct{}{}
		out = append(out, s)
	}
	return out
}

func stringSliceContains(ss []string, v string) bool {
	for _, s := range ss {
		if s == v {
			return true
		}
	}
	return false
}

func coerceBool(v any) (bool, bool) {
	switch x := v.(type) {
	case bool:
		return x, true
	default:
		return false, false
	}
}

func defaultCreateAutoRouteFlags(ch *ChannelCreate) {
	if !ch.AutoRouteEnabled && !ch.AutoRouteMention && !ch.AutoRouteClassifier {
		ch.AutoRouteEnabled = true
		ch.AutoRouteMention = true
		ch.AutoRouteClassifier = true
	}
}

func normalizeChannelBoolUpdates(updates map[string]any) error {
	for _, k := range []string{"enabled", "auto_route_enabled", "auto_route_mention", "auto_route_classifier"} {
		v, ok := updates[k]
		if !ok {
			continue
		}
		b, ok := coerceBool(v)
		if !ok {
			return kratosErrors.BadRequest("INVALID_ARGUMENT", k+" must be a boolean")
		}
		updates[k] = b
	}
	return nil
}

func coerceStringSlice(v any) ([]string, bool) {
	if v == nil {
		return nil, true
	}
	switch x := v.(type) {
	case []string:
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
		return out, true
	default:
		return nil, false
	}
}

func effectiveChannelAgents(current *ChannelMeta, updates map[string]any) (defaultAgent string, allowed []string, allowedPresent bool, err error) {
	defaultAgent = current.DefaultAgent
	if v, ok := updates["default_agent"].(string); ok {
		defaultAgent = v
	}
	if raw, ok := updates["allowed_agents"]; ok {
		allowedPresent = true
		sl, ok := coerceStringSlice(raw)
		if !ok {
			return "", nil, false, kratosErrors.BadRequest("INVALID_ARGUMENT", "allowed_agents must be a string array")
		}
		allowed = sl
	} else {
		allowed = current.AllowedAgents
	}
	return defaultAgent, allowed, allowedPresent, nil
}

// Create 创建渠道
func (uc *ChannelUsecase) Create(ctx context.Context, ch *ChannelCreate) (*ChannelMeta, error) {
	normalized, err := normalizeAllowedAgents(ch.DefaultAgent, ch.AllowedAgents)
	if err != nil {
		return nil, err
	}
	ch.AllowedAgents = normalized
	defaultCreateAutoRouteFlags(ch)
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
	current, err := uc.repo.GetByID(ctx, id)
	if err != nil {
		if errors.Is(err, pkgErrors.ErrNotFound) {
			return nil, ErrChannelNotFound
		}
		return nil, err
	}
	defaultAgent, allowed, allowedPresent, err := effectiveChannelAgents(current, updates)
	if err != nil {
		return nil, err
	}
	normalized, err := normalizeAllowedAgents(defaultAgent, allowed)
	if err != nil {
		return nil, err
	}
	if allowedPresent {
		updates["allowed_agents"] = normalized
	}
	if err := normalizeChannelBoolUpdates(updates); err != nil {
		return nil, err
	}
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
