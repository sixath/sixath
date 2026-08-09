package server

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"strings"

	"github.com/go-kratos/kratos/v2/errors"
	kratoshttp "github.com/go-kratos/kratos/v2/transport/http"
)

// meTokenLookup resolves a SHA-256 bearer token hash to a user id.
// Same contract as middleware.TokenLookup; duplicated here to avoid an import cycle
// if handlers ever grow dependencies on middleware helpers.
type meTokenLookup interface {
	UserIDByTokenHash(ctx context.Context, tokenHash string) (string, error)
}

// AuthMeHandler returns GET /api/v1/auth/me — opaque Bearer → { user_id }.
// The path sits under /api/v1/auth/ which skips global Auth, so this handler
// must authenticate itself via UserIDByTokenHash.
func AuthMeHandler(lookup meTokenLookup) func(kratoshttp.Context) error {
	return func(ctx kratoshttp.Context) error {
		req := ctx.Request()
		if req == nil {
			return errors.Unauthorized("UNAUTHORIZED", "HTTP request required")
		}
		token, ok := bearerTokenFromHeader(req.Header.Get("Authorization"))
		if !ok {
			return errors.Unauthorized("UNAUTHORIZED", "valid bearer token required")
		}
		sum := sha256.Sum256([]byte(token))
		userID, err := lookup.UserIDByTokenHash(ctx, hex.EncodeToString(sum[:]))
		if err != nil || userID == "" {
			return errors.Unauthorized("UNAUTHORIZED", "invalid bearer token")
		}
		return ctx.JSON(200, map[string]any{"user_id": userID})
	}
}

func bearerTokenFromHeader(authorization string) (string, bool) {
	const prefix = "Bearer "
	if !strings.HasPrefix(authorization, prefix) {
		return "", false
	}
	token := strings.TrimSpace(strings.TrimPrefix(authorization, prefix))
	return token, token != ""
}
