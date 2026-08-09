package server

import (
	"context"
	"encoding/json"
	"io"
	"strconv"
	"strings"
	"time"

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

// InsightsHandler serves GET /api/v1/agents/{agent_id}/insights?from=&to=
// from/to: RFC3339 preferred; unix seconds also accepted.
func InsightsHandler(chat *service.ChatService) func(kratoshttp.Context) error {
	return func(ctx kratoshttp.Context) error {
		agentID := strings.TrimSpace(ctx.Vars().Get("agent_id"))
		from := parseTimeQuery(ctx.Query().Get("from"))
		to := parseTimeQuery(ctx.Query().Get("to"))
		out, err := runWithMiddleware(ctx, func(c context.Context) (any, error) {
			return chat.GetInsights(c, agentID, from, to)
		})
		if err != nil {
			return err
		}
		return ctx.JSON(200, out)
	}
}

func parseTimeQuery(raw string) time.Time {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return time.Time{}
	}
	if t, err := time.Parse(time.RFC3339, raw); err == nil {
		return t.UTC()
	}
	if sec, err := strconv.ParseInt(raw, 10, 64); err == nil && sec > 0 {
		return time.Unix(sec, 0).UTC()
	}
	return time.Time{}
}
