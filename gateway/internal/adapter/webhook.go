package adapter

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"encoding/json"
	"io"
	"log"
	"net"
	"net/http"
	"strings"
	"time"

	"github.com/sixath/gateway/internal/channel"
	"github.com/sixath/gateway/internal/idempotency"
	"github.com/sixath/gateway/internal/reply"
	"github.com/sixath/gateway/internal/runtimeclient"
	"github.com/sixath/gateway/internal/session"
)

// Webhook secret is read from the X-Webhook-Secret request header.
const HeaderWebhookSecret = "X-Webhook-Secret"

type webhookBody struct {
	Content        string `json:"content"`
	PeerID         string `json:"peer_id"`
	AgentID        string `json:"agent_id"`
	ReplyURL       string `json:"reply_url"`
	IdempotencyKey string `json:"idempotency_key"`
	ReplyMode      string `json:"reply_mode"`
}

// WebhookDeps wires webhook handler dependencies.
type WebhookDeps struct {
	Registry    *channel.Registry
	Runtime     *runtimeclient.Client
	Sessions    *session.Router
	Idempotency *idempotency.Store
	Reply       *reply.Dispatcher
	TurnTimeout time.Duration
}

// WebhookHandler serves POST /hooks/{channel_id}.
type WebhookHandler struct {
	deps WebhookDeps
}

// NewWebhookHandler builds an HTTP handler for webhook inbound.
func NewWebhookHandler(deps WebhookDeps) http.Handler {
	if deps.TurnTimeout <= 0 {
		deps.TurnTimeout = 120 * time.Second
	}
	if deps.Idempotency == nil {
		deps.Idempotency = idempotency.NewStore(0)
	}
	if deps.Reply == nil {
		deps.Reply = reply.NewDispatcher(nil)
	}
	return &WebhookHandler{deps: deps}
}

func (h *WebhookHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	channelID := r.PathValue("channel_id")
	if channelID == "" {
		// Fallback for muxes that don't populate PathValue.
		channelID = strings.TrimPrefix(r.URL.Path, "/hooks/")
		channelID = strings.Trim(channelID, "/")
	}
	if channelID == "" {
		http.Error(w, "missing channel_id", http.StatusBadRequest)
		return
	}

	ch, err := h.deps.Registry.Get(channelID)
	if err != nil {
		http.Error(w, "unknown channel", http.StatusNotFound)
		return
	}
	if !ch.Enabled {
		http.Error(w, "channel disabled", http.StatusGone)
		return
	}
	if !webhookSecretEqual(ch.WebhookSecret, r.Header.Get(HeaderWebhookSecret)) {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}
	if len(ch.IPWhitelist) > 0 && !ipAllowed(clientIP(r), ch.IPWhitelist) {
		http.Error(w, "forbidden", http.StatusForbidden)
		return
	}

	raw, err := io.ReadAll(io.LimitReader(r.Body, 1<<20))
	if err != nil {
		http.Error(w, "read body", http.StatusBadRequest)
		return
	}
	ev, err := normalizeWebhook(channelID, ch, raw)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	// Idempotency: same key must not open a second turn.
	if ev.IdempotencyKey != "" {
		if existing, ok := h.deps.Idempotency.Get(ev.IdempotencyKey); ok {
			writeJSON(w, http.StatusAccepted, map[string]any{"correlation_id": existing.CorrelationID})
			if existing.Status == idempotency.StatusDone && existing.Result != nil {
				if payload, ok := existing.Result.(reply.FinalPayload); ok && ev.ReplyURL != "" {
					go func() {
						ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
						defer cancel()
						_ = h.deps.Reply.PostReplyURL(ctx, ev.ReplyURL, payload)
					}()
				}
			}
			return
		}
	}

	ctx := r.Context()
	resolved, err := h.deps.Sessions.Resolve(ctx, "", runtimeclient.ResolveRequest{
		ChannelID: ev.ChannelID,
		PeerID:    ev.PeerID,
		AgentID:   ev.AgentID,
	})
	if err != nil {
		log.Printf("webhook resolve: %v", err)
		http.Error(w, "resolve failed", http.StatusBadGateway)
		return
	}
	userID := resolved.UserID

	corr := newCorrelationID()
	ev.CorrelationID = corr
	if _, ok := h.deps.Idempotency.Begin(ev.IdempotencyKey, corr); !ok {
		// Race: another request won Begin between Get and Begin.
		if existing, ok := h.deps.Idempotency.Get(ev.IdempotencyKey); ok {
			writeJSON(w, http.StatusAccepted, map[string]any{"correlation_id": existing.CorrelationID})
			return
		}
	}

	if ev.ReplyMode == "sync" {
		h.handleSync(w, ev, resolved.SessionID, userID)
		return
	}

	writeJSON(w, http.StatusAccepted, map[string]any{"correlation_id": corr})
	go h.runAsync(ev, resolved.SessionID, userID)
}

func (h *WebhookHandler) handleSync(w http.ResponseWriter, ev InboundEvent, sessionID, userID string) {
	ctx, cancel := context.WithTimeout(context.Background(), h.deps.TurnTimeout)
	defer cancel()
	payload := h.runTurn(ctx, ev, sessionID, userID)
	h.deps.Idempotency.Complete(ev.IdempotencyKey, payload)
	if ev.ReplyURL != "" {
		_ = h.deps.Reply.PostReplyURL(ctx, ev.ReplyURL, payload)
	}
	status := http.StatusOK
	if payload.Status == "failed" {
		// Still 200 with failed body per sync contract (client inspects status field).
		status = http.StatusOK
	}
	writeJSON(w, status, payload)
}

func (h *WebhookHandler) runAsync(ev InboundEvent, sessionID, userID string) {
	ctx, cancel := context.WithTimeout(context.Background(), h.deps.TurnTimeout)
	defer cancel()
	payload := h.runTurn(ctx, ev, sessionID, userID)
	h.deps.Idempotency.Complete(ev.IdempotencyKey, payload)
	if err := h.deps.Reply.PostReplyURL(ctx, ev.ReplyURL, payload); err != nil {
		log.Printf("webhook reply_url: %v", err)
	}
}

func (h *WebhookHandler) runTurn(ctx context.Context, ev InboundEvent, sessionID, userID string) reply.FinalPayload {
	out, err := h.deps.Runtime.TurnsFinal(ctx, userID, runtimeclient.TurnRequest{
		SessionID:      sessionID,
		Content:        ev.Content,
		ChannelID:      ev.ChannelID,
		PeerID:         ev.PeerID,
		CorrelationID:  ev.CorrelationID,
		IdempotencyKey: ev.IdempotencyKey,
	})
	if err != nil {
		return reply.FinalPayload{
			CorrelationID: ev.CorrelationID,
			Status:        "failed",
			Error:         err.Error(),
		}
	}
	status := out.Status
	if status == "" {
		status = "ok"
	}
	// Gateway-issued correlation_id is authoritative for callers (202 / reply_url).
	return reply.FinalPayload{
		CorrelationID: ev.CorrelationID,
		Status:        status,
		Content:       out.Content,
		Error:         out.Error,
	}
}

func normalizeWebhook(channelID string, ch channel.Channel, raw []byte) (InboundEvent, error) {
	var body webhookBody
	if err := json.Unmarshal(raw, &body); err != nil {
		return InboundEvent{}, err
	}
	body.Content = strings.TrimSpace(body.Content)
	body.PeerID = strings.TrimSpace(body.PeerID)
	if body.Content == "" {
		return InboundEvent{}, errBadRequest("content is required")
	}
	if body.PeerID == "" {
		return InboundEvent{}, errBadRequest("peer_id is required")
	}
	agentID := strings.TrimSpace(body.AgentID)
	if agentID == "" {
		agentID = ch.DefaultAgent
	}
	mode := strings.ToLower(strings.TrimSpace(body.ReplyMode))
	if mode == "" {
		mode = strings.ToLower(strings.TrimSpace(ch.DefaultReplyMode))
	}
	if mode == "" {
		mode = "async"
	}
	if mode != "async" && mode != "sync" {
		return InboundEvent{}, errBadRequest("reply_mode must be async or sync")
	}
	replyURL := strings.TrimSpace(body.ReplyURL)
	if err := reply.ValidateReplyURL(replyURL); err != nil {
		return InboundEvent{}, errBadRequest(err.Error())
	}
	return InboundEvent{
		ChannelID:      channelID,
		PeerID:         body.PeerID,
		Content:        body.Content,
		AgentID:        agentID,
		ReplyURL:       replyURL,
		IdempotencyKey: strings.TrimSpace(body.IdempotencyKey),
		ReplyMode:      mode,
	}, nil
}

// webhookSecretEqual compares secrets in constant time via fixed-length SHA-256 digests.
func webhookSecretEqual(expected, got string) bool {
	a := sha256.Sum256([]byte(expected))
	b := sha256.Sum256([]byte(got))
	return subtle.ConstantTimeCompare(a[:], b[:]) == 1
}

type badRequestError string

func (e badRequestError) Error() string { return string(e) }

func errBadRequest(msg string) error { return badRequestError(msg) }

func clientIP(r *http.Request) string {
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return r.RemoteAddr
	}
	return host
}

func ipAllowed(ip string, whitelist []string) bool {
	for _, allowed := range whitelist {
		if ip == allowed {
			return true
		}
	}
	return false
}

func newCorrelationID() string {
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		return hex.EncodeToString([]byte(time.Now().Format(time.RFC3339Nano)))
	}
	return hex.EncodeToString(b[:])
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}
