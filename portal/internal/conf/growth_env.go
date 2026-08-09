package conf

import (
	"os"
	"strconv"
	"strings"
)

// EnrichGrowthFromEnv 用环境变量补全 Growth.llm（YAML 未配 model 时）。
// 变量：SATH_GROWTH_LLM_PROVIDER、SATH_GROWTH_LLM_MODEL、SATH_GROWTH_LLM_API_KEY、SATH_GROWTH_LLM_BASE_URL、
// SATH_GROWTH_LLM_MAX_TRANSCRIPT_RUNES。
func EnrichGrowthFromEnv(g *Growth) {
	if g == nil {
		return
	}
	modelName := strings.TrimSpace(os.Getenv("SATH_GROWTH_LLM_MODEL"))
	if modelName == "" {
		return
	}
	if g.Llm == nil {
		g.Llm = &GrowthLLM{}
	}
	if strings.TrimSpace(g.Llm.GetModel()) == "" {
		g.Llm.Model = modelName
	}
	if strings.TrimSpace(g.Llm.GetProvider()) == "" {
		if p := strings.TrimSpace(os.Getenv("SATH_GROWTH_LLM_PROVIDER")); p != "" {
			g.Llm.Provider = p
		}
	}
	if strings.TrimSpace(g.Llm.GetApiKey()) == "" {
		if k := strings.TrimSpace(os.Getenv("SATH_GROWTH_LLM_API_KEY")); k != "" {
			g.Llm.ApiKey = k
		}
	}
	if strings.TrimSpace(g.Llm.GetBaseUrl()) == "" {
		if u := strings.TrimSpace(os.Getenv("SATH_GROWTH_LLM_BASE_URL")); u != "" {
			g.Llm.BaseUrl = u
		}
	}
	if g.Llm.GetMaxTranscriptRunes() <= 0 {
		if v := strings.TrimSpace(os.Getenv("SATH_GROWTH_LLM_MAX_TRANSCRIPT_RUNES")); v != "" {
			if n, err := strconv.Atoi(v); err == nil && n > 0 {
				g.Llm.MaxTranscriptRunes = int32(n)
			}
		}
	}
}
