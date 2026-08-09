package growth

import (
	"context"
	"fmt"
	"strings"
)

// DefaultCuratorSystemPrompt R2b workspace 级合并/归档；输出格式与技能复盘 patch 相同。
const DefaultCuratorSystemPrompt = `你是 workspace 技能库的长期策展员（Curator）。
根据下面的技能索引摘要（无会话 transcript），做「伞形合并」与轻量整理：

- 将明显同主题、可合并的多个技能合并为一个 umbrella 技能（create/patch/delete 组合）。
- 可将次要内容迁入 skills/<umbrella>/references/ 或 scripts/ 子路径（通过 create/patch 表达）。
- 可将长期不用、重复的技能 delete；归档内容可放在 skills/.archive/<name>/ 下（path 仍以 skills/ 开头）。
- 不要修改 skills/.hub/、quarantine 等隔离目录。
- 若索引已足够整洁，输出空数组 []。

严格输出一个 JSON 数组（不要 markdown 代码块），元素：
[{"path":"skills/<name>/SKILL.md","op":"create|patch|delete","content":"...","old":"...","new":"..."}]
path 必须以 "skills/" 开头，不得含 ".."。`

// NewLLMCuratorProposer 实现 CuratorDeps.ProposeCuratorPatches。
func NewLLMCuratorProposer(llm LLMClient, cfg LLMRunnerConfig) func(ctx context.Context, job CuratorJob, skillsSummary string) ([]Patch, error) {
	if llm == nil {
		return nil
	}
	return func(ctx context.Context, job CuratorJob, skillsSummary string) ([]Patch, error) {
		sys := cfg.SystemPrompt
		if strings.TrimSpace(sys) == "" {
			sys = DefaultCuratorSystemPrompt
		}
		var b strings.Builder
		b.WriteString(sys)
		b.WriteString("\n\n# Workspace\nworkspace=")
		b.WriteString(job.WorkspaceKey)
		if job.AgentID != "" {
			b.WriteString("\nagent_id=")
			b.WriteString(job.AgentID)
		}
		b.WriteString("\n\n# Skills index snapshot\n")
		if skillsSummary == "" {
			b.WriteString("(empty)\n")
		} else {
			b.WriteString(skillsSummary)
		}
		raw, err := llm.Complete(ctx, b.String())
		if err != nil {
			return nil, fmt.Errorf("growth curator llm: %w", err)
		}
		patches, err := extractPatchArray(raw)
		if err != nil {
			return nil, fmt.Errorf("growth curator llm parse: %w (raw=%q)", err, snippet(raw, 256))
		}
		return patches, nil
	}
}
