package middleware

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"io"
	"net/http"
	"strings"

	"backend/internal/biz"

	"github.com/go-kratos/kratos/v2/errors"
	"github.com/go-kratos/kratos/v2/middleware"
	khttp "github.com/go-kratos/kratos/v2/transport/http"
)

const webhookPrefix = "/api/v1/webhooks/"

const authPublicPrefix = "/api/v1/auth/"

const runtimePrefix = "/runtime/v1"

// WebhookVerify 对 Webhook 入站请求在解码前做签名校验和 IP 白名单校验，通过后恢复 body 供后续解码
func WebhookVerify(channelUC *biz.ChannelUsecase) middleware.Middleware {
	return func(handler middleware.Handler) middleware.Handler {
		return func(ctx context.Context, req interface{}) (interface{}, error) {
			r, ok := khttp.RequestFromServerContext(ctx)
			if !ok || r == nil {
				return handler(ctx, req)
			}
			path := r.URL.Path
			if !strings.HasPrefix(path, webhookPrefix) {
				return handler(ctx, req)
			}
			channelID := strings.TrimPrefix(path, webhookPrefix)
			if idx := strings.Index(channelID, "/"); idx >= 0 {
				channelID = channelID[:idx]
			}
			if channelID == "" {
				return nil, errors.BadRequest("INVALID", "channel_id required")
			}

			ch, err := channelUC.GetByChannelID(r.Context(), channelID)
			if err != nil {
				return nil, err
			}
			if !ch.Enabled {
				return nil, errors.Forbidden("FORBIDDEN", "channel disabled")
			}
			if ch.Type != "webhook" {
				return nil, errors.BadRequest("INVALID", "channel type must be webhook")
			}
			if len(ch.IPWhitelist) > 0 {
				clientIP := getClientIP(r)
				if !isIPAllowed(clientIP, ch.IPWhitelist) {
					return nil, errors.Forbidden("FORBIDDEN", "IP not in whitelist")
				}
			}

			body, err := io.ReadAll(r.Body)
			if err != nil {
				return nil, errors.BadRequest("INVALID", "invalid body")
			}
			r.Body.Close()

			if ch.WebhookSecret != "" {
				sig := r.Header.Get("X-Signature")
				if sig == "" {
					return nil, errors.Unauthorized("UNAUTHORIZED", "X-Signature required")
				}
				mac := hmac.New(sha256.New, []byte(ch.WebhookSecret))
				mac.Write(body)
				expected := "sha256=" + hex.EncodeToString(mac.Sum(nil))
				if !hmac.Equal([]byte(sig), []byte(expected)) {
					return nil, errors.Unauthorized("UNAUTHORIZED", "invalid signature")
				}
			}

			r.Body = io.NopCloser(bytes.NewReader(body))
			return handler(ctx, req)
		}
	}
}

func getClientIP(r *http.Request) string {
	if xff := r.Header.Get("X-Forwarded-For"); xff != "" {
		return strings.TrimSpace(strings.Split(xff, ",")[0])
	}
	if xri := r.Header.Get("X-Real-IP"); xri != "" {
		return strings.TrimSpace(xri)
	}
	return strings.Split(r.RemoteAddr, ":")[0]
}

func isIPAllowed(ip string, whitelist []string) bool {
	for _, w := range whitelist {
		if w == ip {
			return true
		}
		if strings.HasPrefix(w, ip) || strings.HasPrefix(ip, w) {
			return true
		}
	}
	return false
}
