package runtimeclient

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
)

const headerUserID = "X-Sath-User-Id"

// HTTPError is a non-2xx response from Portal Runtime.
type HTTPError struct {
	StatusCode int
	Method     string
	Path       string
	Body       []byte
}

func (e *HTTPError) Error() string {
	if e == nil {
		return "runtime http error"
	}
	msg := strings.TrimSpace(string(e.Body))
	if msg == "" {
		return fmt.Sprintf("runtime %s %s: status %d", e.Method, e.Path, e.StatusCode)
	}
	return fmt.Sprintf("runtime %s %s: status %d: %s", e.Method, e.Path, e.StatusCode, msg)
}

// Client calls Portal /runtime/v1 with a service token.
type Client struct {
	baseURL    string
	token      string
	httpClient *http.Client
}

// New builds a Runtime client. baseURL is the Portal origin (no trailing slash required).
func New(baseURL, token string) *Client {
	return &Client{
		baseURL: strings.TrimRight(strings.TrimSpace(baseURL), "/"),
		token:   token,
		httpClient: &http.Client{
			Timeout: 120 * time.Second,
		},
	}
}

// ResolveRequest is POST /runtime/v1/sessions/resolve body.
type ResolveRequest struct {
	ChannelID string `json:"channel_id"`
	PeerID    string `json:"peer_id"`
	AgentID   string `json:"agent_id,omitempty"`
	ForceNew  bool   `json:"force_new,omitempty"`
	Reason    string `json:"reason,omitempty"`
}

// ChannelAgentItem is one agent in a channel allowlist response.
type ChannelAgentItem struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	Description string `json:"description,omitempty"`
}

// ChannelAgentsReply is GET /runtime/v1/channels/{channel_id}/agents.
type ChannelAgentsReply struct {
	DefaultAgent        string             `json:"default_agent"`
	Agents              []ChannelAgentItem `json:"agents"`
	AutoRouteEnabled    bool               `json:"auto_route_enabled"`
	AutoRouteMention    bool               `json:"auto_route_mention"`
	AutoRouteClassifier bool               `json:"auto_route_classifier"`
}

// ResolveReply is the resolve response.
type ResolveReply struct {
	SessionID string `json:"session_id"`
	AgentID   string `json:"agent_id"`
	UserID    string `json:"user_id"`
	Created   bool   `json:"created"`
}

// CreateSessionRequest is POST /runtime/v1/sessions body.
type CreateSessionRequest struct {
	AgentID         string `json:"agent_id"`
	Title           string `json:"title,omitempty"`
	ParentSessionID string `json:"parent_session_id,omitempty"`
}

// UpdateSessionRequest is PUT /runtime/v1/sessions/{id} body.
type UpdateSessionRequest struct {
	Title string `json:"title"`
}

// ListSessionsQuery are query params for list endpoints.
type ListSessionsQuery struct {
	Page           int32
	PageSize       int32
	Q              string
	IncludePreview bool
}

// SearchSessionsQuery are query params for GET /runtime/v1/sessions/search.
type SearchSessionsQuery struct {
	Query   string
	AgentID string
	Limit   int
}

// RewindRequest is POST /runtime/v1/sessions/{id}/rewind body.
type RewindRequest struct {
	MessageID string `json:"message_id"`
}

// TurnRequest is POST /runtime/v1/turns body.
type TurnRequest struct {
	SessionID       string          `json:"session_id"`
	Content         string          `json:"content,omitempty"`
	ReplyMode       string          `json:"reply_mode,omitempty"`
	ChannelID       string          `json:"channel_id,omitempty"`
	PeerID          string          `json:"peer_id,omitempty"`
	CorrelationID   string          `json:"correlation_id,omitempty"`
	IdempotencyKey  string          `json:"idempotency_key,omitempty"`
	InputResponse   json.RawMessage `json:"input_response,omitempty"`
	ConfirmResponse json.RawMessage `json:"confirm_response,omitempty"`
}

// TurnFinalReply is the JSON body for reply_mode=final.
type TurnFinalReply struct {
	CorrelationID string `json:"correlation_id"`
	Status        string `json:"status"`
	Content       string `json:"content,omitempty"`
	Error         string `json:"error,omitempty"`
}

// ResolveSession maps channel+peer to a session.
func (c *Client) ResolveSession(ctx context.Context, userID string, req ResolveRequest) (*ResolveReply, error) {
	var out ResolveReply
	if err := c.doJSON(ctx, http.MethodPost, "/runtime/v1/sessions/resolve", userID, nil, req, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// DeleteBinding removes the channel+peer session mapping on Portal (idempotent).
func (c *Client) DeleteBinding(ctx context.Context, channelID, peerID string) error {
	vals := url.Values{}
	vals.Set("channel_id", strings.TrimSpace(channelID))
	vals.Set("peer_id", strings.TrimSpace(peerID))
	return c.doJSON(ctx, http.MethodDelete, "/runtime/v1/sessions/binding", "", vals, nil, nil)
}

// ListChannelAgents returns default and allowlisted agents for a channel.
func (c *Client) ListChannelAgents(ctx context.Context, channelID string) (*ChannelAgentsReply, error) {
	path := "/runtime/v1/channels/" + url.PathEscape(strings.TrimSpace(channelID)) + "/agents"
	var out ChannelAgentsReply
	if err := c.doJSON(ctx, http.MethodGet, path, "", nil, nil, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// CreateSession creates a web chat session.
func (c *Client) CreateSession(ctx context.Context, userID string, req CreateSessionRequest) (json.RawMessage, error) {
	return c.doRawJSON(ctx, http.MethodPost, "/runtime/v1/sessions", userID, nil, req)
}

// ListSessions lists all sessions for the caller.
func (c *Client) ListSessions(ctx context.Context, userID string, q ListSessionsQuery) (json.RawMessage, error) {
	return c.doRawJSON(ctx, http.MethodGet, "/runtime/v1/sessions", userID, listQuery(q), nil)
}

// ListSessionsByAgent lists sessions for one agent.
func (c *Client) ListSessionsByAgent(ctx context.Context, userID, agentID string, q ListSessionsQuery) (json.RawMessage, error) {
	path := "/runtime/v1/agents/" + url.PathEscape(agentID) + "/sessions"
	return c.doRawJSON(ctx, http.MethodGet, path, userID, listQuery(q), nil)
}

// GetSession fetches one session.
func (c *Client) GetSession(ctx context.Context, userID, sessionID string) (json.RawMessage, error) {
	path := "/runtime/v1/sessions/" + url.PathEscape(sessionID)
	return c.doRawJSON(ctx, http.MethodGet, path, userID, nil, nil)
}

// UpdateSession updates session title.
func (c *Client) UpdateSession(ctx context.Context, userID, sessionID string, req UpdateSessionRequest) (json.RawMessage, error) {
	path := "/runtime/v1/sessions/" + url.PathEscape(sessionID)
	return c.doRawJSON(ctx, http.MethodPut, path, userID, nil, req)
}

// DeleteSession deletes a session.
func (c *Client) DeleteSession(ctx context.Context, userID, sessionID string) (json.RawMessage, error) {
	path := "/runtime/v1/sessions/" + url.PathEscape(sessionID)
	return c.doRawJSON(ctx, http.MethodDelete, path, userID, nil, nil)
}

// ListMessages returns session message history.
func (c *Client) ListMessages(ctx context.Context, userID, sessionID string) (json.RawMessage, error) {
	path := "/runtime/v1/sessions/" + url.PathEscape(sessionID) + "/messages"
	return c.doRawJSON(ctx, http.MethodGet, path, userID, nil, nil)
}

// SearchSessions searches sessions.
func (c *Client) SearchSessions(ctx context.Context, userID string, q SearchSessionsQuery) (json.RawMessage, error) {
	vals := url.Values{}
	if q.Query != "" {
		vals.Set("query", q.Query)
	}
	if q.AgentID != "" {
		vals.Set("agent_id", q.AgentID)
	}
	if q.Limit > 0 {
		vals.Set("limit", strconv.Itoa(q.Limit))
	}
	return c.doRawJSON(ctx, http.MethodGet, "/runtime/v1/sessions/search", userID, vals, nil)
}

// Rewind soft-hides messages from an anchor.
func (c *Client) Rewind(ctx context.Context, userID, sessionID string, req RewindRequest) (json.RawMessage, error) {
	path := "/runtime/v1/sessions/" + url.PathEscape(sessionID) + "/rewind"
	return c.doRawJSON(ctx, http.MethodPost, path, userID, nil, req)
}

// TurnsFinal runs a turn with reply_mode=final and returns JSON.
func (c *Client) TurnsFinal(ctx context.Context, userID string, req TurnRequest) (*TurnFinalReply, error) {
	req.ReplyMode = "final"
	var out TurnFinalReply
	if err := c.doJSON(ctx, http.MethodPost, "/runtime/v1/turns", userID, nil, req, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// TurnsStream runs a turn with reply_mode=stream.
// Caller must Close the returned body. Headers are a clone of the response headers.
func (c *Client) TurnsStream(ctx context.Context, userID string, req TurnRequest) (io.ReadCloser, http.Header, error) {
	req.ReplyMode = "stream"
	resp, err := c.doRequest(ctx, http.MethodPost, "/runtime/v1/turns", userID, nil, req)
	if err != nil {
		return nil, nil, err
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		defer resp.Body.Close()
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
		return nil, nil, &HTTPError{
			StatusCode: resp.StatusCode,
			Method:     http.MethodPost,
			Path:       "/runtime/v1/turns",
			Body:       body,
		}
	}
	return resp.Body, resp.Header.Clone(), nil
}

func listQuery(q ListSessionsQuery) url.Values {
	vals := url.Values{}
	if q.Page > 0 {
		vals.Set("page", strconv.FormatInt(int64(q.Page), 10))
	}
	if q.PageSize > 0 {
		vals.Set("page_size", strconv.FormatInt(int64(q.PageSize), 10))
	}
	if q.Q != "" {
		vals.Set("q", q.Q)
	}
	if q.IncludePreview {
		vals.Set("include_preview", "true")
	}
	return vals
}

func (c *Client) doRawJSON(ctx context.Context, method, path, userID string, query url.Values, body any) (json.RawMessage, error) {
	var raw json.RawMessage
	if err := c.doJSON(ctx, method, path, userID, query, body, &raw); err != nil {
		return nil, err
	}
	return raw, nil
}

func (c *Client) doJSON(ctx context.Context, method, path, userID string, query url.Values, body any, out any) error {
	resp, err := c.doRequest(ctx, method, path, userID, query, body)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	raw, err := io.ReadAll(resp.Body)
	if err != nil {
		return fmt.Errorf("read response: %w", err)
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return &HTTPError{
			StatusCode: resp.StatusCode,
			Method:     method,
			Path:       path,
			Body:       raw,
		}
	}
	if out == nil || len(raw) == 0 {
		return nil
	}
	if err := json.Unmarshal(raw, out); err != nil {
		return fmt.Errorf("decode response: %w", err)
	}
	return nil
}

func (c *Client) doRequest(ctx context.Context, method, path, userID string, query url.Values, body any) (*http.Response, error) {
	if c == nil || c.baseURL == "" {
		return nil, fmt.Errorf("runtime client not configured")
	}
	u := c.baseURL + path
	if len(query) > 0 {
		u += "?" + query.Encode()
	}
	var bodyReader io.Reader
	if body != nil {
		raw, err := json.Marshal(body)
		if err != nil {
			return nil, fmt.Errorf("encode body: %w", err)
		}
		bodyReader = bytes.NewReader(raw)
	}
	req, err := http.NewRequestWithContext(ctx, method, u, bodyReader)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+c.token)
	if userID = strings.TrimSpace(userID); userID != "" {
		req.Header.Set(headerUserID, userID)
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, err
	}
	return resp, nil
}
