package chat

import (
	"os"
	"strings"
	"sync"

	"backend/internal/biz"

	"github.com/sixath/framework/model"
)

// CodeModelSpec is the optional stronger model used when FamilyCode is active.
type CodeModelSpec struct {
	Provider string `json:"provider,omitempty"`
	Model    string `json:"model,omitempty"`
	APIKey   string `json:"api_key,omitempty"`
	BaseURL  string `json:"base_url,omitempty"`
}

func (s CodeModelSpec) Usable() bool {
	return strings.TrimSpace(s.Model) != "" || strings.TrimSpace(s.APIKey) != "" || strings.TrimSpace(s.BaseURL) != ""
}

func (s CodeModelSpec) hasAny() bool {
	return strings.TrimSpace(s.Provider) != "" || strings.TrimSpace(s.Model) != "" ||
		strings.TrimSpace(s.APIKey) != "" || strings.TrimSpace(s.BaseURL) != ""
}

var (
	globalCodeMu   sync.RWMutex
	globalCodeSpec CodeModelSpec
)

// SetGlobalCodeModel stores the UI/DB global code-family model (process cache).
func SetGlobalCodeModel(spec CodeModelSpec) {
	globalCodeMu.Lock()
	defer globalCodeMu.Unlock()
	globalCodeSpec = spec
}

// GlobalCodeModel returns the cached global code-family model.
func GlobalCodeModel() CodeModelSpec {
	globalCodeMu.RLock()
	defer globalCodeMu.RUnlock()
	return globalCodeSpec
}

// ResolveTurnModel returns a code-family model when FamilyCode is active.
// Agent code_* overlays the global setting; empty agent fields inherit global
// (and env if global is empty). Build failure fail-opens to the session model.
func ResolveTurnModel(active map[string]struct{}, chatModel model.Model, meta biz.AgentMeta) model.Model {
	if chatModel == nil {
		return nil
	}
	if active == nil || !FamilyActive(active, FamilyCode) {
		return chatModel
	}
	spec := resolveCodeModelSpec(meta)
	if !spec.Usable() {
		return chatModel
	}
	provider := strings.TrimSpace(spec.Provider)
	if provider == "" {
		provider = strings.TrimSpace(meta.ModelConfig.Provider)
	}
	if provider == "" {
		provider = "openai"
	}
	modelName := strings.TrimSpace(spec.Model)
	if modelName == "" {
		modelName = strings.TrimSpace(meta.ModelConfig.Model)
	}
	apiKey := strings.TrimSpace(spec.APIKey)
	if apiKey == "" {
		apiKey = strings.TrimSpace(meta.ModelConfig.APIKey)
	}
	baseURL := strings.TrimSpace(spec.BaseURL)
	if baseURL == "" {
		baseURL = strings.TrimSpace(meta.ModelConfig.BaseURL)
	}
	m, err := BuildModel(provider, modelName, apiKey, baseURL)
	if err != nil || m == nil {
		return chatModel
	}
	return m
}

func resolveCodeModelSpec(meta biz.AgentMeta) CodeModelSpec {
	base := GlobalCodeModel()
	if !base.Usable() {
		base = overlayCodeModel(base, CodeModelSpecFromEnv())
	}
	agent := agentCodeModelSpec(meta)
	if !agent.hasAny() {
		return base
	}
	return overlayCodeModel(base, agent)
}

func agentCodeModelSpec(meta biz.AgentMeta) CodeModelSpec {
	return CodeModelSpec{
		Provider: strings.TrimSpace(meta.ModelConfig.CodeProvider),
		Model:    strings.TrimSpace(meta.ModelConfig.CodeModel),
		APIKey:   strings.TrimSpace(meta.ModelConfig.CodeAPIKey),
		BaseURL:  strings.TrimSpace(meta.ModelConfig.CodeBaseURL),
	}
}

func overlayCodeModel(base, over CodeModelSpec) CodeModelSpec {
	out := base
	if s := strings.TrimSpace(over.Provider); s != "" {
		out.Provider = s
	}
	if s := strings.TrimSpace(over.Model); s != "" {
		out.Model = s
	}
	if s := strings.TrimSpace(over.APIKey); s != "" {
		out.APIKey = s
	}
	if s := strings.TrimSpace(over.BaseURL); s != "" {
		out.BaseURL = s
	}
	return out
}

// CodeModelSpecFromEnv reads SATH_CODE_* (used when global UI/DB is empty).
func CodeModelSpecFromEnv() CodeModelSpec {
	return CodeModelSpec{
		Provider: strings.TrimSpace(os.Getenv("SATH_CODE_PROVIDER")),
		Model:    strings.TrimSpace(os.Getenv("SATH_CODE_MODEL")),
		APIKey:   strings.TrimSpace(os.Getenv("SATH_CODE_API_KEY")),
		BaseURL:  strings.TrimSpace(os.Getenv("SATH_CODE_BASE_URL")),
	}
}
