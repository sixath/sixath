package tooldata

import (
	"fmt"
	"regexp"
	"strings"
	"unicode"

	"github.com/sixath/framework/metadata"
)

var (
	reUnknownColumn = regexp.MustCompile(`(?i)Unknown column '([^']+)'(?: in '([^']+)')?`)
	reNoSuchTable   = regexp.MustCompile(`(?i)Table '([^']+)' doesn't exist`)
	reFromTable     = regexp.MustCompile(`(?i)\bFROM\s+((?:` + "`" + `?[A-Za-z0-9_]+` + "`" + `?\.)?` + "`" + `?[A-Za-z0-9_*\-]+` + "`" + `?)`)
)

const maxSQLHealAttempts = 5

// HealReadSQL proposes one rewritten SELECT after a schema-related execute_read error.
// It never changes WHERE predicates. ok=false means no safe rewrite.
func HealReadSQL(sql string, err error, schema *metadata.Schema) (string, string, bool) {
	if err == nil || strings.TrimSpace(sql) == "" {
		return "", "", false
	}
	if col, clause, ok := parseUnknownColumn(err); ok {
		if clause != "" && !strings.EqualFold(clause, "field list") {
			return "", "", false
		}
		next, dropped := dropSelectColumn(sql, col)
		if !dropped || strings.EqualFold(strings.TrimSpace(next), strings.TrimSpace(sql)) {
			return "", "", false
		}
		return next, "dropped unknown column " + col, true
	}
	if schema == nil {
		return "", "", false
	}
	missing := parseMissingTable(err)
	if missing == "" {
		return "", "", false
	}
	db, table := splitQualIdent(missing)
	fromRaw, fromTable := fromTableName(sql)
	if fromTable == "" {
		return "", "", false
	}
	// Schema name used as table: Table 'db.db' or FROM equals schema.Name.
	schemaAsTable := (db != "" && strings.EqualFold(db, table)) ||
		(schema.Name != "" && strings.EqualFold(fromTable, schema.Name)) ||
		(schema.Name != "" && strings.EqualFold(table, schema.Name) && db == "")
	if schemaAsTable {
		whereCols := whereColumnNames(sql)
		selectCols := selectColumnNames(sql)
		picked := pickTableForHeal(schema.Tables, whereCols, selectCols)
		if picked == nil {
			return "", "", false
		}
		next := replaceFromTable(sql, fromRaw, picked.Name)
		if strings.EqualFold(strings.TrimSpace(next), strings.TrimSpace(sql)) {
			return "", "", false
		}
		return next, "rewrote schema-as-table to " + picked.Name, true
	}
	// db.real_table where real_table exists unprefixed
	if table != "" && schemaTable(schema, table) != nil && fromRaw != table {
		next := replaceFromTable(sql, fromRaw, table)
		if !strings.EqualFold(strings.TrimSpace(next), strings.TrimSpace(sql)) {
			return next, "stripped database prefix to " + table, true
		}
	}
	return "", "", false
}

// SchemaHealHint builds a model-facing hint when heal cannot rewrite.
func SchemaHealHint(sql string, err error, schema *metadata.Schema) string {
	var b strings.Builder
	missing := parseMissingTable(err)
	if missing != "" {
		_, table := splitQualIdent(missing)
		if table == "" {
			table = missing
		}
		fmt.Fprintf(&b, "表 %s 不存在。", table)
		if schema != nil && schema.Name != "" && strings.EqualFold(table, schema.Name) {
			fmt.Fprintf(&b, " %s 是数据库名，不是表名。", schema.Name)
		}
	}
	if col, _, ok := parseUnknownColumn(err); ok {
		fmt.Fprintf(&b, "列 %s 不存在。", col)
	}
	whereCols := whereColumnNames(sql)
	if schema != nil && len(whereCols) > 0 {
		var names []string
		for _, tbl := range schema.Tables {
			if tableHasColumns(tbl, whereCols) && !isAuxTable(tbl.Name) {
				names = append(names, tbl.Name)
			}
		}
		if len(names) > 0 {
			fmt.Fprintf(&b, " 含条件列 %s 的表: %s。", strings.Join(whereCols, ","), strings.Join(names, ", "))
		}
	}
	if schema != nil && len(schema.Tables) > 0 && b.Len() > 0 {
		n := len(schema.Tables)
		if n > 12 {
			n = 12
		}
		var all []string
		for i := 0; i < n; i++ {
			all = append(all, schema.Tables[i].Name)
		}
		fmt.Fprintf(&b, " 可用表(部分): %s。", strings.Join(all, ", "))
	}
	if b.Len() == 0 {
		return "请先对该表调用 describe_table 获取正确结构后再重试 execute_read。"
	}
	b.WriteString(" 请改用上述真实表名/列名重试，不要臆造表名或列名。")
	return b.String()
}

func parseUnknownColumn(err error) (col, clause string, ok bool) {
	if err == nil {
		return "", "", false
	}
	m := reUnknownColumn.FindStringSubmatch(err.Error())
	if len(m) < 2 {
		return "", "", false
	}
	col = strings.TrimSpace(m[1])
	if i := strings.LastIndex(col, "."); i >= 0 {
		col = col[i+1:]
	}
	if col == "" {
		return "", "", false
	}
	if len(m) >= 3 {
		clause = strings.TrimSpace(m[2])
	}
	return col, clause, true
}

func parseMissingTable(err error) string {
	if err == nil {
		return ""
	}
	m := reNoSuchTable.FindStringSubmatch(err.Error())
	if len(m) < 2 {
		return ""
	}
	return strings.Trim(m[1], "`")
}

func splitQualIdent(name string) (db, table string) {
	name = strings.Trim(name, "`")
	if i := strings.LastIndex(name, "."); i >= 0 {
		return strings.Trim(name[:i], "`"), strings.Trim(name[i+1:], "`")
	}
	return "", name
}

func fromTableName(sql string) (raw, table string) {
	m := reFromTable.FindStringSubmatch(sql)
	if len(m) < 2 {
		return "", ""
	}
	raw = m[1]
	_, table = splitQualIdent(strings.Trim(raw, "`"))
	return raw, table
}

func replaceFromTable(sql, fromRaw, newTable string) string {
	old := "FROM " + fromRaw
	next := strings.Replace(sql, old, "FROM "+newTable, 1)
	if next != sql {
		return next
	}
	return strings.Replace(sql, "from "+fromRaw, "FROM "+newTable, 1)
}

func dropSelectColumn(sql, col string) (string, bool) {
	sel, from, ok := splitSelectFrom(sql)
	if !ok {
		return sql, false
	}
	parts := splitSelectList(sel)
	kept := make([]string, 0, len(parts))
	dropped := false
	for _, p := range parts {
		if selectItemIsColumn(p, col) {
			dropped = true
			continue
		}
		kept = append(kept, strings.TrimSpace(p))
	}
	if !dropped {
		return sql, false
	}
	list := "*"
	if len(kept) > 0 {
		list = strings.Join(kept, ", ")
	}
	return "SELECT " + list + " FROM " + from, true
}

func splitSelectFrom(sql string) (selectList, rest string, ok bool) {
	s := strings.TrimSpace(sql)
	if len(s) < 7 || !strings.EqualFold(s[:6], "SELECT") {
		return "", "", false
	}
	body := strings.TrimSpace(s[6:])
	idx := indexKeyword(body, "FROM")
	if idx < 0 {
		return "", "", false
	}
	return strings.TrimSpace(body[:idx]), strings.TrimSpace(body[idx+4:]), true
}

func indexKeyword(s, kw string) int {
	n := len(s)
	k := len(kw)
	depth := 0
	inS, inD, inB := false, false, false
	for i := 0; i < n; i++ {
		c := s[i]
		switch {
		case inS:
			if c == '\'' && (i+1 >= n || s[i+1] != '\'') {
				inS = false
			} else if c == '\'' {
				i++
			}
		case inD:
			if c == '"' && (i+1 >= n || s[i+1] != '"') {
				inD = false
			} else if c == '"' {
				i++
			}
		case inB:
			if c == '`' {
				inB = false
			}
		case c == '\'':
			inS = true
		case c == '"':
			inD = true
		case c == '`':
			inB = true
		case c == '(':
			depth++
		case c == ')':
			if depth > 0 {
				depth--
			}
		default:
			if depth == 0 && i+k <= n && strings.EqualFold(s[i:i+k], kw) {
				prevOK := i == 0 || isIdentBreak(rune(s[i-1]))
				nextOK := i+k == n || isIdentBreak(rune(s[i+k]))
				if prevOK && nextOK {
					return i
				}
			}
		}
	}
	return -1
}

func isIdentBreak(r rune) bool {
	return !unicode.IsLetter(r) && !unicode.IsDigit(r) && r != '_'
}

func splitSelectList(list string) []string {
	var parts []string
	depth := 0
	start := 0
	inS, inD, inB := false, false, false
	for i := 0; i < len(list); i++ {
		c := list[i]
		switch {
		case inS:
			if c == '\'' && (i+1 >= len(list) || list[i+1] != '\'') {
				inS = false
			} else if c == '\'' {
				i++
			}
		case inD:
			if c == '"' && (i+1 >= len(list) || list[i+1] != '"') {
				inD = false
			} else if c == '"' {
				i++
			}
		case inB:
			if c == '`' {
				inB = false
			}
		case c == '\'':
			inS = true
		case c == '"':
			inD = true
		case c == '`':
			inB = true
		case c == '(':
			depth++
		case c == ')':
			if depth > 0 {
				depth--
			}
		case c == ',' && depth == 0:
			parts = append(parts, strings.TrimSpace(list[start:i]))
			start = i + 1
		}
	}
	parts = append(parts, strings.TrimSpace(list[start:]))
	return parts
}

func selectItemIsColumn(item, col string) bool {
	item = strings.TrimSpace(item)
	if item == "*" || strings.HasSuffix(item, ".*") {
		return false
	}
	if i := strings.LastIndex(strings.ToLower(item), " as "); i >= 0 {
		item = strings.TrimSpace(item[:i])
	}
	item = strings.Trim(item, "`")
	if j := strings.LastIndex(item, "."); j >= 0 {
		item = strings.Trim(item[j+1:], "`")
	}
	return strings.EqualFold(item, col)
}

func selectColumnNames(sql string) []string {
	sel, _, ok := splitSelectFrom(sql)
	if !ok {
		return nil
	}
	var cols []string
	for _, p := range splitSelectList(sel) {
		p = strings.TrimSpace(p)
		if p == "*" || strings.HasSuffix(p, ".*") {
			continue
		}
		if i := strings.LastIndex(strings.ToLower(p), " as "); i >= 0 {
			p = strings.TrimSpace(p[:i])
		}
		p = strings.Trim(p, "`")
		if j := strings.LastIndex(p, "."); j >= 0 {
			p = strings.Trim(p[j+1:], "`")
		}
		if p == "" || strings.ContainsAny(p, " \t(") {
			continue
		}
		cols = append(cols, p)
	}
	return cols
}

var whereSkip = map[string]struct{}{
	"AND": {}, "OR": {}, "NOT": {}, "IN": {}, "IS": {}, "NULL": {}, "LIKE": {},
	"BETWEEN": {}, "EXISTS": {}, "SELECT": {}, "FROM": {}, "WHERE": {},
	"TRUE": {}, "FALSE": {}, "AS": {}, "ON": {}, "JOIN": {}, "INNER": {},
	"LEFT": {}, "RIGHT": {}, "OUTER": {}, "CROSS": {},
}

func whereColumnNames(sql string) []string {
	idx := indexKeyword(sql, "WHERE")
	if idx < 0 {
		return nil
	}
	rest := sql[idx+5:]
	for _, kw := range []string{"GROUP", "ORDER", "LIMIT", "HAVING", "UNION"} {
		if j := indexKeyword(rest, kw); j >= 0 {
			rest = rest[:j]
			break
		}
	}
	seen := map[string]struct{}{}
	var cols []string
	var b strings.Builder
	flush := func() {
		tok := b.String()
		b.Reset()
		if tok == "" {
			return
		}
		if _, skip := whereSkip[strings.ToUpper(tok)]; skip {
			return
		}
		if unicode.IsDigit(rune(tok[0])) {
			return
		}
		if _, ok := seen[strings.ToLower(tok)]; ok {
			return
		}
		seen[strings.ToLower(tok)] = struct{}{}
		cols = append(cols, tok)
	}
	inS := false
	for i := 0; i < len(rest); i++ {
		c := rest[i]
		if inS {
			if c == '\'' && (i+1 >= len(rest) || rest[i+1] != '\'') {
				inS = false
			} else if c == '\'' {
				i++
			}
			continue
		}
		if c == '\'' {
			flush()
			inS = true
			continue
		}
		if c == '`' {
			continue
		}
		if unicode.IsLetter(rune(c)) || unicode.IsDigit(rune(c)) || c == '_' {
			b.WriteByte(c)
			continue
		}
		flush()
	}
	flush()
	return cols
}

func tableHasColumns(tbl metadata.Table, cols []string) bool {
	if len(cols) == 0 {
		return true
	}
	have := map[string]struct{}{}
	for _, c := range tbl.Columns {
		have[strings.ToLower(c.Name)] = struct{}{}
	}
	for _, col := range cols {
		if _, ok := have[strings.ToLower(col)]; !ok {
			return false
		}
	}
	return true
}

func isAuxTable(name string) bool {
	n := strings.ToLower(name)
	for _, p := range []string{"_test", "_old", "_gorm", "_debug", "_bak"} {
		if strings.Contains(n, p) {
			return true
		}
	}
	return false
}

func pickTableForHeal(tables []metadata.Table, whereCols, selectCols []string) *metadata.Table {
	var cand []metadata.Table
	for _, tbl := range tables {
		if !tableHasColumns(tbl, whereCols) || !tableHasColumns(tbl, selectCols) {
			continue
		}
		cand = append(cand, tbl)
	}
	if len(cand) == 0 {
		return nil
	}
	best := 0
	bestScore := tableHealScore(cand[0], whereCols)
	for i := 1; i < len(cand); i++ {
		s := tableHealScore(cand[i], whereCols)
		if s > bestScore {
			best, bestScore = i, s
		}
	}
	picked := cand[best]
	return &picked
}

func tableHealScore(tbl metadata.Table, whereCols []string) int {
	score := 0
	if !isAuxTable(tbl.Name) {
		score += 10
	}
	if !strings.Contains(strings.ToLower(tbl.Name), "_extend") {
		score += 4
	}
	needVM := false
	for _, c := range whereCols {
		if strings.EqualFold(c, "flow_id") || strings.EqualFold(c, "vmid") {
			needVM = true
		}
	}
	if needVM && tableHasColumns(tbl, []string{"mgr_ipv4_address"}) {
		score += 8
	}
	score += len(tbl.Columns) / 20
	return score
}

func schemaTable(schema *metadata.Schema, name string) *metadata.Table {
	if schema == nil {
		return nil
	}
	for i := range schema.Tables {
		if strings.EqualFold(schema.Tables[i].Name, name) {
			t := schema.Tables[i]
			return &t
		}
	}
	return nil
}
