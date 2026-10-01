package harness

import (
	"log/slog"
	"strings"
	"unicode/utf8"

	"github.com/sixath/framework/model"
	"github.com/sixath/framework/redact"
)

const emptyFinalReasoningPreviewBytes = 2048

// EmptyFinalDiag 记录终答正文为空时的现场，用于判断是截断、拒答还是只输出了推理内容。只记录，不重试。
type EmptyFinalDiag struct {
	Step             int    `json:"step"`
	FinishReason     string `json:"finish_reason,omitempty"`
	InputTokens      int    `json:"input_tokens,omitempty"`
	OutputTokens     int    `json:"output_tokens,omitempty"`
	ReasoningChars   int    `json:"reasoning_chars,omitempty"`
	ReasoningPreview string `json:"reasoning_preview,omitempty"`
	ToolCalls        int    `json:"tool_calls"`
}

func (t *RunTrace) recordEmptyFinal(gen *model.Generation, step int) {
	if t == nil || gen == nil || strings.TrimSpace(gen.Text) != "" {
		return
	}
	d := &EmptyFinalDiag{Step: step, FinishReason: gen.FinishReason, ToolCalls: len(t.ToolCalls)}
	if gen.TokenUsage != nil {
		d.InputTokens = gen.TokenUsage.InputTokens
		d.OutputTokens = gen.TokenUsage.OutputTokens
	}
	if st, ok := gen.Raw.(model.ToolStep); ok && st.ReasoningContent != "" {
		d.ReasoningChars = utf8.RuneCountInString(st.ReasoningContent)
		d.ReasoningPreview = truncateEmptyFinalPreview(redact.String(st.ReasoningContent), emptyFinalReasoningPreviewBytes)
	}
	t.EmptyFinal = d
	slog.Warn("react: empty final reply",
		"request_id", t.RequestID, "step", step, "finish_reason", d.FinishReason,
		"input_tokens", d.InputTokens, "output_tokens", d.OutputTokens,
		"reasoning_chars", d.ReasoningChars, "tool_calls", d.ToolCalls)
}

// truncateEmptyFinalPreview 按 UTF-8 边界截断，不追加省略号，结果不超过 maxBytes。
func truncateEmptyFinalPreview(s string, maxBytes int) string {
	if len(s) <= maxBytes {
		return s
	}
	cut := maxBytes
	for cut > 0 && !utf8.RuneStart(s[cut]) {
		cut--
	}
	return s[:cut]
}
