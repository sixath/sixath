package tool

import "encoding/json"

var esLogDropFields = map[string]struct{}{
	"reply": {}, "stack": {}, "beat": {}, "host": {}, "prospector": {},
	"input": {}, "source": {}, "offset": {}, "fields": {}, "error": {},
	"@version": {}, "log": {},
}

func compactESLogHits(hits []map[string]any) []map[string]any {
	out := make([]map[string]any, 0, len(hits))
	for _, h := range hits {
		out = append(out, compactESLogHit(h))
	}
	return out
}

func compactESLogHit(h map[string]any) map[string]any {
	if h == nil {
		return nil
	}
	out := make(map[string]any, len(h))
	for k, v := range h {
		if _, drop := esLogDropFields[k]; drop {
			continue
		}
		out[k] = v
	}
	if len(out) == 0 {
		return h
	}
	return out
}

func extractIDsFromHits(hits []map[string]any) []string {
	seen := map[string]struct{}{}
	var out []string
	add := func(s string) {
		s = trimID(s)
		if s == "" {
			return
		}
		if _, ok := seen[s]; ok {
			return
		}
		seen[s] = struct{}{}
		out = append(out, s)
	}
	for _, h := range hits {
		if h == nil {
			continue
		}
		for _, id := range idsFromArgs(h["args"]) {
			add(id)
		}
		for _, k := range []string{"flowId", "flow_id"} {
			add(stringifyScalar(h[k]))
		}
	}
	return out
}

func trimID(s string) string {
	for len(s) > 0 && (s[0] == ' ' || s[0] == '\t' || s[0] == '"') {
		s = s[1:]
	}
	for len(s) > 0 && (s[len(s)-1] == ' ' || s[len(s)-1] == '\t' || s[len(s)-1] == '"') {
		s = s[:len(s)-1]
	}
	return s
}

func stringifyScalar(v any) string {
	s, _ := v.(string)
	return s
}

func idsFromArgs(v any) []string {
	switch x := v.(type) {
	case string:
		var obj any
		if json.Unmarshal([]byte(x), &obj) != nil {
			return nil
		}
		return idsFromArgs(obj)
	case map[string]any:
		var out []string
		for _, k := range []string{"flowIds", "flow_ids", "flowId", "flow_id"} {
			out = append(out, idsFromValue(x[k])...)
		}
		return out
	default:
		return nil
	}
}

func idsFromValue(v any) []string {
	switch x := v.(type) {
	case string:
		x = trimID(x)
		if x == "" {
			return nil
		}
		return []string{x}
	case []any:
		var out []string
		for _, item := range x {
			out = append(out, idsFromValue(item)...)
		}
		return out
	case []string:
		var out []string
		for _, item := range x {
			out = append(out, idsFromValue(item)...)
		}
		return out
	default:
		return nil
	}
}
