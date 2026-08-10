package conf

import (
	"os"
	"strings"
)

// EnrichAuthFromEnv overlays SATH_BOOTSTRAP_* onto auth.
// If auth is nil, a new Auth is allocated when any overlay is present or always
// returned non-nil when env vars are set; if nothing to apply and auth is nil, returns nil.
func EnrichAuthFromEnv(auth *Auth) *Auth {
	email := strings.TrimSpace(os.Getenv("SATH_BOOTSTRAP_EMAIL"))
	password := os.Getenv("SATH_BOOTSTRAP_PASSWORD") // allow intentional spaces; trim only ends
	password = strings.TrimSpace(password)
	token := strings.TrimSpace(os.Getenv("SATH_BOOTSTRAP_TOKEN"))
	if email == "" && password == "" && token == "" {
		return auth
	}
	if auth == nil {
		auth = &Auth{}
	}
	if email != "" {
		auth.BootstrapEmail = email
	}
	if password != "" {
		auth.BootstrapPassword = password
	}
	if token != "" {
		auth.BootstrapToken = token
	}
	return auth
}
