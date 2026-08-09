package model

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestRuntimeToolsConfig_HybridRecallJSONRoundTrip(t *testing.T) {
	t.Run("unset_omitted", func(t *testing.T) {
		c := RuntimeToolsConfig{}
		b, err := json.Marshal(c)
		if err != nil {
			t.Fatal(err)
		}
		if strings.Contains(string(b), "hybrid_recall") {
			t.Fatalf("unset must omit hybrid_recall, got %s", b)
		}
		var out RuntimeToolsConfig
		if err := json.Unmarshal(b, &out); err != nil {
			t.Fatal(err)
		}
		if out.HybridRecall != nil {
			t.Fatalf("round-trip unset: want nil, got %v", *out.HybridRecall)
		}
	})

	t.Run("explicit_false", func(t *testing.T) {
		f := false
		c := RuntimeToolsConfig{HybridRecall: &f}
		b, err := json.Marshal(c)
		if err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(string(b), `"hybrid_recall":false`) {
			t.Fatalf("false must serialize hybrid_recall, got %s", b)
		}
		var out RuntimeToolsConfig
		if err := json.Unmarshal(b, &out); err != nil {
			t.Fatal(err)
		}
		if out.HybridRecall == nil || *out.HybridRecall {
			t.Fatalf("round-trip false: want *bool=false, got %+v", out.HybridRecall)
		}
	})

	t.Run("explicit_true", func(t *testing.T) {
		tr := true
		c := RuntimeToolsConfig{HybridRecall: &tr}
		b, err := json.Marshal(c)
		if err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(string(b), `"hybrid_recall":true`) {
			t.Fatalf("true must serialize hybrid_recall, got %s", b)
		}
		var out RuntimeToolsConfig
		if err := json.Unmarshal(b, &out); err != nil {
			t.Fatal(err)
		}
		if out.HybridRecall == nil || !*out.HybridRecall {
			t.Fatalf("round-trip true: want *bool=true, got %+v", out.HybridRecall)
		}
	})
}

func TestRuntimeToolsConfig_HubFieldsJSONRoundTrip(t *testing.T) {
	t.Run("unset_omitted", func(t *testing.T) {
		b, err := json.Marshal(RuntimeToolsConfig{})
		if err != nil {
			t.Fatal(err)
		}
		s := string(b)
		for _, key := range []string{"hub_governance", "hub_knowledge", "hub_fallback_to_default_on_read_error"} {
			if strings.Contains(s, key) {
				t.Fatalf("unset must omit %s, got %s", key, s)
			}
		}
	})
	t.Run("explicit", func(t *testing.T) {
		g, k := "local", "local"
		fb := false
		c := RuntimeToolsConfig{HubGovernance: &g, HubKnowledge: &k, HubFallbackToDefaultOnReadError: &fb}
		b, err := json.Marshal(c)
		if err != nil {
			t.Fatal(err)
		}
		var out RuntimeToolsConfig
		if err := json.Unmarshal(b, &out); err != nil {
			t.Fatal(err)
		}
		if out.HubGovernance == nil || *out.HubGovernance != "local" {
			t.Fatalf("%+v", out.HubGovernance)
		}
		if out.HubFallbackToDefaultOnReadError == nil || *out.HubFallbackToDefaultOnReadError {
			t.Fatalf("%+v", out.HubFallbackToDefaultOnReadError)
		}
	})
}
