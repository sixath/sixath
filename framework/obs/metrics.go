package obs

import (
	"net/http"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"
)

var (
	requests = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name: "agent_requests_total",
			Help: "Total number of agent requests.",
		},
		[]string{"agent", "status"},
	)
	requestDuration = prometheus.NewHistogramVec(
		prometheus.HistogramOpts{
			Name:    "agent_request_duration_seconds",
			Help:    "Agent request latency distributions.",
			Buckets: prometheus.DefBuckets,
		},
		[]string{"agent"},
	)
	tokensTotal = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name: "agent_tokens_total",
			Help: "Total token usage (input or output).",
		},
		[]string{"agent", "type"}, // type: "input" | "output"
	)

	// dataqueryToolCalls 统计数据查询相关工具被调用的次数及成功/失败情况。
	dataqueryToolCalls = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name: "dataquery_tool_calls_total",
			Help: "Total number of data query tool calls.",
		},
		[]string{"tool", "status"},
	)

	// dataqueryStepDuration 统计数据查询链路中关键步骤的耗时（如 react_run）。
	dataqueryStepDuration = prometheus.NewHistogramVec(
		prometheus.HistogramOpts{
			Name:    "dataquery_step_duration_seconds",
			Help:    "Duration of data query steps.",
			Buckets: prometheus.DefBuckets,
		},
		[]string{"step"},
	)

	// datasourcePoolInUse 为各数据源连接池在用连接数（由 PoolSampler 周期上报）。
	datasourcePoolInUse = prometheus.NewGaugeVec(
		prometheus.GaugeOpts{
			Name: "datasource_pool_in_use",
			Help: "Number of in-use connections in the datasource pool.",
		},
		[]string{"id"},
	)
	// datasourcePoolIdle 为各数据源连接池空闲连接数。
	datasourcePoolIdle = prometheus.NewGaugeVec(
		prometheus.GaugeOpts{
			Name: "datasource_pool_idle",
			Help: "Number of idle connections in the datasource pool.",
		},
		[]string{"id"},
	)

	// executorDurationSeconds 为执行器单次操作耗时（按数据源、类型、操作、状态聚合）。
	executorDurationSeconds = prometheus.NewHistogramVec(
		prometheus.HistogramOpts{
			Name:    "executor_duration_seconds",
			Help:    "Executor operation latency distributions.",
			Buckets: prometheus.DefBuckets,
		},
		[]string{"datasource", "type", "op", "status"},
	)
	// executorRowsReturned 为查询返回行数分布（仅 query/search 有样本）。
	executorRowsReturned = prometheus.NewHistogramVec(
		prometheus.HistogramOpts{
			Name:    "executor_rows_returned",
			Help:    "Number of rows returned by executor queries.",
			Buckets: []float64{0, 1, 5, 10, 50, 100, 500, 1000, 5000, 10000},
		},
		[]string{"datasource", "type"},
	)
	// executorErrorsTotal 为执行器错误计数；error_kind 为有限枚举，避免高基数。
	executorErrorsTotal = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name: "executor_errors_total",
			Help: "Total executor errors by kind.",
		},
		[]string{"datasource", "type", "error_kind"},
	)

	// memory_extract_* — turn fact extraction funnel (P2-C observability).
	memoryExtractTurns = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name: "memory_extract_turns_total",
			Help: "Turn extraction attempts by result (success|parse_fail|model_fail|error|disabled|empty_input|skip_model).",
		},
		[]string{"result"},
	)
	memoryExtractCandidates = prometheus.NewCounter(
		prometheus.CounterOpts{
			Name: "memory_extract_candidates_total",
			Help: "Candidate facts returned by the extractor (before drops).",
		},
	)
	memoryExtractWritten = prometheus.NewCounter(
		prometheus.CounterOpts{
			Name: "memory_extract_written_total",
			Help: "Facts written to MemoryStore from turn extraction.",
		},
	)
	memoryExtractDrops = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name: "memory_extract_drop_total",
			Help: "Candidate facts dropped before write, by reason.",
		},
		[]string{"reason"},
	)
	memoryExtractDuration = prometheus.NewHistogram(
		prometheus.HistogramOpts{
			Name:    "memory_extract_duration_seconds",
			Help:    "Turn extraction latency.",
			Buckets: prometheus.DefBuckets,
		},
	)
	memoryExtractWrittenPerTurn = prometheus.NewHistogram(
		prometheus.HistogramOpts{
			Name:    "memory_extract_written_per_turn",
			Help:    "Facts written per turn.",
			Buckets: []float64{0, 1, 2, 3, 5, 8, 13},
		},
	)
)

var reg = prometheus.NewRegistry()

func init() {
	reg.MustRegister(
		requests, requestDuration, tokensTotal,
		dataqueryToolCalls, dataqueryStepDuration,
		datasourcePoolInUse, datasourcePoolIdle,
		executorDurationSeconds, executorRowsReturned, executorErrorsTotal,
		memoryExtractTurns, memoryExtractCandidates, memoryExtractWritten,
		memoryExtractDrops, memoryExtractDuration, memoryExtractWrittenPerTurn,
	)
}

// MetricsHandler 返回 Prometheus HTTP handler，可挂载到 /metrics。
func MetricsHandler() http.Handler {
	return promhttp.HandlerFor(reg, promhttp.HandlerOpts{})
}

// ObserveAgentRequest 记录一次 Agent 请求的指标。
// agentName 由调用方指定；status 建议为 "ok" 或 "error"。
func ObserveAgentRequest(agentName, status string, d time.Duration) {
	if agentName == "" {
		agentName = "default"
	}
	if status == "" {
		status = "ok"
	}
	requests.WithLabelValues(agentName, status).Inc()
	requestDuration.WithLabelValues(agentName).Observe(d.Seconds())
}

// ObserveTokenUsage 上报一次请求的 token 消耗（B.6.1）。agentName 为空时用 "default"。
func ObserveTokenUsage(agentName string, inputTokens, outputTokens int) {
	if agentName == "" {
		agentName = "default"
	}
	if inputTokens > 0 {
		tokensTotal.WithLabelValues(agentName, "input").Add(float64(inputTokens))
	}
	if outputTokens > 0 {
		tokensTotal.WithLabelValues(agentName, "output").Add(float64(outputTokens))
	}
}

// ObserveDataQueryTool 记录一次数据查询工具调用。
// toolName 如 "list_tables"、"describe_table"、"execute_read"、"execute_write"；status 一般为 "ok" 或 "error"。
func ObserveDataQueryTool(toolName, status string, d time.Duration) {
	if toolName == "" {
		toolName = "unknown"
	}
	if status == "" {
		status = "ok"
	}
	dataqueryToolCalls.WithLabelValues(toolName, status).Inc()
	dataqueryStepDuration.WithLabelValues("tool_" + toolName).Observe(d.Seconds())
}

// ObserveDataQueryStep 记录数据查询链路中任意自定义步骤的耗时。
// 例如 step="react_run"、"confirm_write" 等。
func ObserveDataQueryStep(step string, d time.Duration) {
	if step == "" {
		step = "unknown"
	}
	dataqueryStepDuration.WithLabelValues(step).Observe(d.Seconds())
}

// SetDatasourcePoolStats 上报连接池快照（通常由 PoolSampler 周期调用）。
func SetDatasourcePoolStats(datasourceID string, inUse, idle int) {
	if datasourceID == "" {
		datasourceID = "unknown"
	}
	datasourcePoolInUse.WithLabelValues(datasourceID).Set(float64(inUse))
	datasourcePoolIdle.WithLabelValues(datasourceID).Set(float64(idle))
}

// ObserveMemoryExtract records one turn-extraction funnel sample.
// result should be a finite enum (see memory.ExtractResult*).
// drops keys should be finite drop reasons (see memory.Drop*).
func ObserveMemoryExtract(result string, candidates, written int, drops map[string]int, d time.Duration) {
	if result == "" {
		result = "unknown"
	}
	memoryExtractTurns.WithLabelValues(result).Inc()
	if candidates > 0 {
		memoryExtractCandidates.Add(float64(candidates))
	}
	if written > 0 {
		memoryExtractWritten.Add(float64(written))
	}
	for reason, n := range drops {
		if reason == "" || n <= 0 {
			continue
		}
		memoryExtractDrops.WithLabelValues(reason).Add(float64(n))
	}
	if d > 0 {
		memoryExtractDuration.Observe(d.Seconds())
	}
	memoryExtractWrittenPerTurn.Observe(float64(written))
}

// ObserveExecutorRun 记录一次执行器操作。
// status: ok | error | rejected；errorKind 仅在 err != nil 时使用（schema/readonly/timeout/driver/unsupported/other）。
func ObserveExecutorRun(datasourceID, dsType, op, status string, d time.Duration, rows int, errKind string) {
	if datasourceID == "" {
		datasourceID = "unknown"
	}
	if dsType == "" {
		dsType = "unknown"
	}
	if op == "" {
		op = "unknown"
	}
	if status == "" {
		status = "ok"
	}
	executorDurationSeconds.WithLabelValues(datasourceID, dsType, op, status).Observe(d.Seconds())
	if rows > 0 && (op == "query" || op == "search") {
		executorRowsReturned.WithLabelValues(datasourceID, dsType).Observe(float64(rows))
	}
	if errKind != "" {
		executorErrorsTotal.WithLabelValues(datasourceID, dsType, errKind).Inc()
	}
}
