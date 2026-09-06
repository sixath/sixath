package model

import (
	"context"
	"errors"
	"io"
	"net/http"
	"os"
	"time"

	openai "github.com/sashabaranov/go-openai"
)

const defaultOpenAIModel = "gpt-3.5-turbo"

type OpenAIClient struct {
	apiKey     string
	client     *openai.Client
	model      string
	httpClient *http.Client
	baseURL    string
}

// OpenAIConfig 用于从配置构建 OpenAI 客户端，供 portal 等上层按 Agent 配置创建模型。
type OpenAIConfig struct {
	APIKey  string
	BaseURL string
	Model   string
	Timeout time.Duration // 请求超时，0 表示 120 秒
}

// NewOpenAIClientFromConfig 根据配置构建 OpenAI 客户端，APIKey 必填。
func NewOpenAIClientFromConfig(cfg OpenAIConfig) (*OpenAIClient, error) {
	if cfg.APIKey == "" {
		return nil, errors.New("missing APIKey in OpenAIConfig")
	}
	baseURL := cfg.BaseURL
	if baseURL == "" {
		baseURL = "https://api.openai.com/v1"
	}
	model := cfg.Model
	if model == "" {
		model = defaultOpenAIModel
	}
	ocfg := openai.DefaultConfig(cfg.APIKey)
	ocfg.BaseURL = baseURL
	timeout := cfg.Timeout
	if timeout <= 0 {
		timeout = 120 * time.Second
	}
	httpClient := &http.Client{Timeout: timeout}
	ocfg.HTTPClient = httpClient

	return &OpenAIClient{
		apiKey:     cfg.APIKey,
		client:     openai.NewClientWithConfig(ocfg),
		model:      model,
		httpClient: httpClient,
		baseURL:    baseURL,
	}, nil
}

// NewOpenAIClient 从环境变量构建一个默认客户端。
// 依赖 OPENAI_API_KEY，可选 OPENAI_BASE_URL、OPENAI_MODEL。
func NewOpenAIClient() (*OpenAIClient, error) {
	apiKey := os.Getenv("OPENAI_API_KEY")
	if apiKey == "" {
		return nil, errors.New("missing OPENAI_API_KEY")
	}
	baseURL := os.Getenv("OPENAI_BASE_URL")
	if baseURL == "" {
		baseURL = "https://api.openai.com/v1"
	}
	model := os.Getenv("OPENAI_MODEL")
	if model == "" {
		model = defaultOpenAIModel
	}
	return NewOpenAIClientFromConfig(OpenAIConfig{APIKey: apiKey, BaseURL: baseURL, Model: model})
}

// Generate 使用单轮对话形式实现简单文本生成。
func (c *OpenAIClient) Generate(ctx context.Context, prompt string, opts ...Option) (*Generation, error) {
	msgs := []Message{
		{Role: openai.ChatMessageRoleUser, Content: prompt},
	}
	return c.Chat(ctx, msgs, opts...)
}

func (c *OpenAIClient) Chat(ctx context.Context, messages []Message, opts ...Option) (*Generation, error) {
	if len(messages) == 0 {
		return nil, errors.New("messages is empty")
	}
	callCfg := ApplyOptions(opts...)
	req := c.buildChatRequest(messages, callCfg)
	resp, err := c.client.CreateChatCompletion(ctx, req)
	if err != nil {
		return nil, err
	}
	if len(resp.Choices) == 0 {
		return nil, errors.New("no choices in response")
	}

	text := resp.Choices[0].Message.Content
	gen := &Generation{Text: text, Raw: resp}
	if resp.Usage.PromptTokens > 0 || resp.Usage.CompletionTokens > 0 {
		gen.TokenUsage = &TokenUsage{
			InputTokens:  resp.Usage.PromptTokens,
			OutputTokens: resp.Usage.CompletionTokens,
		}
	}
	return gen, nil
}

// buildChatRequest 构建 Chat 请求体，供 Chat 与 ChatStream 复用。
func (c *OpenAIClient) buildChatRequest(messages []Message, callCfg *CallConfig) openai.ChatCompletionRequest {
	modelName := c.model
	if callCfg != nil && callCfg.ModelName != "" {
		modelName = callCfg.ModelName
	}
	temp, maxTok := float32(0.7), 1024
	if callCfg != nil {
		if callCfg.Temperature > 0 {
			temp = float32(callCfg.Temperature)
		}
		if callCfg.MaxTokens > 0 {
			maxTok = callCfg.MaxTokens
		}
	}
	req := openai.ChatCompletionRequest{
		Model:       modelName,
		Messages:    make([]openai.ChatCompletionMessage, 0, len(messages)),
		Temperature: temp,
		MaxTokens:   maxTok,
	}
	for _, m := range messages {
		msg := openai.ChatCompletionMessage{Role: m.Role}
		if len(m.Parts) > 0 {
			mc := make([]openai.ChatMessagePart, 0, len(m.Parts))
			for _, p := range m.Parts {
				switch p.Type {
				case ContentTypeText:
					mc = append(mc, openai.ChatMessagePart{Type: openai.ChatMessagePartTypeText, Text: sanitizeOpenAIMessageContent(p.Text)})
				case ContentTypeImageURL:
					mc = append(mc, openai.ChatMessagePart{
						Type:     openai.ChatMessagePartTypeImageURL,
						ImageURL: &openai.ChatMessageImageURL{URL: p.URL},
					})
				default:
				}
			}
			if len(mc) > 0 {
				msg.MultiContent = mc
			}
		}
		if msg.MultiContent == nil {
			msg.Content = sanitizeOpenAIMessageContent(m.Content)
		}
		req.Messages = append(req.Messages, msg)
	}
	return req
}

// ChatStream 流式对话，返回增量文本 channel；实现 StreamingModel 接口。
func (c *OpenAIClient) ChatStream(ctx context.Context, messages []Message, opts ...Option) (<-chan string, error) {
	if len(messages) == 0 {
		return nil, errors.New("messages is empty")
	}
	callCfg := ApplyOptions(opts...)
	req := c.buildChatRequest(messages, callCfg)

	stream, err := c.client.CreateChatCompletionStream(ctx, req)
	if err != nil {
		return nil, err
	}

	ch := make(chan string)
	go func() {
		defer close(ch)
		defer stream.Close()
		for {
			select {
			case <-ctx.Done():
				return
			default:
			}
			resp, err := stream.Recv()
			if err != nil {
				if err != io.EOF {
					// 流式错误无法通过 channel 返回，调用方可通过 ctx 取消感知
					return
				}
				return
			}
			if len(resp.Choices) > 0 && resp.Choices[0].Delta.Content != "" {
				select {
				case ch <- resp.Choices[0].Delta.Content:
				case <-ctx.Done():
					return
				}
			}
		}
	}()
	return ch, nil
}

// Embed 使用 OpenAI Embeddings API 生成文本向量。
func (c *OpenAIClient) Embed(ctx context.Context, texts []string, opts ...Option) ([]Embedding, error) {
	if len(texts) == 0 {
		return nil, errors.New("texts is empty")
	}

	// 目前暂不通过 Option 配置 embedding 模型，直接使用默认模型。
	req := openai.EmbeddingRequest{
		Input: texts,
		Model: openai.SmallEmbedding3,
	}

	resp, err := c.client.CreateEmbeddings(ctx, req)
	if err != nil {
		return nil, err
	}
	if len(resp.Data) == 0 {
		return nil, errors.New("no embeddings in response")
	}

	out := make([]Embedding, 0, len(resp.Data))
	for _, e := range resp.Data {
		cp := make([]float32, len(e.Embedding))
		copy(cp, e.Embedding)
		out = append(out, Embedding{
			Vector: cp,
			Raw:    e,
		})
	}
	return out, nil
}
