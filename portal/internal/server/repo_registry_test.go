package server

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"backend/internal/biz"

	kratosErrors "github.com/go-kratos/kratos/v2/errors"
	"github.com/go-kratos/kratos/v2/log"
	khttp "github.com/go-kratos/kratos/v2/transport/http"
	"github.com/sixath/framework/model"
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
		{"handbook building", biz.ErrHandbookBuilding, 409},
		{"handbook not found", biz.ErrHandbookNotFound, 404},
		{"handbook llm disabled", biz.ErrHandbookLLMDisabled, 400},
		{"handbook not ready", biz.ErrHandbookNotReady, 409},
		{"handbook llm busy", biz.ErrHandbookLLMBusy, 409},
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
	for in, reason := range map[error]string{
		biz.ErrHandbookLLMDisabled: "HANDBOOK_LLM_DISABLED",
		biz.ErrHandbookNotReady:    "HANDBOOK_NOT_READY",
		biz.ErrHandbookLLMBusy:     "HANDBOOK_LLM_BUSY",
		biz.ErrHandbookBuilding:    "HANDBOOK_BUILDING",
	} {
		if got := kratosErrors.FromError(repoRegistryErr(in)).Reason; got != reason {
			t.Errorf("%v: reason = %q, want %q", in, got, reason)
		}
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

func TestHandbookRoutes_DisabledWithoutUsecase(t *testing.T) {
	srv := khttp.NewServer(khttp.ErrorEncoder(errorEncoder))
	h := NewRepoRegistryHandlers(nil, nil)
	srv.Route("/").GET("/api/v1/repos/{id}/handbook", h.GetHandbook())
	srv.Route("/").POST("/api/v1/repos/{id}/handbook/rebuild", h.RebuildHandbook())
	srv.Route("/").POST("/api/v1/repos/{id}/handbook/enrich", h.EnrichHandbook())
	srv.Route("/").GET("/api/v1/handbook/config", h.HandbookConfig())
	for _, tc := range []struct{ method, url string }{
		{http.MethodGet, "/api/v1/repos/r1/handbook"},
		{http.MethodPost, "/api/v1/repos/r1/handbook/rebuild"},
		{http.MethodPost, "/api/v1/repos/r1/handbook/enrich?full=1"},
	} {
		rec := httptest.NewRecorder()
		srv.ServeHTTP(rec, httptest.NewRequest(tc.method, tc.url, nil))
		if rec.Code != http.StatusServiceUnavailable {
			t.Fatalf("%s %s: status = %d body = %s", tc.method, tc.url, rec.Code, rec.Body.String())
		}
	}
	rec := httptest.NewRecorder()
	srv.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/v1/handbook/config", nil))
	var cfg map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &cfg); err != nil || rec.Code != http.StatusOK {
		t.Fatalf("config: status = %d body = %s err = %v", rec.Code, rec.Body.String(), err)
	}
	if cfg["model"] != "" || cfg["enabled"] != false || cfg["available"] != false {
		t.Fatalf("config without usecase = %v, want disabled", cfg)
	}
}

func TestHandbookConfigRoute(t *testing.T) {
	uc := biz.NewHandbookUsecase(nil, nil, t.TempDir(), log.DefaultLogger)
	uc.SetLLM(biz.HandbookLLMConfig{Model: "qwen/qwen-max", Concurrency: 2}, func(context.Context, string) (model.Model, error) {
		return nil, errors.New("unused")
	})
	srv := khttp.NewServer(khttp.ErrorEncoder(errorEncoder))
	srv.Route("/").GET("/api/v1/handbook/config", NewRepoRegistryHandlers(nil, nil).WithHandbook(uc).HandbookConfig())
	rec := httptest.NewRecorder()
	srv.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/v1/handbook/config", nil))
	var cfg map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &cfg); err != nil || rec.Code != http.StatusOK {
		t.Fatalf("status = %d body = %s err = %v", rec.Code, rec.Body.String(), err)
	}
	want := map[string]any{
		"model": "qwen/qwen-max", "available": true, "enabled": true, "concurrency": 2.0, "max_cards_per_run": 600.0,
		"max_file_kb": 24.0, "max_run_minutes": 15.0, "skeleton_rebuild_days": 30.0,
	}
	for k, v := range want {
		if cfg[k] != v {
			t.Errorf("%s = %v, want %v (body %s)", k, cfg[k], v, rec.Body.String())
		}
	}
}

func TestHandbookConfigRoute_NoResolver(t *testing.T) {
	uc := biz.NewHandbookUsecase(nil, nil, t.TempDir(), log.DefaultLogger)
	uc.SetLLM(biz.HandbookLLMConfig{Model: "qwen/qwen-max"}, nil)
	srv := khttp.NewServer(khttp.ErrorEncoder(errorEncoder))
	srv.Route("/").GET("/api/v1/handbook/config", NewRepoRegistryHandlers(nil, nil).WithHandbook(uc).HandbookConfig())
	rec := httptest.NewRecorder()
	srv.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/v1/handbook/config", nil))
	var cfg map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &cfg); err != nil || rec.Code != http.StatusOK {
		t.Fatalf("status = %d body = %s err = %v", rec.Code, rec.Body.String(), err)
	}
	if cfg["model"] != "qwen/qwen-max" || cfg["available"] != false || cfg["enabled"] != false {
		t.Fatalf("config without resolver = %v, want model kept but unavailable and disabled", cfg)
	}
}

// missingRepoRegistry knows no repositories; other methods are not used.
type missingRepoRegistry struct{ biz.RepoRegistryRepo }

func (missingRepoRegistry) GetRepositoriesByIDs(context.Context, []string) (map[string]*biz.Repository, error) {
	return map[string]*biz.Repository{}, nil
}

func TestEnrichHandbook_UnknownRepo(t *testing.T) {
	uc := biz.NewHandbookUsecase(missingRepoRegistry{}, nil, t.TempDir(), log.DefaultLogger)
	srv := khttp.NewServer(khttp.ErrorEncoder(errorEncoder))
	srv.Route("/").POST("/api/v1/repos/{id}/handbook/enrich", NewRepoRegistryHandlers(nil, nil).WithHandbook(uc).EnrichHandbook())
	for _, tc := range []struct {
		url    string
		code   int
		reason string
	}{
		{"/api/v1/repos/r1/handbook/enrich", http.StatusNotFound, "NOT_FOUND"},
		{"/api/v1/repos/r1/handbook/enrich?full=", http.StatusNotFound, "NOT_FOUND"},
		{"/api/v1/repos/r1/handbook/enrich?full=1", http.StatusNotFound, "NOT_FOUND"},
		{"/api/v1/repos/r1/handbook/enrich?full=false", http.StatusNotFound, "NOT_FOUND"},
		{"/api/v1/repos/r1/handbook/enrich?full=yes", http.StatusBadRequest, "INVALID_ARGUMENT"},
	} {
		rec := httptest.NewRecorder()
		srv.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, tc.url, nil))
		if rec.Code != tc.code || !strings.Contains(rec.Body.String(), tc.reason) {
			t.Fatalf("%s: status = %d body = %s, want %d %s", tc.url, rec.Code, rec.Body.String(), tc.code, tc.reason)
		}
	}
}
