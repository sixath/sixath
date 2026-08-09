package chat

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/sixath/framework/tool"
	toolskill "github.com/sixath/framework/tool/skillops"
)

type confirmResponseContextKey struct{}

// ConfirmResponse is a structured confirmation from the chat UI (skill_manage / execute_write).
// Applied server-side so the model does not need to re-call the tool with confirm_token.
type ConfirmResponse struct {
	Kind  string `json:"kind"`  // skill_manage | execute_write
	Token string `json:"token"`
}

// WithConfirmResponse binds confirm_response onto context for the SSE path.
func WithConfirmResponse(ctx context.Context, cr *ConfirmResponse) context.Context {
	if cr == nil {
		return ctx
	}
	return context.WithValue(ctx, confirmResponseContextKey{}, cr)
}

// ConfirmResponseFromContext reads a bound confirm_response.
func ConfirmResponseFromContext(ctx context.Context) *ConfirmResponse {
	if ctx == nil {
		return nil
	}
	cr, _ := ctx.Value(confirmResponseContextKey{}).(*ConfirmResponse)
	return cr
}

// UserMessagePlaceholderForConfirm is persisted when the user confirms via the card.
func UserMessagePlaceholderForConfirm(kind string) string {
	if kind == "" {
		kind = "operation"
	}
	return fmt.Sprintf("[confirmed: %s]", kind)
}

// ApplySkillManageConfirm executes a pending skill_manage create/delete using the shared store.
func ApplySkillManageConfirm(ctx context.Context, workspace, sessionID, token string) (map[string]any, error) {
	if workspace == "" || sessionID == "" || token == "" {
		return nil, fmt.Errorf("workspace, session_id and token are required")
	}
	cfg := SkillManageToolConfig(nil)
	runCtx := context.WithValue(ctx, tool.ContextKeyWorkspaceRoot, workspace)
	runCtx = context.WithValue(runCtx, tool.ContextKeySessionID, sessionID)
	if uid := ResolveMemoryUserID(ctx, nil); uid != "" {
		runCtx = context.WithValue(runCtx, tool.ContextKeyUserID, uid)
	}
	runCtx = tool.WithSkillManageUIConfirm(runCtx)

	reg := tool.NewRegistry()
	if err := toolskill.RegisterSkillManageTool(reg, cfg); err != nil {
		return nil, err
	}
	tl, ok := reg.Get("skill_manage")
	if !ok {
		return nil, fmt.Errorf("skill_manage not registered")
	}
	raw, err := tl.Execute(runCtx, map[string]any{
		"action":        "create", // ignored when confirm_token is set
		"name":          "_",
		"confirm_token": token,
	})
	if err != nil {
		return nil, err
	}
	switch v := raw.(type) {
	case map[string]any:
		return v, nil
	default:
		b, _ := json.Marshal(raw)
		return map[string]any{"status": "ok", "result": json.RawMessage(b)}, nil
	}
}
