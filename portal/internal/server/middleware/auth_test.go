package middleware

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"net/http"
	"net/http/httptest"
	"testing"

	"backend/internal/biz"

	"github.com/go-kratos/kratos/v2/errors"
	"github.com/go-kratos/kratos/v2/transport"
	khttp "github.com/go-kratos/kratos/v2/transport/http"
)

type fakeTokenLookup struct {
	userIDByHash map[string]string
}

func (f fakeTokenLookup) UserIDByTokenHash(_ context.Context, tokenHash string) (string, error) {
	return f.userIDByHash[tokenHash], nil
}

type fakeOrgMembership struct {
	orgIDsByUser map[string][]string
}

func (f fakeOrgMembership) UserOrgIDs(_ context.Context, userID string) ([]string, error) {
	return f.orgIDsByUser[userID], nil
}

type fakeHTTPTransport struct {
	request *http.Request
}

type fakeHeader http.Header

func (h fakeHeader) Get(key string) string { return http.Header(h).Get(key) }
func (h fakeHeader) Set(key, value string) { http.Header(h).Set(key, value) }
func (h fakeHeader) Add(key, value string) { http.Header(h).Add(key, value) }
func (h fakeHeader) Keys() []string {
	keys := make([]string, 0, len(h))
	for key := range h {
		keys = append(keys, key)
	}
	return keys
}
func (h fakeHeader) Values(key string) []string { return http.Header(h).Values(key) }

func (f fakeHTTPTransport) Kind() transport.Kind            { return transport.KindHTTP }
func (f fakeHTTPTransport) Endpoint() string                { return "http://test" }
func (f fakeHTTPTransport) Operation() string               { return f.request.URL.Path }
func (f fakeHTTPTransport) RequestHeader() transport.Header { return fakeHeader(f.request.Header) }
func (f fakeHTTPTransport) ReplyHeader() transport.Header   { return fakeHeader{} }
func (f fakeHTTPTransport) Request() *http.Request          { return f.request }
func (f fakeHTTPTransport) PathTemplate() string            { return f.request.URL.Path }

var _ khttp.Transporter = fakeHTTPTransport{}

func TestAuthSetsCallerAndOrgContext(t *testing.T) {
	token := "secret-token"
	sum := sha256.Sum256([]byte(token))
	tokenHash := hex.EncodeToString(sum[:])
	request := httptest.NewRequest(http.MethodGet, "/api/v1/agents", nil)
	request.Header.Set("Authorization", "Bearer "+token)
	request.Header.Set("X-Org-Id", "org-1")

	var callerUserID, orgID string
	handler := Auth(
		fakeTokenLookup{userIDByHash: map[string]string{tokenHash: "user-1"}},
		fakeOrgMembership{orgIDsByUser: map[string][]string{"user-1": {"org-1"}}},
	)(func(ctx context.Context, _ interface{}) (interface{}, error) {
		callerUserID, _ = biz.CallerUserID(ctx)
		orgID, _ = biz.OrgID(ctx)
		return "ok", nil
	})

	ctx := transport.NewServerContext(context.Background(), fakeHTTPTransport{request: request})
	if _, err := handler(ctx, nil); err != nil {
		t.Fatalf("handler returned error: %v", err)
	}
	if callerUserID != "user-1" {
		t.Fatalf("caller user ID = %q, want user-1", callerUserID)
	}
	if orgID != "org-1" {
		t.Fatalf("org ID = %q, want org-1", orgID)
	}
}

func TestAuthRejectsMissingOrInvalidToken(t *testing.T) {
	for _, authorization := range []string{"", "Bearer unknown-token", "Basic secret-token", "Bearer "} {
		t.Run(authorization, func(t *testing.T) {
			request := httptest.NewRequest(http.MethodGet, "/api/v1/agents", nil)
			request.Header.Set("Authorization", authorization)
			handler := Auth(fakeTokenLookup{}, fakeOrgMembership{})(func(context.Context, interface{}) (interface{}, error) {
				t.Fatal("handler must not be called")
				return nil, nil
			})

			ctx := transport.NewServerContext(context.Background(), fakeHTTPTransport{request: request})
			_, err := handler(ctx, nil)
			if err == nil {
				t.Fatal("expected unauthorized error")
			}
			if got := int(errors.FromError(err).Code); got != http.StatusUnauthorized {
				t.Fatalf("error code = %d, want %d", got, http.StatusUnauthorized)
			}
		})
	}
}

func TestAuthRejectsUnauthorizedOrgContext(t *testing.T) {
	token := "secret-token"
	sum := sha256.Sum256([]byte(token))
	request := httptest.NewRequest(http.MethodGet, "/api/v1/agents", nil)
	request.Header.Set("Authorization", "Bearer "+token)
	request.Header.Set("X-Org-Id", "org-2")

	handler := Auth(
		fakeTokenLookup{userIDByHash: map[string]string{hex.EncodeToString(sum[:]): "user-1"}},
		fakeOrgMembership{orgIDsByUser: map[string][]string{"user-1": {"org-1"}}},
	)(func(context.Context, interface{}) (interface{}, error) {
		t.Fatal("handler must not be called")
		return nil, nil
	})

	ctx := transport.NewServerContext(context.Background(), fakeHTTPTransport{request: request})
	_, err := handler(ctx, nil)
	if err == nil {
		t.Fatal("expected invalid org context error")
	}
	if got := int(errors.FromError(err).Code); got != http.StatusBadRequest {
		t.Fatalf("error code = %d, want %d", got, http.StatusBadRequest)
	}
}

func TestAuthSkipsWebhookPaths(t *testing.T) {
	request := httptest.NewRequest(http.MethodPost, "/api/v1/webhooks/channel-1", nil)
	called := false
	handler := Auth(fakeTokenLookup{}, fakeOrgMembership{})(func(context.Context, interface{}) (interface{}, error) {
		called = true
		return "ok", nil
	})

	ctx := transport.NewServerContext(context.Background(), fakeHTTPTransport{request: request})
	if _, err := handler(ctx, nil); err != nil {
		t.Fatalf("handler returned error: %v", err)
	}
	if !called {
		t.Fatal("handler was not called")
	}
}

func TestAuthSkipsAuthPublicPaths(t *testing.T) {
	request := httptest.NewRequest(http.MethodPost, "/api/v1/auth/login", nil)
	called := false
	handler := Auth(fakeTokenLookup{}, fakeOrgMembership{})(func(context.Context, interface{}) (interface{}, error) {
		called = true
		return "ok", nil
	})

	ctx := transport.NewServerContext(context.Background(), fakeHTTPTransport{request: request})
	if _, err := handler(ctx, nil); err != nil {
		t.Fatalf("handler returned error: %v", err)
	}
	if !called {
		t.Fatal("handler was not called without bearer token")
	}
}

func TestAuthSkipsMetricsPath(t *testing.T) {
	request := httptest.NewRequest(http.MethodGet, "/metrics", nil)
	called := false
	handler := Auth(fakeTokenLookup{}, fakeOrgMembership{})(func(context.Context, interface{}) (interface{}, error) {
		called = true
		return "ok", nil
	})
	ctx := transport.NewServerContext(context.Background(), fakeHTTPTransport{request: request})
	if _, err := handler(ctx, nil); err != nil {
		t.Fatalf("handler returned error: %v", err)
	}
	if !called {
		t.Fatal("handler was not called for /metrics")
	}
}

func TestAuthSkipsHealthzAndReadyz(t *testing.T) {
	for _, path := range []string{"/healthz", "/readyz"} {
		t.Run(path, func(t *testing.T) {
			request := httptest.NewRequest(http.MethodGet, path, nil)
			called := false
			handler := Auth(fakeTokenLookup{}, fakeOrgMembership{})(func(context.Context, interface{}) (interface{}, error) {
				called = true
				return "ok", nil
			})
			ctx := transport.NewServerContext(context.Background(), fakeHTTPTransport{request: request})
			if _, err := handler(ctx, nil); err != nil {
				t.Fatalf("handler returned error: %v", err)
			}
			if !called {
				t.Fatalf("handler was not called for %s", path)
			}
		})
	}
}

func TestAuthSkipsRuntimeV1Paths(t *testing.T) {
	for _, path := range []string{"/runtime/v1", "/runtime/v1/_auth_ping", "/runtime/v1/sessions"} {
		t.Run(path, func(t *testing.T) {
			request := httptest.NewRequest(http.MethodGet, path, nil)
			called := false
			handler := Auth(fakeTokenLookup{}, fakeOrgMembership{})(func(context.Context, interface{}) (interface{}, error) {
				called = true
				return "ok", nil
			})
			ctx := transport.NewServerContext(context.Background(), fakeHTTPTransport{request: request})
			if _, err := handler(ctx, nil); err != nil {
				t.Fatalf("handler returned error: %v", err)
			}
			if !called {
				t.Fatal("handler was not called for runtime path")
			}
		})
	}
}

func TestAuthDoesNotSkipRuntimeV10(t *testing.T) {
	request := httptest.NewRequest(http.MethodGet, "/runtime/v10/sessions", nil)
	handler := Auth(fakeTokenLookup{}, fakeOrgMembership{})(func(context.Context, interface{}) (interface{}, error) {
		t.Fatal("handler must not be called for /runtime/v10 without bearer")
		return nil, nil
	})
	ctx := transport.NewServerContext(context.Background(), fakeHTTPTransport{request: request})
	_, err := handler(ctx, nil)
	if err == nil {
		t.Fatal("expected unauthorized for /runtime/v10 without bearer")
	}
	if got := int(errors.FromError(err).Code); got != http.StatusUnauthorized {
		t.Fatalf("error code = %d, want %d", got, http.StatusUnauthorized)
	}
}
