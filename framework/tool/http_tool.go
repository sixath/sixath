package tool

import (
	"context"
	"errors"
	"io"
	"net/http"
	"time"

	"github.com/sixath/framework/events"
)

// RegisterHTTPTool 向 Registry 注册一个通用的 HTTP 请求工具。
// 工具名称：http_request
// 功能：根据给定的 method、url、headers、body 发送 HTTP 请求，并返回状态码、响应头与响应体。
func RegisterHTTPTool(reg *Registry) error {
	if reg == nil {
		return errors.New("http_request: registry is nil")
	}

	return reg.Register(Tool{
		Name:        "http_request",
		Description: "Send an HTTP request with method, url, headers and optional body, and return status, headers and body.",
		Parameters: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"method": map[string]any{
					"type":        "string",
					"description": "HTTP method, e.g. GET, POST, PUT, DELETE, PATCH.",
				},
				"url": map[string]any{
					"type":        "string",
					"description": "Request URL, including query string if needed.",
				},
				"headers": map[string]any{
					"type":        "object",
					"description": "Optional HTTP headers as key-value pairs.",
					"additionalProperties": map[string]any{
						"type": "string",
					},
				},
				"body": map[string]any{
					"type":        "string",
					"description": "Optional request body as string (typically JSON).",
				},
				"timeout_seconds": map[string]any{
					"type":        "number",
					"description": "Optional timeout in seconds for the request (default 30s, max 300s).",
				},
			},
			"required": []string{"method", "url"},
		},
		Execute: func(ctx context.Context, params map[string]any) (any, error) {
			rawMethod, _ := params["method"].(string)
			rawURL, _ := params["url"].(string)
			if rawMethod == "" {
				return nil, errors.New("http_request: method is required")
			}
			if rawURL == "" {
				return nil, errors.New("http_request: url is required")
			}

			method := rawMethod
			if method == "" {
				method = http.MethodGet
			}

			var bodyReader io.Reader
			bodyReq := ""
			if b, ok := params["body"].(string); ok && b != "" {
				bodyReader = &stringReader{s: b}
				bodyReq = b
			}

			// 解析 headers
			headers := http.Header{}
			if h, ok := params["headers"].(map[string]any); ok {
				for k, v := range h {
					if vs, ok := v.(string); ok {
						headers.Add(k, vs)
					}
				}
			}

			// 解析 timeout
			timeout := 30 * time.Second
			if t, ok := params["timeout_seconds"].(float64); ok && t > 0 {
				if t > 300 {
					t = 300
				}
				timeout = time.Duration(t * float64(time.Second))
			}
			// HTTP 请求事件：invoked
			rid, _ := ctx.Value(ContextKeyRequestID).(string)
			invokedPayload := map[string]any{
				"method": method,
				"url":    rawURL,
				"body":   bodyReq,
			}
			events.DefaultBus().Publish(ctx, events.Event{
				Kind:      events.HttpRequestInvoked,
				RequestID: rid,
				Payload:   invokedPayload,
			})

			req, err := http.NewRequestWithContext(ctx, method, rawURL, bodyReader)
			if err != nil {
				return nil, err
			}
			for k, values := range headers {
				for _, v := range values {
					req.Header.Add(k, v)
				}
			}

			client := &http.Client{
				Timeout: timeout,
			}

			resp, err := client.Do(req)
			if err != nil {
				completedPayload := map[string]any{
					"method": method,
					"url":    rawURL,
					"error":  err.Error(),
				}
				events.DefaultBus().Publish(ctx, events.Event{
					Kind:      events.HttpRequestCompleted,
					RequestID: rid,
					Payload:   completedPayload,
				})
				return nil, err
			}
			defer resp.Body.Close()

			respBody, err := io.ReadAll(resp.Body)
			if err != nil {
				return nil, err
			}

			outHeaders := map[string][]string{}
			for k, v := range resp.Header {
				copied := make([]string, len(v))
				copy(copied, v)
				outHeaders[k] = copied
			}

			completedPayload := map[string]any{
				"method":      method,
				"url":         rawURL,
				"status_code": resp.StatusCode,
				"body":        string(respBody),
			}
			events.DefaultBus().Publish(ctx, events.Event{
				Kind:      events.HttpRequestCompleted,
				RequestID: rid,
				Payload:   completedPayload,
			})

			return map[string]any{
				"status_code": resp.StatusCode,
				"status":      resp.Status,
				"headers":     outHeaders,
				"body":        string(respBody),
			}, nil
		},
	})
}

// stringReader 是一个简单的只读字符串 reader，用于避免引入额外依赖。
type stringReader struct {
	s string
	i int64
}

func (r *stringReader) Read(p []byte) (int, error) {
	if r.i >= int64(len(r.s)) {
		return 0, io.EOF
	}
	n := copy(p, r.s[r.i:])
	r.i += int64(n)
	return n, nil
}
