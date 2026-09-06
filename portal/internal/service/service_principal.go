package service

import (
	"strings"

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
