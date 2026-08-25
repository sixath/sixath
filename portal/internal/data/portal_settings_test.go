package data

import "testing"

func TestParseStoredCodeModel(t *testing.T) {
	got := parseStoredCodeModel(`{"provider":"openai","model":"gpt-code","api_key":"k","base_url":"http://x"}`)
	if got.Provider != "openai" || got.Model != "gpt-code" || got.APIKey != "k" || got.BaseURL != "http://x" {
		t.Fatalf("got %#v", got)
	}
	if parseStoredCodeModel("").Model != "" || parseStoredCodeModel("").APIKey != "" {
		t.Fatal("empty should have no fields")
	}
}
