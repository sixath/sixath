package web

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestBochaBackend_SearchNormalizesResponse(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer test-key" {
			t.Fatalf("missing auth header")
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{
			"data": {
				"webPages": {
					"value": [
						{
							"name": "Example",
							"url": "https://example.com/a",
							"snippet": "short",
							"summary": "long summary",
							"siteName": "Example Site",
							"datePublished": "2024-01-01T00:00:00Z"
						}
					]
				}
			}
		}`))
	}))
	defer srv.Close()

	b := NewBochaBackend(BochaConfig{
		APIKey:     "test-key",
		Endpoint:   srv.URL,
		HTTPClient: srv.Client(),
	})
	if err := b.Check(context.Background()); err != nil {
		t.Fatal(err)
	}
	resp, err := b.Search(context.Background(), SearchRequest{
		Query:   "test query",
		Count:   8,
		Summary: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(resp.Results) != 1 {
		t.Fatalf("got %d results", len(resp.Results))
	}
	got := resp.Results[0]
	if got.Title != "Example" || got.URL != "https://example.com/a" || got.Summary != "long summary" {
		t.Fatalf("%#v", got)
	}
}

func TestBochaBackend_CheckRequiresAPIKey(t *testing.T) {
	b := NewBochaBackend(BochaConfig{})
	if err := b.Check(context.Background()); err == nil {
		t.Fatal("expected missing key error")
	}
}

func TestNormalizeSearchRequest_Defaults(t *testing.T) {
	req := NormalizeSearchRequest(SearchRequest{})
	if req.Count != DefaultSearchCount || req.Freshness != DefaultSearchFreshness {
		t.Fatalf("%#v", req)
	}
	req = NormalizeSearchRequest(SearchRequest{Count: 999})
	if req.Count != MaxSearchCount {
		t.Fatalf("count=%d", req.Count)
	}
}
