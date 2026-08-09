package web

import "testing"

func TestExtractSourcesFromToolOutput_webSearch(t *testing.T) {
	out := &SearchResponse{
		Query: "贵州 生猪",
		Results: []SearchResult{
			{Title: "贵州畜牧业统计", URL: "https://gov.example/stats", SiteName: "现代畜牧网"},
			{Title: "", URL: "https://news.example/pig", SiteName: ""},
		},
	}
	items := ExtractSourcesFromToolOutput("web_search", out)
	if len(items) != 2 {
		t.Fatalf("got %d items", len(items))
	}
	if items[0].Title != "贵州畜牧业统计" || items[0].URL != "https://gov.example/stats" {
		t.Fatalf("first: %#v", items[0])
	}
	if items[1].Title != "news.example" {
		t.Fatalf("fallback title: %q", items[1].Title)
	}
}

func TestExtractSourcesFromToolOutput_webExtract(t *testing.T) {
	out := map[string]any{
		"results": []any{
			map[string]any{"url": "https://a.test/page", "format": "markdown", "content": "x"},
			map[string]any{"url": "https://b.test/fail", "error": "timeout"},
		},
	}
	items := ExtractSourcesFromToolOutput("web_extract", out)
	if len(items) != 1 || items[0].URL != "https://a.test/page" {
		t.Fatalf("got %#v", items)
	}
}

func TestExtractSourcesFromToolOutput_dedupe(t *testing.T) {
	out := &SearchResponse{
		Results: []SearchResult{
			{Title: "A", URL: "https://dup.test"},
			{Title: "B", URL: "https://dup.test"},
		},
	}
	items := ExtractSourcesFromToolOutput("web_search", out)
	if len(items) != 1 {
		t.Fatalf("expected dedupe, got %#v", items)
	}
}

func TestExtractSourcesFromToolCalls_dedupeAcrossCalls(t *testing.T) {
	items := ExtractSourcesFromToolCalls([]ToolCallRecord{
		{ToolName: "web_search", Result: &SearchResponse{Results: []SearchResult{{Title: "A", URL: "https://dup.test"}}}},
		{ToolName: "web_extract", Result: map[string]any{
			"results": []any{map[string]any{"url": "https://dup.test", "content": "x"}},
		}},
	})
	if len(items) != 1 {
		t.Fatalf("expected dedupe across calls, got %#v", items)
	}
}
