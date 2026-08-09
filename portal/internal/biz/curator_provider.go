package biz

// ProvideCuratorUsecase wires curator repo with growth lease usecase.
func ProvideCuratorUsecase(curatorRepo CuratorRepo, agentRepo AgentRepo, growthUC *GrowthUsecase) *CuratorUsecase {
	return NewCuratorUsecase(curatorRepo, agentRepo, growthUC)
}
