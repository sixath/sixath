package chat

import (
	"errors"
	"fmt"
	"os"
	"strings"
	"sync"

	"backend/internal/biz"

	"github.com/sixath/framework/model"
)

const codeModelRequiredMsg = "本轮是源码分析，需要配置 code 模型后才能继续，不会用对话模型代替。\n请在 Agent 的 code_model、门户全局 code 模型，或环境变量 SATH_CODE_MODEL 中填写模型名。"

const codeModelBuildPrefix = "本轮是源码分析，code 模型创建失败，不会用对话模型代替。"

var (
	ErrCodeModelRequired = errors.New(codeModelRequiredMsg)
	ErrCodeModelBuild    = errors.New(codeModelBuildPrefix)
)

func wrapCodeModelBuild(err error) error {
	if err == nil {
		return ErrCodeModelBuild
	}
	extra := err.Error()
	r := []rune(extra)
	if len(r) > 200 {
		extra = string(r[:200])
	}
	return fmt.Errorf("%w %s", ErrCodeModelBuild, extra)
}

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
// (and env if global Model is empty). Missing code model name or BuildModel
// failure returns an error; the session chat model is never used as a fallback.
func ResolveTurnModel(active map[string]struct{}, chatModel model.Model, meta biz.AgentMeta) (model.Model, error) {
	if chatModel == nil {
		return nil, nil
	}
	if active == nil || !FamilyActive(active, FamilyCode) {
		return chatModel, nil
	}
	spec := resolveCodeModelSpec(meta)
	if strings.TrimSpace(spec.Model) == "" {
		return nil, ErrCodeModelRequired
	}
	provider := strings.TrimSpace(spec.Provider)
	if provider == "" {
		provider = strings.TrimSpace(meta.ModelConfig.Provider)
	}
	if provider == "" {
		provider = "openai"
	}
	modelName := strings.TrimSpace(spec.Model)
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
		return nil, wrapCodeModelBuild(err)
	}
	return m, nil
}

func resolveCodeModelSpec(meta biz.AgentMeta) CodeModelSpec {
	base := GlobalCodeModel()
	if strings.TrimSpace(base.Model) == "" {
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

// CodeModelSpecFromEnv reads SATH_CODE_* (used when global Model is empty).
func CodeModelSpecFromEnv() CodeModelSpec {
	return CodeModelSpec{
		Provider: strings.TrimSpace(os.Getenv("SATH_CODE_PROVIDER")),
		Model:    strings.TrimSpace(os.Getenv("SATH_CODE_MODEL")),
		APIKey:   strings.TrimSpace(os.Getenv("SATH_CODE_API_KEY")),
		BaseURL:  strings.TrimSpace(os.Getenv("SATH_CODE_BASE_URL")),
	}
}
