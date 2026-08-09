package tool

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"regexp"
	"strings"
	"time"

	"github.com/sixath/framework/tool/web"
)

// WebToolsConfig configures web_search and web_extract registration.
type WebToolsConfig struct {
	SearchBackend web.WebSearchBackend
	DefaultCount  int
	DefaultSummary bool
	HTTPClient    *http.Client
}

// NewWebSearchBackendFromEnv selects backend from WEB_SEARCH_BACKEND (default bocha).
func NewWebSearchBackendFromEnv() web.WebSearchBackend {
	return NewWebSearchBackend("", "", "")
}

// NewWebSearchBackend builds a search backend; empty fields fall back to env (WEB_SEARCH_BACKEND, BOCHA_API_KEY, TAVILY_API_KEY).
func NewWebSearchBackend(searchBackend, bochaAPIKey, tavilyAPIKey string) web.WebSearchBackend {
	name := strings.ToLower(strings.TrimSpace(searchBackend))
	if name == "" {
		name = strings.ToLower(strings.TrimSpace(os.Getenv("WEB_SEARCH_BACKEND")))
	}
	if name == "" {
		name = "bocha"
	}
	switch name {
	case "tavily":
		key := strings.TrimSpace(tavilyAPIKey)
		if key == "" {
			key = os.Getenv("TAVILY_API_KEY")
		}
		return web.NewTavilyBackend(web.TavilyConfig{APIKey: key})
	default:
		key := strings.TrimSpace(bochaAPIKey)
		if key == "" {
			key = os.Getenv("BOCHA_API_KEY")
		}
		return web.NewBochaBackend(web.BochaConfig{APIKey: key})
	}
}

// RegisterWebTools registers web_search and web_extract.
func RegisterWebTools(reg *Registry, cfg *WebToolsConfig) error {
	if reg == nil {
		return errors.New("web tools: registry is nil")
	}
	var backend web.WebSearchBackend = web.NewBochaBackend(web.BochaConfig{APIKey: os.Getenv("BOCHA_API_KEY")})
	defaultCount := web.DefaultSearchCount
	defaultSummary := true
	client := &http.Client{Timeout: 30 * time.Second}
	if cfg != nil {
		if cfg.SearchBackend != nil {
			backend = cfg.SearchBackend
		}
		if cfg.DefaultCount > 0 {
			defaultCount = cfg.DefaultCount
		}
		defaultSummary = cfg.DefaultSummary
		if cfg.HTTPClient != nil {
			client = cfg.HTTPClient
		}
	}
	if err := registerWebSearchTool(reg, backend, defaultCount, defaultSummary); err != nil {
		return err
	}
	return registerWebExtractTool(reg, client)
}

func registerWebSearchTool(reg *Registry, backend web.WebSearchBackend, defaultCount int, defaultSummary bool) error {
	checkFn := func(ctx context.Context) error {
		if backend == nil {
			return errors.New("web search backend not configured")
		}
		return backend.Check(ctx)
	}
	return reg.Register(Tool{
		Name: "web_search",
		Description: "Search the web for current information. Returns titles, URLs, snippets, and optional summaries. " +
			"Prefer web_search over raw http_request for discovery; use web_extract for single-URL full content.",
		Toolset: ToolsetWeb,
		CheckFn: checkFn,
		Parameters: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"query": map[string]any{
					"type":        "string",
					"description": "Search query.",
				},
				"count": map[string]any{
					"type":        "integer",
					"description": fmt.Sprintf("Number of results (default %d, max %d).", defaultCount, web.MaxSearchCount),
				},
				"freshness": map[string]any{
					"type":        "string",
					"description": "Time range: noLimit, oneDay, oneWeek, oneMonth, oneYear, or YYYY-MM-DD.",
				},
				"include_summary": map[string]any{
					"type":        "boolean",
					"description": "Request server-generated summaries when supported by backend.",
				},
			},
			"required": []string{"query"},
		},
		Execute: func(ctx context.Context, params map[string]any) (any, error) {
			query, _ := params["query"].(string)
			if strings.TrimSpace(query) == "" {
				return map[string]any{"error": "query is required"}, nil
			}
			count := intFromParam(params["count"], defaultCount)
			freshness, _ := params["freshness"].(string)
			includeSummary := defaultSummary
			if v, ok := params["include_summary"].(bool); ok {
				includeSummary = v
			}
			resp, err := backend.Search(ctx, web.SearchRequest{
				Query:     query,
				Count:     count,
				Freshness: freshness,
				Summary:   includeSummary,
			})
			if err != nil {
				return map[string]any{"error": err.Error()}, nil
			}
			return resp, nil
		},
	})
}

const (
	webExtractMaxURLs   = 5
	webExtractMaxBytes  = 2_000_000
	webExtractFullChars = 5000
)

func registerWebExtractTool(reg *Registry, client *http.Client) error {
	checkFn := func(ctx context.Context) error {
		// Same gate as web_search: require configured search backend env for P0.
		backend := NewWebSearchBackendFromEnv()
		return backend.Check(ctx)
	}
	return reg.Register(Tool{
		Name: "web_extract",
		Description: "Extract content from public web page URLs as markdown-like text. " +
			"Supports up to 5 URLs. SSRF-protected. Prefer web_search summaries when sufficient.",
		Toolset: ToolsetWeb,
		CheckFn: checkFn,
		Parameters: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"urls": map[string]any{
					"type":        "array",
					"items":       map[string]any{"type": "string"},
					"description": "Up to 5 HTTP/HTTPS URLs to extract.",
				},
			},
			"required": []string{"urls"},
		},
		Execute: func(ctx context.Context, params map[string]any) (any, error) {
			raw, ok := params["urls"].([]any)
			if !ok || len(raw) == 0 {
				return map[string]any{"error": "urls is required"}, nil
			}
			if len(raw) > webExtractMaxURLs {
				return map[string]any{"error": fmt.Sprintf("at most %d urls allowed", webExtractMaxURLs)}, nil
			}
			results := make([]map[string]any, 0, len(raw))
			for _, item := range raw {
				u, _ := item.(string)
				results = append(results, extractURL(ctx, client, u))
			}
			return map[string]any{"results": results}, nil
		},
	})
}

func extractURL(ctx context.Context, client *http.Client, rawURL string) map[string]any {
	if err := ValidateOutboundURL(rawURL); err != nil {
		return map[string]any{"url": rawURL, "error": err.Error()}
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, rawURL, nil)
	if err != nil {
		return map[string]any{"url": rawURL, "error": err.Error()}
	}
	req.Header.Set("User-Agent", "sixath-web-extract/1.0")
	resp, err := client.Do(req)
	if err != nil {
		return map[string]any{"url": rawURL, "error": err.Error()}
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(io.LimitReader(resp.Body, webExtractMaxBytes+1))
	if err != nil {
		return map[string]any{"url": rawURL, "error": err.Error()}
	}
	if len(body) > webExtractMaxBytes {
		return map[string]any{"url": rawURL, "error": "page exceeds 2MB limit"}
	}
	ct := strings.ToLower(resp.Header.Get("Content-Type"))
	if strings.Contains(ct, "application/pdf") || strings.HasSuffix(strings.ToLower(rawURL), ".pdf") {
		return map[string]any{
			"url":          rawURL,
			"content_type": ct,
			"format":       "pdf",
			"content":      fmt.Sprintf("[PDF document, %d bytes — full PDF text extraction not available in P0; try browser or download manually]", len(body)),
		}
	}
	text := htmlToMarkdown(string(body))
	if len(text) > webExtractFullChars {
		text = text[:webExtractFullChars] + "\n\n[truncated — page exceeds 5000 chars; use offset reads or browser for full content]"
	}
	return map[string]any{
		"url":          rawURL,
		"content_type": ct,
		"format":       "markdown",
		"content":      text,
	}
}

var (
	reScriptStyle = regexp.MustCompile(`(?is)<(script|style)[^>]*>.*?</(script|style)>`)
	reTags        = regexp.MustCompile(`(?s)<[^>]+>`)
	reSpaces      = regexp.MustCompile(`[ \t]+\n`)
)

func htmlToMarkdown(html string) string {
	s := reScriptStyle.ReplaceAllString(html, "")
	s = strings.ReplaceAll(s, "<br>", "\n")
	s = strings.ReplaceAll(s, "<br/>", "\n")
	s = strings.ReplaceAll(s, "<br />", "\n")
	s = strings.ReplaceAll(s, "</p>", "\n\n")
	s = strings.ReplaceAll(s, "</div>", "\n")
	s = strings.ReplaceAll(s, "</h1>", "\n\n")
	s = strings.ReplaceAll(s, "</h2>", "\n\n")
	s = strings.ReplaceAll(s, "</h3>", "\n\n")
	s = reTags.ReplaceAllString(s, "")
	s = strings.TrimSpace(s)
	s = reSpaces.ReplaceAllString(s, "\n")
	return s
}
