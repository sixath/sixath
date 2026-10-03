package tooldata

import (
	"regexp"
	"strings"
)

// sqlRefs.Table 保留原始限定名（如 db1.users）；Qualified 时跳过表结构校验（默认库的 schema 不适用）。
type sqlRefs struct {
	Table        string
	Qualified    bool
	Columns      []string
	HasCondition bool
}

var (
	sqlLeadComment = regexp.MustCompile(`(?s)^\s*(/\*.*?\*/|--[^\n]*\n)\s*`)
	sqlLead        = regexp.MustCompile(`(?is)^\s*(\(|select|show|desc|describe|explain|with)\b`)
	sqlSelectAlias = regexp.MustCompile(`(?i)^.+?\s+(?:as\s+)?([A-Za-z_]\w*)$`)
	luceneField    = regexp.MustCompile(`^[\w.@-]+:\S`)
	sqlStringLit   = regexp.MustCompile(`'(?:[^'\\]|\\.)*'|"(?:[^"\\]|\\.)*"`)
	sqlSingle      = regexp.MustCompile("(?is)^\\s*select\\s+(.+?)\\s+from\\s+([`\\w.]+)(?:\\s+(?:as\\s+)?(\\w+))?\\s*(?:(where|order\\s+by|group\\s+by|limit)\\b(.*))?;?\\s*$")
	sqlCondColumn  = regexp.MustCompile("(?i)([`\\w.]+)\\s*(?:=|!=|<>|>=|<=|>|<|\\s(?:not\\s+)?like\\s|\\s(?:not\\s+)?in\\s*\\(|\\sis\\s|\\sbetween\\s)")
	sqlOrderBy     = regexp.MustCompile("(?i)order\\s+by\\s+([`\\w.]+)")
	sqlIdent       = regexp.MustCompile("^`?([A-Za-z_][\\w]*)`?$")
	sqlKeywords    = map[string]struct{}{"and": {}, "or": {}, "not": {}, "null": {}, "where": {}, "by": {}}
)

// nonSQLReason 判断 dsl 是否明显不是 SQL（Lucene 或 ES JSON DSL）。
func nonSQLReason(s string) (string, bool) {
	t := strings.TrimSpace(s)
	if t == "" {
		return "", false
	}
	if strings.HasPrefix(t, "{") {
		return "looks like Elasticsearch JSON DSL, not SQL", true
	}
	for sqlLeadComment.MatchString(t) {
		t = sqlLeadComment.ReplaceAllString(t, "")
	}
	if sqlLead.MatchString(t) {
		return "", false
	}
	if luceneField.MatchString(t) || strings.Contains(t, " AND ") || strings.Contains(t, " OR ") {
		return "looks like a Lucene/query_string expression, not SQL", true
	}
	return "", false
}

// parseSingleTableSQL 只识别单表 SELECT（无 JOIN/子查询/CTE），返回表名与 SELECT/WHERE/ORDER BY 中可识别的列名。
// 无法确定时返回 ok=false，调用方应放行。
func parseSingleTableSQL(sql string) (sqlRefs, bool) {
	clean := sqlStringLit.ReplaceAllString(sql, "''")
	low := strings.ToLower(clean)
	if strings.Contains(low, " join ") || strings.Count(low, "select") != 1 {
		return sqlRefs{}, false
	}
	m := sqlSingle.FindStringSubmatch(clean)
	if m == nil {
		return sqlRefs{}, false
	}
	table := strings.ReplaceAll(m[2], "`", "")
	refs := sqlRefs{Table: table, Qualified: strings.Contains(table, ".")}
	// SELECT 别名先占位，避免 ORDER BY cnt 之类被当成列。
	seen := map[string]struct{}{}
	selectParts := strings.Split(m[1], ",")
	for _, part := range selectParts {
		if am := sqlSelectAlias.FindStringSubmatch(strings.TrimSpace(part)); am != nil {
			seen[am[1]] = struct{}{}
		}
	}
	add := func(raw string) {
		raw = strings.Trim(raw, "`")
		if i := strings.LastIndex(raw, "."); i >= 0 {
			raw = raw[i+1:]
		}
		id := sqlIdent.FindStringSubmatch(raw)
		if id == nil {
			return
		}
		name := id[1]
		if _, kw := sqlKeywords[strings.ToLower(name)]; kw {
			return
		}
		if _, dup := seen[name]; dup {
			return
		}
		seen[name] = struct{}{}
		refs.Columns = append(refs.Columns, name)
	}
	for _, part := range selectParts {
		p := strings.TrimSpace(part)
		if p == "*" || strings.ContainsAny(p, "() ") {
			continue
		}
		add(p)
	}
	if clause, tail := m[4], m[5]; clause != "" && tail != "" {
		refs.HasCondition = strings.EqualFold(strings.Fields(clause)[0], "where")
		rest := clause + tail
		for _, cm := range sqlCondColumn.FindAllStringSubmatch(rest, -1) {
			add(cm[1])
		}
		if ob := sqlOrderBy.FindStringSubmatch(rest); ob != nil {
			add(ob[1])
		}
	}
	return refs, true
}

func quoteSQLTable(t string) string {
	parts := strings.Split(t, ".")
	for i, p := range parts {
		parts[i] = "`" + p + "`"
	}
	return strings.Join(parts, ".")
}
