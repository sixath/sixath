package chat

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/sixath/framework/model"
	"github.com/sixath/framework/tool"
)

type inputResponseContextKey struct{}

// InputResponse 用户通过 input_required 卡片提交的结构化响应。
type InputResponse struct {
	Token      string `json:"token"`
	RequestID  string `json:"request_id"`
	Field      string `json:"field"`
	Value      string `json:"value"`
	Cancelled  bool   `json:"cancelled"`
	ToolCallID string `json:"tool_call_id"`
}

// WithInputResponse 将结构化输入响应绑定到 context（SSE HTTP 路径使用）。
func WithInputResponse(ctx context.Context, ir *InputResponse) context.Context {
	if ir == nil {
		return ctx
	}
	return context.WithValue(ctx, inputResponseContextKey{}, ir)
}

// InputResponseFromContext 读取绑定的 input_response。
func InputResponseFromContext(ctx context.Context) *InputResponse {
	if ctx == nil {
		return nil
	}
	ir, _ := ctx.Value(inputResponseContextKey{}).(*InputResponse)
	return ir
}

type SyntheticAskUserOutcome int

const (
	SyntheticAskUserOutcomeFulfilled SyntheticAskUserOutcome = iota
	SyntheticAskUserOutcomeCancelled
)

// UserMessagePlaceholderForInput 持久化到 chat_messages 的占位符，不含明文 secret。
func UserMessagePlaceholderForInput(field string) string {
	if field == "" {
		field = "input"
	}
	return fmt.Sprintf("[input provided: %s]", field)
}

// UserMessageContentForTurn 决定本轮 user 消息写入 DB 与注入 agent 的文本。
func UserMessageContentForTurn(content string, ir *InputResponse) string {
	if ir == nil {
		return content
	}
	if ir.Cancelled {
		if content != "" {
			return content
		}
		return "[input cancelled]"
	}
	if content != "" {
		return content
	}
	return UserMessagePlaceholderForInput(ir.Field)
}

// ApplyInputResponse 校验 token、写入 fulfillment store，并返回 pending 元数据。
func ApplyInputResponse(ctx context.Context, sessionID string, ir InputResponse, pendingStore tool.AskUserPendingStore, fulfillStore tool.AskUserFulfillmentStore) (tool.PendingInputRequest, SyntheticAskUserOutcome, error) {
	if pendingStore == nil {
		return tool.PendingInputRequest{}, 0, errors.New("ask_user: pending store not configured")
	}
	if sessionID == "" || ir.Token == "" {
		return tool.PendingInputRequest{}, 0, errors.New("ask_user: session_id and token required")
	}
	pending, err := pendingStore.GetPending(ctx, sessionID, ir.Token)
	if err != nil {
		return tool.PendingInputRequest{}, 0, err
	}
	if pending == nil {
		return tool.PendingInputRequest{}, 0, errors.New("ask_user: invalid or expired token")
	}
	if ir.Cancelled {
		if pending.ToolCallID == "" && ir.ToolCallID != "" {
			pending.ToolCallID = ir.ToolCallID
		}
		if pending.ToolCallID == "" {
			pending.ToolCallID = "ask_user_" + ir.Token
		}
		_ = pendingStore.DeletePending(ctx, sessionID, ir.Token)
		return *pending, SyntheticAskUserOutcomeCancelled, nil
	}
	if ir.Value == "" && pending.Kind != "confirm" {
		return tool.PendingInputRequest{}, 0, errors.New("ask_user: value required")
	}
	value := ir.Value
	if pending.Kind == "confirm" && value == "" {
		value = "yes"
	}
	if pending.Kind == "password" {
		if fulfillStore == nil {
			return tool.PendingInputRequest{}, 0, errors.New("ask_user: fulfillment store not configured")
		}
		if err := fulfillStore.PutSecret(ctx, sessionID, pending.Field, value, 0); err != nil {
			return tool.PendingInputRequest{}, 0, err
		}
	}
	if pending.ToolCallID == "" && ir.ToolCallID != "" {
		pending.ToolCallID = ir.ToolCallID
	}
	if pending.ToolCallID == "" {
		pending.ToolCallID = "ask_user_" + ir.Token
	}
	_ = pendingStore.DeletePending(ctx, sessionID, ir.Token)
	return *pending, SyntheticAskUserOutcomeFulfilled, nil
}

// BuildSyntheticAskUserMessages 构造 assistant tool_call + tool result，供下轮 Run 继续 ReAct。
func BuildSyntheticAskUserMessages(p tool.PendingInputRequest, outcome SyntheticAskUserOutcome) []model.Message {
	args := map[string]any{
		"prompt": p.Prompt,
		"kind":   p.Kind,
		"field":  p.Field,
		"title":  p.Title,
	}
	if len(p.Options) > 0 {
		args["options"] = p.Options
	}
	toolCalls := []model.ToolCall{{
		ID:        p.ToolCallID,
		Name:      "ask_user",
		Arguments: args,
	}}
	// DeepSeek thinking：带 tool_calls 的 assistant 必须含 reasoning_content（可为空串）。
	meta := map[string]any{
		"tool_calls":                    toolCalls,
		model.MetadataKeyReasoningContent: p.ReasoningContent,
	}
	assistant := model.Message{
		Role:     "assistant",
		Content:  "",
		Metadata: meta,
	}
	resultPayload := map[string]any{
		"status":     "fulfilled",
		"request_id": p.RequestID,
		"field":      p.Field,
		"kind":       p.Kind,
	}
	if outcome == SyntheticAskUserOutcomeCancelled {
		resultPayload = map[string]any{
			"status":     "cancelled",
			"request_id": p.RequestID,
			"field":      p.Field,
		}
	} else if p.Kind == "password" {
		resultPayload["value_redacted"] = true
	}
	body, _ := json.Marshal(resultPayload)
	toolMsg := model.Message{
		Role:    "tool",
		Content: string(body),
		Metadata: map[string]any{
			"tool_name":    "ask_user",
			"tool_call_id": p.ToolCallID,
		},
	}
	return []model.Message{assistant, toolMsg}
}

// InjectSyntheticBeforeLastUser 在会话末尾 user 消息之前插入 synthetic 消息。
func InjectSyntheticBeforeLastUser(messages []model.Message, synthetic []model.Message) []model.Message {
	if len(synthetic) == 0 {
		return messages
	}
	if len(messages) == 0 {
		return append([]model.Message(nil), synthetic...)
	}
	last := messages[len(messages)-1]
	if last.Role != "user" {
		out := append([]model.Message(nil), messages...)
		return append(out, synthetic...)
	}
	out := append([]model.Message(nil), messages[:len(messages)-1]...)
	out = append(out, synthetic...)
	out = append(out, last)
	return out
}

// AppendAskUserToolPrompt 当注册了 ask_user 时追加 system 指引（spec §7）。
func AppendAskUserToolPrompt(prompt string) string {
	snippet := `## 用户输入工具 ask_user

- 仅当工具目录中**没有**对应能力时，才用 ask_user 收集用户输入；已绑定数据源/企微时**禁止**索取数据库 host/端口/账号/密码或 Webhook URL。
- 需要用户做选择或明确确认时，调用 ask_user，不要只在文本里索要敏感信息。
- kind=password 用于密码/密钥；用户提交后通过安全通道传递，你不会在上下文中看到明文。
- 收到 status=fulfilled 后，继续调用后续工具；若 cancelled 或 expired，向用户说明并给出替代方案。
- 不要重复 ask_user 同一 field，除非上一次 cancelled/expired。`
	if prompt == "" {
		return snippet
	}
	return prompt + "\n\n---\n\n" + snippet
}
