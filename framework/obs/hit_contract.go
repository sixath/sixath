package obs

import (
	"context"
	"log/slog"
	"sync"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/trace"
)

type HitContractLog struct {
	Tool   string
	Status string
	Index  string
	Repo   string
}

var (
	hitHookMu sync.Mutex
	hitHook   func(HitContractLog)
)

func SetHitContractHook(fn func(HitContractLog)) func() {
	hitHookMu.Lock()
	prev := hitHook
	hitHook = fn
	hitHookMu.Unlock()
	return func() {
		hitHookMu.Lock()
		hitHook = prev
		hitHookMu.Unlock()
	}
}

func LogHitContract(ctx context.Context, toolName, status, queriedIndex, repo string) {
	if ctx == nil {
		ctx = context.Background()
	}
	rec := HitContractLog{Tool: toolName, Status: status, Index: queriedIndex, Repo: repo}
	hitHookMu.Lock()
	hook := hitHook
	hitHookMu.Unlock()
	if hook != nil {
		hook(rec)
	}
	attrs := []any{"tool", toolName}
	if status != "" {
		attrs = append(attrs, "hit_status", status)
	}
	if queriedIndex != "" {
		attrs = append(attrs, "queried_index", queriedIndex)
	}
	if repo != "" {
		attrs = append(attrs, "repo", repo)
	}
	slog.InfoContext(ctx, "hit_contract", attrs...)
	if span := trace.SpanFromContext(ctx); span.IsRecording() {
		var kv []attribute.KeyValue
		if toolName != "" {
			kv = append(kv, attribute.String("sixath.tool", toolName))
		}
		if status != "" {
			kv = append(kv, attribute.String("sixath.hit_status", status))
		}
		if queriedIndex != "" {
			kv = append(kv, attribute.String("sixath.queried_index", queriedIndex))
		}
		if repo != "" {
			kv = append(kv, attribute.String("sixath.repo", repo))
		}
		if len(kv) > 0 {
			span.SetAttributes(kv...)
		}
	}
}
