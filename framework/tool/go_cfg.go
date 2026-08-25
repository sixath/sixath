package tool

import (
	"bytes"
	"go/ast"
	"go/parser"
	"go/printer"
	"go/token"
	"strings"
	"unicode"
)

const maxCFGPaths = 32

// ControlFlowFunc is AST-derived path table for one function overlapping a read window.
type ControlFlowFunc struct {
	Function  string            `json:"function"`
	File      string            `json:"file"`
	StartLine int               `json:"start_line"`
	EndLine   int               `json:"end_line"`
	Paths     []ControlFlowPath `json:"paths"`
}

// ControlFlowPath is one reachable execution path inside a function.
type ControlFlowPath struct {
	ID      string   `json:"id"`
	When    []string `json:"when,omitempty"`
	Calls   []string `json:"calls,omitempty"`
	Returns bool     `json:"returns,omitempty"`
}

var cfgSkipPkgs = map[string]struct{}{
	"fmt": {}, "strings": {}, "strconv": {}, "errors": {}, "log": {}, "slog": {},
	"os": {}, "io": {}, "path": {}, "filepath": {}, "time": {}, "context": {},
	"json": {}, "http": {}, "sync": {}, "unsafe": {}, "reflect": {}, "sort": {},
}

var cfgSkipNames = map[string]struct{}{
	"make": {}, "len": {}, "cap": {}, "append": {}, "copy": {}, "delete": {},
	"new": {}, "close": {}, "panic": {}, "recover": {}, "print": {}, "println": {},
	"complex": {}, "real": {}, "imag": {}, "min": {}, "max": {}, "clear": {},
	"Errorf": {}, "Infof": {}, "Debugf": {}, "Warnf": {}, "Printf": {}, "Println": {},
	"Print": {}, "Fatalf": {}, "Fatal": {}, "Panicf": {}, "Sprintf": {}, "Fprintf": {},
}

type cfgPath struct {
	when    []string
	calls   []string
	returns bool
}

type cfgWalker struct {
	fset *token.FileSet
}

// extractGoControlFlow parses a Go file and returns path tables for functions that
// overlap [startLine, endLine] (1-based, inclusive). Parse errors return nil.
func extractGoControlFlow(src []byte, file string, startLine, endLine int) []ControlFlowFunc {
	if startLine < 1 {
		startLine = 1
	}
	if endLine < startLine {
		endLine = startLine
	}
	fset := token.NewFileSet()
	parsed, err := parser.ParseFile(fset, file, src, parser.SkipObjectResolution)
	if err != nil || parsed == nil {
		return nil
	}
	w := &cfgWalker{fset: fset}
	var out []ControlFlowFunc
	for _, decl := range parsed.Decls {
		fn, ok := decl.(*ast.FuncDecl)
		if !ok || fn.Body == nil {
			continue
		}
		start := fset.Position(fn.Pos()).Line
		end := fset.Position(fn.End()).Line
		if end < startLine || start > endLine {
			continue
		}
		paths := w.walkStmts(fn.Body.List, nil, nil)
		out = append(out, ControlFlowFunc{
			Function:  fn.Name.Name,
			File:      file,
			StartLine: start,
			EndLine:   end,
			Paths:     assignPathIDs(dedupCFGPaths(paths)),
		})
	}
	return out
}

func (w *cfgWalker) walkStmts(stmts []ast.Stmt, when, calls []string) []cfgPath {
	if len(stmts) == 0 {
		return []cfgPath{{when: cloneCFG(when), calls: cloneCFG(calls)}}
	}
	stmt := stmts[0]
	rest := stmts[1:]
	if ls, ok := stmt.(*ast.LabeledStmt); ok && ls.Stmt != nil {
		stmt = ls.Stmt
	}
	switch s := stmt.(type) {
	case *ast.IfStmt:
		return w.walkIf(s, rest, when, calls)
	case *ast.SwitchStmt:
		return w.walkSwitch(s, rest, when, calls)
	case *ast.TypeSwitchStmt:
		return w.walkTypeSwitch(s, rest, when, calls)
	case *ast.SelectStmt:
		return w.walkSelect(s, rest, when, calls)
	case *ast.RangeStmt:
		return w.walkRange(s, rest, when, calls)
	case *ast.ForStmt:
		return w.walkFor(s, rest, when, calls)
	case *ast.ReturnStmt:
		calls = appendCFGCalls(calls, s)
		return []cfgPath{{when: cloneCFG(when), calls: cloneCFG(calls), returns: true}}
	case *ast.BlockStmt:
		return w.walkStmts(append(append([]ast.Stmt{}, s.List...), rest...), when, calls)
	case *ast.BranchStmt:
		if s.Tok == token.RETURN {
			return []cfgPath{{when: cloneCFG(when), calls: cloneCFG(calls), returns: true}}
		}
		return w.walkStmts(rest, when, appendCFGCalls(calls, s))
	default:
		return w.walkStmts(rest, when, appendCFGCalls(calls, s))
	}
}

func (w *cfgWalker) walkIf(s *ast.IfStmt, rest []ast.Stmt, when, calls []string) []cfgPath {
	if s.Init != nil {
		calls = appendCFGCalls(calls, s.Init)
	}
	cond := w.exprString(s.Cond)
	thenPaths := w.walkStmts(bodyList(s.Body), appendWhen(when, cond), calls)
	out := continueCFG(w, thenPaths, rest)

	neg := negateCond(cond)
	var elseStmts []ast.Stmt
	switch el := s.Else.(type) {
	case nil:
		elseStmts = rest
	case *ast.BlockStmt:
		elseStmts = append(append([]ast.Stmt{}, el.List...), rest...)
	case *ast.IfStmt:
		elseStmts = append([]ast.Stmt{el}, rest...)
	default:
		elseStmts = rest
	}
	out = append(out, w.walkStmts(elseStmts, appendWhen(when, neg), calls)...)
	return capCFGPaths(out)
}

func (w *cfgWalker) walkSwitch(s *ast.SwitchStmt, rest []ast.Stmt, when, calls []string) []cfgPath {
	if s.Init != nil {
		calls = appendCFGCalls(calls, s.Init)
	}
	tag := ""
	if s.Tag != nil {
		tag = w.exprString(s.Tag)
	}
	var out []cfgPath
	hasDefault := false
	for _, clause := range bodyList(s.Body) {
		cc, ok := clause.(*ast.CaseClause)
		if !ok {
			continue
		}
		cond := "default"
		if cc.List == nil {
			hasDefault = true
		} else {
			cond = w.caseCond(tag, cc.List)
		}
		bodyPaths := w.walkStmts(cc.Body, appendWhen(when, cond), calls)
		out = append(out, continueCFG(w, bodyPaths, rest)...)
	}
	if !hasDefault {
		out = append(out, w.walkStmts(rest, when, calls)...)
	}
	return capCFGPaths(out)
}

func (w *cfgWalker) walkTypeSwitch(s *ast.TypeSwitchStmt, rest []ast.Stmt, when, calls []string) []cfgPath {
	if s.Init != nil {
		calls = appendCFGCalls(calls, s.Init)
	}
	if s.Assign != nil {
		calls = appendCFGCalls(calls, s.Assign)
	}
	var out []cfgPath
	hasDefault := false
	for _, clause := range bodyList(s.Body) {
		cc, ok := clause.(*ast.CaseClause)
		if !ok {
			continue
		}
		cond := "default"
		if cc.List == nil {
			hasDefault = true
		} else {
			cond = w.caseCond("", cc.List)
		}
		bodyPaths := w.walkStmts(cc.Body, appendWhen(when, cond), calls)
		out = append(out, continueCFG(w, bodyPaths, rest)...)
	}
	if !hasDefault {
		out = append(out, w.walkStmts(rest, when, calls)...)
	}
	return capCFGPaths(out)
}

func (w *cfgWalker) walkSelect(s *ast.SelectStmt, rest []ast.Stmt, when, calls []string) []cfgPath {
	var out []cfgPath
	hasDefault := false
	for _, clause := range bodyList(s.Body) {
		cc, ok := clause.(*ast.CommClause)
		if !ok {
			continue
		}
		cond := "select"
		if cc.Comm == nil {
			hasDefault = true
			cond = "default"
		} else {
			cond = w.stmtString(cc.Comm)
			calls = appendCFGCalls(calls, cc.Comm)
		}
		bodyPaths := w.walkStmts(cc.Body, appendWhen(when, cond), calls)
		out = append(out, continueCFG(w, bodyPaths, rest)...)
	}
	if !hasDefault {
		out = append(out, w.walkStmts(rest, when, calls)...)
	}
	return capCFGPaths(out)
}

func (w *cfgWalker) walkRange(s *ast.RangeStmt, rest []ast.Stmt, when, calls []string) []cfgPath {
	if s.X != nil {
		calls = appendCFGCalls(calls, s.X)
	}
	loopWhen := "range"
	if s.X != nil {
		loopWhen = "range " + w.exprString(s.X)
	}
	bodyPaths := w.walkStmts(bodyList(s.Body), appendWhen(when, loopWhen), calls)
	out := continueCFG(w, bodyPaths, rest)
	out = append(out, w.walkStmts(rest, when, calls)...)
	return capCFGPaths(out)
}

func (w *cfgWalker) walkFor(s *ast.ForStmt, rest []ast.Stmt, when, calls []string) []cfgPath {
	if s.Init != nil {
		calls = appendCFGCalls(calls, s.Init)
	}
	loopWhen := "for"
	if s.Cond != nil {
		loopWhen = w.exprString(s.Cond)
	}
	bodyPaths := w.walkStmts(bodyList(s.Body), appendWhen(when, loopWhen), calls)
	out := continueCFG(w, bodyPaths, rest)
	out = append(out, w.walkStmts(rest, when, calls)...)
	return capCFGPaths(out)
}

func continueCFG(w *cfgWalker, paths []cfgPath, rest []ast.Stmt) []cfgPath {
	if len(rest) == 0 {
		return paths
	}
	var out []cfgPath
	for _, p := range paths {
		if p.returns {
			out = append(out, p)
			continue
		}
		out = append(out, w.walkStmts(rest, p.when, p.calls)...)
	}
	return out
}

func (w *cfgWalker) caseCond(tag string, list []ast.Expr) string {
	parts := make([]string, 0, len(list))
	for _, e := range list {
		v := w.exprString(e)
		if v == "" {
			continue
		}
		if tag != "" {
			parts = append(parts, tag+" == "+v)
		} else {
			parts = append(parts, v)
		}
	}
	return strings.Join(parts, " || ")
}

func (w *cfgWalker) exprString(e ast.Expr) string {
	if e == nil {
		return ""
	}
	var buf bytes.Buffer
	if err := printer.Fprint(&buf, w.fset, e); err != nil {
		return ""
	}
	return compactCFGExpr(buf.String())
}

func (w *cfgWalker) stmtString(s ast.Stmt) string {
	if s == nil {
		return ""
	}
	var buf bytes.Buffer
	if err := printer.Fprint(&buf, w.fset, s); err != nil {
		return ""
	}
	return compactCFGExpr(buf.String())
}

func bodyList(b *ast.BlockStmt) []ast.Stmt {
	if b == nil {
		return nil
	}
	return b.List
}

func compactCFGExpr(s string) string {
	s = strings.ReplaceAll(s, "\r", " ")
	s = strings.ReplaceAll(s, "\n", " ")
	s = strings.ReplaceAll(s, "\t", " ")
	return strings.Join(strings.Fields(s), " ")
}

func negateCond(cond string) string {
	cond = strings.TrimSpace(cond)
	if cond == "" || cond == "default" || cond == "select" || cond == "for" {
		return cond
	}
	if strings.HasPrefix(cond, "range ") {
		return ""
	}
	if strings.HasPrefix(cond, "!(") && strings.HasSuffix(cond, ")") && balancedParens(cond[1:]) {
		return strings.TrimSpace(cond[2 : len(cond)-1])
	}
	if strings.HasPrefix(cond, "!") && !strings.HasPrefix(cond, "!=") {
		rest := strings.TrimSpace(cond[1:])
		if rest != "" {
			return rest
		}
	}
	if i := indexOp(cond, "!="); i >= 0 {
		return strings.TrimSpace(cond[:i]) + " == " + strings.TrimSpace(cond[i+2:])
	}
	if i := indexOp(cond, "=="); i >= 0 {
		return strings.TrimSpace(cond[:i]) + " != " + strings.TrimSpace(cond[i+2:])
	}
	return "!(" + cond + ")"
}

func indexOp(s, op string) int {
	depth := 0
	inString := false
	escaped := false
	for i := 0; i < len(s); i++ {
		c := s[i]
		if inString {
			if escaped {
				escaped = false
				continue
			}
			if c == '\\' {
				escaped = true
				continue
			}
			if c == '"' {
				inString = false
			}
			continue
		}
		switch c {
		case '"':
			inString = true
		case '(':
			depth++
		case ')':
			if depth > 0 {
				depth--
			}
		default:
			if depth == 0 && strings.HasPrefix(s[i:], op) {
				if i > 0 && unicode.IsLetter(rune(s[i-1])) {
					continue
				}
				return i
			}
		}
	}
	return -1
}

func balancedParens(s string) bool {
	depth := 0
	for _, r := range s {
		switch r {
		case '(':
			depth++
		case ')':
			depth--
			if depth < 0 {
				return false
			}
		}
	}
	return depth == 0
}

func appendWhen(when []string, cond string) []string {
	cond = strings.TrimSpace(cond)
	if cond == "" {
		return when
	}
	for _, w := range when {
		if w == cond {
			return when
		}
	}
	out := make([]string, len(when)+1)
	copy(out, when)
	out[len(when)] = cond
	return out
}

func appendCFGCalls(calls []string, n ast.Node) []string {
	if n == nil {
		return calls
	}
	extra := collectCFGCalls(n)
	if len(extra) == 0 {
		return calls
	}
	out := append([]string(nil), calls...)
	seen := make(map[string]struct{}, len(out))
	for _, c := range out {
		seen[c] = struct{}{}
	}
	for _, c := range extra {
		if _, ok := seen[c]; ok {
			continue
		}
		seen[c] = struct{}{}
		out = append(out, c)
	}
	return out
}

func collectCFGCalls(n ast.Node) []string {
	var out []string
	ast.Inspect(n, func(n ast.Node) bool {
		switch x := n.(type) {
		case *ast.FuncLit:
			return false
		case *ast.CallExpr:
			name := cfgCallName(x.Fun)
			if name == "" || skipCFGCall(x.Fun, name) {
				return true
			}
			out = append(out, name)
		}
		return true
	})
	return out
}

func cfgCallName(fun ast.Expr) string {
	switch f := fun.(type) {
	case *ast.Ident:
		return f.Name
	case *ast.SelectorExpr:
		return f.Sel.Name
	case *ast.IndexExpr:
		return cfgCallName(f.X)
	case *ast.IndexListExpr:
		return cfgCallName(f.X)
	case *ast.ParenExpr:
		return cfgCallName(f.X)
	default:
		return ""
	}
}

func skipCFGCall(fun ast.Expr, name string) bool {
	if _, ok := cfgSkipNames[name]; ok {
		return true
	}
	sel, ok := fun.(*ast.SelectorExpr)
	if !ok {
		return false
	}
	id, ok := sel.X.(*ast.Ident)
	if !ok {
		return false
	}
	_, skip := cfgSkipPkgs[id.Name]
	return skip
}

func cloneCFG(s []string) []string {
	if len(s) == 0 {
		return nil
	}
	return append([]string(nil), s...)
}

func capCFGPaths(paths []cfgPath) []cfgPath {
	if len(paths) <= maxCFGPaths {
		return paths
	}
	return paths[:maxCFGPaths]
}

func dedupCFGPaths(paths []cfgPath) []cfgPath {
	seen := map[string]struct{}{}
	out := make([]cfgPath, 0, len(paths))
	for _, p := range paths {
		key := strings.Join(p.when, "\x00") + "\x01" + strings.Join(p.calls, ",")
		if p.returns {
			key += "\x01R"
		}
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		out = append(out, p)
	}
	return out
}

func assignPathIDs(paths []cfgPath) []ControlFlowPath {
	out := make([]ControlFlowPath, 0, len(paths))
	for i, p := range paths {
		out = append(out, ControlFlowPath{
			ID:      "P" + itoaCFG(i+1),
			When:    p.when,
			Calls:   p.calls,
			Returns: p.returns,
		})
	}
	return out
}

func itoaCFG(n int) string {
	if n == 0 {
		return "0"
	}
	var b [12]byte
	i := len(b)
	for n > 0 {
		i--
		b[i] = byte('0' + n%10)
		n /= 10
	}
	return string(b[i:])
}
