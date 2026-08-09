package web

import (
	"net/url"
	"strings"
)

// SourceItem is a normalized citation source for UI and metadata.
type SourceItem struct {
	Title    string `json:"title"`
	URL      string `json:"url"`
	SiteName string `json:"site_name,omitempty"`
}

// ExtractSourcesFromToolOutput extracts citation sources from web_search / web_extract tool output.
func ExtractSourcesFromToolOutput(toolName string, output any) []SourceItem {
	switch toolName {
	case "web_search":
		return sourcesFromWebSearch(output)
	case "web_extract":
		return sourcesFromWebExtract(output)
	default:
		return nil
	}
}

func sourcesFromWebSearch(output any) []SourceItem {
	var results []SearchResult
	switch v := output.(type) {
	case *SearchResponse:
		if v == nil {
			return nil
		}
		results = v.Results
	case SearchResponse:
		results = v.Results
	case map[string]any:
		raw, _ := v["results"].([]any)
		for _, item := range raw {
			m, _ := item.(map[string]any)
			if m == nil {
				continue
			}
			results = append(results, SearchResult{
				Title:    stringField(m, "title"),
				URL:      stringField(m, "url"),
				SiteName: stringField(m, "site_name"),
			})
		}
	default:
		return nil
	}
	seen := make(map[string]struct{})
	out := make([]SourceItem, 0, len(results))
	for _, r := range results {
		u := strings.TrimSpace(r.URL)
		if u == "" {
			continue
		}
		if _, ok := seen[u]; ok {
			continue
		}
		seen[u] = struct{}{}
		title := strings.TrimSpace(r.Title)
		if title == "" {
			title = hostLabel(u)
		}
		out = append(out, SourceItem{Title: title, URL: u, SiteName: strings.TrimSpace(r.SiteName)})
	}
	return out
}

func sourcesFromWebExtract(output any) []SourceItem {
	m, ok := output.(map[string]any)
	if !ok {
		return nil
	}
	raw, _ := m["results"].([]any)
	seen := make(map[string]struct{})
	out := make([]SourceItem, 0, len(raw))
	for _, item := range raw {
		row, _ := item.(map[string]any)
		if row == nil {
			continue
		}
		if errMsg, _ := row["error"].(string); strings.TrimSpace(errMsg) != "" {
			continue
		}
		u := strings.TrimSpace(stringField(row, "url"))
		if u == "" {
			continue
		}
		if _, ok := seen[u]; ok {
			continue
		}
		seen[u] = struct{}{}
		out = append(out, SourceItem{Title: hostLabel(u), URL: u})
	}
	return out
}

func stringField(m map[string]any, key string) string {
	v, _ := m[key].(string)
	return v
}

// ToolCallRecord is a minimal tool call view for source extraction (avoids importing agent).
type ToolCallRecord struct {
	ToolName string
	Result   any
}

// ExtractSourcesFromToolCalls aggregates sources from multiple tool calls, deduped by URL.
func ExtractSourcesFromToolCalls(calls []ToolCallRecord) []SourceItem {
	seen := make(map[string]struct{})
	out := make([]SourceItem, 0)
	for _, call := range calls {
		items := ExtractSourcesFromToolOutput(call.ToolName, call.Result)
		for _, it := range items {
			if _, ok := seen[it.URL]; ok {
				continue
			}
			seen[it.URL] = struct{}{}
			out = append(out, it)
		}
	}
	return out
}

func hostLabel(rawURL string) string {
	u, err := url.Parse(rawURL)
	if err != nil || u.Host == "" {
		return rawURL
	}
	return u.Host
}
