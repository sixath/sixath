package web

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

const defaultBochaEndpoint = "https://api.bochaai.com/v1/web-search"

// BochaBackend implements WebSearchBackend against Bocha Web Search API.
type BochaBackend struct {
	apiKey     string
	endpoint   string
	httpClient *http.Client
}

// BochaConfig configures a Bocha backend.
type BochaConfig struct {
	APIKey     string
	Endpoint   string
	HTTPClient *http.Client
}

// NewBochaBackend returns a Bocha Web Search backend.
func NewBochaBackend(cfg BochaConfig) *BochaBackend {
	client := cfg.HTTPClient
	if client == nil {
		client = &http.Client{Timeout: 30 * time.Second}
	}
	endpoint := cfg.Endpoint
	if endpoint == "" {
		endpoint = defaultBochaEndpoint
	}
	return &BochaBackend{
		apiKey:     cfg.APIKey,
		endpoint:   endpoint,
		httpClient: client,
	}
}

func (b *BochaBackend) Name() string { return "bocha" }

func (b *BochaBackend) Check(_ context.Context) error {
	if strings.TrimSpace(b.apiKey) == "" {
		return errors.New("BOCHA_API_KEY is not configured")
	}
	return nil
}

type bochaSearchBody struct {
	Query     string `json:"query"`
	Count     int    `json:"count"`
	Freshness string `json:"freshness"`
	Summary   bool   `json:"summary"`
}

func (b *BochaBackend) Search(ctx context.Context, req SearchRequest) (*SearchResponse, error) {
	req = NormalizeSearchRequest(req)
	if strings.TrimSpace(req.Query) == "" {
		return nil, errors.New("query is required")
	}
	body, err := json.Marshal(bochaSearchBody{
		Query:     req.Query,
		Count:     req.Count,
		Freshness: req.Freshness,
		Summary:   req.Summary,
	})
	if err != nil {
		return nil, err
	}
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, b.endpoint, bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	httpReq.Header.Set("Authorization", "Bearer "+b.apiKey)
	httpReq.Header.Set("Content-Type", "application/json")

	resp, err := b.httpClient.Do(httpReq)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	raw, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("bocha: HTTP %d: %s", resp.StatusCode, truncate(string(raw), 200))
	}
	results, err := parseBochaResponse(raw)
	if err != nil {
		return nil, err
	}
	if len(results) > req.Count {
		results = results[:req.Count]
	}
	return &SearchResponse{Query: req.Query, Results: results}, nil
}

func parseBochaResponse(raw []byte) ([]SearchResult, error) {
	var envelope map[string]json.RawMessage
	if err := json.Unmarshal(raw, &envelope); err != nil {
		return nil, fmt.Errorf("bocha: decode response: %w", err)
	}
	payload := raw
	if data, ok := envelope["data"]; ok && len(data) > 0 {
		payload = data
	}
	var pageRoot struct {
		WebPages struct {
			Value []struct {
				Name          string `json:"name"`
				URL           string `json:"url"`
				Snippet       string `json:"snippet"`
				Summary       string `json:"summary"`
				SiteName      string `json:"siteName"`
				DatePublished string `json:"datePublished"`
				DateLast      string `json:"dateLastCrawled"`
			} `json:"value"`
		} `json:"webPages"`
	}
	if err := json.Unmarshal(payload, &pageRoot); err != nil {
		return nil, fmt.Errorf("bocha: decode webPages: %w", err)
	}
	out := make([]SearchResult, 0, len(pageRoot.WebPages.Value))
	for _, item := range pageRoot.WebPages.Value {
		published := item.DatePublished
		if published == "" {
			published = item.DateLast
		}
		out = append(out, SearchResult{
			Title:       item.Name,
			URL:         item.URL,
			Snippet:     item.Snippet,
			Summary:     item.Summary,
			SiteName:    item.SiteName,
			PublishedAt: published,
		})
	}
	return out, nil
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "..."
}
