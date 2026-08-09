package server

import (
	"backend/internal/chat"

	kratoshttp "github.com/go-kratos/kratos/v2/transport/http"
	kratosErrors "github.com/go-kratos/kratos/v2/errors"
)

// MemoryHubCatalogHandler returns registered governance/knowledge provider names.
func MemoryHubCatalogHandler() func(kratoshttp.Context) error {
	return func(ctx kratoshttp.Context) error {
		if !chat.MemoryHubReady() {
			chat.InitLocalMemoryHub()
		}
		snap, err := chat.ListCatalog()
		if err != nil {
			return kratosErrors.InternalServer("HUB_CATALOG", err.Error())
		}
		return ctx.JSON(200, snap)
	}
}
