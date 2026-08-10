//go:build wireinject
// +build wireinject

// The build tag makes sure the stub is not built in the final build.

package main

import (
	"backend/internal/biz"
	"backend/internal/conf"
	"backend/internal/cron"
	"backend/internal/data"
	"backend/internal/runtime"
	"backend/internal/server"
	"backend/internal/service"

	"github.com/go-kratos/kratos/v2"
	"github.com/go-kratos/kratos/v2/log"
	"github.com/google/wire"
)

// wireApp init kratos application.
func wireApp(*conf.Server, *conf.Data, *conf.Auth, llmReviewEnabledInput, growthReviewPatchFileInput, curatorPatchFileInput, workerEnabledInput, *conf.Growth, log.Logger) (*kratos.App, func(), error) {
	panic(wire.Build(
		server.ProviderSet,
		data.ProviderSet,
		biz.ProviderSet,
		cron.ProviderSet,
		service.NewToolService,
		service.NewMcpServerService,
		service.NewAgentService,
		service.ProvideChatServiceWithTurnTrace,
		service.NewChannelService,
		runtime.NewService,
		wire.Bind(new(runtime.RewindBackend), new(*service.ChatService)),
		wire.Bind(new(runtime.TurnBackend), new(*service.ChatService)),
		wire.Bind(new(server.DBPinger), new(*data.Data)),
		provideGrowthWorker,
		provideCuratorWorker,
		newApp,
	))
}
