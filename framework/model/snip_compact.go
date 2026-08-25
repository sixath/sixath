package model

import (
	"encoding/json"
	"fmt"
	"strings"
)

// SnipCompactMessages 在 L2 摘要前移除已被后续同键 tool 链 supersede 的僵尸消息（Claude Code snipCompact 语义）。
// 以 assistant(tool_calls)+tool 为原子单元；含不可 snip 工具或无法提取 dedup 键的链保留不动。
func SnipCompactMessages(msgs []Message) (out []Message, removed int) {
	if len(msgs) == 0 {
		return msgs, 0
	}
	head := leadingSystemCount(msgs)
	chains := enumerateSnipToolChains(msgs, head)
	if len(chains) == 0 {
		return msgs, 0
	}

	latestChain := make(map[string]int)
	chainKeys := make([][]string, len(chains))
	for ci, ch := range chains {
		calls := toolCallsInChain(msgs, ch.start, ch.end)
		keys, blocked := snipKeysForCalls(calls)
		if blocked || len(keys) == 0 {
			continue
		}
		chainKeys[ci] = keys
		for _, k := range keys {
			latestChain[k] = ci
		}
	}

	removeChain := make([]bool, len(chains))
	for ci, keys := range chainKeys {
		if len(keys) == 0 {
			continue
		}
		allSuperseded := true
		for _, k := range keys {
			if latestChain[k] <= ci {
				allSuperseded = false
				break
			}
		}
		if allSuperseded {
			removeChain[ci] = true
		}
	}

	removeIdx := make(map[int]struct{})
	for ci, ch := range chains {
		if !removeChain[ci] {
			continue
		}
		for i := ch.start; i < ch.end; i++ {
			removeIdx[i] = struct{}{}
		}
	}
	if len(removeIdx) == 0 {
		return msgs, 0
	}
	out = make([]Message, 0, len(msgs)-len(removeIdx))
	for i, m := range msgs {
		if _, drop := removeIdx[i]; drop {
			continue
		}
		out = append(out, m)
	}
	return out, len(removeIdx)
}

type snipToolChain struct {
	start int
	end   int // exclusive
}

func enumerateSnipToolChains(msgs []Message, head int) []snipToolChain {
	var chains []snipToolChain
	for i := head; i < len(msgs); {
		if isProtectedRuntimeMessage(msgs[i]) {
			i++
			continue
		}
		if strings.EqualFold(msgs[i].Role, "assistant") && hasToolCallsMeta(msgs[i]) {
			end := toolChainEnd(msgs, i)
			if end > i {
				chains = append(chains, snipToolChain{start: i, end: end})
			}
			i = end
			continue
		}
		i++
	}
	return chains
}

func isProtectedRuntimeMessage(m Message) bool {
	if m.Metadata == nil {
		return false
	}
	origin, _ := m.Metadata[MetadataKeySixathOrigin].(string)
	switch origin {
	case OriginL2Handoff, OriginMemoryFence, OriginCompressionNotice, OriginCompactBoundary, OriginGuardrailHalt, OriginCodeWorkset, OriginCodePin:
		return true
	default:
		return false
	}
}

type snipCall struct {
	name string
	args map[string]any
}

func toolCallsInChain(msgs []Message, start, end int) []snipCall {
	if start < 0 || start >= len(msgs) || end > len(msgs) || start >= end {
		return nil
	}
	if calls := toolCallsFromMessage(msgs[start]); len(calls) > 0 {
		out := make([]snipCall, 0, len(calls))
		for _, c := range calls {
			out = append(out, snipCall{name: c.Name, args: c.Arguments})
		}
		return out
	}
	// 回退：从 tool 消息 JSON / metadata 推断（DB 回放无 tool_calls 时）
	out := make([]snipCall, 0, end-start-1)
	for i := start + 1; i < end; i++ {
		if !strings.EqualFold(msgs[i].Role, "tool") {
			continue
		}
		name := strings.TrimSpace(toolNameFromToolMessage(msgs[i]))
		if name == "" {
			continue
		}
		out = append(out, snipCall{name: name, args: nil})
	}
	return out
}

func toolCallsFromMessage(m Message) []ToolCall {
	if m.Metadata == nil {
		return nil
	}
	raw, ok := m.Metadata["tool_calls"]
	if !ok || raw == nil {
		return nil
	}
	switch v := raw.(type) {
	case []ToolCall:
		return v
	default:
		return nil
	}
}

func toolNameFromToolMessage(m Message) string {
	if m.Metadata != nil {
		if n, _ := m.Metadata["tool_name"].(string); strings.TrimSpace(n) != "" {
			return n
		}
	}
	var payload struct {
		Tool string `json:"tool"`
	}
	if err := json.Unmarshal([]byte(m.Content), &payload); err == nil {
		return payload.Tool
	}
	return ""
}

func snipKeysForCalls(calls []snipCall) (keys []string, blocked bool) {
	if len(calls) == 0 {
		return nil, true
	}
	for _, c := range calls {
		name := strings.ToLower(strings.TrimSpace(c.name))
		if !isSnipEligibleTool(name) {
			return nil, true
		}
		key := snipDedupKey(name, c.args)
		if key == "" {
			return nil, true
		}
		keys = append(keys, name+"\x00"+key)
	}
	return keys, false
}

func isSnipEligibleTool(name string) bool {
	switch name {
	case "read_file", "read_skill_file", "search_files", "web_search", "web_extract",
		"memory_search", "memory_get", "session_search", "list_tables", "describe_table", "todo":
		return true
	default:
		return false
	}
}

func snipDedupKey(toolName string, args map[string]any) string {
	switch toolName {
	case "read_file", "read_skill_file", "memory_get":
		return normalizeSnipPath(stringArg(args, "path"))
	case "search_files":
		return normalizeSnipQuery(stringArg(args, "query")) + "\x01" + normalizeSnipPath(stringArg(args, "path"))
	case "web_search", "memory_search", "session_search":
		return normalizeSnipQuery(stringArg(args, "query"))
	case "web_extract":
		return strings.TrimSpace(stringArg(args, "url"))
	case "list_tables":
		return strings.TrimSpace(stringArg(args, "database"))
	case "describe_table":
		return strings.TrimSpace(stringArg(args, "database")) + "\x01" + strings.TrimSpace(stringArg(args, "table"))
	case "todo":
		return "todo"
	default:
		return ""
	}
}

func stringArg(args map[string]any, key string) string {
	if args == nil {
		return ""
	}
	v, ok := args[key]
	if !ok || v == nil {
		return ""
	}
	switch t := v.(type) {
	case string:
		return t
	default:
		return strings.TrimSpace(fmt.Sprint(v))
	}
}

func normalizeSnipPath(p string) string {
	p = strings.TrimSpace(p)
	p = strings.ReplaceAll(p, "\\", "/")
	for strings.HasPrefix(p, "./") {
		p = strings.TrimPrefix(p, "./")
	}
	return p
}

func normalizeSnipQuery(q string) string {
	return strings.ToLower(strings.Join(strings.Fields(strings.TrimSpace(q)), " "))
}
