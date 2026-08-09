package chat

import (
	"os"
	"strings"

	"github.com/sixath/framework/model"
	"github.com/sixath/framework/tool"
)

// visionEnabledFromEnv returns false when SATH_VISION_ENABLED is 0/false/off/no.
// Default is enabled when a model is available.
func visionEnabledFromEnv() bool {
	v := strings.ToLower(strings.TrimSpace(os.Getenv("SATH_VISION_ENABLED")))
	switch v {
	case "0", "false", "off", "no":
		return false
	default:
		return true
	}
}

// VisionAnalyzerForModel builds a vision analyzer from the agent chat model,
// optionally overridden by SATH_VISION_* env (provider/model/api_key/base_url).
func VisionAnalyzerForModel(agentModel model.Model) tool.VisionAnalyzer {
	if !visionEnabledFromEnv() {
		return nil
	}
	m := agentModel
	if v := visionModelFromEnv(); v != nil {
		m = v
	}
	if m == nil {
		return nil
	}
	opts := []model.Option{model.WithMaxTokens(1024)}
	if name := strings.TrimSpace(os.Getenv("SATH_VISION_MODEL")); name != "" && visionModelFromEnv() == nil {
		opts = append(opts, model.WithModelName(name))
	}
	return model.NewVisionAnalyzer(m, opts...)
}

func visionModelFromEnv() model.Model {
	provider := strings.TrimSpace(os.Getenv("SATH_VISION_PROVIDER"))
	apiKey := strings.TrimSpace(os.Getenv("SATH_VISION_API_KEY"))
	baseURL := strings.TrimSpace(os.Getenv("SATH_VISION_BASE_URL"))
	modelName := strings.TrimSpace(os.Getenv("SATH_VISION_MODEL"))
	if provider == "" && apiKey == "" && baseURL == "" {
		return nil
	}
	if modelName == "" {
		modelName = "gpt-4o-mini"
	}
	if provider == "" {
		provider = "openai"
	}
	m, err := model.NewModelFromConfig(model.ModelConfig{
		Provider: provider,
		Model:    modelName,
		APIKey:   apiKey,
		BaseURL:  baseURL,
	})
	if err != nil {
		return nil
	}
	return m
}
