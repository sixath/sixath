package main

import (
	"encoding/json"
	"regexp"
	"strconv"
	"strings"
	"unicode/utf8"

	agent "github.com/sixath/framework/harness"
	"github.com/sixath/framework/redact"
)

const (
	traceArgsMaxRunes  = 500
	traceErrorMaxRunes = 500
)

// TraceCall 一次工具调用的摘要。Hits 为 -1 表示无法从结果判断命中数。
// AggEmpty 表示请求了聚合但聚合桶全空（如对未映射字段聚合：total>0 而 buckets 为空），此时 Empty 也为 true。
type TraceCall struct {
	Tool     string `json:"tool"`
	Args     string `json:"args,omitempty"`
	Error    string `json:"error,omitempty"`
	Hits     int    `json:"hits"`
	Empty    bool   `json:"empty,omitempty"`
	AggEmpty bool   `json:"agg_empty,omitempty"`
}

// TraceSummary live 与 portal 两种运行方式共用的轨迹摘要；judge 与辅助指标只依赖它。
type TraceSummary struct {
	Calls []TraceCall `json:"calls"`
}

func (s TraceSummary) ErrorCount() int {
	n := 0
	for _, c := range s.Calls {
		if c.Error != "" {
			n++
		}
	}
	return n
}

func (s TraceSummary) EmptyCount() int {
	n := 0
	for _, c := range s.Calls {
		if c.Empty {
			n++
		}
	}
	return n
}

func summarizeRunTrace(tr *agent.RunTrace) TraceSummary {
	var s TraceSummary
	if tr == nil {
		return s
	}
	for _, c := range tr.ToolCalls {
		var args any
		// 空 map 装进 any 后非 nil，会被序列化成 "null"。
		if len(c.Arguments) > 0 {
			args = c.Arguments
		}
		s.Calls = append(s.Calls, newTraceCall(c.ToolName, args, c.Result, c.Error))
	}
	return s
}

// summarizeTimeline 从 Portal 消息 metadata.timeline（camelCase 键）构造摘要，只取 kind=tool 的节点。
func summarizeTimeline(timeline []any) TraceSummary {
	var s TraceSummary
	for _, n := range timeline {
		m, ok := n.(map[string]any)
		if !ok || m["kind"] != "tool" {
			continue
		}
		name, _ := m["toolName"].(string)
		errStr, _ := m["error"].(string)
		phase, _ := m["phase"].(string)
		if errStr == "" && (phase == "failed" || phase == "interrupted") {
			errStr = phase
		}
		s.Calls = append(s.Calls, newTraceCall(name, m["arguments"], m["result"], errStr))
	}
	return s
}

func newTraceCall(name string, args, result any, errStr string) TraceCall {
	c := TraceCall{Tool: name, Error: truncateRunes(errStr, traceErrorMaxRunes), Hits: -1}
	normArgs := jsonNormalize(args)
	if args != nil {
		if raw, err := json.Marshal(redact.Value(normArgs)); err == nil {
			c.Args = truncateRunes(string(raw), traceArgsMaxRunes)
		}
	}
	if errStr == "" {
		hits, resultErr := classifyResult(result)
		if resultErr != "" {
			c.Error = truncateRunes(resultErr, traceErrorMaxRunes)
		} else {
			c.Hits = hits
			c.Empty = hits == 0
			if !c.Empty && wantsAggregation(normArgs) && aggregationsEmpty(result) {
				c.AggEmpty, c.Empty = true, true
			}
		}
	}
	return c
}

// wantsAggregation 参数里是否请求了聚合：es_log_query 的 agg_field/agg_interval，或 JSON search body 顶层的 aggs/aggregations。
func wantsAggregation(args any) bool {
	m, ok := args.(map[string]any)
	if !ok {
		return false
	}
	for _, k := range []string{"agg_field", "agg_interval"} {
		if s, _ := m[k].(string); strings.TrimSpace(s) != "" {
			return true
		}
	}
	for _, v := range m {
		body, ok := v.(map[string]any)
		if s, isStr := v.(string); isStr {
			s = strings.TrimSpace(s)
			if !strings.HasPrefix(s, "{") || json.Unmarshal([]byte(s), &body) != nil {
				continue
			}
			ok = true
		}
		if !ok {
			continue
		}
		if _, has := body["aggs"]; has {
			return true
		}
		if _, has := body["aggregations"]; has {
			return true
		}
	}
	return false
}

// aggregationsEmpty 结果中所有带 buckets 的聚合都为空时返回 true；缺少 aggregations 或无法解析时返回 false。
func aggregationsEmpty(result any) bool {
	var aggs any
	if s, ok := result.(string); ok {
		s = strings.TrimSpace(s)
		if strings.HasSuffix(s, portalTruncatedSuffix) {
			aggs, ok = truncatedAggregations(s)
			if !ok {
				return false
			}
		} else {
			var v map[string]any
			if json.Unmarshal([]byte(s), &v) != nil {
				return false
			}
			aggs = v["aggregations"]
		}
	} else {
		m, ok := jsonNormalize(result).(map[string]any)
		if !ok {
			return false
		}
		aggs = m["aggregations"]
	}
	am, ok := aggs.(map[string]any)
	if !ok {
		return false
	}
	sawBuckets := false
	for _, a := range am {
		bm, _ := a.(map[string]any)
		if b, ok := bm["buckets"].([]any); ok {
			sawBuckets = true
			if len(b) > 0 {
				return false
			}
		}
	}
	return sawBuckets
}

// truncatedAggregations 从 portal 截断的结果前缀中解码 aggregations 值；es_log_query 的 payload 键按字典序序列化，aggregations 靠前，通常完整保留。
func truncatedAggregations(s string) (any, bool) {
	const key = `"aggregations":`
	i := strings.Index(s, key)
	if i < 0 {
		return nil, false
	}
	var v any
	if json.NewDecoder(strings.NewReader(s[i+len(key):])).Decode(&v) != nil {
		return nil, false
	}
	return v, true
}

// portalTruncatedSuffix 与 portal truncateField 的截断后缀一致：超长结果以 "<JSON 前缀>…[truncated]" 字符串到达。
const portalTruncatedSuffix = "…[truncated]"

var (
	reTruncHitStatus = regexp.MustCompile(`"hit_status"\s*:\s*"(\w+)"`)
	reTruncOKFalse   = regexp.MustCompile(`"ok"\s*:\s*false`)
	reTruncError     = regexp.MustCompile(`"error"\s*:\s*"((?:[^"\\]|\\.)*)"`)
	reTruncCount     = regexp.MustCompile(`"(?:total|row_count|count)"\s*:\s*(\d+)`)
)

// resultHits 从工具结果中提取命中数；无法判断（或结果本身是错误）时返回 -1。
func resultHits(v any) int {
	hits, errStr := classifyResult(v)
	if errStr != "" {
		return -1
	}
	return hits
}

// classifyResult 返回命中数（-1 未知）以及结果形态的错误（ok=false / hit_status=error，Go error 为 nil 的 RCA 工具）。
func classifyResult(v any) (int, string) {
	if s, ok := v.(string); ok {
		s = strings.TrimSpace(s)
		if s == "" {
			return 0, ""
		}
		var parsed any
		if json.Unmarshal([]byte(s), &parsed) == nil {
			return classifyValue(parsed)
		}
		if strings.HasSuffix(s, portalTruncatedSuffix) {
			return classifyTruncated(strings.TrimSuffix(s, portalTruncatedSuffix))
		}
		return -1, ""
	}
	return classifyValue(jsonNormalize(v))
}

func classifyValue(v any) (int, string) {
	switch t := v.(type) {
	case []any:
		return len(t), ""
	case map[string]any:
		hs, _ := t["hit_status"].(string)
		if t["ok"] == false || hs == "error" {
			if msg, _ := t["error"].(string); msg != "" {
				return -1, msg
			}
			if hs == "error" {
				return -1, "hit_status=error"
			}
			return -1, "ok=false"
		}
		if hs == "empty" || hs == "suspect" {
			return 0, ""
		}
		for _, k := range []string{"total", "row_count", "count"} {
			if n, ok := asInt(t[k]); ok {
				return n, ""
			}
		}
		for _, k := range []string{"rows", "hits", "items", "results"} {
			if arr, ok := lookupFold(t, k).([]any); ok {
				return len(arr), ""
			}
		}
		return -1, ""
	}
	return -1, ""
}

// classifyTruncated 从被截断的 JSON 前缀中尽力提取 hit_status / 计数 / 错误。
func classifyTruncated(prefix string) (int, string) {
	hs := ""
	if m := reTruncHitStatus.FindStringSubmatch(prefix); m != nil {
		hs = m[1]
	}
	// hit_status 已确定时不再扫 "ok":false，否则 hits 里的日志文档会误判为错误。
	if hs == "error" || (hs == "" && reTruncOKFalse.MatchString(prefix)) {
		if m := reTruncError.FindStringSubmatch(prefix); m != nil {
			if msg, err := strconv.Unquote(`"` + m[1] + `"`); err == nil && msg != "" {
				return -1, msg
			}
		}
		if hs == "error" {
			return -1, "hit_status=error"
		}
		return -1, "ok=false"
	}
	if hs == "empty" || hs == "suspect" {
		return 0, ""
	}
	if m := reTruncCount.FindStringSubmatch(prefix); m != nil {
		if n, err := strconv.Atoi(m[1]); err == nil {
			return n, ""
		}
	}
	return -1, ""
}

// jsonNormalize 把 live 模式的强类型值（结构体、[]map[string]any 等）经 JSON 往返转成 map[string]any / []any。
func jsonNormalize(v any) any {
	if v == nil {
		return nil
	}
	raw, err := json.Marshal(v)
	if err != nil {
		return v
	}
	var out any
	if json.Unmarshal(raw, &out) != nil {
		return v
	}
	return out
}

// lookupFold 先精确匹配键，再忽略大小写匹配（executor.QueryResult 的 Rows 没有 json tag）。
func lookupFold(m map[string]any, key string) any {
	if v, ok := m[key]; ok {
		return v
	}
	for k, v := range m {
		if strings.EqualFold(k, key) {
			return v
		}
	}
	return nil
}

func asInt(v any) (int, bool) {
	switch n := v.(type) {
	case float64:
		return int(n), true
	case int:
		return n, true
	case int64:
		return int(n), true
	case json.Number:
		i, err := n.Int64()
		return int(i), err == nil
	}
	return 0, false
}

func truncateRunes(s string, max int) string {
	if utf8.RuneCountInString(s) <= max {
		return s
	}
	r := []rune(s)
	return string(r[:max]) + "…"
}
