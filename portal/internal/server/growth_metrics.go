package server

import (
	"encoding/json"
	"net/http"

	"github.com/sixath/framework/growth"

	kratoshttp "github.com/go-kratos/kratos/v2/transport/http"
)

// GrowthMetricsHandler 返回 growth.DefaultMetrics 的 JSON 快照（spec phase2 §E2）。
// 路径：GET /api/v1/growth/metrics。
// 字段：
//   - reviews_scheduled / completed / failed：累计复盘次数
//   - lease_contention / lease_acquire_err：租约争抢与抢取错误次数
//   - pending_dropped：A5 超阈值清 pending 次数
//   - idle_sweep_runs：空闲扫描批次数
//   - pending_depth：{workspace -> pending session 数}
func GrowthMetricsHandler() func(ctx kratoshttp.Context) error {
	return func(ctx kratoshttp.Context) error {
		snap := growth.DefaultMetrics.Snapshot()
		out := map[string]any{
			"reviews_scheduled": snap.ReviewsScheduled,
			"reviews_completed": snap.ReviewsCompleted,
			"reviews_failed":    snap.ReviewsFailed,
			"lease_contention":  snap.LeaseContention,
			"lease_acquire_err": snap.LeaseAcquireErr,
			"pending_dropped":   snap.PendingDropped,
			"idle_sweep_runs":   snap.IdleSweepRuns,
			"curator_runs":      snap.CuratorRuns,
			"curator_failed":      snap.CuratorFailed,
			"cron_refs_rewritten": snap.CronRefsRewritten,
			"pending_depth":       snap.PendingDepth,
		}
		body, err := json.Marshal(out)
		if err != nil {
			return err
		}
		w := ctx.Response()
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write(body)
		return nil
	}
}
