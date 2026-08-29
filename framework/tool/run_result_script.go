package tool

import (
	"encoding/json"
	"strings"
	"unicode/utf8"
)

func splitScriptOutput(raw string) []string {
	raw = strings.ReplaceAll(raw, "\r\n", "\n")
	parts := strings.Split(raw, "\n")
	if len(parts) > 0 && parts[len(parts)-1] == "" {
		parts = parts[:len(parts)-1]
	}
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		if p == "" {
			continue
		}
		out = append(out, p)
	}
	return out
}

func rowsFromScriptLines(lines []string) []map[string]any {
	if len(lines) == 0 {
		return nil
	}
	objs := make([]map[string]any, 0, len(lines))
	allObj := true
	for _, line := range lines {
		var m map[string]any
		if err := json.Unmarshal([]byte(line), &m); err != nil || m == nil {
			allObj = false
			break
		}
		objs = append(objs, m)
	}
	if allObj {
		return objs
	}
	out := make([]map[string]any, len(lines))
	for i, line := range lines {
		out[i] = map[string]any{"line": line}
	}
	return out
}

func truncateUTF8Bytes(s string, n int) string {
	if n < 0 {
		return ""
	}
	if len(s) <= n {
		return s
	}
	s = s[:n]
	for len(s) > 0 && !utf8.ValidString(s) {
		s = s[:len(s)-1]
	}
	return s
}
