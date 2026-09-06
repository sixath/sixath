package data

import "testing"

func TestModelConfigToBiz_maxOutputTokens(t *testing.T) {
	biz := modelConfigToBiz(map[string]interface{}{
		"provider":          "openai",
		"model":             "gpt-4",
		"max_output_tokens": float64(4096),
	})
	if biz.MaxOutputTokens != 4096 {
		t.Fatalf("MaxOutputTokens=%d", biz.MaxOutputTokens)
	}
}

func TestModelConfigToBiz_codeModelIgnored(t *testing.T) {
	biz := modelConfigToBiz(map[string]interface{}{
		"provider":      "openai",
		"model":         "gpt-4",
		"code_provider": "openai",
		"code_model":    "gpt-code",
		"code_api_key":  "sk-c",
		"code_base_url": "http://code",
	})
	m := bizModelConfigToMap(biz)
	for _, key := range []string{"code_provider", "code_model", "code_api_key", "code_base_url"} {
		if _, ok := m[key]; ok {
			t.Fatalf("dead code_* key %s must not round-trip, map=%v", key, m)
		}
	}
}
