package obs

import (
	"testing"
	"time"
)

func TestObserveDataQueryMetrics_NoPanic(t *testing.T) {
	// 调用数据查询相关指标函数，确保不会 panic，且能被 /metrics 暴露。
	ObserveDataQueryTool("list_tables", "ok", 10*time.Millisecond)
	ObserveDataQueryTool("execute_read", "error", 5*time.Millisecond)
	ObserveDataQueryStep("react_run", 20*time.Millisecond)
}

func TestObserveExecutorAndPool_NoPanic(t *testing.T) {
	SetDatasourcePoolStats("mysql-1", 2, 5)
	ObserveExecutorRun("mysql-1", "mysql", "query", "ok", 50*time.Millisecond, 10, "")
	ObserveExecutorRun("mysql-1", "mysql", "query", "rejected", time.Millisecond, 0, "readonly")
}

func TestObserveMemoryExtract_NoPanic(t *testing.T) {
	ObserveMemoryExtract("success", 3, 2, map[string]int{"hash_dedupe": 1}, 15*time.Millisecond)
	ObserveMemoryExtract("parse_fail", 0, 0, nil, 8*time.Millisecond)
	ObserveMemoryExtract("", 0, 0, map[string]int{"": 1, "empty": 0}, 0)
}
