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
