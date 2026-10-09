package server

import (
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"backend/internal/biz"

	kratosErrors "github.com/go-kratos/kratos/v2/errors"
	khttp "github.com/go-kratos/kratos/v2/transport/http"
)

func TestRepoRegistryErr(t *testing.T) {
	cases := []struct {
		name string
		in   error
		code int
	}{
		{"invalid repo", fmt.Errorf("%w: status", biz.ErrInvalidRepo), 400},
		{"invalid binding", fmt.Errorf("%w: x", biz.ErrInvalidRepoBinding), 400},
		{"invalid group", fmt.Errorf("%w: x", biz.ErrInvalidRepoGroup), 400},
		{"not found", biz.ErrRepoNotFound, 404},
		{"scan running", biz.ErrRepoScanRunning, 409},
		{"group in use", fmt.Errorf("%w: bound by a1", biz.ErrRepoGroupInUse), 409},
		{"kratos passthrough", kratosErrors.Forbidden("FORBIDDEN", "no"), 403},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := kratosErrors.FromError(repoRegistryErr(tc.in))
			if int(got.Code) != tc.code {
				t.Fatalf("code = %d, want %d", got.Code, tc.code)
			}
		})
	}
	if repoRegistryErr(nil) != nil {
		t.Fatal("nil error should stay nil")
	}
	plain := errors.New("db down")
	if !errors.Is(repoRegistryErr(plain), plain) {
		t.Fatal("unknown errors should pass through unchanged")
	}
}

func TestPutBindings_MissingFieldRejected(t *testing.T) {
	srv := khttp.NewServer(khttp.ErrorEncoder(errorEncoder))
	srv.Route("/").PUT("/api/v1/agents/{agent_id}/repo-bindings", NewRepoRegistryHandlers(nil, nil).PutBindings())

	req := httptest.NewRequest(http.MethodPut, "/api/v1/agents/ag/repo-bindings", strings.NewReader(`{}`))
	rec := httptest.NewRecorder()
	srv.ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest || !strings.Contains(rec.Body.String(), "bindings is required") {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body.String())
	}
}
