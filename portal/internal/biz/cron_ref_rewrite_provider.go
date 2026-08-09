package biz

// ProvideCronRefRewriteUsecase wires cron ref rewrite for R2c.
func ProvideCronRefRewriteUsecase(cron CronTaskRepo, agents AgentRepo) *CronRefRewriteUsecase {
	return NewCronRefRewriteUsecase(cron, agents)
}
