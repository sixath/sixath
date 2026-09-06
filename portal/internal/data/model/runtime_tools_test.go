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
