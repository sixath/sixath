package chat

import (
	"context"
	"testing"

	"backend/internal/biz"

	kratosErrors "github.com/go-kratos/kratos/v2/errors"
)

type fakeTurnModelLoader struct {
	kind      string
	baseURL   string
	apiKey    string
	enabled   bool
	usable    bool
	getErr    error
	usableErr error
}

func (f *fakeTurnModelLoader) GetProviderSecret(context.Context, string) (kind, baseURL, apiKey string, enabled bool, err error) {
	if f.getErr != nil {
		return "", "", "", false, f.getErr
	}
	return f.kind, f.baseURL, f.apiKey, f.enabled, nil
}

func (f *fakeTurnModelLoader) HasUsableEntry(context.Context, string, string) (bool, error) {
	if f.usableErr != nil {
		return false, f.usableErr
	}
	return f.usable, nil
}

func defaultAgent() *biz.AgentMeta {
	return &biz.AgentMeta{ModelConfig: biz.ModelConfig{
		Provider:        "openai",
		Model:           "gpt-4",
		APIKey:          "ak",
		BaseURL:         "https://api.openai.com/v1",
		MaxOutputTokens: 4096,
	}}
}

func turnModelReason(err error) string {
	if err == nil {
		return ""
	}
	return kratosErrors.FromError(err).Reason
}

func TestResolveTurnModelConfig_AgentDefault(t *testing.T) {
	agent := defaultAgent()
	sess := &biz.ChatSession{}
	cfg, err := ResolveTurnModelConfig(context.Background(), nil, agent, sess)
	if err != nil || cfg.Model != "gpt-4" || cfg.APIKey != "ak" || cfg.MaxOutputTokens != 4096 {
		t.Fatalf("%+v %v", cfg, err)
	}
}

func TestResolveTurnModelConfig_Override(t *testing.T) {
	agent := defaultAgent()
	sess := &biz.ChatSession{ModelProviderID: "p1", Model: "deepseek-v3"}
	loader := &fakeTurnModelLoader{
		kind:    "openai_compat",
		baseURL: "https://relay.example/v1",
		apiKey:  "sk-relay",
		enabled: true,
		usable:  true,
	}
	cfg, err := ResolveTurnModelConfig(context.Background(), loader, agent, sess)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Provider != "openai" {
		t.Fatalf("provider=%q want openai", cfg.Provider)
	}
	if cfg.Model != "deepseek-v3" {
		t.Fatalf("model=%q", cfg.Model)
	}
	if cfg.APIKey != "sk-relay" {
		t.Fatalf("apiKey=%q", cfg.APIKey)
	}
	if cfg.BaseURL != "https://relay.example/v1" {
		t.Fatalf("baseURL=%q", cfg.BaseURL)
	}
	if cfg.MaxOutputTokens != 4096 {
		t.Fatalf("maxOutputTokens=%d want agent value", cfg.MaxOutputTokens)
	}
}

func TestResolveTurnModelConfig_DisabledProvider(t *testing.T) {
	agent := defaultAgent()
	sess := &biz.ChatSession{ModelProviderID: "p1", Model: "gpt-4o"}
	loader := &fakeTurnModelLoader{
		kind:    "openai_compat",
		baseURL: "https://relay.example/v1",
		apiKey:  "sk-relay",
		enabled: false,
		usable:  true,
	}
	_, err := ResolveTurnModelConfig(context.Background(), loader, agent, sess)
	if turnModelReason(err) != "MODEL_PROVIDER_UNAVAILABLE" {
		t.Fatalf("reason=%q err=%v", turnModelReason(err), err)
	}
}

func TestResolveTurnModelConfig_HiddenModel(t *testing.T) {
	agent := defaultAgent()
	sess := &biz.ChatSession{ModelProviderID: "p1", Model: "hidden-model"}
	loader := &fakeTurnModelLoader{
		kind:    "openai_compat",
		baseURL: "https://relay.example/v1",
		apiKey:  "sk-relay",
		enabled: true,
		usable:  false,
	}
	_, err := ResolveTurnModelConfig(context.Background(), loader, agent, sess)
	if turnModelReason(err) != "MODEL_CHOICE_UNAVAILABLE" {
		t.Fatalf("reason=%q err=%v", turnModelReason(err), err)
	}
}

func TestResolveTurnModelConfig_PartialOverlay(t *testing.T) {
	agent := defaultAgent()
	sess := &biz.ChatSession{ModelProviderID: "p1"}
	_, err := ResolveTurnModelConfig(context.Background(), nil, agent, sess)
	if err == nil {
		t.Fatal("expected error for partial overlay")
	}
	if turnModelReason(err) != "MODEL_CHOICE_INVALID" {
		t.Fatalf("reason=%q err=%v", turnModelReason(err), err)
	}
}

func TestResolveTurnModelConfig_NilLoaderOverlay(t *testing.T) {
	agent := defaultAgent()
	sess := &biz.ChatSession{ModelProviderID: "p1", Model: "gpt-4o"}
	_, err := ResolveTurnModelConfig(context.Background(), nil, agent, sess)
	if turnModelReason(err) != "MODEL_PROVIDER_UNAVAILABLE" {
		t.Fatalf("reason=%q err=%v", turnModelReason(err), err)
	}
}

func TestResolveTurnModelConfig_UnknownKind(t *testing.T) {
	agent := defaultAgent()
	sess := &biz.ChatSession{ModelProviderID: "p1", Model: "gpt-4o"}
	loader := &fakeTurnModelLoader{
		kind:    "ollama",
		baseURL: "http://localhost:11434",
		apiKey:  "x",
		enabled: true,
		usable:  true,
	}
	_, err := ResolveTurnModelConfig(context.Background(), loader, agent, sess)
	if turnModelReason(err) != "MODEL_PROVIDER_UNAVAILABLE" {
		t.Fatalf("reason=%q err=%v", turnModelReason(err), err)
	}
}

func TestResolveTurnModelConfig_DashScope(t *testing.T) {
	agent := defaultAgent()
	sess := &biz.ChatSession{ModelProviderID: "p-ds", Model: "qwen-max"}
	loader := &fakeTurnModelLoader{
		kind:    "dashscope",
		baseURL: "https://dashscope.aliyuncs.com/compatible-mode/v1",
		apiKey:  "sk-ds",
		enabled: true,
		usable:  true,
	}
	cfg, err := ResolveTurnModelConfig(context.Background(), loader, agent, sess)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Provider != "dashscope" || cfg.Model != "qwen-max" || cfg.APIKey != "sk-ds" || cfg.MaxOutputTokens != 4096 {
		t.Fatalf("%+v", cfg)
	}
}

func TestResolveTurnModelConfig_EmptyAPIKey(t *testing.T) {
	agent := defaultAgent()
	sess := &biz.ChatSession{ModelProviderID: "p1", Model: "gpt-4o"}
	loader := &fakeTurnModelLoader{
		kind:    "openai_compat",
		baseURL: "https://relay.example/v1",
		enabled: true,
		usable:  true,
	}
	_, err := ResolveTurnModelConfig(context.Background(), loader, agent, sess)
	if turnModelReason(err) != "MODEL_PROVIDER_UNAVAILABLE" {
		t.Fatalf("reason=%q err=%v", turnModelReason(err), err)
	}
}

func TestResolveTurnModelConfig_NilSession(t *testing.T) {
	agent := defaultAgent()
	cfg, err := ResolveTurnModelConfig(context.Background(), nil, agent, nil)
	if err != nil || cfg.Model != "gpt-4" || cfg.APIKey != "ak" {
		t.Fatalf("%+v %v", cfg, err)
	}
}

func TestResolveTurnModelConfig_NilAgent(t *testing.T) {
	_, err := ResolveTurnModelConfig(context.Background(), nil, nil, &biz.ChatSession{})
	if err == nil {
		t.Fatal("expected error for nil agent")
	}
}
