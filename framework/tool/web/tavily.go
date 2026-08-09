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

const defaultTavilyEndpoint = "https://api.tavily.com/search"

// TavilyBackend implements WebSearchBackend against Tavily Search API.
type TavilyBackend struct {
	apiKey     string
	endpoint   string
	httpClient *http.Client
}

// TavilyConfig configures a Tavily backend.
type TavilyConfig struct {
	APIKey     string
	Endpoint   string
	HTTPClient *http.Client
}

// NewTavilyBackend returns a Tavily search backend.
func NewTavilyBackend(cfg TavilyConfig) *TavilyBackend {
	client := cfg.HTTPClient
	if client == nil {
		client = &http.Client{Timeout: 30 * time.Second}
	}
	endpoint := cfg.Endpoint
	if endpoint == "" {
		endpoint = defaultTavilyEndpoint
	}
	return &TavilyBackend{
		apiKey:     cfg.APIKey,
		endpoint:   endpoint,
		httpClient: client,
	}
}

func (t *TavilyBackend) Name() string { return "tavily" }

func (t *TavilyBackend) Check(_ context.Context) error {
	if strings.TrimSpace(t.apiKey) == "" {
		return errors.New("TAVILY_API_KEY is not configured")
	}
	return nil
}

type tavilySearchBody struct {
	APIKey         string `json:"api_key"`
	Query          string `json:"query"`
	MaxResults     int    `json:"max_results"`
	IncludeAnswer  bool   `json:"include_answer"`
	SearchDepth    string `json:"search_depth"`
}

func (t *TavilyBackend) Search(ctx context.Context, req SearchRequest) (*SearchResponse, error) {
	req = NormalizeSearchRequest(req)
	if strings.TrimSpace(req.Query) == "" {
		return nil, errors.New("query is required")
	}
	body, err := json.Marshal(tavilySearchBody{
		APIKey:        t.apiKey,
		Query:         req.Query,
		MaxResults:    req.Count,
		IncludeAnswer: false,
		SearchDepth:   "basic",
	})
	if err != nil {
		return nil, err
	}
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, t.endpoint, bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	httpReq.Header.Set("Content-Type", "application/json")

	resp, err := t.httpClient.Do(httpReq)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	raw, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("tavily: HTTP %d: %s", resp.StatusCode, truncate(string(raw), 200))
	}
	var parsed struct {
		Results []struct {
			Title   string `json:"title"`
			URL     string `json:"url"`
			Content string `json:"content"`
		} `json:"results"`
	}
	if err := json.Unmarshal(raw, &parsed); err != nil {
		return nil, fmt.Errorf("tavily: decode response: %w", err)
	}
	out := make([]SearchResult, 0, len(parsed.Results))
	for _, item := range parsed.Results {
		out = append(out, SearchResult{
			Title:   item.Title,
			URL:     item.URL,
			Snippet: item.Content,
			Summary: item.Content,
		})
	}
	return &SearchResponse{Query: req.Query, Results: out}, nil
}
