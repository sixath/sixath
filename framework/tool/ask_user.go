package tool

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"
	"time"
)

// AskUserConfig 构造 ask_user 工具的依赖。
type AskUserConfig struct {
	PendingStore     AskUserPendingStore
	FulfillmentStore AskUserFulfillmentStore
	TokenGen         TokenGenerator
	TTLSeconds       int
	GuardConfig      *AskUserGuardConfig // nil = disabled
}

// RegisterAskUserTool 向注册表中注册 ask_user 工具。
func RegisterAskUserTool(r *Registry, cfg *AskUserConfig) error {
	if cfg == nil || cfg.PendingStore == nil || cfg.TokenGen == nil {
		return errors.New("ask_user: missing pending store or token generator")
	}
	return r.Register(Tool{
		Name:        "ask_user",
		Description: "Request structured input from the user (text, password, select, or confirm). Returns pending until fulfilled.",
		Toolset:     ToolsetCore,
		AlwaysLoad:  true,
		Parameters: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"prompt": map[string]any{
					"type":        "string",
					"description": "Shown to the user; explain why this input is needed.",
				},
				"kind": map[string]any{
					"type":        "string",
					"enum":        []string{"text", "password", "select", "confirm"},
					"description": "Input widget type. Default: text.",
				},
				"field": map[string]any{
					"type":        "string",
					"description": "Stable field key for this request (e.g. ssh_username).",
				},
				"title": map[string]any{
					"type":        "string",
					"description": "Short card title; defaults from prompt.",
				},
				"options": map[string]any{
					"type":        "array",
					"items":       map[string]any{"type": "string"},
					"description": "Required when kind=select.",
				},
				"required": map[string]any{
					"type":        "boolean",
					"description": "Default true.",
				},
				"response_token": map[string]any{
					"type":        "string",
					"description": "Fulfillment token from a previous pending response.",
				},
				"value": map[string]any{
					"type":        "string",
					"description": "User-provided value when fulfilling.",
				},
				"cancelled": map[string]any{
					"type":        "boolean",
					"description": "When true with response_token, marks request cancelled.",
				},
				"session_id": map[string]any{
					"type":        "string",
					"description": "Session ID; if omitted, read from context.",
				},
			},
			"required": []string{"prompt"},
		},
		Execute: buildAskUserExecute(cfg),
	})
}

func buildAskUserExecute(cfg *AskUserConfig) ExecuteFunc {
	return func(ctx context.Context, params map[string]any) (any, error) {
		sessionID := sessionIDFromAskUserParams(ctx, params)
		if token, _ := params["response_token"].(string); token != "" {
			return fulfillAskUser(ctx, cfg, sessionID, token, params)
		}
		return proposeAskUser(ctx, cfg, sessionID, params)
	}
}

func sessionIDFromAskUserParams(ctx context.Context, params map[string]any) string {
	if s, ok := params["session_id"].(string); ok && s != "" {
		return s
	}
	if s, ok := ctx.Value(ContextKeySessionID).(string); ok {
		return s
	}
	return ""
}

func proposeAskUser(ctx context.Context, cfg *AskUserConfig, sessionID string, params map[string]any) (any, error) {
	if sessionID == "" {
		return nil, errors.New("ask_user: session_id is required")
	}
	prompt, _ := params["prompt"].(string)
	if prompt == "" {
		return nil, errors.New("ask_user: prompt is required")
	}
	kind, _ := params["kind"].(string)
	if kind == "" {
		kind = "text"
	}
	field, _ := params["field"].(string)
	if field == "" {
		field = "input"
	}
	title, _ := params["title"].(string)
	if title == "" {
		title = truncatePromptTitle(prompt)
	}
	required := true
	if v, ok := params["required"].(bool); ok {
		required = v
	}
	var options []string
	if raw, ok := params["options"].([]any); ok {
		for _, item := range raw {
			if s, ok := item.(string); ok && s != "" {
				options = append(options, s)
			}
		}
	}
	if kind == "select" && len(options) == 0 {
		return nil, errors.New("ask_user: options required when kind=select")
	}

	if cfg.GuardConfig != nil {
		if cat, ok := ctx.Value(ContextKeyToolCatalog).(ToolCatalog); ok {
			if match, blocked := MatchAskUserIntent(cat, *cfg.GuardConfig, prompt, kind); blocked {
				return fmt.Sprintf("ask_user blocked: configured tool %q already provides this capability. Bindings: %v. Use that tool directly instead of asking the user. Description: %s",
					match.Name, match.Bindings, match.Description), nil
			}
		}
	}

	ttl := cfg.TTLSeconds
	if ttl <= 0 {
		ttl = 600
	}
	token, err := cfg.TokenGen.NewToken()
	if err != nil {
		return nil, fmt.Errorf("ask_user: generate token: %w", err)
	}
	requestID, err := newAskUserRequestID()
	if err != nil {
		return nil, err
	}
	now := time.Now()
	pending := PendingInputRequest{
		RequestID: requestID,
		Token:     token,
		SessionID: sessionID,
		Kind:      kind,
		Field:     field,
		Prompt:    prompt,
		Title:     title,
		Options:   options,
		Required:  required,
		CreatedAt: now,
	}
	if id, ok := ctx.Value(ContextKeyToolCallID).(string); ok && id != "" {
		pending.ToolCallID = id
	}
	if rc, ok := ctx.Value(ContextKeyReasoningContent).(string); ok {
		pending.ReasoningContent = rc
	}
	if err := cfg.PendingStore.SavePending(ctx, sessionID, pending); err != nil {
		return nil, fmt.Errorf("ask_user: save pending: %w", err)
	}
	return map[string]any{
		"status":     "pending",
		"request_id": requestID,
		"token":      token,
		"kind":       kind,
		"field":      field,
		"prompt":     prompt,
		"title":      title,
		"options":    options,
		"required":   required,
		"expires_in": ttl,
	}, nil
}

func fulfillAskUser(ctx context.Context, cfg *AskUserConfig, sessionID, token string, params map[string]any) (any, error) {
	if sessionID == "" {
		return nil, errors.New("ask_user: session_id is required")
	}
	pending, err := cfg.PendingStore.GetPending(ctx, sessionID, token)
	if err != nil {
		return nil, fmt.Errorf("ask_user: load pending: %w", err)
	}
	if pending == nil {
		return map[string]any{
			"status":     "expired",
			"request_id": "",
			"field":      stringValue(params["field"]),
		}, nil
	}
	ttl := cfg.TTLSeconds
	if ttl <= 0 {
		ttl = 600
	}
	if time.Since(pending.CreatedAt) > time.Duration(ttl)*time.Second {
		_ = cfg.PendingStore.DeletePending(ctx, sessionID, token)
		return map[string]any{
			"status":     "expired",
			"request_id": pending.RequestID,
			"field":      pending.Field,
		}, nil
	}
	if cancelled, _ := params["cancelled"].(bool); cancelled {
		_ = cfg.PendingStore.DeletePending(ctx, sessionID, token)
		return map[string]any{
			"status":     "cancelled",
			"request_id": pending.RequestID,
			"field":      pending.Field,
		}, nil
	}
	value, _ := params["value"].(string)
	if pending.Kind == "confirm" {
		if value == "" {
			value = "yes"
		}
	}
	if value == "" {
		return nil, errors.New("ask_user: value is required to fulfill")
	}
	_ = cfg.PendingStore.DeletePending(ctx, sessionID, token)

	result := map[string]any{
		"status":     "fulfilled",
		"request_id": pending.RequestID,
		"field":      pending.Field,
		"kind":       pending.Kind,
	}
	switch pending.Kind {
	case "password":
		if cfg.FulfillmentStore == nil {
			return nil, errors.New("ask_user: fulfillment store not configured")
		}
		if err := cfg.FulfillmentStore.PutSecret(ctx, sessionID, pending.Field, value, time.Duration(ttl)*time.Second); err != nil {
			return nil, fmt.Errorf("ask_user: store secret: %w", err)
		}
		result["value_redacted"] = true
	default:
		result["value"] = value
	}
	return result, nil
}

func newAskUserRequestID() (string, error) {
	b := make([]byte, 4)
	if _, err := rand.Read(b); err != nil {
		return "", fmt.Errorf("ask_user: request id: %w", err)
	}
	return "req_" + hex.EncodeToString(b), nil
}

func truncatePromptTitle(prompt string) string {
	prompt = strings.TrimSpace(prompt)
	if len(prompt) <= 48 {
		return prompt
	}
	return prompt[:45] + "..."
}

func stringValue(v any) string {
	s, _ := v.(string)
	return s
}
