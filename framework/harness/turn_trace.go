package harness

import (
	"bytes"
	"encoding/json"
	"fmt"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/sixath/framework/redact"
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

	// ModelCalls/InputTokens/OutputTokens 为本 turn 的模型调用次数与聚合用量
	// （来自 RunTrace；provider 未返回 usage 时 token 为 0）。
	// 随 payload_json 一起持久化，无需额外列即可在 turn_trace 中查询。
	ModelCalls   int `json:"model_calls,omitempty"`
	InputTokens  int `json:"input_tokens,omitempty"`
	OutputTokens int `json:"output_tokens,omitempty"`
	// EstimatedCostUSD 为本 turn 估算成本（美元），由 Portal 计算并随 payload_json 持久化。
	EstimatedCostUSD float64 `json:"estimated_cost_usd,omitempty"`
	// EmptyFinal 终答为空时的诊断，随 payload_json 持久化。
	EmptyFinal *EmptyFinalDiag `json:"empty_final,omitempty"`
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

func BuildTurnTrace(meta TurnTraceMeta, tr *RunTrace) *TurnTrace {
	if tr == nil {
		return nil
	}
	out := &TurnTrace{
		SessionID:        meta.SessionID,
		AgentID:          meta.AgentID,
		RequestID:        meta.RequestID,
		CreatedAt:        time.Now().UTC(),
		ModelCalls:       tr.ModelCalls,
		InputTokens:      tr.InputTokens,
		OutputTokens:     tr.OutputTokens,
		EstimatedCostUSD: tr.EstimatedCostUSD,
		EmptyFinal:       tr.EmptyFinal,
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
			Error:         redact.String(r.Error),
			Blocked:       r.Blocked,
			Decision:      r.Decision,
			DurationMS:    r.DurationMS,
		})
	}
	return out
}

func redactArgs(args map[string]any) map[string]any {
	if args == nil {
		return nil
	}
	out := make(map[string]any, len(args))
	for k, v := range args {
		if redact.SecretKey(k) {
			out[k] = "[redacted]"
		} else {
			out[k] = redact.Value(v)
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
		if isLargeBase64(v) {
			return "[omitted binary]"
		}
		s = redact.String(v)
	default:
		s = redactedJSON(v)
		if isLargeBase64(s) {
			return "[omitted binary]"
		}
	}
	return truncateRunes(s, maxResultRunes)
}

// redactedJSON 解码为通用值后逐个字符串遮盖再序列化；不对 JSON 文本跑正则，避免跨字段匹配与转义引号漏遮。
func redactedJSON(v any) string {
	b, err := json.Marshal(v)
	if err != nil {
		return redact.String(fmt.Sprint(v))
	}
	var generic any
	dec := json.NewDecoder(bytes.NewReader(b))
	dec.UseNumber()
	if err := dec.Decode(&generic); err != nil {
		return redact.String(string(b))
	}
	before, err1 := json.Marshal(generic)
	after, err2 := json.Marshal(redact.Value(generic))
	if err1 != nil || err2 != nil {
		return redact.String(string(b))
	}
	if string(before) == string(after) {
		return string(b)
	}
	return string(after)
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
