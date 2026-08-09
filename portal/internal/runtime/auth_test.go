package runtime

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"backend/internal/biz"

	"github.com/go-kratos/kratos/v2/errors"
	"github.com/go-kratos/kratos/v2/transport"
	khttp "github.com/go-kratos/kratos/v2/transport/http"
)

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

func TestRuntimeAuth_RejectsMissingToken(t *testing.T) {
	request := httptest.NewRequest(http.MethodGet, "/runtime/v1/_auth_ping", nil)
	handler := Auth("dev-runtime-token")(func(context.Context, interface{}) (interface{}, error) {
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
}

func TestRuntimeAuth_AcceptsServiceTokenAndUserHeader(t *testing.T) {
	const serviceToken = "dev-runtime-token"
	request := httptest.NewRequest(http.MethodGet, "/runtime/v1/_auth_ping", nil)
	request.Header.Set("Authorization", "Bearer "+serviceToken)
	request.Header.Set("X-Sath-User-Id", "user-7")

	var gotUser string
	handler := Auth(serviceToken)(func(ctx context.Context, _ interface{}) (interface{}, error) {
		gotUser, _ = biz.CallerUserID(ctx)
		return "ok", nil
	})
	ctx := transport.NewServerContext(context.Background(), fakeHTTPTransport{request: request})
	if _, err := handler(ctx, nil); err != nil {
		t.Fatalf("handler returned error: %v", err)
	}
	if gotUser != "user-7" {
		t.Fatalf("caller user = %q, want user-7", gotUser)
	}
}

func TestRuntimeAuth_RejectsUserSessionTokenAlone(t *testing.T) {
	request := httptest.NewRequest(http.MethodGet, "/runtime/v1/_auth_ping", nil)
	request.Header.Set("Authorization", "Bearer opaque-user-session-token")
	request.Header.Set("X-Sath-User-Id", "user-7")

	handler := Auth("dev-runtime-token")(func(context.Context, interface{}) (interface{}, error) {
		t.Fatal("handler must not be called with user session token")
		return nil, nil
	})
	ctx := transport.NewServerContext(context.Background(), fakeHTTPTransport{request: request})
	_, err := handler(ctx, nil)
	if err == nil {
		t.Fatal("expected unauthorized error for user session token")
	}
	if got := int(errors.FromError(err).Code); got != http.StatusUnauthorized {
		t.Fatalf("error code = %d, want %d", got, http.StatusUnauthorized)
	}
}

func TestRuntimeAuth_RejectsEmptyServiceTokenConfig(t *testing.T) {
	request := httptest.NewRequest(http.MethodGet, "/runtime/v1/_auth_ping", nil)
	request.Header.Set("Authorization", "Bearer anything")
	handler := Auth("")(func(context.Context, interface{}) (interface{}, error) {
		t.Fatal("handler must not be called when service token unset")
		return nil, nil
	})
	ctx := transport.NewServerContext(context.Background(), fakeHTTPTransport{request: request})
	_, err := handler(ctx, nil)
	if err == nil {
		t.Fatal("expected unauthorized when service token not configured")
	}
	if got := int(errors.FromError(err).Code); got != http.StatusUnauthorized {
		t.Fatalf("error code = %d, want %d", got, http.StatusUnauthorized)
	}
}
