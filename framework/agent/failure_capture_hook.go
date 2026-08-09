package agent

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/sixath/framework/growth"
	"github.com/sixath/framework/tool"
)

// contextKeyRequestMetadata carries Request.Metadata into tool hooks.
type contextKeyRequestMetadata struct{}

// WithRequestMetadata stores req metadata on ctx for ToolHook.After / Before.
func WithRequestMetadata(ctx context.Context, md map[string]any) context.Context {
	if ctx == nil || md == nil {
		return ctx
	}
	return context.WithValue(ctx, contextKeyRequestMetadata{}, md)
}

// RequestMetadataFromContext returns metadata injected via WithRequestMetadata.
func RequestMetadataFromContext(ctx context.Context) map[string]any {
	if ctx == nil {
		return nil
	}
	md, _ := ctx.Value(contextKeyRequestMetadata{}).(map[string]any)
	return md
}

// FailureCaptureConfig configures FailureCaptureHook (G4).
type FailureCaptureConfig struct {
	// DedupTTL skips re-append of the same fingerprint within this window. <=0 → 5m.
	DedupTTL time.Duration
	// Append overrides growth.AppendErrorLearning (tests).
	Append func(workspace, summary, details, area string) error
}

// FailureCaptureHook writes tool hard/soft failures to .learnings/ERRORS.md.
// Write failures are swallowed (do not alter tool result). Default-off at Portal.
type FailureCaptureHook struct {
	cfg   FailureCaptureConfig
	mu    sync.Mutex
	seen  map[string]time.Time
	appendFn func(workspace, summary, details, area string) error
}

// NewFailureCaptureHook builds a ToolHook that captures tool failures into ERRORS.md.
func NewFailureCaptureHook(cfg FailureCaptureConfig) *FailureCaptureHook {
	fn := cfg.Append
	if fn == nil {
		fn = growth.AppendErrorLearning
	}
	ttl := cfg.DedupTTL
	if ttl <= 0 {
		ttl = 5 * time.Minute
	}
	cfg.DedupTTL = ttl
	return &FailureCaptureHook{
		cfg:      cfg,
		seen:     make(map[string]time.Time),
		appendFn: fn,
	}
}

// Before passes args through unchanged.
func (h *FailureCaptureHook) Before(_ context.Context, _ string, params map[string]any) (map[string]any, error) {
	return params, nil
}

// After appends ERRORS.md on hard/soft failure; never changes result/err.
func (h *FailureCaptureHook) After(ctx context.Context, name string, result any, err error) (any, error) {
	if h == nil {
		return result, err
	}
	if shouldSkipFailureCapture(name, RequestMetadataFromContext(ctx)) {
		return result, err
	}
	summary, details, ok := failureCaptureMessage(name, result, err)
	if !ok {
		return result, err
	}
	ws, _ := ctx.Value(tool.ContextKeyWorkspaceRoot).(string)
	ws = strings.TrimSpace(ws)
	if ws == "" {
		return result, err
	}
	fp := failureFingerprint(name, summary)
	if h.seenRecently(fp) {
		return result, err
	}
	_ = h.appendFn(ws, summary, details, "tool_failure")
	return result, err
}

func shouldSkipFailureCapture(name string, md map[string]any) bool {
	switch name {
	case "append_learning", "skill_manage":
		return true
	}
	if growth.IsGrowthReviewMetadata(md) || growth.ShouldSkipGrowthReview(md) {
		return true
	}
	return false
}

func failureCaptureMessage(name string, result any, execErr error) (summary, details string, ok bool) {
	if execErr != nil {
		summary = fmt.Sprintf("tool %s failed: %s", name, truncateRunes(execErr.Error(), 200))
		details = fmt.Sprintf("tool=%s\nkind=hard\nerror=%s", name, execErr.Error())
		return summary, details, true
	}
	m, isMap := result.(map[string]any)
	if !isMap {
		return "", "", false
	}
	if okVal, has := m["ok"]; has {
		switch v := okVal.(type) {
		case bool:
			if v {
				return "", "", false
			}
		case string:
			if strings.EqualFold(v, "true") || v == "1" {
				return "", "", false
			}
		}
		errText := softErrorText(m)
		if errText == "" {
			errText = "ok=false"
		}
		summary = fmt.Sprintf("tool %s soft-fail: %s", name, truncateRunes(errText, 200))
		details = fmt.Sprintf("tool=%s\nkind=soft\nok=false\nerror=%s", name, errText)
		return summary, details, true
	}
	errText := softErrorText(m)
	if errText == "" {
		return "", "", false
	}
	summary = fmt.Sprintf("tool %s soft-fail: %s", name, truncateRunes(errText, 200))
	details = fmt.Sprintf("tool=%s\nkind=soft\nerror=%s", name, errText)
	return summary, details, true
}

func softErrorText(m map[string]any) string {
	if m == nil {
		return ""
	}
	for _, key := range []string{"error", "Error", "err"} {
		if v, ok := m[key]; ok && v != nil {
			s := strings.TrimSpace(fmt.Sprint(v))
			if s != "" && s != "<nil>" {
				return s
			}
		}
	}
	return ""
}

func failureFingerprint(name, summary string) string {
	return name + "|" + summary
}

func (h *FailureCaptureHook) seenRecently(fp string) bool {
	h.mu.Lock()
	defer h.mu.Unlock()
	now := time.Now()
	cutoff := now.Add(-h.cfg.DedupTTL)
	for k, at := range h.seen {
		if at.Before(cutoff) {
			delete(h.seen, k)
		}
	}
	if at, ok := h.seen[fp]; ok && at.After(cutoff) {
		return true
	}
	h.seen[fp] = now
	return false
}

func truncateRunes(s string, max int) string {
	if max <= 0 {
		return s
	}
	r := []rune(s)
	if len(r) <= max {
		return s
	}
	return string(r[:max]) + "..."
}
