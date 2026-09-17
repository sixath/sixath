package chat

import (
	"context"
	"errors"

	"backend/internal/biz"

	kratosErrors "github.com/go-kratos/kratos/v2/errors"
)

var (
	ErrModelProviderUnavailable = kratosErrors.BadRequest("MODEL_PROVIDER_UNAVAILABLE", "model provider unavailable")
	ErrModelChoiceUnavailable   = kratosErrors.BadRequest("MODEL_CHOICE_UNAVAILABLE", "model choice unavailable")
	ErrModelChoiceInvalid       = kratosErrors.BadRequest("MODEL_CHOICE_INVALID", "model choice invalid")
)

// TurnModelLoader loads provider secrets and catalog usability for a session overlay.
// HasUsableEntry is true only when the store finds an enabled provider, non-empty key,
// !hidden catalog row, and exact model match. The resolver trusts the bool.
type TurnModelLoader interface {
	GetProviderSecret(ctx context.Context, id string) (kind, baseURL, apiKey string, enabled bool, err error)
	HasUsableEntry(ctx context.Context, providerID, model string) (bool, error)
}

// ResolveTurnModelConfig builds the per-turn ModelConfig.
// If sess is nil, treat as no overlay (agent default) so SendMessage stays robust.
func ResolveTurnModelConfig(ctx context.Context, loader TurnModelLoader, agent *biz.AgentMeta, sess *biz.ChatSession) (biz.ModelConfig, error) {
	if agent == nil {
		return biz.ModelConfig{}, errors.New("chat: agent is required")
	}

	var providerID, model string
	if sess != nil {
		providerID = sess.ModelProviderID
		model = sess.Model
	}

	if providerID != "" && model != "" {
		if loader == nil {
			return biz.ModelConfig{}, ErrModelProviderUnavailable
		}
		kind, baseURL, apiKey, enabled, err := loader.GetProviderSecret(ctx, providerID)
		if err != nil || !enabled || apiKey == "" {
			return biz.ModelConfig{}, ErrModelProviderUnavailable
		}
		provider, ok := factoryProvider(kind)
		if !ok {
			return biz.ModelConfig{}, ErrModelProviderUnavailable
		}
		usable, err := loader.HasUsableEntry(ctx, providerID, model)
		if err != nil {
			return biz.ModelConfig{}, err
		}
		if !usable {
			return biz.ModelConfig{}, ErrModelChoiceUnavailable
		}
		return biz.ModelConfig{
			Provider:        provider,
			Model:           model,
			APIKey:          apiKey,
			BaseURL:         baseURL,
			MaxOutputTokens: agent.ModelConfig.MaxOutputTokens,
		}, nil
	}
	if providerID != "" || model != "" {
		return biz.ModelConfig{}, ErrModelChoiceInvalid
	}
	return agent.ModelConfig, nil
}

func factoryProvider(kind string) (string, bool) {
	switch kind {
	case "openai_compat":
		return "openai", true
	case "dashscope":
		return "dashscope", true
	default:
		return "", false
	}
}
