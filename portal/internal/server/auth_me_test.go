package server

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	khttp "github.com/go-kratos/kratos/v2/transport/http"
)

type meFakeLookup struct {
	userIDByHash map[string]string
}

func (f meFakeLookup) UserIDByTokenHash(_ context.Context, tokenHash string) (string, error) {
	return f.userIDByHash[tokenHash], nil
}

func TestAuthMe_ReturnsUserID(t *testing.T) {
	token := "opaque-session-token"
	sum := sha256.Sum256([]byte(token))
	lookup := meFakeLookup{userIDByHash: map[string]string{
		hex.EncodeToString(sum[:]): "user-42",
	}}

	srv := khttp.NewServer()
	r := srv.Route("/")
	r.GET("/api/v1/auth/me", AuthMeHandler(lookup))

	req := httptest.NewRequest(http.MethodGet, "/api/v1/auth/me", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	rec := httptest.NewRecorder()
	srv.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body=%s", rec.Code, rec.Body.String())
	}
	var body struct {
		UserID string `json:"user_id"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode: %v body=%s", err, rec.Body.String())
	}
	if body.UserID != "user-42" {
		t.Fatalf("user_id = %q, want user-42", body.UserID)
	}
}
