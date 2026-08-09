package server

import (
	"context"
	"strconv"
	"strings"

	"backend/internal/biz"
	"backend/internal/service"

	kratoshttp "github.com/go-kratos/kratos/v2/transport/http"
)

// TranscriptSearchHandler serves GET /api/v1/agents/{agent_id}/transcript/search.
//
// Query params: q, exclude_session, include_tools (default 1), window (default 5).
// AuthZ: agent read (PermView), same as Agent Get. Response: {hits, count}.
// Hand-written (not chat.proto) to avoid regenerating AnchoredHit nested messages.
func TranscriptSearchHandler(chat *service.ChatService) func(kratoshttp.Context) error {
	return func(ctx kratoshttp.Context) error {
		agentID := strings.TrimSpace(ctx.Vars().Get("agent_id"))
		q := strings.TrimSpace(ctx.Query().Get("q"))
		exclude := strings.TrimSpace(ctx.Query().Get("exclude_session"))
		includeTools := parseBoolQuery(ctx.Query().Get("include_tools"), true)
		window := parseIntQuery(ctx.Query().Get("window"), 5)

		opts := biz.TranscriptSearchOpts{
			AgentID:          agentID,
			Query:            q,
			ExcludeSessionID: exclude,
			IncludeTools:     includeTools,
			Window:           window,
		}
		out, err := runWithMiddleware(ctx, func(c context.Context) (any, error) {
			return chat.SearchTranscript(c, opts)
		})
		if err != nil {
			return err
		}
		return ctx.JSON(200, out)
	}
}

func parseBoolQuery(raw string, defaultVal bool) bool {
	raw = strings.TrimSpace(strings.ToLower(raw))
	if raw == "" {
		return defaultVal
	}
	switch raw {
	case "1", "true", "yes", "on":
		return true
	case "0", "false", "no", "off":
		return false
	default:
		return defaultVal
	}
}

func parseIntQuery(raw string, defaultVal int) int {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return defaultVal
	}
	n, err := strconv.Atoi(raw)
	if err != nil {
		return defaultVal
	}
	return n
}
