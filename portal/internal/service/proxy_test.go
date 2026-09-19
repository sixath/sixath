package service

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	"backend/internal/biz"
)

func TestProxyDTOFromMeta_HidesPassword(t *testing.T) {
	dto := ProxyDTOFromMeta(&biz.ProxyMeta{
		ID:          "office",
		Name:        "Office",
		Description: "corp",
		Type:        "http",
		Host:        "127.0.0.1",
		Port:        8080,
		User:        "alice",
		Password:    "s3cret",
		HasPassword: true,
		NoProxy:     []string{"es.local"},
		CreatedAt:   time.Unix(0, 0).UTC(),
		UpdatedAt:   time.Unix(0, 0).UTC(),
	})
	if dto.HasPassword != true {
		t.Fatal("want has_password")
	}
	raw, err := json.Marshal(dto)
	if err != nil {
		t.Fatal(err)
	}
	body := string(raw)
	if strings.Contains(body, "s3cret") || strings.Contains(body, `"password"`) {
		t.Fatalf("DTO leaked password: %s", body)
	}
}
