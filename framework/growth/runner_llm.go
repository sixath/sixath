package growth

import (
	"context"
	"encoding/json"
	"fmt"
	"regexp"
	"strings"
)

// LLMClient 是 framework/growth 对接真实 LLM 的最小接口（避免直接耦合 framework/model）。
// portal 适配器需把 framework/model.Model 的 Chat 调用包装成此接口，便于 runner 单测注入。
type LLMClient interface {
	// Complete 返回 prompt 的文本响应；调用方负责设置 ctx 超时。
	Complete(ctx context.Context, prompt string) (string, error)
}

// LLMRunnerConfig 控制 LLM 复盘 prompt 与解析行为。
type LLMRunnerConfig struct {
	// SystemPrompt 注入到 prompt 头部的指令；空值时使用 DefaultSkillReviewSystemPrompt。
	SystemPrompt string
	// MaxTranscriptRunes 限制写入 prompt 的 transcript 长度（按 rune 截断）；<=0 不截断。
	MaxTranscriptRunes int
}

// DefaultSkillReviewSystemPrompt 技能环复盘默认指令：要求 LLM 仅输出 JSON patch 数组。
const DefaultSkillReviewSystemPrompt = `你是一名技能策展员（Skill Curator）。根据下面的会话 transcript 与现有技能索引摘要，
判断是否需要新增、修改或删除 SKILL.md / 子文件。

若 prompt 中包含「Workspace learnings」章节，应优先把其中可沉淀的 correction/best_practice 写入或更新对应 SKILL.md，而不是仅复述会话。
严格输出一个 JSON 数组（不要 markdown 代码块、不要注释、不要其他文字），数组元素结构：
[
  {"path": "skills/<name>/SKILL.md", "op": "create|patch|delete", "content": "<create 时的完整文件正文>", "old": "<patch 必填：要替换的原文>", "new": "<patch 时的新文本>"}
]
op=create 必须提供 content；op=patch 必须提供非空 old；op=delete 仅需 path。
若无需任何变更，输出 [] 即可。
path 必须以 "skills/" 开头且不得越权（不得包含 ".."、绝对路径）。`

// DefaultCombinedReviewSystemPrompt 合并双 pending 默认指令：除 patch 外还要回答是否需要触发记忆刷新。
const DefaultCombinedReviewSystemPrompt = `你是一名技能 + 记忆策展员。根据 transcript 与技能摘要，输出一个 JSON 对象：
{"patches": [<同技能复盘 patch 数组>], "notify_memory": true|false}
notify_memory=true 表示让记忆子系统重新整理该会话。仅输出 JSON 对象本体，无任何其他文字。`

// NewLLMSkillProposer 用 LLMClient 实现 RunnerDeps.ProposeSkillPatches；
// llm 为 nil 时返回 nil（NewRunner 会回退到 NoopLLMRunner）。
func NewLLMSkillProposer(llm LLMClient, cfg LLMRunnerConfig) func(ctx context.Context, job ReviewJob, transcript, skillsSummary string) ([]Patch, error) {
	if llm == nil {
		return nil
	}
	return func(ctx context.Context, job ReviewJob, transcript, skillsSummary string) ([]Patch, error) {
		prompt := buildSkillReviewPrompt(cfg, job, transcript, skillsSummary)
		raw, err := llm.Complete(ctx, prompt)
		if err != nil {
			return nil, fmt.Errorf("growth llm: %w", err)
		}
		patches, err := extractPatchArray(raw)
		if err != nil {
			return nil, fmt.Errorf("growth llm parse: %w (raw=%q)", err, snippet(raw, 256))
		}
		return patches, nil
	}
}

// NewLLMCombinedProposer 用 LLMClient 实现 RunnerDeps.ProposeCombinedReview；
// llm 为 nil 时返回 nil。
func NewLLMCombinedProposer(llm LLMClient, cfg LLMRunnerConfig) func(ctx context.Context, job ReviewJob, transcript, skillsSummary string) ([]Patch, bool, error) {
	if llm == nil {
		return nil
	}
	return func(ctx context.Context, job ReviewJob, transcript, skillsSummary string) ([]Patch, bool, error) {
		prompt := buildCombinedReviewPrompt(cfg, job, transcript, skillsSummary)
		raw, err := llm.Complete(ctx, prompt)
		if err != nil {
			return nil, false, fmt.Errorf("growth llm: %w", err)
		}
		patches, notify, err := extractCombinedResult(raw)
		if err != nil {
			return nil, false, fmt.Errorf("growth llm parse combined: %w (raw=%q)", err, snippet(raw, 256))
		}
		return patches, notify, nil
	}
}

func buildSkillReviewPrompt(cfg LLMRunnerConfig, job ReviewJob, transcript, skillsSummary string) string {
	sys := cfg.SystemPrompt
	if strings.TrimSpace(sys) == "" {
		sys = DefaultSkillReviewSystemPrompt
	}
	tr := truncateRunes(transcript, cfg.MaxTranscriptRunes)
	var b strings.Builder
	b.WriteString(sys)
	b.WriteString("\n\n# Session\n")
	b.WriteString("session_id=")
	b.WriteString(job.SessionID)
	b.WriteString("\nworkspace=")
	b.WriteString(job.WorkspaceKey)
	b.WriteString("\n\n# Skills index snapshot\n")
	if skillsSummary == "" {
		b.WriteString("(empty)\n")
	} else {
		b.WriteString(skillsSummary)
	}
	b.WriteString("\n# Transcript\n")
	if tr == "" {
		b.WriteString("(empty)\n")
	} else {
		b.WriteString(tr)
		if !strings.HasSuffix(tr, "\n") {
			b.WriteByte('\n')
		}
	}
	return b.String()
}

func buildCombinedReviewPrompt(cfg LLMRunnerConfig, job ReviewJob, transcript, skillsSummary string) string {
	sys := cfg.SystemPrompt
	if strings.TrimSpace(sys) == "" {
		sys = DefaultCombinedReviewSystemPrompt
	}
	cfg.SystemPrompt = sys
	return buildSkillReviewPrompt(cfg, job, transcript, skillsSummary)
}

// extractPatchArray 容忍 LLM 输出在 JSON 前后混入 markdown 代码栅或闲聊：
// 优先尝试整体解析；失败时尝试抓取首个 `[` 到匹配的 `]`。
func extractPatchArray(raw string) ([]Patch, error) {
	s := stripCodeFence(raw)
	if patches, err := ParsePatchBatchJSON([]byte(s)); err == nil {
		return patches, nil
	}
	if arr := extractFirstJSONArray(s); arr != "" {
		return ParsePatchBatchJSON([]byte(arr))
	}
	return nil, fmt.Errorf("no JSON array found")
}

type combinedJSON struct {
	Patches      json.RawMessage `json:"patches"`
	NotifyMemory bool            `json:"notify_memory"`
}

func extractCombinedResult(raw string) ([]Patch, bool, error) {
	s := stripCodeFence(raw)
	if obj := extractFirstJSONObject(s); obj != "" {
		s = obj
	}
	var c combinedJSON
	if err := json.Unmarshal([]byte(s), &c); err != nil {
		return nil, false, err
	}
	patches, err := ParsePatchBatchJSON(c.Patches)
	if err != nil {
		return nil, false, err
	}
	return patches, c.NotifyMemory, nil
}

var codeFenceRe = regexp.MustCompile("(?s)```(?:json)?\\s*(.*?)```")

func stripCodeFence(raw string) string {
	s := strings.TrimSpace(raw)
	if m := codeFenceRe.FindStringSubmatch(s); len(m) == 2 {
		return strings.TrimSpace(m[1])
	}
	return s
}

func extractFirstJSONArray(s string) string {
	return extractBracketed(s, '[', ']')
}

func extractFirstJSONObject(s string) string {
	return extractBracketed(s, '{', '}')
}

// extractBracketed 找到 s 中首个 open..close 平衡子串（忽略字符串字面量内的括号）。
func extractBracketed(s string, open, close byte) string {
	start := -1
	depth := 0
	inStr := false
	escape := false
	for i := 0; i < len(s); i++ {
		c := s[i]
		if escape {
			escape = false
			continue
		}
		if c == '\\' {
			escape = true
			continue
		}
		if c == '"' {
			inStr = !inStr
			continue
		}
		if inStr {
			continue
		}
		if c == open {
			if start < 0 {
				start = i
			}
			depth++
		} else if c == close && depth > 0 {
			depth--
			if depth == 0 && start >= 0 {
				return s[start : i+1]
			}
		}
	}
	return ""
}

func truncateRunes(s string, max int) string {
	if max <= 0 {
		return s
	}
	r := []rune(s)
	if len(r) <= max {
		return s
	}
	return string(r[:max]) + "\n…(truncated)"
}

func snippet(s string, n int) string {
	r := []rune(s)
	if len(r) <= n {
		return s
	}
	return string(r[:n]) + "…"
}
