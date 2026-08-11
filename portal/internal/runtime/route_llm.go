package runtime

import (
	"context"
	"strings"

	"backend/internal/biz"
	"backend/internal/conf"

	"github.com/go-kratos/kratos/v2/log"
	"github.com/sixath/framework/model"
)

// routeModelCompleter adapts framework model.Model to biz.RouteCompleter.
type routeModelCompleter struct {
	m model.Model
}

func (c *routeModelCompleter) Complete(ctx context.Context, prompt string) (string, error) {
	gen, err := c.m.Generate(ctx, prompt, model.WithTemperature(0), model.WithMaxTokens(128))
	if err != nil {
		return "", err
	}
	if gen == nil {
		return "", nil
	}
	return gen.Text, nil
}

// NewRouteCompleter builds a classifier Completer from growth.llm (auxiliary-first).
// Returns nil when LLM is not configured so Route fail-opens without crashing boot.
func NewRouteCompleter(growthCfg *conf.Growth, logger log.Logger) biz.RouteCompleter {
	helper := log.NewHelper(logger)
	if growthCfg == nil || growthCfg.GetLlm() == nil {
		helper.Info("runtime route: growth.llm unset; classifier disabled (fail-open)")
		return nil
	}
	cfg := growthCfg.GetLlm()
	target := cfg
	if aux := cfg.GetAuxiliary(); aux != nil && strings.TrimSpace(aux.GetModel()) != "" {
		target = aux
	}
	if strings.TrimSpace(target.GetModel()) == "" {
		helper.Info("runtime route: growth.llm.model empty; classifier disabled (fail-open)")
		return nil
	}
	m, err := model.NewModelFromConfig(model.ModelConfig{
		Provider: target.GetProvider(),
		Model:    target.GetModel(),
		APIKey:   target.GetApiKey(),
		BaseURL:  target.GetBaseUrl(),
	})
	if err != nil {
		helper.Warnf("runtime route: failed to build classifier model: %v", err)
		return nil
	}
	helper.Infof("runtime route: classifier model=%s provider=%s", target.GetModel(), target.GetProvider())
	return &routeModelCompleter{m: m}
}

// ProvideAgentRouteUsecase wires AgentRouteUsecase for the runtime /route endpoint.
func ProvideAgentRouteUsecase(
	channels biz.ChannelRepo,
	peers biz.ChannelPeerSessionRepo,
	agents *biz.AgentUsecase,
	growthCfg *conf.Growth,
	logger log.Logger,
) *biz.AgentRouteUsecase {
	return biz.NewAgentRouteUsecase(channels, peers, agents, NewRouteCompleter(growthCfg, logger), 0)
}
