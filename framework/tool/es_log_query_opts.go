package tool

import (
	"encoding/json"
	"fmt"
	"regexp"
	"strings"
)

// isESQueryParseError 识别 query_string 语法错误（如裸 token 里的 [ ] / 等）。
func isESQueryParseError(err error) bool {
	if err == nil {
		return false
	}
	s := strings.ToLower(err.Error())
	for _, k := range []string{"parse_exception", "failed to parse query", "token_mgr_error", "lexical error", "cannot parse"} {
		if strings.Contains(s, k) {
			return true
		}
	}
	return false
}

var (
	luceneRangeRe      = regexp.MustCompile(`[A-Za-z_@][\w.@]*\s*:\s*[\[{][^\]}]*[\]}]`)
	luceneFieldNameRe  = regexp.MustCompile(`^[A-Za-z_@][\w.@]*$`)
	luceneRangeFieldRe = regexp.MustCompile(`(?i)([A-Za-z_@][\w.@]*)\s*:\s*[\[{]\s*\S+\s+TO\s+\S+?\s*[\]}]`)
)

const luceneSpecialChars = "[]{}/\\~^!"

// quoteLuceneSpecialTokens 把含 Lucene 特殊字符的裸 token 改写成短语（field:value 只包 value），
// 保留 field:[a TO b] 范围、已加引号的短语和括号分组。
func quoteLuceneSpecialTokens(q string) (string, bool) {
	ranges := luceneRangeRe.FindAllStringIndex(q, -1)
	inRange := func(i int) bool {
		for _, r := range ranges {
			if i >= r[0] && i < r[1] {
				return true
			}
		}
		return false
	}
	var b strings.Builder
	changed := false
	i := 0
	for i < len(q) {
		c := q[i]
		if c == ' ' || c == '\t' || c == '\n' || inRange(i) {
			b.WriteByte(c)
			i++
			continue
		}
		if c == '"' {
			j := i + 1
			for j < len(q) && !(q[j] == '"' && q[j-1] != '\\') {
				j++
			}
			if j < len(q) {
				j++
			}
			b.WriteString(q[i:j])
			i = j
			continue
		}
		j := i
		for j < len(q) && q[j] != ' ' && q[j] != '\t' && q[j] != '\n' && !inRange(j) {
			j++
		}
		tok := q[i:j]
		i = j
		lead := len(tok) - len(strings.TrimLeft(tok, "(+-"))
		trail := len(tok) - len(strings.TrimRight(tok, ")"))
		if lead+trail >= len(tok) {
			b.WriteString(tok)
			continue
		}
		core := tok[lead : len(tok)-trail]
		if !strings.ContainsAny(core, luceneSpecialChars) || strings.Contains(core, `"`) {
			b.WriteString(tok)
			continue
		}
		prefix, value := "", core
		if k := strings.Index(core, ":"); k > 0 && luceneFieldNameRe.MatchString(core[:k]) {
			prefix, value = core[:k+1], core[k+1:]
		}
		value = strings.ReplaceAll(value, `\`, `\\`)
		b.WriteString(tok[:lead] + prefix + `"` + value + `"` + tok[len(tok)-trail:])
		changed = true
	}
	return b.String(), changed
}

// queryStringRangeHint 在 query_string 内写了时间范围却 0 命中时，提示改用 time_from/time_to：
// query_string 的日期解析依赖字段 format，常与模型写的时间格式不匹配。
func queryStringRangeHint(q, timeField string) string {
	m := luceneRangeRe.FindString(q)
	if m == "" {
		return ""
	}
	sub := luceneRangeFieldRe.FindStringSubmatch(m)
	if len(sub) < 2 {
		return ""
	}
	field := sub[1]
	lf := strings.ToLower(field)
	if field != timeField && !strings.Contains(lf, "time") && lf != "ts" && !strings.HasSuffix(lf, "_ts") && !strings.Contains(lf, "date") {
		return ""
	}
	tf := timeField
	if tf == "" {
		tf = esLogDefaultTimeField
	}
	return fmt.Sprintf("0 hits with a range on %q inside the query string; date parsing there depends on the field format and often silently matches nothing. Drop the range from query and pass time_from/time_to instead (filters on %q, accepts ISO-8601 or now-7d style).", field, tf)
}

const (
	esLogDefaultTimeField = "@timestamp"
	esLogDefaultAggSize   = 20
	esLogMaxAggSize       = 200
	esLogAggByField       = "by_field"
	esLogAggTimeline      = "timeline"
)

// esLogQueryOpts 是 es_log_query 的通用查询原语：排序、时间范围、聚合、字段裁剪。
// 它们让模型能回答「最早一条异常是什么时候」「哪个值出现最多」「每分钟多少条」这类问题，
// 而不必翻页拉全量日志。
type esLogQueryOpts struct {
	sortField   string
	sortOrder   string
	timeField   string
	timeFrom    string
	timeTo      string
	aggField    string
	aggSize     int
	aggInterval string
	fields      []string
}

func (o esLogQueryOpts) hasAggs() bool { return o.aggField != "" || o.aggInterval != "" }

func parseESLogQueryOpts(params map[string]any, defaultTimeField string) (esLogQueryOpts, error) {
	str := func(k string) string { v, _ := params[k].(string); return strings.TrimSpace(v) }
	o := esLogQueryOpts{
		timeField:   str("time_field"),
		timeFrom:    str("time_from"),
		timeTo:      str("time_to"),
		aggField:    str("agg_field"),
		aggInterval: str("agg_interval"),
		aggSize:     intFromParam(params["agg_size"], esLogDefaultAggSize),
	}
	if o.timeField == "" {
		o.timeField = strings.TrimSpace(defaultTimeField)
	}
	if o.timeField == "" {
		o.timeField = esLogDefaultTimeField
	}
	if o.aggSize <= 0 {
		o.aggSize = esLogDefaultAggSize
	}
	if o.aggSize > esLogMaxAggSize {
		o.aggSize = esLogMaxAggSize
	}
	if s := str("sort"); s != "" {
		field, order := o.timeField, s
		if i := strings.LastIndex(s, ":"); i > 0 {
			field, order = strings.TrimSpace(s[:i]), strings.TrimSpace(s[i+1:])
		}
		order = strings.ToLower(order)
		if order != "asc" && order != "desc" {
			return o, fmt.Errorf("sort must be asc, desc or <field>:<asc|desc>, got %q", s)
		}
		o.sortField, o.sortOrder = field, order
	}
	switch v := params["fields"].(type) {
	case string:
		for _, f := range strings.Split(v, ",") {
			if f = strings.TrimSpace(f); f != "" {
				o.fields = append(o.fields, f)
			}
		}
	case []any:
		for _, f := range v {
			if s, ok := f.(string); ok && strings.TrimSpace(s) != "" {
				o.fields = append(o.fields, strings.TrimSpace(s))
			}
		}
	case []string:
		o.fields = append(o.fields, v...)
	}
	return o, nil
}

// apply 把选项写入 search body；body 已显式给出的同名部分不覆盖。
func (o esLogQueryOpts) apply(dsl map[string]any) {
	if o.timeFrom != "" || o.timeTo != "" {
		rng := map[string]any{}
		if o.timeFrom != "" {
			rng["gte"] = o.timeFrom
		}
		if o.timeTo != "" {
			rng["lte"] = o.timeTo
		}
		filter := map[string]any{"range": map[string]any{o.timeField: rng}}
		q, ok := dsl["query"].(map[string]any)
		if !ok || len(q) == 0 {
			q = map[string]any{"match_all": map[string]any{}}
		}
		dsl["query"] = map[string]any{"bool": map[string]any{
			"must":   []any{q},
			"filter": []any{filter},
		}}
	}
	if o.sortField != "" {
		if _, exists := dsl["sort"]; !exists {
			dsl["sort"] = []any{map[string]any{o.sortField: map[string]any{"order": o.sortOrder, "unmapped_type": "date"}}}
		}
	}
	if len(o.fields) > 0 {
		if _, exists := dsl["_source"]; !exists {
			dsl["_source"] = o.fields
		}
	}
	if o.hasAggs() {
		_, hasAggs := dsl["aggs"]
		_, hasAggregations := dsl["aggregations"]
		if !hasAggs && !hasAggregations {
			aggs := map[string]any{}
			if o.aggField != "" {
				aggs[esLogAggByField] = map[string]any{"terms": map[string]any{"field": o.aggField, "size": o.aggSize}}
			}
			if o.aggInterval != "" {
				aggs[esLogAggTimeline] = map[string]any{"date_histogram": map[string]any{
					"field": o.timeField, "fixed_interval": o.aggInterval, "min_doc_count": 1,
				}}
			}
			dsl["aggs"] = aggs
		}
	}
}

// summarizeESAggregations 把 ES 聚合原文压成 {name: [{key, count}]}；非 bucket 聚合原样保留。
func summarizeESAggregations(raw json.RawMessage) map[string]any {
	if len(raw) == 0 {
		return nil
	}
	var aggs map[string]json.RawMessage
	if json.Unmarshal(raw, &aggs) != nil || len(aggs) == 0 {
		return nil
	}
	out := make(map[string]any, len(aggs))
	for name, body := range aggs {
		var b struct {
			Buckets []struct {
				Key         any    `json:"key"`
				KeyAsString string `json:"key_as_string"`
				DocCount    int64  `json:"doc_count"`
			} `json:"buckets"`
			SumOtherDocCount int64 `json:"sum_other_doc_count"`
		}
		if json.Unmarshal(body, &b) != nil || b.Buckets == nil {
			var generic any
			_ = json.Unmarshal(body, &generic)
			out[name] = generic
			continue
		}
		buckets := make([]map[string]any, 0, len(b.Buckets))
		for i, bk := range b.Buckets {
			if i >= esLogMaxAggSize {
				break
			}
			key := bk.Key
			if bk.KeyAsString != "" {
				key = bk.KeyAsString
			}
			buckets = append(buckets, map[string]any{"key": key, "count": bk.DocCount})
		}
		entry := map[string]any{"buckets": buckets}
		if b.SumOtherDocCount > 0 {
			entry["other_count"] = b.SumOtherDocCount
		}
		out[name] = entry
	}
	return out
}
