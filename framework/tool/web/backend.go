package web

import "context"

// SearchResult is a normalized web search hit for Agent consumption.
type SearchResult struct {
	Title       string `json:"title"`
	URL         string `json:"url"`
	Snippet     string `json:"snippet"`
	Summary     string `json:"summary,omitempty"`
	SiteName    string `json:"site_name,omitempty"`
	PublishedAt string `json:"published_at,omitempty"`
}

// SearchRequest maps to backend-specific search parameters.
type SearchRequest struct {
	Query     string
	Count     int
	Freshness string
	Summary   bool
}

// SearchResponse is the normalized web_search tool output payload.
type SearchResponse struct {
	Query   string         `json:"query"`
	Results []SearchResult `json:"results"`
}

// WebSearchBackend abstracts pluggable search providers (Bocha, Tavily, etc.).
type WebSearchBackend interface {
	Name() string
	Check(ctx context.Context) error
	Search(ctx context.Context, req SearchRequest) (*SearchResponse, error)
}

const (
	DefaultSearchCount     = 8
	MaxSearchCount         = 50
	DefaultSearchFreshness = "noLimit"
)

// NormalizeSearchRequest fills defaults and clamps count.
func NormalizeSearchRequest(req SearchRequest) SearchRequest {
	if req.Count <= 0 {
		req.Count = DefaultSearchCount
	}
	if req.Count > MaxSearchCount {
		req.Count = MaxSearchCount
	}
	if req.Freshness == "" {
		req.Freshness = DefaultSearchFreshness
	}
	return req
}
