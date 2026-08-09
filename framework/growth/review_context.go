package growth

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/sixath/framework/skills"
)

// fetchReviewTranscript 拉取会话 Markdown；deps.Transcript 为 nil 时返回空串。
func fetchReviewTranscript(ctx context.Context, job ReviewJob, deps RunnerDeps) (string, error) {
	if deps.Transcript == nil {
		return "", nil
	}
	tr, err := deps.Transcript(ctx, job.SessionID)
	if err != nil {
		return "", fmt.Errorf("growth: transcript: %w", err)
	}
	return tr, nil
}

// buildReviewIndex 扫描 workspace/skills 构建技能索引；无 skills 目录时返回空索引。
func buildReviewIndex(workspace string) (*skills.Index, error) {
	skillsDir := filepath.Join(workspace, "skills")
	if st, err := os.Stat(skillsDir); err == nil && st.IsDir() {
		idx, err := skills.NewIndex([]string{skillsDir}, nil, nil)
		if err != nil {
			return nil, fmt.Errorf("growth: skills index: %w", err)
		}
		return idx, nil
	}
	return skills.NewIndex(nil, nil, nil)
}

// fetchReviewMemoryState 容错拉取记忆状态摘要；nil/error 返回空串。
func fetchReviewMemoryState(ctx context.Context, job ReviewJob, deps RunnerDeps) string {
	if deps.MemoryState == nil {
		return ""
	}
	s, err := deps.MemoryState(ctx, job.SessionID)
	if err != nil {
		return ""
	}
	return s
}

// buildReviewSummary 组装技能索引快照 + 记忆状态 + workspace learnings。
func buildReviewSummary(ctx context.Context, job ReviewJob, idx *skills.Index, deps RunnerDeps) string {
	summary := FormatSkillsIndexSnapshot(idx, 64, 200)
	if mem := fetchReviewMemoryState(ctx, job, deps); mem != "" {
		summary = summary + "\n# Memory state\n" + mem + "\n"
	}
	if strings.TrimSpace(job.LearningsSummary) != "" {
		summary = summary + "\n# Workspace learnings (.learnings/)\n" + job.LearningsSummary + "\n"
	}
	return summary
}
