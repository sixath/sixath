package tool

import (
	"context"
	"encoding/json"
	"net/http"
	"net/url"
	"strings"

	"github.com/sixath/framework/datasource"
	"github.com/sixath/framework/executor"
)

// ESFieldMapping is the query-relevant view of one mapped field.
type ESFieldMapping struct {
	Type            string // text, keyword, date, long, integer, boolean, ...
	KeywordSubfield bool   // text field with fields.keyword
}

// ESFieldHint tells the model which clauses fit this field.
type ESFieldHint struct {
	Field  string   `json:"field"`
	Type   string   `json:"type"`
	Prefer []string `json:"prefer"`
}

// ESFieldMapper looks up a field's mapping on an index or pattern.
type ESFieldMapper interface {
	Lookup(ctx context.Context, index, field string) (ESFieldMapping, bool)
	ListFields(ctx context.Context, index string) []string
}

func (m ESFieldMapping) SuggestedQueries(field string) []string {
	field = strings.TrimSpace(field)
	if field == "" {
		return nil
	}
	switch strings.ToLower(m.Type) {
	case "keyword", "boolean":
		return []string{"term on " + field}
	case "date", "long", "integer", "short", "byte", "double", "float":
		return []string{"range on " + field, "term on " + field}
	case "text":
		if m.KeywordSubfield {
			return []string{"term on " + field + ".keyword", "match on " + field, "match_phrase on " + field}
		}
		return []string{"match on " + field, "match_phrase on " + field, "query_string " + field + ":value"}
	default:
		if m.KeywordSubfield {
			return []string{"term on " + field + ".keyword"}
		}
		return []string{"query_string " + field + ":value"}
	}
}

func baseFieldName(field string) string {
	field = strings.TrimSpace(field)
	if i := strings.Index(field, "."); i > 0 {
		// operation.keyword → operation; keep nested paths like foo.bar if not .keyword
		if strings.HasSuffix(field, ".keyword") {
			return strings.TrimSuffix(field, ".keyword")
		}
	}
	return field
}

func rewriteEmptyHitQuery(dsl map[string]any, fields map[string]ESFieldMapping) (map[string]any, bool, []ESFieldHint) {
	cloned := cloneJSONMap(dsl)
	hints := fieldHintsFromQuery(cloned, fields)
	q, _ := cloned["query"].(map[string]any)
	if q == nil || len(fields) == 0 {
		return cloned, false, hints
	}
	changed := rewriteQueryNode(q, fields)
	return cloned, changed, hints
}

func fieldHintsFromQuery(dsl map[string]any, fields map[string]ESFieldMapping) []ESFieldHint {
	seen := map[string]struct{}{}
	var hints []ESFieldHint
	collectQueryFields(dsl["query"], func(field string) {
		base := baseFieldName(field)
		if _, dup := seen[base]; dup {
			return
		}
		seen[base] = struct{}{}
		m, ok := fields[base]
		if !ok {
			return
		}
		hints = append(hints, ESFieldHint{Field: base, Type: m.Type, Prefer: m.SuggestedQueries(base)})
	})
	return hints
}

func collectQueryFields(node any, visit func(string)) {
	switch n := node.(type) {
	case map[string]any:
		for _, key := range []string{
			"term", "terms", "match", "match_phrase", "match_phrase_prefix",
			"range", "prefix", "wildcard", "regexp", "fuzzy", "exists",
		} {
			leaf, ok := n[key].(map[string]any)
			if !ok {
				continue
			}
			for _, field := range leafFieldNames(leaf) {
				visit(field)
			}
		}
		if b, ok := n["bool"].(map[string]any); ok {
			for _, k := range []string{"must", "should", "filter", "must_not"} {
				collectQueryFields(b[k], visit)
			}
		}
		for _, key := range []string{"query_string", "simple_query_string"} {
			qs, ok := n[key].(map[string]any)
			if !ok {
				continue
			}
			s, _ := qs["query"].(string)
			for _, f := range parseLuceneQueryFields(s) {
				visit(f)
			}
		}
		if q, ok := n["query"]; ok {
			collectQueryFields(q, visit)
		}
	case []any:
		for _, item := range n {
			collectQueryFields(item, visit)
		}
	}
}

func leafFieldNames(leaf map[string]any) []string {
	if f, ok := leaf["field"].(string); ok {
		f = strings.TrimSpace(f)
		if f != "" {
			return []string{f}
		}
	}
	var names []string
	for k := range leaf {
		if isQueryClauseMetaKey(k) {
			continue
		}
		names = append(names, k)
	}
	return names
}

func isQueryClauseMetaKey(k string) bool {
	switch k {
	case "boost", "_name", "field", "value", "query", "size", "min_doc_count",
		"include", "exclude", "execution_hint", "order", "missing", "format",
		"time_zone", "gte", "gt", "lte", "lt", "from", "to", "relation",
		"case_insensitive", "rewrite", "fuzziness", "prefix_length", "max_expansions",
		"operator", "analyzer", "slop":
		return true
	default:
		return false
	}
}

func rewriteQueryNode(node map[string]any, fields map[string]ESFieldMapping) bool {
	if node == nil {
		return false
	}
	if leaf, ok := node["term"].(map[string]any); ok {
		return rewriteSingleLeaf(node, "term", leaf, fields)
	}
	if leaf, ok := node["match"].(map[string]any); ok {
		return rewriteSingleLeaf(node, "match", leaf, fields)
	}
	if leaf, ok := node["match_phrase"].(map[string]any); ok {
		return rewriteSingleLeaf(node, "match_phrase", leaf, fields)
	}
	if leaf, ok := node["terms"].(map[string]any); ok {
		return rewriteSingleLeaf(node, "terms", leaf, fields)
	}
	changed := false
	if b, ok := node["bool"].(map[string]any); ok {
		for _, k := range []string{"must", "should", "filter", "must_not"} {
			changed = rewriteBoolClause(b, k, fields) || changed
		}
	}
	return changed
}

func rewriteBoolClause(b map[string]any, key string, fields map[string]ESFieldMapping) bool {
	switch v := b[key].(type) {
	case []any:
		changed := false
		for _, item := range v {
			m, ok := item.(map[string]any)
			if !ok {
				continue
			}
			changed = rewriteQueryNode(m, fields) || changed
		}
		return changed
	case map[string]any:
		return rewriteQueryNode(v, fields)
	default:
		return false
	}
}

func rewriteSingleLeaf(parent map[string]any, kind string, leaf map[string]any, fields map[string]ESFieldMapping) bool {
	if len(leaf) != 1 {
		return false
	}
	var field string
	var val any
	for k, v := range leaf {
		field, val = k, v
	}
	m, ok := fields[baseFieldName(field)]
	if !ok {
		return false
	}
	nextKind, nextField, ok := mappingRewrite(kind, field, m)
	if !ok || (nextKind == kind && nextField == field) {
		return false
	}
	delete(parent, kind)
	parent[nextKind] = map[string]any{nextField: scalarQueryValue(val)}
	return true
}

func mappingRewrite(kind, field string, m ESFieldMapping) (nextKind, nextField string, ok bool) {
	typ := strings.ToLower(m.Type)
	switch typ {
	case "keyword", "boolean":
		if kind == "match" || kind == "match_phrase" {
			return "term", field, true
		}
		return kind, field, false
	case "text":
		if (kind == "term" || kind == "terms") && !strings.HasSuffix(field, ".keyword") {
			if m.KeywordSubfield {
				return kind, baseFieldName(field) + ".keyword", true
			}
			if kind == "term" {
				return "match_phrase", field, true
			}
			return kind, field, false
		}
		return kind, field, false
	default:
		return kind, field, false
	}
}

func scalarQueryValue(v any) any {
	m, ok := v.(map[string]any)
	if !ok {
		return v
	}
	if x, ok := m["value"]; ok {
		return x
	}
	if x, ok := m["query"]; ok {
		return x
	}
	return v
}

func cloneJSONMap(in map[string]any) map[string]any {
	if in == nil {
		return nil
	}
	b, err := json.Marshal(in)
	if err != nil {
		return in
	}
	var out map[string]any
	if err := json.Unmarshal(b, &out); err != nil {
		return in
	}
	return out
}

func parseESFieldMappingJSON(raw []byte, field string) (ESFieldMapping, bool) {
	var top map[string]any
	if err := json.Unmarshal(raw, &top); err != nil {
		return ESFieldMapping{}, false
	}
	field = strings.TrimSpace(field)
	var merged ESFieldMapping
	found := false
	for _, idx := range top {
		entry, ok := idx.(map[string]any)
		if !ok {
			continue
		}
		if m, ok := fieldMappingFromIndexEntry(entry, field); ok {
			found = true
			if m.KeywordSubfield {
				merged.KeywordSubfield = true
			}
			if merged.Type == "" || m.Type == "text" {
				merged.Type = m.Type
			}
			if merged.Type == "" {
				merged.Type = m.Type
			}
		}
	}
	return merged, found && merged.Type != ""
}

func fieldMappingFromIndexEntry(entry map[string]any, field string) (ESFieldMapping, bool) {
	mappings, _ := entry["mappings"].(map[string]any)
	if mappings == nil {
		return ESFieldMapping{}, false
	}
	// GET _mapping/field/X → mappings[X].mapping[X]
	if fm, ok := mappings[field].(map[string]any); ok {
		if inner, ok := fm["mapping"].(map[string]any); ok {
			if def, ok := inner[field].(map[string]any); ok {
				return mappingFromDef(def), true
			}
			for _, v := range inner {
				if def, ok := v.(map[string]any); ok {
					return mappingFromDef(def), true
				}
			}
		}
	}
	props, _ := mappings["properties"].(map[string]any)
	if props == nil {
		for _, inner := range mappings {
			m, ok := inner.(map[string]any)
			if !ok {
				continue
			}
			if p, ok := m["properties"].(map[string]any); ok {
				props = p
				break
			}
		}
	}
	if def, ok := walkMappingProps(props, field); ok {
		return mappingFromDef(def), true
	}
	return ESFieldMapping{}, false
}

func walkMappingProps(props map[string]any, field string) (map[string]any, bool) {
	if props == nil || field == "" {
		return nil, false
	}
	parts := strings.Split(field, ".")
	cur := props
	for i, p := range parts {
		def, ok := cur[p].(map[string]any)
		if !ok {
			return nil, false
		}
		if i == len(parts)-1 {
			return def, true
		}
		next, _ := def["properties"].(map[string]any)
		cur = next
	}
	return nil, false
}

func mappingFromDef(def map[string]any) ESFieldMapping {
	typ, _ := def["type"].(string)
	m := ESFieldMapping{Type: strings.ToLower(strings.TrimSpace(typ))}
	if fields, ok := def["fields"].(map[string]any); ok {
		if kw, ok := fields["keyword"].(map[string]any); ok {
			kt, _ := kw["type"].(string)
			if strings.EqualFold(kt, "keyword") {
				m.KeywordSubfield = true
			}
		}
	}
	if m.Type == "" && m.KeywordSubfield {
		m.Type = "text"
	}
	return m
}

type esRegistryFieldMapper struct {
	reg  *datasource.Registry
	dsID string
}

func mapperFromReader(reader executor.Reader, dsID string) ESFieldMapper {
	ex, ok := reader.(*executor.ESExecutor)
	if !ok || ex == nil || ex.Registry == nil {
		return nil
	}
	return &esRegistryFieldMapper{reg: ex.Registry, dsID: dsID}
}

func (m *esRegistryFieldMapper) Lookup(ctx context.Context, index, field string) (ESFieldMapping, bool) {
	if m == nil || m.reg == nil || strings.TrimSpace(field) == "" {
		return ESFieldMapping{}, false
	}
	ds, err := m.reg.Get(m.dsID)
	if err != nil || ds == nil {
		return ESFieldMapping{}, false
	}
	ep, ok := ds.(datasource.ESHTTPProvider)
	if !ok || ep.ESHTTP() == nil {
		return ESFieldMapping{}, false
	}
	idx := strings.TrimSpace(index)
	if idx == "" {
		idx = "_all"
	}
	path := "/" + strings.Trim(idx, "/") + "/_mapping/field/" + url.PathEscape(baseFieldName(field))
	status, body, err := ep.ESHTTP().Do(ctx, http.MethodGet, path, nil)
	if err != nil || status >= 400 {
		return ESFieldMapping{}, false
	}
	return parseESFieldMappingJSON(body, baseFieldName(field))
}

type mapFieldMapper map[string]ESFieldMapping

func (m mapFieldMapper) Lookup(_ context.Context, _, field string) (ESFieldMapping, bool) {
	v, ok := m[baseFieldName(field)]
	return v, ok
}

func (m mapFieldMapper) ListFields(_ context.Context, _ string) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	return out
}

func (m *esRegistryFieldMapper) ListFields(ctx context.Context, index string) []string {
	if m == nil || m.reg == nil {
		return nil
	}
	ds, err := m.reg.Get(m.dsID)
	if err != nil || ds == nil {
		return nil
	}
	ep, ok := ds.(datasource.ESHTTPProvider)
	if !ok || ep.ESHTTP() == nil {
		return nil
	}
	idx := strings.TrimSpace(index)
	if idx == "" {
		idx = "_all"
	}
	path := "/" + strings.Trim(idx, "/") + "/_mapping"
	status, body, err := ep.ESHTTP().Do(ctx, http.MethodGet, path, nil)
	if err != nil || status >= 400 {
		return nil
	}
	return flattenMappingFieldNames(body)
}

func collectQueryFieldNames(dsl map[string]any) []string {
	seen := map[string]struct{}{}
	var out []string
	if dsl == nil {
		return out
	}
	collectQueryFields(dsl["query"], func(field string) {
		base := baseFieldName(field)
		if base == "" {
			return
		}
		if _, ok := seen[base]; ok {
			return
		}
		seen[base] = struct{}{}
		out = append(out, base)
	})
	collectAggFields(dsl["aggs"], func(field string) {
		base := baseFieldName(field)
		if base == "" {
			return
		}
		if _, ok := seen[base]; ok {
			return
		}
		seen[base] = struct{}{}
		out = append(out, base)
	})
	collectAggFields(dsl["aggregations"], func(field string) {
		base := baseFieldName(field)
		if base == "" {
			return
		}
		if _, ok := seen[base]; ok {
			return
		}
		seen[base] = struct{}{}
		out = append(out, base)
	})
	return out
}

func collectAggFields(node any, visit func(string)) {
	switch n := node.(type) {
	case map[string]any:
		if f, ok := n["field"].(string); ok {
			if f = strings.TrimSpace(f); f != "" {
				visit(f)
			}
		}
		for k, v := range n {
			if k == "field" {
				continue
			}
			collectAggFields(v, visit)
		}
	case []any:
		for _, item := range n {
			collectAggFields(item, visit)
		}
	}
}

func parseLuceneQueryFields(q string) []string {
	q = strings.TrimSpace(q)
	if q == "" {
		return nil
	}
	var fields []string
	seen := map[string]struct{}{}
	inQuote := byte(0)
	for i := 0; i < len(q); {
		c := q[i]
		if inQuote != 0 {
			if c == '\\' && i+1 < len(q) {
				i += 2
				continue
			}
			if c == inQuote {
				inQuote = 0
			}
			i++
			continue
		}
		if c == '"' || c == '\'' {
			inQuote = c
			i++
			continue
		}
		if isMappedFieldStart(c) {
			j := i + 1
			for j < len(q) && isMappedFieldCont(q[j]) {
				j++
			}
			k := j
			for k < len(q) && (q[k] == ' ' || q[k] == '\t') {
				k++
			}
			if k < len(q) && q[k] == ':' {
				name := q[i:j]
				if !isLuceneMetaField(name) {
					if _, ok := seen[name]; !ok {
						seen[name] = struct{}{}
						fields = append(fields, name)
					}
				}
				i = k + 1
				continue
			}
			i = j
			continue
		}
		i++
	}
	return fields
}

func isMappedFieldStart(c byte) bool {
	return c == '_' || (c >= 'A' && c <= 'Z') || (c >= 'a' && c <= 'z')
}

func isMappedFieldCont(c byte) bool {
	return isMappedFieldStart(c) || (c >= '0' && c <= '9') || c == '.'
}

func isLuceneMetaField(name string) bool {
	switch strings.ToLower(name) {
	case "and", "or", "not", "to", "_exists_", "_missing_":
		return true
	default:
		return false
	}
}

func flattenMappingFieldNames(raw []byte) []string {
	var top map[string]any
	if err := json.Unmarshal(raw, &top); err != nil {
		return nil
	}
	seen := map[string]struct{}{}
	var out []string
	add := func(name string) {
		name = strings.TrimSpace(name)
		if name == "" {
			return
		}
		if _, ok := seen[name]; ok {
			return
		}
		seen[name] = struct{}{}
		out = append(out, name)
	}
	for _, idx := range top {
		entry, ok := idx.(map[string]any)
		if !ok {
			continue
		}
		mappings, _ := entry["mappings"].(map[string]any)
		if mappings == nil {
			continue
		}
		props, _ := mappings["properties"].(map[string]any)
		if props == nil {
			for _, inner := range mappings {
				m, ok := inner.(map[string]any)
				if !ok {
					continue
				}
				if p, ok := m["properties"].(map[string]any); ok {
					props = p
					break
				}
			}
		}
		walkMappingFieldNames("", props, add)
	}
	return out
}

func walkMappingFieldNames(prefix string, props map[string]any, add func(string)) {
	if props == nil {
		return
	}
	for name, raw := range props {
		def, ok := raw.(map[string]any)
		if !ok {
			continue
		}
		full := name
		if prefix != "" {
			full = prefix + "." + name
		}
		add(full)
		if fields, ok := def["fields"].(map[string]any); ok {
			for sub := range fields {
				add(full + "." + sub)
			}
		}
		if nested, ok := def["properties"].(map[string]any); ok {
			walkMappingFieldNames(full, nested, add)
		}
	}
}

func unknownQueryFields(names, catalog []string) []string {
	if len(catalog) == 0 || len(names) == 0 {
		return nil
	}
	have := mappedFieldSet(catalog)
	var unknown []string
	seen := map[string]struct{}{}
	for _, n := range names {
		n = baseFieldName(n)
		if n == "" {
			continue
		}
		if mappedFieldInSet(n, have) {
			continue
		}
		if _, ok := seen[n]; ok {
			continue
		}
		seen[n] = struct{}{}
		unknown = append(unknown, n)
	}
	return unknown
}

func mappedFieldSet(catalog []string) map[string]struct{} {
	have := make(map[string]struct{}, len(catalog))
	for _, f := range catalog {
		f = strings.TrimSpace(f)
		if f == "" {
			continue
		}
		have[f] = struct{}{}
		have[baseFieldName(f)] = struct{}{}
	}
	return have
}

func mappedFieldInSet(field string, have map[string]struct{}) bool {
	if _, ok := have[field]; ok {
		return true
	}
	if _, ok := have[field+".keyword"]; ok {
		return true
	}
	return false
}

func suggestSimilarMappedFields(unknown string, catalog []string) []string {
	want := normalizeMappedField(unknown)
	if want == "" || len(catalog) == 0 {
		return nil
	}
	var out []string
	seen := map[string]struct{}{}
	add := func(name string) {
		name = baseFieldName(name)
		if name == "" || strings.EqualFold(name, unknown) {
			return
		}
		if strings.HasSuffix(name, ".keyword") {
			name = strings.TrimSuffix(name, ".keyword")
		}
		if _, ok := seen[name]; ok {
			return
		}
		seen[name] = struct{}{}
		out = append(out, name)
	}
	for _, f := range catalog {
		if len(out) >= 5 {
			break
		}
		n := normalizeMappedField(baseFieldName(f))
		if n == "" {
			continue
		}
		if n == want {
			add(f)
			continue
		}
		if len(want) >= 4 && (strings.Contains(n, want) || strings.Contains(want, n)) {
			add(f)
			continue
		}
		if len(want) >= 4 && levenshtein(n, want) <= 2 {
			add(f)
		}
	}
	return out
}

func normalizeMappedField(s string) string {
	s = strings.ToLower(strings.TrimSpace(s))
	var b strings.Builder
	b.Grow(len(s))
	for i := 0; i < len(s); i++ {
		c := s[i]
		if c == '_' || c == '-' || c == '.' {
			continue
		}
		b.WriteByte(c)
	}
	return b.String()
}

func levenshtein(a, b string) int {
	if a == b {
		return 0
	}
	if a == "" {
		return len(b)
	}
	if b == "" {
		return len(a)
	}
	prev := make([]int, len(b)+1)
	cur := make([]int, len(b)+1)
	for j := range prev {
		prev[j] = j
	}
	for i := 1; i <= len(a); i++ {
		cur[0] = i
		for j := 1; j <= len(b); j++ {
			cost := 1
			if a[i-1] == b[j-1] {
				cost = 0
			}
			del := prev[j] + 1
			ins := cur[j-1] + 1
			sub := prev[j-1] + cost
			cur[j] = del
			if ins < cur[j] {
				cur[j] = ins
			}
			if sub < cur[j] {
				cur[j] = sub
			}
		}
		prev, cur = cur, prev
	}
	return prev[len(b)]
}

func unknownFieldsNote(unknown []string) string {
	if len(unknown) == 0 {
		return ""
	}
	return "field " + strings.Join(unknown, ", ") + " is not in index mapping; 0 hits are not evidence of missing logs. Do not invent field names. Query only mapped fields, or the value as query_string without a field prefix."
}

func lookupQueryFields(ctx context.Context, mapper ESFieldMapper, index string, dsl map[string]any) map[string]ESFieldMapping {
	out := map[string]ESFieldMapping{}
	if mapper == nil {
		return out
	}
	collectQueryFields(dsl["query"], func(field string) {
		base := baseFieldName(field)
		if _, ok := out[base]; ok {
			return
		}
		if m, ok := mapper.Lookup(ctx, index, base); ok {
			out[base] = m
		}
	})
	return out
}
