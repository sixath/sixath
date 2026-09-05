package service

import (
	"context"
	"fmt"
	"time"

	agent "github.com/sixath/framework/harness"
	"github.com/sixath/framework/growth"
	"github.com/sixath/framework/model"
	"github.com/sixath/framework/skills"
	"github.com/sixath/framework/tool"
)

// runForkReviewAgent is used by worker spawnReviewAgent (opt-in poll Loop).
// When messages is non-empty it is used as conversation history (skills summary prepended as user section).
// When messages is empty, falls back to transcript+summary single user message (legacy worker path).
// Child agent: MetaGrowthReview set — no growth success hooks.
func (w *GrowthWorker) runForkReviewAgent(ctx context.Context, job growth.ReviewJob, messages []model.Message, transcript, summary string) error {
	ctx = w.internalContext(ctx)
	workspace := job.WorkspaceRoot
	if workspace == "" {
		workspace = job.WorkspaceKey
	}

	m := w.reviewModel
	if m == nil {
		return errNoReviewModel
	}

	beforeNames, err := listWorkspaceSkillNames(workspace)
	if err != nil {
		return fmt.Errorf("growth: list skills before fork-agent review: %w", err)
	}

	reg, err := w.buildReviewRegistry(workspace)
	if err != nil {
		return err
	}

	maxSteps := 12
	if w.growthCfg != nil {
		if n := int(w.growthCfg.GetAgentReviewMaxSteps()); n > 0 {
			maxSteps = n
		}
	}
	a := agent.NewReActAgent(m, nil, reg, agent.WithReActMaxSteps(maxSteps))

	timeout := 120 * time.Second
	if w.growthCfg != nil {
		if d := w.growthCfg.GetAgentReviewTimeout().AsDuration(); d > 0 {
			timeout = d
		}
	}
	rctx := context.WithValue(ctx, tool.ContextKeyWorkspaceRoot, workspace)
	rctx, cancel := context.WithTimeout(rctx, timeout)
	defer cancel()

	if summary == "" && workspace != "" {
		summary = skillsIndexSummary(workspace)
	}

	var reqMessages []model.Message
	if len(messages) > 0 {
		reqMessages = make([]model.Message, 0, len(messages)+1)
		if summary != "" {
			reqMessages = append(reqMessages, model.Message{
				Role:    "user",
				Content: "# Skills index snapshot\n" + summary,
			})
		}
		reqMessages = append(reqMessages, messages...)
	} else {
		reqMessages = []model.Message{{
			Role:    "user",
			Content: "# Skills index snapshot\n" + summary + "\n\n# Transcript\n" + transcript,
		}}
	}

	req := &agent.Request{
		SystemPrompt: agentReviewSystemPrompt,
		Messages:     reqMessages,
		Metadata:     growth.MergeReviewMetadata(map[string]any{"session_id": job.SessionID}),
	}
	if _, err := a.Run(rctx, req); err != nil {
		return err
	}

	return w.rewriteCronAfterForkReview(ctx, workspace, beforeNames)
}

func skillsIndexSummary(workspace string) string {
	skillsDir := workspace + "/skills"
	idx, err := skills.NewIndex([]string{skillsDir}, nil, nil)
	if err != nil || idx == nil {
		return ""
	}
	return growth.FormatSkillsIndexSnapshot(idx, 64, 200)
}
