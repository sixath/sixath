package tool

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"sort"
	"strings"
	"time"
)

// jaegerAPIResponse 映射 Jaeger Query API 的最小子集。
type jaegerAPIResponse struct {
	Data []struct {
		TraceID   string       `json:"traceID"`
		Spans     []jaegerSpan `json:"spans"`
		Processes map[string]struct {
			ServiceName string `json:"serviceName"`
		} `json:"processes"`
	} `json:"data"`
}

type jaegerSpan struct {
	SpanID        string `json:"spanID"`
	OperationName string `json:"operationName"`
	Duration      int64  `json:"duration"`  // microseconds
	StartTime     int64  `json:"startTime"` // microseconds since epoch
	ProcessID     string `json:"processID"`
	Tags          []struct {
		Key   string `json:"key"`
		Value any    `json:"value"`
	} `json:"tags"`
}

// RegisterJaegerTool 注册 jaeger_trace 工具。queryURL 为 Jaeger Query 基址(无鉴权)。
func RegisterJaegerTool(reg *Registry, queryURL string) error {
	if reg == nil {
		return errors.New("jaeger tool: registry is nil")
	}
	base := strings.TrimRight(queryURL, "/")
	return reg.Register(Tool{
		Name:        "jaeger_trace",
		Description: "Fetch a Jaeger trace by trace_id (or search by service/operation) and return structured spans, error spans and involved services.",
		Toolset:     ToolsetRCA,
		Parameters: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"trace_id":  map[string]any{"type": "string", "description": "Exact trace ID to fetch the full chain."},
				"service":   map[string]any{"type": "string", "description": "Search mode: service name."},
				"operation": map[string]any{"type": "string", "description": "Search mode: operation name."},
				"limit":     map[string]any{"type": "integer", "description": "Search mode: max traces (default 20)."},
			},
		},
		Execute: func(ctx context.Context, params map[string]any) (any, error) {
			const toolName = "jaeger_trace"
			traceID, _ := params["trace_id"].(string)
			service, _ := params["service"].(string)
			if strings.TrimSpace(traceID) == "" && strings.TrimSpace(service) == "" {
				return rcaErr(toolName, "either trace_id or service is required", ErrorPermanent), nil
			}

			var endpoint string
			if strings.TrimSpace(traceID) != "" {
				endpoint = base + "/api/traces/" + url.PathEscape(traceID)
			} else {
				operation, _ := params["operation"].(string)
				limit := intFromParam(params["limit"], 20)
				q := url.Values{}
				q.Set("service", service)
				if operation != "" {
					q.Set("operation", operation)
				}
				q.Set("limit", fmt.Sprintf("%d", limit))
				endpoint = base + "/api/traces?" + q.Encode()
			}

			body, err := jaegerGET(ctx, endpoint)
			if err != nil {
				return rcaErrFrom(toolName, err), nil
			}
			var parsed jaegerAPIResponse
			if err := json.Unmarshal(body, &parsed); err != nil {
				return rcaErr(toolName, fmt.Sprintf("decode jaeger response: %v", err), ErrorPermanent), nil
			}
			result := summarizeJaeger(parsed)
			if tid := strings.TrimSpace(traceID); tid != "" {
				result["trace_id"] = tid
			} else if len(parsed.Data) > 0 && parsed.Data[0].TraceID != "" {
				result["trace_id"] = parsed.Data[0].TraceID
			}
			return rcaOK(toolName, result), nil
		},
	})
}

func jaegerGET(ctx context.Context, endpoint string) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return nil, err
	}
	client := &http.Client{Timeout: 30 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	b, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode >= 400 {
		return nil, fmt.Errorf("jaeger returned %d: %s", resp.StatusCode, strings.TrimSpace(string(b)))
	}
	return b, nil
}

func summarizeJaeger(parsed jaegerAPIResponse) map[string]any {
	spans := []map[string]any{}
	errs := []map[string]any{}
	serviceSet := map[string]struct{}{}

	for _, tr := range parsed.Data {
		for _, sp := range tr.Spans {
			svc := ""
			if p, ok := tr.Processes[sp.ProcessID]; ok {
				svc = p.ServiceName
			}
			if svc != "" {
				serviceSet[svc] = struct{}{}
			}
			isErr := false
			for _, tg := range sp.Tags {
				if tg.Key == "error" {
					switch v := tg.Value.(type) {
					case bool:
						if v {
							isErr = true
						}
					case string:
						if strings.EqualFold(v, "true") {
							isErr = true
						}
					}
				}
			}
			row := map[string]any{
				"service":     svc,
				"operation":   sp.OperationName,
				"start":       sp.StartTime,
				"duration_ms": float64(sp.Duration) / 1000.0,
				"error":       isErr,
			}
			spans = append(spans, row)
			if isErr {
				errs = append(errs, map[string]any{
					"service":   svc,
					"operation": sp.OperationName,
				})
			}
		}
	}

	services := make([]string, 0, len(serviceSet))
	for s := range serviceSet {
		services = append(services, s)
	}
	sort.Strings(services)

	return map[string]any{
		"spans":    spans,
		"errors":   errs,
		"services": services,
	}
}
