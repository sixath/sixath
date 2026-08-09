package datasource

import (
	"context"
	"database/sql"
	"time"

	"github.com/sixath/framework/obs"
)

// StartPoolSampler 启动后台 goroutine，周期采样带 *sql.DB 的数据源连接池并上报 Prometheus。
// interval <= 0 时默认 30s。ctx 取消后 goroutine 退出。
func StartPoolSampler(ctx context.Context, reg *Registry, interval time.Duration) {
	if reg == nil {
		return
	}
	if interval <= 0 {
		interval = 30 * time.Second
	}
	go func() {
		tick := time.NewTicker(interval)
		defer tick.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-tick.C:
				samplePools(reg)
			}
		}
	}()
}

func samplePools(reg *Registry) {
	for _, ds := range reg.List() {
		p, ok := ds.(interface{ DB() *sql.DB })
		if !ok {
			continue
		}
		s := p.DB().Stats()
		obs.SetDatasourcePoolStats(ds.ID(), int(s.InUse), int(s.Idle))
	}
}
