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

func TestModelConfigToBiz_codeModel(t *testing.T) {
	biz := modelConfigToBiz(map[string]interface{}{
		"provider":      "openai",
		"model":         "gpt-4",
		"code_provider": "openai",
		"code_model":    "gpt-code",
		"code_api_key":  "sk-c",
		"code_base_url": "http://code",
	})
	if biz.CodeModel != "gpt-code" || biz.CodeAPIKey != "sk-c" || biz.CodeBaseURL != "http://code" {
		t.Fatalf("code fields: %#v", biz)
	}
	m := bizModelConfigToMap(biz)
	if m["code_model"] != "gpt-code" || m["code_api_key"] != "sk-c" {
		t.Fatalf("map=%v", m)
	}
}
