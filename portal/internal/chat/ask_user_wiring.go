package chat

import (
	"github.com/sixath/framework/tool"
)

// RegisterAskUserTools 注册 ask_user 工具（进程内共享 store）。
func RegisterAskUserTools(reg *tool.Registry) error {
	if reg == nil {
		return nil
	}
	return tool.RegisterAskUserTool(reg, &tool.AskUserConfig{
		PendingStore:     defaultAskUserPending,
		FulfillmentStore: defaultAskUserFulfill,
		TokenGen:         tool.RandomTokenGenerator{},
		TTLSeconds:       600,
		GuardConfig: &tool.AskUserGuardConfig{
			MinScore:    2.0,
			ExemptKinds: []string{"confirm", "select"},
		},
	})
}

var (
	defaultAskUserPending = tool.NewInMemoryAskUserPendingStore()
	defaultAskUserFulfill = tool.NewInMemoryAskUserFulfillmentStore()
)

func AskUserPendingStore() tool.AskUserPendingStore {
	return defaultAskUserPending
}

func AskUserFulfillmentStore() tool.AskUserFulfillmentStore {
	return defaultAskUserFulfill
}
