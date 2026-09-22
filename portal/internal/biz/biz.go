package biz

import "github.com/google/wire"

// ProviderSet is biz providers.
var ProviderSet = wire.NewSet(NewToolUsecase, NewMcpServerUsecase, NewProxyUsecase, NewAgentUsecase, NewProjectUsecase, NewSkillResourceUsecase, NewChatUsecase, NewChannelUsecase, NewChannelPeerUsecase, NewCronUsecase, ProvideAccessChecker, ProvideACLAPIUsecase, ProvideAuthUsecase)
