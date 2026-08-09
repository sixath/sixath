package cron

import (
	"time"

	"github.com/google/wire"
)

// ProviderSet 提供 Cron 相关依赖
var ProviderSet = wire.NewSet(
	NewExecutor,
	NewCronService,
	NewScheduler,
	NewServer,
	ProvideSchedulerInterval,
)

// ProvideSchedulerInterval 提供调度器扫描间隔
func ProvideSchedulerInterval() time.Duration {
	return 30 * time.Second
}
