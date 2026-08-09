package web

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestTavilyBackend_Search(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{
			"results": [
				{"title": "Tavily Hit", "url": "https://example.com", "content": "details"}
			]
		}`))
	}))
	defer srv.Close()

	b := NewTavilyBackend(TavilyConfig{
		APIKey:     "tv-key",
		Endpoint:   srv.URL,
		HTTPClient: srv.Client(),
	})
	resp, err := b.Search(context.Background(), SearchRequest{Query: "hello", Count: 3})
	if err != nil {
		t.Fatal(err)
	}
	if len(resp.Results) != 1 || resp.Results[0].Title != "Tavily Hit" {
		t.Fatalf("%#v", resp.Results)
	}
}
