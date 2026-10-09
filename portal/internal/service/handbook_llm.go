package service

import (
	"context"
	"strings"

	"backend/internal/biz"
	"backend/internal/chat"
	"backend/internal/conf"

	"github.com/sixath/framework/model"
)

// ConfigureHandbookLLM enables the handbook LLM layer with models from the catalog. It does so
// even without a global model, since repositories may name their own.
func ConfigureHandbookLLM(uc *biz.HandbookUsecase, cfg *conf.HandbookConfig, cat criticModelCatalog) {
	if uc == nil || cfg == nil || cat == nil {
		return
	}
	resolve := catalogModelResolver(cat, chat.BuildModel)
	uc.SetLLM(biz.HandbookLLMConfig{
		Model: strings.TrimSpace(cfg.Model), Concurrency: cfg.Concurrency, MaxCardsPerRun: cfg.MaxCardsPerRun,
		MaxFileKB: cfg.MaxFileKB, MaxRunMinutes: cfg.MaxRunMinutes, SkeletonRebuildDays: cfg.SkeletonRebuildDays,
	}, func(ctx context.Context, name string) (model.Model, error) {
		ctx, cancel := context.WithTimeout(ctx, criticModelLookupTimeout)
		defer cancel()
		return resolve(ctx, name)
	})
}
