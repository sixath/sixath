package service

import (
	"backend/internal/chat"
	"backend/internal/data"
)

// WireMemoryHubFromData installs MySQL BindingStore and initializes the local Catalog.
// UnitWriter is wired later in newChatService once BuildMemoryStore is ready
// (SetHubUnitWriter resets hubReady so InitLocalMemoryHub rebuilds with UnitsWrite).
func WireMemoryHubFromData(d *data.Data) {
	if d != nil && d.DB() != nil {
		chat.SetHubBindingStore(data.NewMySQLBindingStore(d.DB()))
	}
	chat.InitLocalMemoryHub()
}
