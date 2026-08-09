package server

import (
	"encoding/json"
	"net/http"
	"strings"
	"sync/atomic"

	"backend/api/common"

	httptransport "github.com/go-kratos/kratos/v2/transport/http"
)

// publicInboundEnabled defaults to false (Gateway is the public ingress).
// Tests and local debugging may call ConfigurePublicInbound(true).
var publicInboundEnabled atomic.Bool

// ConfigurePublicInbound sets whether legacy public Chat inbound routes are open.
func ConfigurePublicInbound(enabled bool) {
	publicInboundEnabled.Store(enabled)
}

// PublicInboundEnabled reports whether POST messages / create-session are open on /api/v1.
func PublicInboundEnabled() bool {
	return publicInboundEnabled.Load()
}

// PublicChatInboundFilter rejects gated public Chat inbound when disabled.
// Runtime (/runtime/v1) and Channel management APIs are unaffected.
func PublicChatInboundFilter() httptransport.FilterFunc {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if !PublicInboundEnabled() && isGatedPublicChatInbound(r.Method, r.URL.Path) {
				writePublicInboundDisabled(w)
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}

func isGatedPublicChatInbound(method, path string) bool {
	if method != http.MethodPost {
		return false
	}
	parts := strings.Split(strings.Trim(path, "/"), "/")
	// POST /api/v1/agents/{agent_id}/sessions
	if len(parts) == 5 && parts[0] == "api" && parts[1] == "v1" && parts[2] == "agents" && parts[4] == "sessions" {
		return true
	}
	// POST /api/v1/sessions/{session_id}/messages
	if len(parts) == 5 && parts[0] == "api" && parts[1] == "v1" && parts[2] == "sessions" && parts[4] == "messages" {
		return true
	}
	// POST /api/v1/sessions/{session_id}/messages/stream
	if len(parts) == 6 && parts[0] == "api" && parts[1] == "v1" && parts[2] == "sessions" && parts[4] == "messages" && parts[5] == "stream" {
		return true
	}
	return false
}

func writePublicInboundDisabled(w http.ResponseWriter) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusForbidden)
	_ = json.NewEncoder(w).Encode(&common.Response{
		Ret: &common.BaseResponse{
			Code:    http.StatusForbidden,
			Reason:  "FORBIDDEN",
			Message: "public chat inbound is disabled; use gateway /runtime/v1",
		},
	})
}
