package model

import (
	"fmt"
	"strings"
	"unicode/utf8"
)

// DefaultMaxContextRunes 默认上下文预算（Unicode 码点近似），用于 ReAct 等多轮场景；
// 实际 token 与语言相关，调用方可按需调整。
const DefaultMaxContextRunes = 200_000

// plainTextForBudget 估算单条消息占用的「文本量」（与 DashScope 侧 plain 文本规则一致）。
func plainTextForBudget(m Message) string {
	if len(strings.TrimSpace(m.Content)) > 0 {
		return m.Content
	}
	var b strings.Builder
	for _, p := range m.Parts {
		if p.Type != ContentTypeText {
			continue
		}
		t := strings.TrimSpace(p.Text)
		if t == "" {
			continue
		}
		if b.Len() > 0 {
			b.WriteByte('\n')
		}
		b.WriteString(t)
	}
	return b.String()
}

func totalMessageRunes(msgs []Message) int {
	n := 0
	for i := range msgs {
		n += utf8.RuneCountInString(plainTextForBudget(msgs[i]))
	}
	return n
}

func leadingSystemCount(msgs []Message) int {
	i := 0
	for i < len(msgs) && strings.EqualFold(msgs[i].Role, "system") {
		i++
	}
	return i
}

func hasToolCallsMeta(m Message) bool {
	if m.Metadata == nil {
		return false
	}
	tc, ok := m.Metadata["tool_calls"]
	if !ok || tc == nil {
		return false
	}
	switch v := tc.(type) {
	case []ToolCall:
		return len(v) > 0
	case []any:
		return len(v) > 0
	default:
		return true
	}
}

// toolChainEnd 返回从 msgs[i] 开始的「assistant(tool_calls) + 连续 tool」子链之后的首个下标；若 i 不是带 tool_calls 的 assistant，返回 i。
func toolChainEnd(msgs []Message, i int) int {
	if i >= len(msgs) || !strings.EqualFold(msgs[i].Role, "assistant") || !hasToolCallsMeta(msgs[i]) {
		return i
	}
	j := i + 1
	for j < len(msgs) && strings.EqualFold(msgs[j].Role, "tool") {
		j++
	}
	return j
}

// userBlocks 将 msgs[head:] 按「user 分段」切成若干块，每块为下标切片（保序）。
func userBlocks(msgs []Message, head int) [][]int {
	var blocks [][]int
	n := len(msgs)
	j := head
	for j < n {
		if strings.EqualFold(msgs[j].Role, "user") {
			start := j
			j++
			for j < n && !strings.EqualFold(msgs[j].Role, "user") {
				j++
			}
			idx := make([]int, j-start)
			for k := start; k < j; k++ {
				idx[k-start] = k
			}
			blocks = append(blocks, idx)
			continue
		}
		start := j
		for j < n && !strings.EqualFold(msgs[j].Role, "user") {
			j++
		}
		if start < j {
			idx := make([]int, j-start)
			for k := start; k < j; k++ {
				idx[k-start] = k
			}
			blocks = append(blocks, idx)
		}
	}
	return blocks
}

func assembleFromBlocks(msgs []Message, head int, blocks [][]int, fromBlock int) []Message {
	out := make([]Message, 0, len(msgs))
	out = append(out, msgs[:head]...)
	for b := fromBlock; b < len(blocks); b++ {
		for _, idx := range blocks[b] {
			out = append(out, msgs[idx])
		}
	}
	return out
}

func dropLeadingToolRoundsWhileOverBudget(msgs []Message, maxRunes int) []Message {
	out := append([]Message(nil), msgs...)
	head := leadingSystemCount(out)
	if len(out) <= head+1 {
		return out
	}
	i := head + 1
	for totalMessageRunes(out) > maxRunes && i < len(out) {
		if strings.EqualFold(out[i].Role, "assistant") && hasToolCallsMeta(out[i]) {
			end := toolChainEnd(out, i)
			if end <= i {
				break
			}
			out = append(out[:i], out[end:]...)
			continue
		}
		break
	}
	return out
}

func compressMessagesByRunesBudgetInner(msgs []Message, maxRunes int) []Message {
	head := leadingSystemCount(msgs)
	blocks := userBlocks(msgs, head)
	if len(blocks) == 0 {
		return dropLeadingToolRoundsWhileOverBudget(msgs, maxRunes)
	}
	from := 0
	for from < len(blocks)-1 && totalMessageRunes(assembleFromBlocks(msgs, head, blocks, from)) > maxRunes {
		from++
	}
	out := assembleFromBlocks(msgs, head, blocks, from)
	return dropLeadingToolRoundsWhileOverBudget(out, maxRunes)
}

// CompressMessagesByRunesBudget 在总字符量（Unicode 码点近似）超过 maxRunes 时压缩消息列表：
// 1) 保留全部前缀 system；2) 按 user 分段丢弃最旧的若干「用户轮」；3) 若仍超限，在同一轮内从左侧丢弃完整的 assistant(tool)+tool 链。
// maxRunes<=0 时不修改。若确实删去了消息，会插入一条简短 user 说明（中文）。
func CompressMessagesByRunesBudget(msgs []Message, maxRunes int) []Message {
	if maxRunes <= 0 || len(msgs) == 0 {
		return msgs
	}
	beforeRunes := totalMessageRunes(msgs)
	if beforeRunes <= maxRunes {
		return msgs
	}
	beforeLen := len(msgs)
	out := stripLeadingOrphanToolsAfterSystem(compressMessagesByRunesBudgetInner(msgs, maxRunes))
	if len(out) >= beforeLen {
		return out
	}
	dropped := beforeLen - len(out)
	if dropped < 1 {
		dropped = 1
	}
	h := leadingSystemCount(out)
	return stripLeadingOrphanToolsAfterSystem(insertCompressionNotice(out, h, dropped))
}

func compressionNoticeUserMessage(m Message) bool {
	if m.Metadata != nil {
		if v, ok := m.Metadata[MetadataKeySixathOrigin]; ok {
			if s, ok := v.(string); ok && s == OriginCompressionNotice {
				return true
			}
		}
	}
	return strings.Contains(m.Content, "上下文已压缩")
}

func insertCompressionNotice(msgs []Message, head int, droppedCount int) []Message {
	text := fmt.Sprintf("[上下文已压缩：已省略较早的 %d 条消息；以下为保留的最近对话。]", droppedCount)
	note := Message{
		Role:    "user",
		Content: text,
		Metadata: map[string]any{
			MetadataKeySixathOrigin: OriginCompressionNotice,
		},
	}
	out := make([]Message, 0, len(msgs)+1)
	out = append(out, msgs[:head]...)
	out = append(out, note)
	out = append(out, msgs[head:]...)
	return out
}

// stripLeadingOrphanToolsAfterSystem 去掉「全部前缀 system」之后、在合法 user/assistant 之前出现的孤立 tool，
// 以及「压缩说明 user」后紧跟的 tool（否则 OpenAI 兼容网关常返回 invalid request）。
func stripLeadingOrphanToolsAfterSystem(msgs []Message) []Message {
	h := leadingSystemCount(msgs)
	if h >= len(msgs) {
		return msgs
	}
	i := h
	for i < len(msgs) {
		role := strings.ToLower(msgs[i].Role)
		if role == "tool" {
			i++
			continue
		}
		if role == "user" && compressionNoticeUserMessage(msgs[i]) {
			if i+1 < len(msgs) && strings.EqualFold(msgs[i+1].Role, "tool") {
				i++
				continue
			}
		}
		break
	}
	if i == h {
		return msgs
	}
	out := make([]Message, 0, len(msgs)-(i-h))
	out = append(out, msgs[:h]...)
	out = append(out, msgs[i:]...)
	return out
}

const l2ToolPrePruneSuffix = "\n...[tool output truncated for L2 pre-prune]"

// pruneToolMessageBodies 仅截断 tool 角色 content 的码点长度（设计 §5.3 第 2 步）；不拆 assistant(tool_calls)+tool 原子单元。
func pruneToolMessageBodies(msgs []Message, maxRunesEach int) []Message {
	if maxRunesEach <= 0 || len(msgs) == 0 {
		return msgs
	}
	out := make([]Message, len(msgs))
	copy(out, msgs)
	for i := range out {
		if !strings.EqualFold(out[i].Role, "tool") {
			continue
		}
		c := out[i].Content
		if utf8.RuneCountInString(c) <= maxRunesEach {
			continue
		}
		cp := out[i]
		cp.Content = pruneToolBodyPreservingPinnedJSON(c, maxRunesEach)
		out[i] = cp
	}
	return out
}
