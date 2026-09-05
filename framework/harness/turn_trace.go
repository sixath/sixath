package harness

import (
	"encoding/json"
	"fmt"
	"strings"
	"time"
	"unicode/utf8"
)

type TurnTraceMeta struct {
	SessionID, AgentID, RequestID string
}

type TurnTrace struct {
	SessionID string         `json:"session_id"`
	AgentID   string         `json:"agent_id"`
	RequestID string         `json:"request_id"`
	TurnSeq   int            `json:"turn_seq"`
	CreatedAt time.Time      `json:"created_at"`
	Calls     []TurnToolCall `json:"calls"`
}

type TurnToolCall struct {
	Step          int            `json:"step"`
	ToolCallID    string         `json:"tool_call_id"`
	ToolName      string         `json:"tool_name"`
	BridgeName    string         `json:"bridge_name,omitempty"`
	Arguments     map[string]any `json:"arguments,omitempty"`
	ResultPreview string         `json:"result_preview,omitempty"`
	Error         string         `json:"error,omitempty"`
	Blocked       bool           `json:"blocked,omitempty"`
	Decision      string         `json:"decision,omitempty"`
	DurationMS    int64          `json:"duration_ms,omitempty"`
}

const (
	maxArgBytes    = 2048
	maxResultRunes = 4096
	maxCalls       = 40
)

var secretKeySubstrings = []string{
	"password",
	"token",
	"secret",
	"api_key",
	"authorization",
}

func BuildTurnTrace(meta TurnTraceMeta, tr *RunTrace) *TurnTrace {
	if tr == nil {
		return nil
	}
	out := &TurnTrace{
		SessionID: meta.SessionID,
		AgentID:   meta.AgentID,
		RequestID: meta.RequestID,
		CreatedAt: time.Now().UTC(),
	}
	recs := tr.ToolCalls
	if len(recs) > maxCalls {
		recs = preferFailedThenTrim(recs, maxCalls)
	}
	for _, r := range recs {
		out.Calls = append(out.Calls, TurnToolCall{
			Step:          r.Step,
			ToolCallID:    r.ToolCallID,
			ToolName:      r.ToolName,
			Arguments:     redactArgs(r.Arguments),
			ResultPreview: previewResult(r.Result),
			Error:         r.Error,
			Blocked:       r.Blocked,
			Decision:      r.Decision,
			DurationMS:    r.DurationMS,
		})
	}
	return out
}

func isSecretKey(key string) bool {
	kl := strings.ToLower(key)
	for _, sub := range secretKeySubstrings {
		if strings.Contains(kl, sub) {
			return true
		}
	}
	return false
}

func redactArgs(args map[string]any) map[string]any {
	if args == nil {
		return nil
	}
	out := make(map[string]any, len(args))
	for k, v := range args {
		if isSecretKey(k) {
			out[k] = "[redacted]"
		} else {
			out[k] = v
		}
	}
	return truncateArgsMap(out)
}

func truncateArgsMap(m map[string]any) map[string]any {
	b, err := json.Marshal(m)
	if err != nil || len(b) <= maxArgBytes {
		return m
	}
	for len(b) > maxArgBytes {
		trimmed := false
		for k, v := range m {
			s, ok := v.(string)
			if !ok || len(s) <= 64 {
				continue
			}
			cut := len(s) / 2
			if cut < 32 {
				cut = 32
			}
			if cut > len(s) {
				cut = len(s)
			}
			m[k] = s[:cut] + "…"
			trimmed = true
			break
		}
		if !trimmed {
			preview := truncateUTF8Bytes(string(b), maxArgBytes)
			return map[string]any{"_truncated": preview}
		}
		b, err = json.Marshal(m)
		if err != nil {
			return map[string]any{"_truncated": truncateUTF8Bytes(string(b), maxArgBytes)}
		}
	}
	return m
}

func truncateUTF8Bytes(s string, maxBytes int) string {
	if len(s) <= maxBytes {
		return s
	}
	for maxBytes > 0 && !utf8.RuneStart(s[maxBytes]) {
		maxBytes--
	}
	return s[:maxBytes] + "…"
}

func previewResult(result any) string {
	if result == nil {
		return ""
	}
	var s string
	switch v := result.(type) {
	case string:
		s = v
	default:
		b, err := json.Marshal(v)
		if err != nil {
			s = fmt.Sprint(v)
		} else {
			s = string(b)
		}
	}
	if isLargeBase64(s) {
		return "[omitted binary]"
	}
	return truncateRunes(s, maxResultRunes)
}

func isLargeBase64(s string) bool {
	const minLen = 256
	if len(s) < minLen {
		return false
	}
	trim := strings.TrimSpace(s)
	if len(trim) < minLen {
		return false
	}
	for i := 0; i < len(trim); i++ {
		c := trim[i]
		if (c >= 'A' && c <= 'Z') || (c >= 'a' && c <= 'z') || (c >= '0' && c <= '9') ||
			c == '+' || c == '/' || c == '=' || c == '\n' || c == '\r' {
			continue
		}
		return false
	}
	return true
}

func preferFailedThenTrim(recs []ToolCallRecord, limit int) []ToolCallRecord {
	if len(recs) <= limit {
		return recs
	}
	failed := make([]ToolCallRecord, 0, len(recs))
	rest := make([]ToolCallRecord, 0, len(recs))
	for _, r := range recs {
		if r.Error != "" || r.Blocked {
			failed = append(failed, r)
		} else {
			rest = append(rest, r)
		}
	}
	out := append(failed, rest...)
	if len(out) > limit {
		out = out[:limit]
	}
	return out
}
