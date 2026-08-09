package runtime

import "strings"

var configuredServiceToken string

// Configure stores the Runtime service token for later route registration.
func Configure(serviceToken string) {
	configuredServiceToken = strings.TrimSpace(serviceToken)
}

// ServiceToken returns the configured Runtime service token.
func ServiceToken() string {
	return configuredServiceToken
}
