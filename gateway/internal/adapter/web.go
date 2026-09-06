package adapter

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/sixath/gateway/internal/runtimeclient"
)

// WebDeps wires the Web chat session proxy.
type WebDeps struct {
	PortalBaseURL string
	Runtime       *runtimeclient.Client
	HTTPClient    *http.Client // used for Portal auth/me; nil → default
}

// WebHandler proxies Web session routes to Portal Runtime after resolving user via auth/me.
type WebHandler struct {
	portalBase string
	runtime    *runtimeclient.Client
	httpClient *http.Client
}

// MountWeb registers the public Web session routes on mux.
func MountWeb(mux *http.ServeMux, deps WebDeps) {
	h := newWebHandler(deps)
	mux.HandleFunc("POST /api/v1/agents/{agent_id}/sessions", h.createSession)
	mux.HandleFunc("GET /api/v1/agents/{agent_id}/sessions", h.listSessionsByAgent)
	mux.HandleFunc("GET /api/v1/sessions", h.listSessions)
	mux.HandleFunc("GET /api/v1/sessions/search", h.searchSessions)
	mux.HandleFunc("GET /api/v1/sessions/{id}", h.getSession)
	mux.HandleFunc("PUT /api/v1/sessions/{id}", h.updateSession)
	mux.HandleFunc("DELETE /api/v1/sessions/{id}", h.deleteSession)
	mux.HandleFunc("GET /api/v1/sessions/{id}/messages", h.listMessages)
	mux.HandleFunc("GET /api/v1/sessions/{id}/result-files", h.listResultFiles)
	mux.HandleFunc("POST /api/v1/sessions/{id}/messages/stream", h.streamMessages)
	mux.HandleFunc("POST /api/v1/sessions/{id}/rewind", h.rewind)
}

func newWebHandler(deps WebDeps) *WebHandler {
	hc := deps.HTTPClient
	if hc == nil {
		hc = &http.Client{Timeout: 15 * time.Second}
	}
	return &WebHandler{
		portalBase: strings.TrimRight(strings.TrimSpace(deps.PortalBaseURL), "/"),
		runtime:    deps.Runtime,
		httpClient: hc,
	}
}

type streamRequestBody struct {
	Content         string          `json:"content"`
	ConfirmResponse json.RawMessage `json:"confirm_response"`
	InputResponse   json.RawMessage `json:"input_response"`
}

func (h *WebHandler) createSession(w http.ResponseWriter, r *http.Request) {
	userID, ok := h.requireUser(w, r)
	if !ok {
		return
	}
	var body runtimeclient.CreateSessionRequest
	if err := decodeJSONBody(r, &body); err != nil {
		http.Error(w, "invalid body", http.StatusBadRequest)
		return
	}
	if body.AgentID == "" {
		body.AgentID = r.PathValue("agent_id")
	}
	raw, err := h.runtime.CreateSession(r.Context(), userID, body)
	writeRuntimeJSON(w, raw, err)
}

func (h *WebHandler) listSessionsByAgent(w http.ResponseWriter, r *http.Request) {
	userID, ok := h.requireUser(w, r)
	if !ok {
		return
	}
	raw, err := h.runtime.ListSessionsByAgent(r.Context(), userID, r.PathValue("agent_id"), parseListQuery(r))
	writeRuntimeJSON(w, raw, err)
}

func (h *WebHandler) listSessions(w http.ResponseWriter, r *http.Request) {
	userID, ok := h.requireUser(w, r)
	if !ok {
		return
	}
	raw, err := h.runtime.ListSessions(r.Context(), userID, parseListQuery(r))
	writeRuntimeJSON(w, raw, err)
}

func (h *WebHandler) searchSessions(w http.ResponseWriter, r *http.Request) {
	userID, ok := h.requireUser(w, r)
	if !ok {
		return
	}
	q := r.URL.Query()
	limit, _ := strconv.Atoi(q.Get("limit"))
	raw, err := h.runtime.SearchSessions(r.Context(), userID, runtimeclient.SearchSessionsQuery{
		Query:   q.Get("query"),
		AgentID: q.Get("agent_id"),
		Limit:   limit,
	})
	writeRuntimeJSON(w, raw, err)
}

func (h *WebHandler) getSession(w http.ResponseWriter, r *http.Request) {
	userID, ok := h.requireUser(w, r)
	if !ok {
		return
	}
	raw, err := h.runtime.GetSession(r.Context(), userID, r.PathValue("id"))
	writeRuntimeJSON(w, raw, err)
}

func (h *WebHandler) updateSession(w http.ResponseWriter, r *http.Request) {
	userID, ok := h.requireUser(w, r)
	if !ok {
		return
	}
	var body runtimeclient.UpdateSessionRequest
	if err := decodeJSONBody(r, &body); err != nil {
		http.Error(w, "invalid body", http.StatusBadRequest)
		return
	}
	raw, err := h.runtime.UpdateSession(r.Context(), userID, r.PathValue("id"), body)
	writeRuntimeJSON(w, raw, err)
}

func (h *WebHandler) deleteSession(w http.ResponseWriter, r *http.Request) {
	userID, ok := h.requireUser(w, r)
	if !ok {
		return
	}
	raw, err := h.runtime.DeleteSession(r.Context(), userID, r.PathValue("id"))
	writeRuntimeJSON(w, raw, err)
}

func (h *WebHandler) listMessages(w http.ResponseWriter, r *http.Request) {
	userID, ok := h.requireUser(w, r)
	if !ok {
		return
	}
	raw, err := h.runtime.ListMessages(r.Context(), userID, r.PathValue("id"))
	writeRuntimeJSON(w, raw, err)
}

func (h *WebHandler) listResultFiles(w http.ResponseWriter, r *http.Request) {
	userID, ok := h.requireUser(w, r)
	if !ok {
		return
	}
	raw, err := h.runtime.ListResultFile(r.Context(), userID, r.PathValue("id"), r.URL.Query().Get("path"))
	writeRuntimeJSON(w, raw, err)
}

func (h *WebHandler) rewind(w http.ResponseWriter, r *http.Request) {
	userID, ok := h.requireUser(w, r)
	if !ok {
		return
	}
	var body runtimeclient.RewindRequest
	if err := decodeJSONBody(r, &body); err != nil {
		http.Error(w, "invalid body", http.StatusBadRequest)
		return
	}
	raw, err := h.runtime.Rewind(r.Context(), userID, r.PathValue("id"), body)
	writeRuntimeJSON(w, raw, err)
}

func (h *WebHandler) streamMessages(w http.ResponseWriter, r *http.Request) {
	userID, ok := h.requireUser(w, r)
	if !ok {
		return
	}
	var body streamRequestBody
	if err := decodeJSONBody(r, &body); err != nil {
		http.Error(w, "invalid body", http.StatusBadRequest)
		return
	}
	turn := runtimeclient.TurnRequest{
		SessionID:       r.PathValue("id"),
		Content:         body.Content,
		ConfirmResponse: body.ConfirmResponse,
		InputResponse:   body.InputResponse,
	}
	rc, hdr, err := h.runtime.TurnsStream(r.Context(), userID, turn)
	if err != nil {
		writeRuntimeError(w, err)
		return
	}
	// Close Runtime body when the client disconnects so the upstream request ends promptly.
	defer rc.Close()
	go func() {
		<-r.Context().Done()
		_ = rc.Close()
	}()

	copySSEHeaders(w, hdr)
	w.WriteHeader(http.StatusOK)
	if f, ok := w.(http.Flusher); ok {
		f.Flush()
	}
	buf := make([]byte, 32*1024)
	for {
		n, readErr := rc.Read(buf)
		if n > 0 {
			if _, writeErr := w.Write(buf[:n]); writeErr != nil {
				return
			}
			if f, ok := w.(http.Flusher); ok {
				f.Flush()
			}
		}
		if readErr != nil {
			return
		}
	}
}

func (h *WebHandler) requireUser(w http.ResponseWriter, r *http.Request) (string, bool) {
	userID, status, err := h.resolveUser(r.Context(), r)
	if err != nil {
		http.Error(w, err.Error(), status)
		return "", false
	}
	return userID, true
}

func (h *WebHandler) resolveUser(ctx context.Context, r *http.Request) (string, int, error) {
	token, ok := bearerToken(r.Header.Get("Authorization"))
	if !ok {
		return "", http.StatusUnauthorized, fmt.Errorf("unauthorized")
	}
	if h.portalBase == "" {
		return "", http.StatusInternalServerError, fmt.Errorf("portal not configured")
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, h.portalBase+"/api/v1/auth/me", nil)
	if err != nil {
		return "", http.StatusInternalServerError, err
	}
	req.Header.Set("Authorization", "Bearer "+token)
	resp, err := h.httpClient.Do(req)
	if err != nil {
		return "", http.StatusBadGateway, fmt.Errorf("auth/me failed")
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if resp.StatusCode == http.StatusUnauthorized || resp.StatusCode == http.StatusForbidden {
		return "", http.StatusUnauthorized, fmt.Errorf("unauthorized")
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return "", http.StatusBadGateway, fmt.Errorf("auth/me status %d", resp.StatusCode)
	}
	var out struct {
		UserID string `json:"user_id"`
	}
	if err := json.Unmarshal(raw, &out); err != nil || strings.TrimSpace(out.UserID) == "" {
		return "", http.StatusBadGateway, fmt.Errorf("auth/me invalid response")
	}
	return out.UserID, 0, nil
}

func bearerToken(authorization string) (string, bool) {
	const prefix = "Bearer "
	if !strings.HasPrefix(authorization, prefix) {
		return "", false
	}
	token := strings.TrimSpace(strings.TrimPrefix(authorization, prefix))
	return token, token != ""
}

func parseListQuery(r *http.Request) runtimeclient.ListSessionsQuery {
	q := r.URL.Query()
	var out runtimeclient.ListSessionsQuery
	if v := q.Get("page"); v != "" {
		if n, err := strconv.ParseInt(v, 10, 32); err == nil {
			out.Page = int32(n)
		}
	}
	if v := q.Get("page_size"); v != "" {
		if n, err := strconv.ParseInt(v, 10, 32); err == nil {
			out.PageSize = int32(n)
		}
	}
	out.Q = q.Get("q")
	if v := q.Get("include_preview"); v != "" {
		out.IncludePreview, _ = strconv.ParseBool(v)
	}
	return out
}

func decodeJSONBody(r *http.Request, dst any) error {
	defer r.Body.Close()
	dec := json.NewDecoder(io.LimitReader(r.Body, 1<<20))
	return dec.Decode(dst)
}

func writeRuntimeJSON(w http.ResponseWriter, raw json.RawMessage, err error) {
	if err != nil {
		writeRuntimeError(w, err)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	if len(raw) == 0 {
		_, _ = w.Write([]byte("{}"))
		return
	}
	_, _ = w.Write(raw)
}

func writeRuntimeError(w http.ResponseWriter, err error) {
	var httpErr *runtimeclient.HTTPError
	if errors.As(err, &httpErr) && httpErr.StatusCode > 0 {
		body := httpErr.Body
		if len(body) > 0 && json.Valid(body) {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(httpErr.StatusCode)
			_, _ = w.Write(body)
			return
		}
		http.Error(w, strings.TrimSpace(string(body)), httpErr.StatusCode)
		return
	}
	http.Error(w, "runtime request failed", http.StatusBadGateway)
}

func copySSEHeaders(dst http.ResponseWriter, src http.Header) {
	for _, key := range []string{"Content-Type", "Cache-Control", "Connection", "X-Accel-Buffering"} {
		if v := src.Get(key); v != "" {
			dst.Header().Set(key, v)
		}
	}
	if dst.Header().Get("Content-Type") == "" {
		dst.Header().Set("Content-Type", "text/event-stream")
	}
	if dst.Header().Get("Cache-Control") == "" {
		dst.Header().Set("Cache-Control", "no-cache")
	}
}
