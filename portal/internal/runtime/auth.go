package runtime

import (
	"context"
	"crypto/sha256"
	"crypto/subtle"
	"strings"

	"backend/internal/biz"

	"github.com/go-kratos/kratos/v2/errors"
	"github.com/go-kratos/kratos/v2/middleware"
	khttp "github.com/go-kratos/kratos/v2/transport/http"
)

// HeaderUserID is the Gateway-asserted caller identity for Runtime requests.
const HeaderUserID = "X-Sath-User-Id"

// Auth accepts only the configured Runtime service token as Bearer.
// User session tokens are never trusted as the Runtime trust root.
// When X-Sath-User-Id is present, it is stored as the caller user id.
func Auth(serviceToken string) middleware.Middleware {
	expected := strings.TrimSpace(serviceToken)
	return func(handler middleware.Handler) middleware.Handler {
		return func(ctx context.Context, req interface{}) (interface{}, error) {
			request, ok := khttp.RequestFromServerContext(ctx)
			if !ok || request == nil {
				return nil, errors.Unauthorized("UNAUTHORIZED", "HTTP request required")
			}
			if expected == "" {
				return nil, errors.Unauthorized("UNAUTHORIZED", "runtime service token not configured")
			}
			token, ok := bearerToken(request.Header.Get("Authorization"))
			if !ok || !serviceTokenEqual(expected, token) {
				return nil, errors.Unauthorized("UNAUTHORIZED", "valid runtime service token required")
			}
			if userID := strings.TrimSpace(request.Header.Get(HeaderUserID)); userID != "" {
				ctx = biz.WithCallerUserID(ctx, userID)
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

// serviceTokenEqual compares tokens in constant time via fixed-length SHA-256 digests.
func serviceTokenEqual(expected, got string) bool {
	a := sha256.Sum256([]byte(expected))
	b := sha256.Sum256([]byte(got))
	return subtle.ConstantTimeCompare(a[:], b[:]) == 1
}
