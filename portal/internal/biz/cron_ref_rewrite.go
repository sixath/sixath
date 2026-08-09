package biz

import (
	"context"

	"github.com/sixath/framework/growth"
)

// CronRefRewriteUsecase R2c：将 cron_tasks 中 skill_execute 的 payload 与技能改名同步。
type CronRefRewriteUsecase struct {
	cron   CronTaskRepo
	agents AgentRepo
}

// NewCronRefRewriteUsecase constructs CronRefRewriteUsecase.
func NewCronRefRewriteUsecase(cron CronTaskRepo, agents AgentRepo) *CronRefRewriteUsecase {
	return &CronRefRewriteUsecase{cron: cron, agents: agents}
}

// RewriteForWorkspace 更新该 workspace 下所有 agent 的 skill_execute 任务引用。
func (uc *CronRefRewriteUsecase) RewriteForWorkspace(ctx context.Context, workspace string, renames map[string]string) (updated int, err error) {
	if uc == nil || uc.cron == nil || uc.agents == nil || workspace == "" || len(renames) == 0 {
		return 0, nil
	}
	agentIDs, err := uc.agents.ListAgentIDsByWorkspace(ctx, workspace)
	if err != nil {
		return 0, err
	}
	if len(agentIDs) == 0 {
		return 0, nil
	}
	tasks, err := uc.cron.ListSkillExecuteByAgentIDs(ctx, agentIDs)
	if err != nil {
		return 0, err
	}
	for _, t := range tasks {
		if t == nil {
			continue
		}
		newPayload, changed := growth.RewriteSkillExecutePayload(t.PayloadContent, renames)
		if !changed {
			continue
		}
		if _, err := uc.cron.Update(ctx, t.ID, map[string]any{"payload_content": newPayload}); err != nil {
			return updated, err
		}
		updated++
		growth.DefaultMetrics.IncCronRefsRewritten()
	}
	return updated, nil
}
