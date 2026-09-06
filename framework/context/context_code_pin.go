package context

import (
	"encoding/json"
	"github.com/sixath/framework/model"
	"strings"
	"unicode/utf8"
)

const (
	codePinMaxReads        = 3
	codePinMaxRunes        = 8000
	codePinContentMaxRunes = 4000
	codePinPrefix          = "[code_pin]"
)

// ensureCodePinMessages copies the last N rca_read content windows and CFG
// summaries into a leading system message so L0/L2 can drop tool bodies
// without losing the source window.
func ensureCodePinMessages(msgs []model.Message) []model.Message {
	reads := extractCodePinReads(msgs)
	if len(reads) == 0 {
		return dropCodePinMessages(msgs)
	}
	content, err := assembleCodePinContent(reads)
	if err != nil {
		return msgs
	}
	pin := model.Message{
		Role:    "system",
		Content: content,
		Metadata: map[string]any{
			model.MetadataKeySixathOrigin: model.OriginCodePin,
		},
	}
	out := dropCodePinMessages(msgs)
	head := leadingSystemCount(out)
	with := make([]model.Message, 0, len(out)+1)
	with = append(with, out[:head]...)
	with = append(with, pin)
	with = append(with, out[head:]...)
	return with
}

func assembleCodePinContent(reads []map[string]any) (string, error) {
	content, err := marshalCodePin(reads)
	if err != nil {
		return "", err
	}
	if utf8.RuneCountInString(content) <= codePinMaxRunes {
		return content, nil
	}

	for _, r := range reads {
		delete(r, "call_graph")
	}
	content, err = marshalCodePin(reads)
	if err != nil {
		return "", err
	}
	if utf8.RuneCountInString(content) <= codePinMaxRunes {
		return content, nil
	}

	for _, r := range reads {
		shortenControlFlowWhen(r)
	}
	content, err = marshalCodePin(reads)
	if err != nil {
		return "", err
	}
	if utf8.RuneCountInString(content) <= codePinMaxRunes {
		return content, nil
	}

	if pinContentWouldBeWiped(content, reads) {
		for _, r := range reads {
			delete(r, "control_flow")
		}
		content, err = marshalCodePin(reads)
		if err != nil {
			return "", err
		}
	}

	if utf8.RuneCountInString(content) > codePinMaxRunes {
		content = model.TruncateMessageRunes(content, codePinMaxRunes, "")
	}
	return content, nil
}

func marshalCodePin(reads []map[string]any) (string, error) {
	body, err := json.Marshal(map[string]any{"reads": reads})
	if err != nil {
		return "", err
	}
	return codePinPrefix + "\n" + string(body), nil
}

func pinContentWouldBeWiped(pinContent string, reads []map[string]any) bool {
	prefix := firstContentPrefix(reads, 32)
	if prefix == "" {
		return false
	}
	trunc := model.TruncateMessageRunes(pinContent, codePinMaxRunes, "")
	return !strings.Contains(trunc, prefix)
}

func firstContentPrefix(reads []map[string]any, n int) string {
	for _, r := range reads {
		src, _ := r["content"].(string)
		if src == "" {
			continue
		}
		runes := []rune(src)
		if n > len(runes) {
			n = len(runes)
		}
		if n < 1 {
			return src
		}
		return string(runes[:n])
	}
	return ""
}

func shortenControlFlowWhen(pin map[string]any) {
	cf, ok := pin["control_flow"].([]any)
	if !ok {
		return
	}
	for _, item := range cf {
		fn, _ := item.(map[string]any)
		if fn == nil {
			continue
		}
		paths, _ := fn["paths"].([]any)
		if len(paths) > 1 {
			paths = paths[:1]
			fn["paths"] = paths
		}
		for _, p := range paths {
			pm, _ := p.(map[string]any)
			if pm == nil {
				continue
			}
			switch w := pm["when"].(type) {
			case []any:
				if len(w) > 1 {
					pm["when"] = w[:1]
					w = pm["when"].([]any)
				}
				if len(w) == 1 {
					if s, ok := w[0].(string); ok && utf8.RuneCountInString(s) > 80 {
						pm["when"] = []any{model.TruncateMessageRunes(s, 80, "")}
					}
				}
			}
		}
	}
}

func dropCodePinMessages(msgs []model.Message) []model.Message {
	out := make([]model.Message, 0, len(msgs))
	for _, m := range msgs {
		if isCodePinMessage(m) {
			continue
		}
		out = append(out, m)
	}
	return out
}

func isCodePinMessage(m model.Message) bool {
	if m.Metadata != nil {
		if v, ok := m.Metadata[model.MetadataKeySixathOrigin].(string); ok && v == model.OriginCodePin {
			return true
		}
	}
	return strings.HasPrefix(strings.TrimSpace(m.Content), codePinPrefix)
}

func extractCodePinReads(msgs []model.Message) []map[string]any {
	var reads []map[string]any
	for _, m := range msgs {
		if !strings.EqualFold(m.Role, "tool") {
			continue
		}
		pin := pinFromToolContent(m.Content)
		if pin == nil {
			continue
		}
		reads = append(reads, pin)
	}
	if len(reads) > codePinMaxReads {
		reads = reads[len(reads)-codePinMaxReads:]
	}
	return reads
}

func pinFromToolContent(content string) map[string]any {
	var raw any
	if err := json.Unmarshal([]byte(content), &raw); err != nil {
		return nil
	}
	obj, _ := raw.(map[string]any)
	if obj == nil {
		return nil
	}
	toolName, _ := obj["tool"].(string)
	result, _ := obj["result"].(map[string]any)
	if result == nil {
		result = obj
	}
	if strings.TrimSpace(toolName) != "" && toolName != "rca_read" {
		return nil
	}
	src, _ := result["content"].(string)
	cf := result["control_flow"]
	cg := result["call_graph"]
	if src == "" && cf == nil && cg == nil {
		return nil
	}
	pin := map[string]any{}
	if file := strings.TrimSpace(anyStringPin(result["file"])); file != "" {
		pin["file"] = file
	} else if path := strings.TrimSpace(anyStringPin(result["path"])); path != "" {
		pin["file"] = path
	}
	if repo := strings.TrimSpace(anyStringPin(result["repo"])); repo != "" {
		pin["repo"] = repo
	}
	if src != "" {
		pin["content"] = model.TruncateMessageRunes(src, codePinContentMaxRunes, "")
	}
	if summary := summarizeControlFlow(cf); summary != nil {
		pin["control_flow"] = summary
	}
	if v, ok := result["start_line"]; ok {
		pin["start_line"] = v
	}
	if v, ok := result["end_line"]; ok {
		pin["end_line"] = v
	}
	if len(pin) == 0 {
		return nil
	}
	return pin
}

func summarizeControlFlow(cf any) any {
	arr, ok := cf.([]any)
	if !ok {
		return nil
	}
	out := make([]any, 0, len(arr))
	for _, item := range arr {
		fn, _ := item.(map[string]any)
		if fn == nil {
			continue
		}
		summary := map[string]any{}
		if name := strings.TrimSpace(anyStringPin(fn["function"])); name != "" {
			summary["function"] = name
		}
		if paths, ok := fn["paths"].([]any); ok {
			var sp []any
			for _, p := range paths {
				pm, _ := p.(map[string]any)
				if pm == nil {
					continue
				}
				if when := pm["when"]; when != nil {
					sp = append(sp, map[string]any{"when": when})
				}
			}
			if len(sp) > 0 {
				summary["paths"] = sp
			}
		}
		if len(summary) > 0 {
			out = append(out, summary)
		}
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

func anyStringPin(v any) string {
	s, _ := v.(string)
	return s
}
