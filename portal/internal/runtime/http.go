package runtime

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"strconv"
	"strings"

	"backend/internal/biz"
	"backend/internal/chatsse"

	kratosErrors "github.com/go-kratos/kratos/v2/errors"
	"github.com/go-kratos/kratos/v2/transport"
	khttp "github.com/go-kratos/kratos/v2/transport/http"
)

// RegisterRoutes mounts /runtime/v1 session routes with Runtime service-token auth.
func RegisterRoutes(srv *khttp.Server, svc *Service) {
	if srv == nil || svc == nil {
		return
	}
	r := srv.Route("/")
	// Register static paths before /sessions/{id} so "search" / "resolve" / "binding" are not captured.
	r.POST("/runtime/v1/sessions/resolve", svc.wrap(svc.handleResolve))
	r.GET("/runtime/v1/sessions/binding", svc.wrap(svc.handleGetBinding))
	r.DELETE("/runtime/v1/sessions/binding", svc.wrap(svc.handleDeleteBinding))
	r.GET("/runtime/v1/sessions/search", svc.wrap(svc.handleSearch))
	r.POST("/runtime/v1/sessions", svc.wrap(svc.handleCreate))
	r.GET("/runtime/v1/sessions", svc.wrap(svc.handleListAll))
	r.GET("/runtime/v1/channels/{channel_id}/agents", svc.wrap(svc.handleListChannelAgents))
	r.GET("/runtime/v1/agents/{agent_id}/sessions", svc.wrap(svc.handleListByAgent))
	r.GET("/runtime/v1/sessions/{id}", svc.wrap(svc.handleGet))
	r.PUT("/runtime/v1/sessions/{id}", svc.wrap(svc.handleUpdate))
	r.DELETE("/runtime/v1/sessions/{id}", svc.wrap(svc.handleDelete))
	r.GET("/runtime/v1/sessions/{id}/messages", svc.wrap(svc.handleMessages))
	r.POST("/runtime/v1/sessions/{id}/rewind", svc.wrap(svc.handleRewind))
	r.POST("/runtime/v1/turns", svc.wrap(svc.handleTurns))
}

type runtimeHandler func(ctx context.Context, hctx khttp.Context) error

func (s *Service) wrap(fn runtimeHandler) func(khttp.Context) error {
	return func(hctx khttp.Context) error {
		var outErr error
		auth := Auth(ServiceToken())
		handler := auth(func(c context.Context, _ interface{}) (interface{}, error) {
			outErr = fn(c, hctx)
			return nil, outErr
		})
		ctx := context.Context(hctx)
		if _, ok := transport.FromServerContext(ctx); !ok {
			ctx = transport.NewServerContext(ctx, routeHTTPTransport{request: hctx.Request()})
		}
		if _, err := handler(ctx, nil); err != nil {
			return err
		}
		return outErr
	}
}

type routeHTTPTransport struct {
	request *http.Request
}

type routeHeader http.Header

func (h routeHeader) Get(key string) string { return http.Header(h).Get(key) }
func (h routeHeader) Set(key, value string) { http.Header(h).Set(key, value) }
func (h routeHeader) Add(key, value string) { http.Header(h).Add(key, value) }
func (h routeHeader) Keys() []string {
	keys := make([]string, 0, len(h))
	for key := range h {
		keys = append(keys, key)
	}
	return keys
}
func (h routeHeader) Values(key string) []string { return http.Header(h).Values(key) }

func (f routeHTTPTransport) Kind() transport.Kind            { return transport.KindHTTP }
func (f routeHTTPTransport) Endpoint() string                { return "http://runtime" }
func (f routeHTTPTransport) Operation() string               { return f.request.URL.Path }
func (f routeHTTPTransport) RequestHeader() transport.Header { return routeHeader(f.request.Header) }
func (f routeHTTPTransport) ReplyHeader() transport.Header   { return routeHeader{} }
func (f routeHTTPTransport) Request() *http.Request          { return f.request }
func (f routeHTTPTransport) PathTemplate() string            { return f.request.URL.Path }

var _ khttp.Transporter = routeHTTPTransport{}

func (s *Service) handleResolve(ctx context.Context, hctx khttp.Context) error {
	var req resolveRequest
	if err := decodeJSON(hctx, &req); err != nil {
		return err
	}
	out, err := s.resolve(ctx, req)
	if err != nil {
		return err
	}
	return hctx.JSON(200, out)
}

func (s *Service) handleDeleteBinding(ctx context.Context, hctx khttp.Context) error {
	channelID := strings.TrimSpace(hctx.Query().Get("channel_id"))
	peerID := strings.TrimSpace(hctx.Query().Get("peer_id"))
	if err := s.deleteBinding(ctx, channelID, peerID); err != nil {
		return err
	}
	return hctx.JSON(200, map[string]any{"ok": true})
}

func (s *Service) handleGetBinding(ctx context.Context, hctx khttp.Context) error {
	channelID := strings.TrimSpace(hctx.Query().Get("channel_id"))
	peerID := strings.TrimSpace(hctx.Query().Get("peer_id"))
	out, err := s.getBinding(ctx, channelID, peerID)
	if err != nil {
		return err
	}
	return hctx.JSON(200, out)
}

func (s *Service) handleListChannelAgents(ctx context.Context, hctx khttp.Context) error {
	channelID := strings.TrimSpace(hctx.Vars().Get("channel_id"))
	out, err := s.listChannelAgents(ctx, channelID)
	if err != nil {
		return err
	}
	return hctx.JSON(200, out)
}

func (s *Service) handleCreate(ctx context.Context, hctx khttp.Context) error {
	var req createSessionRequest
	if err := decodeJSON(hctx, &req); err != nil {
		return err
	}
	out, err := s.createSession(ctx, req)
	if err != nil {
		return err
	}
	return hctx.JSON(200, out)
}

func (s *Service) handleListAll(ctx context.Context, hctx khttp.Context) error {
	page := queryInt32(hctx, "page", 1)
	pageSize := queryInt32(hctx, "page_size", 50)
	includePreview := queryBool(hctx, "include_preview")
	out, err := s.listAllSessions(ctx, page, pageSize, includePreview)
	if err != nil {
		return err
	}
	return hctx.JSON(200, out)
}

func (s *Service) handleListByAgent(ctx context.Context, hctx khttp.Context) error {
	agentID := strings.TrimSpace(hctx.Vars().Get("agent_id"))
	page := queryInt32(hctx, "page", 1)
	pageSize := queryInt32(hctx, "page_size", 10)
	q := strings.TrimSpace(hctx.Query().Get("q"))
	includePreview := queryBool(hctx, "include_preview")
	out, err := s.listSessionsByAgent(ctx, agentID, q, page, pageSize, includePreview)
	if err != nil {
		return err
	}
	return hctx.JSON(200, out)
}

func (s *Service) handleGet(ctx context.Context, hctx khttp.Context) error {
	id := strings.TrimSpace(hctx.Vars().Get("id"))
	out, err := s.getSession(ctx, id)
	if err != nil {
		return err
	}
	return hctx.JSON(200, out)
}

func (s *Service) handleUpdate(ctx context.Context, hctx khttp.Context) error {
	id := strings.TrimSpace(hctx.Vars().Get("id"))
	var req updateSessionRequest
	if err := decodeJSON(hctx, &req); err != nil {
		return err
	}
	out, err := s.updateSession(ctx, id, req.Title)
	if err != nil {
		return err
	}
	return hctx.JSON(200, out)
}

func (s *Service) handleDelete(ctx context.Context, hctx khttp.Context) error {
	id := strings.TrimSpace(hctx.Vars().Get("id"))
	out, err := s.deleteSession(ctx, id)
	if err != nil {
		return err
	}
	return hctx.JSON(200, out)
}

func (s *Service) handleMessages(ctx context.Context, hctx khttp.Context) error {
	id := strings.TrimSpace(hctx.Vars().Get("id"))
	out, err := s.listMessages(ctx, id)
	if err != nil {
		return err
	}
	return hctx.JSON(200, out)
}

func (s *Service) handleSearch(ctx context.Context, hctx khttp.Context) error {
	query := strings.TrimSpace(hctx.Query().Get("query"))
	if query == "" {
		query = strings.TrimSpace(hctx.Query().Get("q"))
	}
	agentID := strings.TrimSpace(hctx.Query().Get("agent_id"))
	limit := int(queryInt32(hctx, "limit", 20))
	out, err := s.searchSessions(ctx, query, agentID, limit)
	if err != nil {
		return err
	}
	return hctx.JSON(200, out)
}

func (s *Service) handleRewind(ctx context.Context, hctx khttp.Context) error {
	id := strings.TrimSpace(hctx.Vars().Get("id"))
	var req rewindRequest
	raw, _ := io.ReadAll(hctx.Request().Body)
	_ = json.Unmarshal(raw, &req)
	out, err := s.rewindSession(ctx, id, req.MessageID)
	if err != nil {
		return err
	}
	return hctx.JSON(200, out)
}

func (s *Service) handleTurns(ctx context.Context, hctx khttp.Context) error {
	var req turnRequest
	if err := decodeJSON(hctx, &req); err != nil {
		return kratosErrors.BadRequest("INVALID_ARGUMENT", "invalid body")
	}
	req.SessionID = strings.TrimSpace(req.SessionID)
	req.ReplyMode = strings.ToLower(strings.TrimSpace(req.ReplyMode))
	if req.SessionID == "" {
		return kratosErrors.BadRequest("INVALID_ARGUMENT", "session_id required")
	}
	if req.Content == "" && req.InputResponse == nil && req.ConfirmResponse == nil {
		return kratosErrors.BadRequest("INVALID_ARGUMENT", "content or input_response or confirm_response required")
	}
	switch req.ReplyMode {
	case "", "final":
		if req.ReplyMode == "" {
			req.ReplyMode = "final"
		}
		out, err := s.runFinalTurn(ctx, req)
		if err != nil {
			return err
		}
		return hctx.JSON(200, out)
	case "stream":
		// Prefer request context for cancel-on-disconnect; Auth already attached caller to ctx.
		runCtx := ctx
		if reqCtx := hctx.Request().Context(); reqCtx != nil {
			if caller, ok := biz.CallerUserID(ctx); ok {
				runCtx = biz.WithCallerUserID(reqCtx, caller)
			} else {
				runCtx = reqCtx
			}
		}
		ch, sessionID, err := s.startStreamTurn(runCtx, req)
		w := hctx.Response()
		if err != nil {
			// ACL / validation errors should remain HTTP errors (e.g. 403), not SSE.
			if se := kratosErrors.FromError(err); se != nil && se.Code >= 400 && se.Code < 500 {
				return err
			}
			chatsse.SetHeaders(w)
			chatsse.WriteEvent(w, "error", map[string]any{"error": err.Error()})
			return nil
		}
		chatsse.SetHeaders(w)
		chatsse.WriteStream(context.WithoutCancel(ctx), w, ch, sessionID, func(c context.Context, sid, content string, meta map[string]any) error {
			_, err := s.turns.SaveAssistantMessage(c, sid, content, meta)
			return err
		})
		return nil
	default:
		return kratosErrors.BadRequest("INVALID_ARGUMENT", "reply_mode must be final or stream")
	}
}

func decodeJSON(hctx khttp.Context, dst any) error {
	raw, err := io.ReadAll(hctx.Request().Body)
	if err != nil {
		return err
	}
	if len(raw) == 0 {
		return nil
	}
	return json.Unmarshal(raw, dst)
}

func queryInt32(hctx khttp.Context, key string, def int32) int32 {
	raw := strings.TrimSpace(hctx.Query().Get(key))
	if raw == "" {
		return def
	}
	n, err := strconv.ParseInt(raw, 10, 32)
	if err != nil {
		return def
	}
	return int32(n)
}

func queryBool(hctx khttp.Context, key string) bool {
	raw := strings.TrimSpace(strings.ToLower(hctx.Query().Get(key)))
	return raw == "1" || raw == "true" || raw == "yes"
}
