package service

import (
	"context"
	"strings"

	"backend/internal/biz"
	"backend/internal/conf"
)

const defaultServicePrincipalUserID = "bootstrap"

func servicePrincipalUserID(auth *conf.Auth) string {
	if auth == nil {
		return defaultServicePrincipalUserID
	}
	if userID := strings.TrimSpace(auth.GetServicePrincipalUserId()); userID != "" {
		return userID
	}
	if userID := strings.TrimSpace(auth.GetBootstrapUserId()); userID != "" {
		return userID
	}
	return defaultServicePrincipalUserID
}

func (w *GrowthWorker) internalContext(ctx context.Context) context.Context {
	if ctx == nil {
		ctx = context.Background()
	}
	return biz.WithCallerUserID(ctx, w.servicePrincipalUserID)
}
