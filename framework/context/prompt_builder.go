package context

import (
	"crypto/sha256"
	"encoding/hex"
	"sort"
	"strings"
)

// Input 是 PromptBuilder 的纯字符串输入（不 import portal / skills）。
type Input struct {
	AgentSystem string
	SkillsIndex string
	MemoryMD    string
	UserMD      string
	ToolNames   []string
	Ephemeral   string
}

// Result 一次 Build 的稳定块、一次性块与 hash。
type Result struct {
	Stable     string
	Ephemeral  string
	StableHash string
}

// Build 按固定块顺序序列化 Stable，并计算 prompt_stable_hash。
func Build(in Input) Result {
	stable := serializeStable(in)
	return Result{
		Stable:     stable,
		Ephemeral:  in.Ephemeral,
		StableHash: stableHash(stable),
	}
}

// Encode 默认一条 system：Stable 在前；有 Ephemeral 时中间 `\n\n---\n\n`。
// 无（或空白）Ephemeral 时结果等于 Stable，不含 `---`。
func Encode(stable, ephemeral string) string {
	if strings.TrimSpace(ephemeral) == "" {
		return stable
	}
	return stable + "\n\n---\n\n" + ephemeral
}

func serializeStable(in Input) string {
	var blocks []string
	if strings.TrimSpace(in.AgentSystem) != "" {
		blocks = append(blocks, in.AgentSystem)
	}
	if s := strings.TrimSpace(in.SkillsIndex); s != "" {
		blocks = append(blocks, "## Skills\n"+in.SkillsIndex)
	}
	if s := strings.TrimSpace(in.MemoryMD); s != "" {
		blocks = append(blocks, "## MEMORY.md\n"+in.MemoryMD)
	}
	if s := strings.TrimSpace(in.UserMD); s != "" {
		blocks = append(blocks, "## USER.md\n"+in.UserMD)
	}
	if tools := formatToolNames(in.ToolNames); tools != "" {
		blocks = append(blocks, "## Tools\n"+tools)
	}
	return strings.Join(blocks, "\n\n")
}

func formatToolNames(names []string) string {
	seen := make(map[string]struct{}, len(names))
	uniq := make([]string, 0, len(names))
	for _, n := range names {
		n = strings.TrimSpace(n)
		if n == "" {
			continue
		}
		if _, ok := seen[n]; ok {
			continue
		}
		seen[n] = struct{}{}
		uniq = append(uniq, n)
	}
	if len(uniq) == 0 {
		return ""
	}
	sort.Strings(uniq)
	var b strings.Builder
	for i, n := range uniq {
		if i > 0 {
			b.WriteByte('\n')
		}
		b.WriteString("- ")
		b.WriteString(n)
	}
	return b.String()
}

func stableHash(stable string) string {
	sum := sha256.Sum256([]byte(stable))
	return hex.EncodeToString(sum[:])[:16]
}
