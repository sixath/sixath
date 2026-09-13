// Package metrics 暴露入站网关的 Prometheus 指标。
//
// 设计取舍：gateway 刻意保持轻依赖（go.mod 仅 yaml + websocket）。因此这里**不**引入
// prometheus/client_golang（它连带 15+ 传递依赖，且本仓库离线环境无法可靠 go get），
// 而是直接用 sync/atomic + 手写 Prometheus text 格式导出 /metrics。指标面很小
// （几十个序列），自研成本远低于依赖成本。
//
// 标签基数控制：channel 来自配置文件（有界）、type/op/status/outcome 均为有限枚举。
// 切勿把 peer_id / session_id / msgid 这类无界值做成标签。
package metrics

import (
	"fmt"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
)

// StatusClass 把状态码归类为有界枚举，便于告警，避免用原始码做维度。
func StatusClass(code int) string {
	switch {
	case code >= 200 && code < 400:
		return "ok"
	case code >= 400 && code < 500:
		return "client_error"
	case code >= 500:
		return "server_error"
	default:
		return "unknown"
	}
}

// Handler 返回 Prometheus 抓取端点，挂到 GET /metrics。
func Handler() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/plain; version=0.0.4; charset=utf-8")
		_, _ = w.Write([]byte(Render()))
	})
}

// Render 输出全部指标的 Prometheus text 格式（供 /metrics 与测试使用）。
func Render() string {
	regMu.Lock()
	families := make([]*family, 0, len(registry))
	for _, f := range registry {
		families = append(families, f)
	}
	regMu.Unlock()
	sort.Slice(families, func(i, j int) bool { return families[i].name < families[j].name })

	var b strings.Builder
	for _, f := range families {
		b.WriteString("# HELP " + f.name + " " + f.help + "\n")
		b.WriteString("# TYPE " + f.name + " " + f.typ + "\n")
		f.render(&b)
	}
	return b.String()
}

// ---------------------------------------------------------------------------
// 内部实现：三种极简 vec（counter / gauge / histogram）
// ---------------------------------------------------------------------------

type family struct {
	name    string
	help    string
	typ     string // counter | gauge | histogram
	labels  []string
	buckets []float64 // histogram 专属，升序上界（不含 +Inf）

	mu     sync.Mutex
	values map[string]float64    // counter / gauge
	series map[string]*histogram // histogram
}

type histogram struct {
	count   uint64
	sum     float64
	buckets []uint64 // 与 family.buckets 等长
}

func (f *family) key(labelValues []string) string {
	if len(labelValues) == 0 {
		return ""
	}
	return strings.Join(labelValues, "\x00")
}

func (f *family) add(labelValues []string, delta float64) {
	f.mu.Lock()
	defer f.mu.Unlock()
	k := f.key(labelValues)
	if f.values == nil {
		f.values = make(map[string]float64)
	}
	f.values[k] += delta
}

func (f *family) set(labelValues []string, v float64) {
	f.mu.Lock()
	defer f.mu.Unlock()
	k := f.key(labelValues)
	if f.values == nil {
		f.values = make(map[string]float64)
	}
	f.values[k] = v
}

func (f *family) observe(labelValues []string, v float64) {
	f.mu.Lock()
	defer f.mu.Unlock()
	k := f.key(labelValues)
	if f.series == nil {
		f.series = make(map[string]*histogram)
	}
	h := f.series[k]
	if h == nil {
		h = &histogram{buckets: make([]uint64, len(f.buckets))}
		f.series[k] = h
	}
	h.count++
	h.sum += v
	for i, upper := range f.buckets {
		if v <= upper {
			h.buckets[i]++
		}
	}
}

func (f *family) render(b *strings.Builder) {
	// 快照后再渲染，避免与写入者长时间竞争。
	f.mu.Lock()
	defer f.mu.Unlock()

	switch f.typ {
	case "histogram":
		keys := make([]string, 0, len(f.series))
		for k := range f.series {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		for _, k := range keys {
			h := f.series[k]
			labels := splitKey(k)
			for i, upper := range f.buckets {
				fmt.Fprintf(b, "%s_bucket{%s,le=\"%s\"} %d\n",
					f.name, labelPairs(f.labels, labels), formatFloat(upper), h.buckets[i])
			}
			fmt.Fprintf(b, "%s_bucket{%s,le=\"+Inf\"} %d\n",
				f.name, labelPairs(f.labels, labels), h.count)
			fmt.Fprintf(b, "%s_sum{%s} %s\n", f.name, labelPairs(f.labels, labels), formatFloat(h.sum))
			fmt.Fprintf(b, "%s_count{%s} %d\n", f.name, labelPairs(f.labels, labels), h.count)
		}
	default:
		keys := make([]string, 0, len(f.values))
		for k := range f.values {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		for _, k := range keys {
			labels := splitKey(k)
			fmt.Fprintf(b, "%s{%s} %s\n", f.name, labelPairs(f.labels, labels), formatFloat(f.values[k]))
		}
	}
}

func splitKey(k string) []string {
	if k == "" {
		return nil
	}
	return strings.Split(k, "\x00")
}

// labelPairs 拼接 name="value" 形式的标签，缺值用 "unknown" 兜底。
func labelPairs(names, values []string) string {
	var parts []string
	for i, n := range names {
		v := "unknown"
		if i < len(values) && values[i] != "" {
			v = values[i]
		}
		parts = append(parts, n+"="+strconv.Quote(v))
	}
	return strings.Join(parts, ",")
}

func formatFloat(v float64) string {
	// 最短表示（能精确还原的最小位数）：整数不带小数，0.5 输出 0.5 而非 0.500000。
	return strconv.FormatFloat(v, 'f', -1, 64)
}

// ---------------------------------------------------------------------------
// 指标定义与 Observe* 辅助函数
// ---------------------------------------------------------------------------

var (
	httpRequests = &family{
		name: "gateway_http_requests_total",
		help: "Total inbound HTTP requests by method and status class.",
		typ:  "counter", labels: []string{"method", "status"},
	}
	httpDuration = &family{
		name: "gateway_http_request_duration_seconds",
		help: "Inbound HTTP request latency by method.",
		typ:  "histogram", labels: []string{"method"},
		buckets: durationBuckets,
	}

	inboundRequests = &family{
		name: "gateway_inbound_events_total",
		help: "Inbound channel events by channel, type, status class and HTTP code.",
		typ:  "counter", labels: []string{"channel", "type", "status", "code"},
	}
	inboundDuration = &family{
		name: "gateway_inbound_duration_seconds",
		help: "End-to-end latency of an inbound event (accept + turn).",
		typ:  "histogram", labels: []string{"channel", "type"},
		buckets: durationBuckets,
	}
	inboundInFlight = &family{
		name: "gateway_inbound_in_flight",
		help: "Inbound events currently being processed.",
		typ:  "gauge", labels: []string{"channel", "type"},
	}

	idempotencyEvents = &family{
		name: "gateway_idempotency_events_total",
		help: "Idempotency outcomes (new|duplicate|race) by channel.",
		typ:  "counter", labels: []string{"channel", "outcome"},
	}

	turnsTotal = &family{
		name: "gateway_turns_total",
		help: "Turns triggered by inbound events, by channel and final status.",
		typ:  "counter", labels: []string{"channel", "status"},
	}
	turnDuration = &family{
		name: "gateway_turn_duration_seconds",
		help: "Portal turn latency as seen by the gateway.",
		typ:  "histogram", labels: []string{"channel"},
		buckets: []float64{0.5, 1, 2, 5, 10, 20, 30, 60, 120, 300},
	}

	portalRequests = &family{
		name: "gateway_portal_requests_total",
		help: "Portal runtime calls by operation and result (ok|error|http_<code>).",
		typ:  "counter", labels: []string{"op", "status"},
	}
	portalDuration = &family{
		name: "gateway_portal_request_duration_seconds",
		help: "Portal runtime call latency by operation.",
		typ:  "histogram", labels: []string{"op"},
		buckets: durationBuckets,
	}

	replyDeliveries = &family{
		name: "gateway_reply_deliveries_total",
		help: "Outbound reply attempts by channel, kind (reply_url|wecom_card) and status.",
		typ:  "counter", labels: []string{"channel", "kind", "status"},
	}

	wecomConnections = &family{
		name: "gateway_wecom_active_connections",
		help: "Active WeCom bot websocket connections.",
		typ:  "gauge", labels: []string{"channel"},
	}
	wecomReconnects = &family{
		name: "gateway_wecom_reconnects_total",
		help: "WeCom bot reconnect attempts.",
		typ:  "counter", labels: []string{"channel"},
	}
)

var durationBuckets = []float64{0.005, 0.01, 0.025, 0.05, 0.1, 0.25, 0.5, 1, 2.5, 5, 10, 30}

var (
	registry = map[string]*family{}
	regMu    sync.Mutex
)

func init() {
	for _, f := range []*family{
		httpRequests, httpDuration,
		inboundRequests, inboundDuration, inboundInFlight,
		idempotencyEvents,
		turnsTotal, turnDuration,
		portalRequests, portalDuration,
		replyDeliveries,
		wecomConnections, wecomReconnects,
	} {
		registry[f.name] = f
	}
}

// ObserveHTTP 记录一次入口 HTTP 请求（由 observability 中间件调用）。
func ObserveHTTP(method string, code int, d time.Duration) {
	if method == "" {
		method = "unknown"
	}
	httpRequests.add([]string{method, StatusClass(code)}, 1)
	httpDuration.observe([]string{method}, d.Seconds())
}

// StartInbound 标记一次入站事件开始处理（in-flight +1），返回起始时间。
func StartInbound(channelID, channelType string) time.Time {
	channelID, channelType = normalizeChannel(channelID, channelType)
	inboundInFlight.add([]string{channelID, channelType}, 1)
	return time.Now()
}

// FinishInbound 记录入站事件终态：in-flight -1，并累计状态码与耗时。
func FinishInbound(channelID, channelType string, code int, start time.Time) {
	channelID, channelType = normalizeChannel(channelID, channelType)
	inboundInFlight.add([]string{channelID, channelType}, -1)
	inboundRequests.add([]string{channelID, channelType, StatusClass(code), strconv.Itoa(code)}, 1)
	if !start.IsZero() {
		inboundDuration.observe([]string{channelID, channelType}, time.Since(start).Seconds())
	}
}

// ObserveIdempotency 记录一次幂等判定结果：new | duplicate | race。
func ObserveIdempotency(channelID, outcome string) {
	channelID, _ = normalizeChannel(channelID, "")
	idempotencyEvents.add([]string{channelID, outcome}, 1)
}

// ObserveTurn 记录一次由入站事件触发的 turn 及其终态（ok|failed）。
func ObserveTurn(channelID, status string, d time.Duration) {
	channelID, _ = normalizeChannel(channelID, "")
	if status == "" {
		status = "ok"
	}
	turnsTotal.add([]string{channelID, status}, 1)
	if d > 0 {
		turnDuration.observe([]string{channelID}, d.Seconds())
	}
}

// ObservePortalRequest 记录一次 Portal runtime 调用。op 为有限枚举
// （resolve|sessions|messages|search|rewind|turns 等）。
func ObservePortalRequest(op string, code int, err error, d time.Duration) {
	if op == "" {
		op = "unknown"
	}
	status := "ok"
	switch {
	case code >= 400:
		status = "http_" + strconv.Itoa(code)
	case err != nil:
		status = "error"
	}
	portalRequests.add([]string{op, status}, 1)
	portalDuration.observe([]string{op}, d.Seconds())
}

// ObserveReply 记录一次出站投递（reply_url / wecom_card）。
func ObserveReply(channelID, kind string, err error) {
	channelID, _ = normalizeChannel(channelID, "")
	status := "ok"
	if err != nil {
		status = "error"
	}
	replyDeliveries.add([]string{channelID, kind, status}, 1)
}

// SetWecomConnections 设置某渠道当前活跃长连接数（连接建立/断开时调用）。
func SetWecomConnections(channelID string, n int) {
	channelID, _ = normalizeChannel(channelID, "")
	wecomConnections.set([]string{channelID}, float64(n))
}

// IncWecomReconnect 记录一次企微长连接重连尝试。
func IncWecomReconnect(channelID string) {
	channelID, _ = normalizeChannel(channelID, "")
	wecomReconnects.add([]string{channelID}, 1)
}

func normalizeChannel(channelID, channelType string) (string, string) {
	if channelID == "" {
		channelID = "unknown"
	}
	if channelType == "" {
		channelType = "unknown"
	}
	return channelID, channelType
}
