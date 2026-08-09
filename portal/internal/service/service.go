package service

import "github.com/google/wire"

// ProviderSet is service providers.
var ProviderSet = wire.NewSet(NewToolService, NewMcpServerService, NewAgentService, NewChatService, NewChannelService, NewGrowthWorker, NewCuratorWorker)
