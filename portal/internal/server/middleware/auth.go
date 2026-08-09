package middleware

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"strings"

	"backend/internal/biz"

	"github.com/go-kratos/kratos/v2/errors"
	"github.com/go-kratos/kratos/v2/middleware"
	khttp "github.com/go-kratos/kratos/v2/transport/http"
)

// TokenLookup resolves a SHA-256 bearer token hash to its authenticated user.
type TokenLookup interface {
	UserIDByTokenHash(ctx context.Context, tokenHash string) (string, error)
}

// OrgMembership lists the organizations a user can act within.
type OrgMembership interface {
	UserOrgIDs(ctx context.Context, userID string) ([]string, error)
}

// Auth authenticates bearer tokens and, when provided, validates the active organization.
func Auth(lookup TokenLookup, orgChecker OrgMembership) middleware.Middleware {
	return func(handler middleware.Handler) middleware.Handler {
		return func(ctx context.Context, req interface{}) (interface{}, error) {
			request, ok := khttp.RequestFromServerContext(ctx)
			if !ok || request == nil {
				return nil, errors.Unauthorized("UNAUTHORIZED", "HTTP request required")
			}
			path := request.URL.Path
			if path == "/metrics" ||
				strings.HasPrefix(path, webhookPrefix) ||
				strings.HasPrefix(path, authPublicPrefix) ||
				isRuntimePath(path) {
				// /runtime/v1 uses explicit runtime.Auth (service token), not user sessions.
				return handler(ctx, req)
			}

			token, ok := bearerToken(request.Header.Get("Authorization"))
			if !ok {
				return nil, errors.Unauthorized("UNAUTHORIZED", "valid bearer token required")
			}
			sum := sha256.Sum256([]byte(token))
			userID, err := lookup.UserIDByTokenHash(ctx, hex.EncodeToString(sum[:]))
			if err != nil || userID == "" {
				return nil, errors.Unauthorized("UNAUTHORIZED", "invalid bearer token")
			}
			ctx = biz.WithCallerUserID(ctx, userID)

			if orgID := strings.TrimSpace(request.Header.Get("X-Org-Id")); orgID != "" {
				orgIDs, err := orgChecker.UserOrgIDs(ctx, userID)
				if err != nil || !containsOrgID(orgIDs, orgID) {
					return nil, biz.ErrInvalidOrgContext
				}
				ctx = biz.WithOrgID(ctx, orgID)
			}
			return handler(ctx, req)
		}
	}
}

func bearerToken(authorization string) (string, bool) {
	const prefix = "Bearer "
	if !strings.HasPrefix(authorization, prefix) {
		return "", false
	}
	token := strings.TrimSpace(strings.TrimPrefix(authorization, prefix))
	return token, token != ""
}

// isRuntimePath matches /runtime/v1 and /runtime/v1/..., but not /runtime/v10.
func isRuntimePath(path string) bool {
	return path == runtimePrefix || strings.HasPrefix(path, runtimePrefix+"/")
}

func containsOrgID(orgIDs []string, orgID string) bool {
	for _, candidate := range orgIDs {
		if candidate == orgID {
			return true
		}
	}
	return false
}
