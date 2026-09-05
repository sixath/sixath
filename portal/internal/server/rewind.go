package server

import (
	"context"
	"encoding/json"
	"io"
	"strings"

	"backend/internal/service"

	kratoshttp "github.com/go-kratos/kratos/v2/transport/http"
)

// RewindHandler serves POST /api/v1/sessions/{session_id}/rewind
// Body: {"message_id":"..."}.
func RewindHandler(chat *service.ChatService) func(kratoshttp.Context) error {
	return func(ctx kratoshttp.Context) error {
		sessionID := strings.TrimSpace(ctx.Vars().Get("session_id"))
		var body struct {
			MessageID string `json:"message_id"`
		}
		raw, _ := io.ReadAll(ctx.Request().Body)
		_ = json.Unmarshal(raw, &body)
		out, err := runWithMiddleware(ctx, func(c context.Context) (any, error) {
			return chat.RewindToMessage(c, sessionID, body.MessageID)
		})
		if err != nil {
			return err
		}
		return ctx.JSON(200, out)
	}
}
