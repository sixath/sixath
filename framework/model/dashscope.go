package model

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"time"
	"unicode/utf8"
)

const dashScopeBaseURL = "https://dashscope.aliyuncs.com/api/v1/services/aigc/text-generation/generation"
const defaultDashScopeModel = "qwen-turbo"

// dashScopeMaxMessageRunes 与官方「单段输入」上限一致（见 InvalidParameter: Range of input length should be [1, 1000000]）。
const dashScopeMaxMessageRunes = 1_000_000

// DashScopeClient 阿里云通义千问（DashScope）文本模型适配器，一期仅 Chat/Generate（B.1.1）。
type DashScopeClient struct {
	apiKey string
	model  string
	client *http.Client
}

// DashScopeConfig 用于从配置构建 DashScope 客户端。
type DashScopeConfig struct {
	APIKey string
	Model  string
}

// NewDashScopeClientFromConfig 根据配置构建 DashScope 客户端。
func NewDashScopeClientFromConfig(cfg DashScopeConfig) (*DashScopeClient, error) {
	if cfg.APIKey == "" {
		return nil, errors.New("missing APIKey in DashScopeConfig")
	}
	model := cfg.Model
	if model == "" {
		model = defaultDashScopeModel
	}
	return &DashScopeClient{
		apiKey: cfg.APIKey,
		model:  model,
		client: &http.Client{Timeout: 60 * time.Second},
	}, nil
}

// NewDashScopeClient 从环境变量 DASHSCOPE_API_KEY 创建客户端，可选 DASHSCOPE_MODEL。
func NewDashScopeClient() (*DashScopeClient, error) {
	apiKey := os.Getenv("DASHSCOPE_API_KEY")
	if apiKey == "" {
		return nil, errors.New("missing DASHSCOPE_API_KEY")
	}
	model := os.Getenv("DASHSCOPE_MODEL")
	if model == "" {
		model = defaultDashScopeModel
	}
	return NewDashScopeClientFromConfig(DashScopeConfig{APIKey: apiKey, Model: model})
}

type dashScopeReq struct {
	Model      string          `json:"model"`
	Input      dashScopeInput  `json:"input"`
	Parameters dashScopeParams `json:"parameters"`
}

type dashScopeInput struct {
	Messages []dashScopeMsg `json:"messages"`
}

type dashScopeMsg struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

type dashScopeParams struct {
	ResultFormat string  `json:"result_format,omitempty"` // "message" | "text"
	MaxTokens    int     `json:"max_tokens,omitempty"`
	Temperature  float64 `json:"temperature,omitempty"`
}

type dashScopeResp struct {
	Output dashScopeOutput `json:"output"`
	Usage  *dashScopeUsage `json:"usage"`
}

type dashScopeOutput struct {
	Text string `json:"text"`
}

type dashScopeUsage struct {
	InputTokens  int `json:"input_tokens"`
	OutputTokens int `json:"output_tokens"`
}

// dashScopePlainContent 组装发往 DashScope 的单条文本：优先 Content；若为空则从多模态 Parts 抽取文本块。
func dashScopePlainContent(m Message) string {
	if len(strings.TrimSpace(m.Content)) > 0 {
		return m.Content
	}
	var b strings.Builder
	for _, p := range m.Parts {
		if p.Type != ContentTypeText {
			continue
		}
		t := strings.TrimSpace(p.Text)
		if t == "" {
			continue
		}
		if b.Len() > 0 {
			b.WriteByte('\n')
		}
		b.WriteString(t)
	}
	return b.String()
}

func readDashScopeErrorBody(r io.Reader) string {
	var buf bytes.Buffer
	_, _ = io.CopyN(&buf, r, 64<<10)
	s := strings.TrimSpace(buf.String())
	if s == "" {
		return "(empty body)"
	}
	return s
}

func (c *DashScopeClient) Generate(ctx context.Context, prompt string, opts ...Option) (*Generation, error) {
	msgs := []Message{{Role: "user", Content: prompt}}
	return c.Chat(ctx, msgs, opts...)
}

func (c *DashScopeClient) Chat(ctx context.Context, messages []Message, opts ...Option) (*Generation, error) {
	if len(messages) == 0 {
		return nil, errors.New("messages is empty")
	}
	callCfg := ApplyOptions(opts...)
	modelName := c.model
	if callCfg.ModelName != "" {
		modelName = callCfg.ModelName
	}
	dsMsgs := make([]dashScopeMsg, 0, len(messages))
	for i, m := range messages {
		content := dashScopePlainContent(m)
		if content == "" {
			return nil, fmt.Errorf("dashscope: message[%d] role=%q has empty content (API requires length in [1,%d])", i, m.Role, dashScopeMaxMessageRunes)
		}
		n := utf8.RuneCountInString(content)
		if n > dashScopeMaxMessageRunes {
			return nil, fmt.Errorf("dashscope: message[%d] role=%q content too long: %d runes (max %d)", i, m.Role, n, dashScopeMaxMessageRunes)
		}
		dsMsgs = append(dsMsgs, dashScopeMsg{Role: m.Role, Content: content})
	}
	params := dashScopeParams{ResultFormat: "message"}
	if callCfg.MaxTokens > 0 {
		params.MaxTokens = callCfg.MaxTokens
	}
	if callCfg.Temperature > 0 {
		params.Temperature = float64(callCfg.Temperature)
	}
	body := dashScopeReq{
		Model:      modelName,
		Input:      dashScopeInput{Messages: dsMsgs},
		Parameters: params,
	}
	data, err := json.Marshal(body)
	if err != nil {
		return nil, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, dashScopeBaseURL, bytes.NewReader(data))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+c.apiKey)
	req.Header.Set("Content-Type", "application/json")
	resp, err := c.client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		detail := readDashScopeErrorBody(resp.Body)
		return nil, fmt.Errorf("dashscope api: %s: %s", resp.Status, detail)
	}
	var out dashScopeResp
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return nil, err
	}
	gen := &Generation{Text: out.Output.Text}
	if out.Usage != nil {
		gen.TokenUsage = &TokenUsage{
			InputTokens:  out.Usage.InputTokens,
			OutputTokens: out.Usage.OutputTokens,
		}
	}
	return gen, nil
}

func (c *DashScopeClient) Embed(ctx context.Context, texts []string, opts ...Option) ([]Embedding, error) {
	return nil, errors.New("dashscope: Embed not implemented")
}
