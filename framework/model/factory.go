package model

import (
	"fmt"
	"strings"
	"sync"
	"time"
)

// ModelConfig 用于按 Agent 配置创建模型，供 portal 等上层使用。
type ModelConfig struct {
	Provider string // openai, dashscope, ollama
	Model    string
	APIKey   string
	BaseURL  string
	Timeout  time.Duration // 请求超时，0 表示默认 120 秒
}

// NewModelFromConfig 根据配置创建模型实例，支持 openai、dashscope、ollama。
// ollama 暂不支持 APIKey/BaseURL 覆盖，仍使用默认端点。
func NewModelFromConfig(cfg ModelConfig) (Model, error) {
	provider := strings.ToLower(strings.TrimSpace(cfg.Provider))
	if provider == "" {
		provider = "openai"
	}
	modelName := strings.TrimSpace(cfg.Model)

	switch provider {
	case "openai":
		ocfg := OpenAIConfig{APIKey: cfg.APIKey, BaseURL: cfg.BaseURL, Model: modelName, Timeout: cfg.Timeout}
		if ocfg.APIKey == "" {
			// 回退到 env
			return NewOpenAIClient()
		}
		return NewOpenAIClientFromConfig(ocfg)
	case "dashscope":
		dscfg := DashScopeConfig{APIKey: cfg.APIKey, Model: modelName}
		if dscfg.APIKey == "" {
			return NewDashScopeClient()
		}
		return NewDashScopeClientFromConfig(dscfg)
	case "ollama":
		cli, err := NewOllamaClient()
		if err != nil {
			return nil, err
		}
		if modelName != "" {
			cli.model = modelName
		}
		return cli, nil
	default:
		return nil, fmt.Errorf("unsupported model provider: %s", provider)
	}
}

// ModelProvider 根据完整标识（如 "openai/gpt-4o"）创建模型实例，供插件等扩展注册。
type ModelProvider func(id string) (Model, error)

var (
	providerMu sync.RWMutex
	providers  = make(map[string]ModelProvider)
)

// RegisterProvider 注册一个模型 Provider，供 NewFromIdentifier 使用。
// 通常由插件在 init() 中调用。provider 名称为小写，如 "openai"、"ollama"。
func RegisterProvider(provider string, f ModelProvider) {
	if f == nil {
		return
	}
	providerMu.Lock()
	defer providerMu.Unlock()
	providers[strings.ToLower(provider)] = f
}

// NewFromIdentifier 根据配置标识创建模型实例。
// 约定格式类似： "openai/gpt-4o"、"openai/gpt-3.5-turbo"。
// 先查找插件注册的 Provider，再回退到内置的 openai/ollama。
func NewFromIdentifier(id string) (Model, error) {
	provider, modelName := parseModelIdentifier(id)

	providerMu.RLock()
	ext, ok := providers[provider]
	providerMu.RUnlock()
	if ok && ext != nil {
		return ext(id)
	}

	switch provider {
	case "openai", "":
		cli, err := NewOpenAIClient()
		if err != nil {
			return nil, err
		}
		if modelName != "" {
			cli.model = modelName
		}
		return cli, nil
	case "ollama":
		cli, err := NewOllamaClient()
		if err != nil {
			return nil, err
		}
		if modelName != "" {
			cli.model = modelName
		}
		return cli, nil
	case "dashscope":
		cli, err := NewDashScopeClient()
		if err != nil {
			return nil, err
		}
		if modelName != "" {
			cli.model = modelName
		}
		return cli, nil
	default:
		return nil, fmt.Errorf("unsupported model provider: %s", provider)
	}
}

func parseModelIdentifier(id string) (provider, modelName string) {
	if id == "" {
		return "", ""
	}
	parts := strings.SplitN(id, "/", 2)
	if len(parts) == 1 {
		return strings.ToLower(strings.TrimSpace(parts[0])), ""
	}
	return strings.ToLower(strings.TrimSpace(parts[0])), strings.TrimSpace(parts[1])
}
