package model

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"unicode/utf8"

	openai "github.com/sashabaranov/go-openai"

	"github.com/sixath/framework/tool"
)

// openAICompatMaxMessageRunes 与若干 OpenAI 兼容网关（如通义）对单条 message.content 的限制一致：
// InvalidParameter: Range of input length should be [1, 1000000]
const openAICompatMaxMessageRunes = 1_000_000

const openAICompatTruncationSuffix = "\n...[truncated for API max length]"

// sanitizeOpenAIMessageContent 清理单条 message.content：非法 UTF-8、NUL、除 \n\r\t 外的 ASCII 控制符、
// U+2028/U+2029（部分网关/JSON 链路对脚本输出敏感）。避免 execute_skill_script 等二进制/日志片段触发泛型 400。
func sanitizeOpenAIMessageContent(s string) string {
	return SanitizeMessageContent(s)
}

// patchChatCompletionMessageForStrictGateways 修正/截断单条消息，避免兼容网关对空 content 或超长 content 返回 400。
func patchChatCompletionMessageForStrictGateways(msg *openai.ChatCompletionMessage) {
	if msg == nil {
		return
	}
	msg.Content = sanitizeOpenAIMessageContent(msg.Content)
	role := strings.ToLower(msg.Role)
	// 仅发起 tool_calls 时 OpenAI 允许 content 为空；部分兼容实现（如通义 OpenAI 兼容）要求长度 ∈ [1,1e6]。
	if role == "assistant" && len(msg.ToolCalls) > 0 && strings.TrimSpace(msg.Content) == "" {
		msg.Content = " "
	}
	if role == "tool" && strings.TrimSpace(msg.Content) == "" {
		msg.Content = `{"error":"empty tool message"}`
	}
	// 兜底：任何既无内容又无 tool_calls 的消息（如历史里一条空的 assistant 回复），
	// 补一个空格占位，避免严格网关返回 400 "content is a required field"。
	if strings.TrimSpace(msg.Content) == "" && len(msg.ToolCalls) == 0 {
		msg.Content = " "
	}
	if msg.Content == "" {
		return
	}
	if n := utf8.RuneCountInString(msg.Content); n <= openAICompatMaxMessageRunes {
		return
	}
	suf := openAICompatTruncationSuffix
	sufN := utf8.RuneCountInString(suf)
	budget := openAICompatMaxMessageRunes - sufN
	if budget < 1 {
		budget = 1
	}
	runes := []rune(msg.Content)
	if len(runes) > budget {
		msg.Content = string(runes[:budget]) + suf
	}
}

func (c *OpenAIClient) ChatWithTools(ctx context.Context, messages []Message, reg *tool.Registry, opts ...Option) (*Generation, error) {
	if len(messages) == 0 {
		return nil, errors.New("messages is empty")
	}
	if reg == nil {
		return nil, errors.New("tool registry is nil")
	}

	callCfg := ApplyOptions(opts...)
	modelName := c.model
	if callCfg.ModelName != "" {
		modelName = callCfg.ModelName
	}

	msgs := PrepareChatContextCtx(ctx, messages, callCfg)

	deferActive, _ := ctx.Value(tool.ContextKeyToolSearchActive).(bool)
	tools := reg.ListForAPIWithDefer(ctx, nil, deferActive)
	if len(tools) == 0 {
		return nil, errors.New("no tools registered")
	}

	req := openai.ChatCompletionRequest{
		Model:       modelName,
		Messages:    make([]openai.ChatCompletionMessage, 0, len(msgs)),
		Tools:       make([]openai.Tool, 0, len(tools)),
		ToolChoice:  "auto",
		Temperature: float32(callCfg.Temperature),
		MaxTokens:   callCfg.MaxTokens,
	}

	for _, m := range msgs {
		msg, err := openAIChatMessage(m)
		if err != nil {
			return nil, err
		}
		req.Messages = append(req.Messages, msg)
	}
	for _, tl := range tools {
		req.Tools = append(req.Tools, openai.Tool{
			Type: openai.ToolTypeFunction,
			Function: &openai.FunctionDefinition{
				Name:        tl.Name,
				Description: tl.Description,
				Parameters:  tl.Parameters,
			},
		})
	}

	resp, err := c.client.CreateChatCompletion(ctx, req)
	if err != nil {
		return nil, err
	}
	if len(resp.Choices) == 0 {
		return nil, errors.New("no choices in response")
	}

	msg := resp.Choices[0].Message
	if len(msg.ToolCalls) == 0 {
		return &Generation{
			Text: msg.Content,
			Raw: ToolStep{
				Used:             false,
				ReasoningContent: msg.ReasoningContent,
			},
		}, nil
	}

	calls := make([]ToolCall, 0, len(msg.ToolCalls))
	for _, call := range msg.ToolCalls {
		if _, ok := reg.Get(call.Function.Name); !ok {
			return nil, fmt.Errorf("tool not found: %s", call.Function.Name)
		}

		params, repaired, parseErr := parseToolArguments(call.Function.Arguments)
		calls = append(calls, ToolCall{
			ID:                     call.ID,
			Name:                   call.Function.Name,
			Arguments:              params,
			ArgumentsRepaired:      repaired,
			RawArgumentsPreview:    rawArgumentsPreview(call.Function.Arguments),
			RawArgumentsParseError: parseErr,
		})
	}
	first := calls[0]

	return &Generation{
		Text: msg.Content,
		Raw: ToolStep{
			Used:             true,
			ToolCallID:       first.ID,
			ToolName:         first.Name,
			Arguments:        first.Arguments,
			ToolCalls:        calls,
			ReasoningContent: msg.ReasoningContent,
		},
	}, nil
}

func openAIChatMessage(m Message) (openai.ChatCompletionMessage, error) {
	msg := openai.ChatCompletionMessage{
		Role:    m.Role,
		Content: m.Content,
	}
	if m.Metadata != nil {
		if id, _ := m.Metadata["tool_call_id"].(string); id != "" {
			msg.ToolCallID = id
		}
		if tn, _ := m.Metadata["tool_name"].(string); tn != "" {
			msg.Name = tn
		} else if name, _ := m.Metadata["name"].(string); name != "" {
			msg.Name = name
		}
		calls, err := metadataToolCalls(m.Metadata["tool_calls"])
		if err != nil {
			return msg, err
		}
		if len(calls) > 0 {
			msg.ToolCalls = calls
			// DeepSeek thinking：有 tool_calls 时必须带 reasoning_content（缺省空串）。
			if rc, ok := m.Metadata[MetadataKeyReasoningContent].(string); ok {
				msg.ReasoningContent = rc
			} else {
				msg.ReasoningContent = ""
			}
		} else if rc, _ := m.Metadata[MetadataKeyReasoningContent].(string); rc != "" {
			msg.ReasoningContent = rc
		}
	}
	patchChatCompletionMessageForStrictGateways(&msg)
	return msg, nil
}

func metadataToolCalls(raw any) ([]openai.ToolCall, error) {
	if raw == nil {
		return nil, nil
	}
	switch calls := raw.(type) {
	case []ToolCall:
		out := make([]openai.ToolCall, 0, len(calls))
		for _, call := range calls {
			args, err := json.Marshal(call.Arguments)
			if err != nil {
				return nil, fmt.Errorf("marshal tool call arguments: %w", err)
			}
			out = append(out, openai.ToolCall{
				ID:   call.ID,
				Type: openai.ToolTypeFunction,
				Function: openai.FunctionCall{
					Name:      call.Name,
					Arguments: string(args),
				},
			})
		}
		return out, nil
	case []openai.ToolCall:
		return calls, nil
	default:
		return nil, fmt.Errorf("unsupported tool_calls metadata type %T", raw)
	}
}

func parseToolArguments(raw string) (map[string]any, bool, string) {
	var params map[string]any
	if err := json.Unmarshal([]byte(raw), &params); err == nil {
		return params, false, ""
	}

	fixed, ok := tryRepairJSONObject(raw)
	if !ok {
		return map[string]any{}, false, "invalid arguments json"
	}
	if err := json.Unmarshal([]byte(fixed), &params); err != nil {
		return map[string]any{}, false, err.Error()
	}
	return params, true, ""
}

// tryRepairJSONObject 对常见的截断 JSON 做最小补全（仅补齐未闭合的大括号）。
func tryRepairJSONObject(raw string) (string, bool) {
	s := strings.TrimSpace(raw)
	if s == "" {
		return "", false
	}
	// 只修复 object 形态参数，避免对数组/其他形态做激进改写。
	if !strings.HasPrefix(s, "{") {
		return "", false
	}

	inString := false
	escape := false
	braces := 0
	for _, r := range s {
		if inString {
			if escape {
				escape = false
				continue
			}
			if r == '\\' {
				escape = true
				continue
			}
			if r == '"' {
				inString = false
			}
			continue
		}
		if r == '"' {
			inString = true
			continue
		}
		if r == '{' {
			braces++
		}
		if r == '}' {
			braces--
			if braces < 0 {
				return "", false
			}
		}
	}
	if inString || braces < 0 {
		return "", false
	}
	if braces == 0 {
		return s, true
	}
	return s + strings.Repeat("}", braces), true
}

func rawArgumentsPreview(raw string) string {
	const maxLen = 240
	s := strings.TrimSpace(raw)
	if len(s) <= maxLen {
		return s
	}
	return s[:maxLen] + "...(truncated)"
}
