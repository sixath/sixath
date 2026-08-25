package model

import (
	"encoding/json"
	"unicode/utf8"
)

var l2PinnedJSONKeys = map[string]struct{}{
	"control_flow": {},
	"call_graph":   {},
}

var l2TruncatableJSONKeys = map[string]struct{}{
	"content": {},
	"snippet": {},
}

// pruneToolBodyPreservingPinnedJSON truncates oversized tool JSON by shrinking
// content/snippet strings while leaving control_flow intact. Non-JSON bodies
// still use rune truncation.
func pruneToolBodyPreservingPinnedJSON(content string, maxRunes int) string {
	if maxRunes <= 0 || utf8.RuneCountInString(content) <= maxRunes {
		return content
	}
	var raw any
	if err := json.Unmarshal([]byte(content), &raw); err != nil {
		return TruncateMessageRunes(content, maxRunes, l2ToolPrePruneSuffix)
	}
	if !jsonHasPinnedKey(raw, l2PinnedJSONKeys) {
		return TruncateMessageRunes(content, maxRunes, l2ToolPrePruneSuffix)
	}
	pinBudget := utf8.RuneCountInString(content) - jsonPinnedRunes(raw) - 64
	if pinBudget < 64 {
		pinBudget = 64
	}
	stringBudget := pinBudget
	if stringBudget > maxRunes {
		stringBudget = maxRunes
	}
	for pass := 0; pass < 4; pass++ {
		shrinkJSONStrings(raw, stringBudget, l2PinnedJSONKeys, l2TruncatableJSONKeys)
		b, err := json.Marshal(raw)
		if err != nil {
			return TruncateMessageRunes(content, maxRunes, l2ToolPrePruneSuffix)
		}
		if utf8.RuneCount(b) <= maxRunes || stringBudget <= 32 {
			return string(b)
		}
		stringBudget /= 2
		if stringBudget < 32 {
			stringBudget = 32
		}
	}
	b, err := json.Marshal(raw)
	if err != nil {
		return TruncateMessageRunes(content, maxRunes, l2ToolPrePruneSuffix)
	}
	// Never mid-cut JSON that still carries control_flow.
	return string(b)
}

func jsonHasPinnedKey(v any, keys map[string]struct{}) bool {
	switch x := v.(type) {
	case map[string]any:
		for k, val := range x {
			if _, ok := keys[k]; ok && val != nil {
				return true
			}
			if jsonHasPinnedKey(val, keys) {
				return true
			}
		}
	case []any:
		for _, item := range x {
			if jsonHasPinnedKey(item, keys) {
				return true
			}
		}
	}
	return false
}

func jsonPinnedRunes(v any) int {
	n := 0
	switch x := v.(type) {
	case map[string]any:
		for k, val := range x {
			if _, ok := l2PinnedJSONKeys[k]; ok {
				if b, err := json.Marshal(val); err == nil {
					n += utf8.RuneCount(b)
				}
				continue
			}
			n += jsonPinnedRunes(val)
		}
	case []any:
		for _, item := range x {
			n += jsonPinnedRunes(item)
		}
	}
	return n
}

func shrinkJSONStrings(v any, maxStringRunes int, preserve, truncKeys map[string]struct{}) {
	switch x := v.(type) {
	case map[string]any:
		for k, val := range x {
			if _, keep := preserve[k]; keep {
				continue
			}
			if s, ok := val.(string); ok {
				if _, trunc := truncKeys[k]; trunc && utf8.RuneCountInString(s) > maxStringRunes {
					x[k] = TruncateMessageRunes(s, maxStringRunes, l2ToolPrePruneSuffix)
				}
				continue
			}
			shrinkJSONStrings(val, maxStringRunes, preserve, truncKeys)
		}
	case []any:
		for i := range x {
			shrinkJSONStrings(x[i], maxStringRunes, preserve, truncKeys)
		}
	}
}
