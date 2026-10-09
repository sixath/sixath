package handbook

import (
	"crypto/sha256"
	"encoding/hex"
	"go/ast"
	"go/parser"
	"go/token"
	"strconv"
	"strings"
)

// Symbol is one top-level Go declaration.
type Symbol struct {
	Name     string `json:"name"` // "Func", "(*T).Method" or "T"
	Kind     string `json:"kind"` // func | method | type
	Line     int    `json:"line"`
	EndLine  int    `json:"end_line"`
	BodyHash string `json:"body_hash,omitempty"` // first 12 hex chars of sha256 of the declaration
}

// GoModule is the module path and direct requirements of go.mod.
type GoModule struct {
	Path     string   `json:"path"`
	Requires []string `json:"requires,omitempty"`
}

type goFileInfo struct {
	Package string
	Doc     string
	Symbols []Symbol
	Tables  []RegisterHit
}

func parseGoFile(rel string, src []byte) (*goFileInfo, error) {
	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, rel, src, parser.ParseComments|parser.SkipObjectResolution)
	if err != nil {
		return nil, err
	}
	info := &goFileInfo{Package: f.Name.Name}
	if f.Doc != nil {
		info.Doc = firstSentence(f.Doc.Text())
	}
	for _, decl := range f.Decls {
		switch d := decl.(type) {
		case *ast.FuncDecl:
			start, end := fset.Position(d.Pos()), fset.Position(d.End())
			s := Symbol{Name: d.Name.Name, Kind: "func", Line: start.Line, EndLine: end.Line, BodyHash: shortHash(src[start.Offset:end.Offset])}
			if d.Recv != nil && len(d.Recv.List) > 0 {
				s.Kind = "method"
				s.Name = "(" + recvString(d.Recv.List[0].Type) + ")." + d.Name.Name
				if d.Name.Name == "TableName" {
					if lit := returnedStringLiteral(d); lit != "" {
						info.Tables = append(info.Tables, RegisterHit{Kind: RegTable, Name: lit, Access: AccessRef, Path: rel, Line: start.Line})
					}
				}
			}
			info.Symbols = append(info.Symbols, s)
		case *ast.GenDecl:
			if d.Tok != token.TYPE {
				continue
			}
			for _, sp := range d.Specs {
				ts, ok := sp.(*ast.TypeSpec)
				if !ok {
					continue
				}
				start, end := fset.Position(ts.Pos()), fset.Position(ts.End())
				info.Symbols = append(info.Symbols, Symbol{Name: ts.Name.Name, Kind: "type", Line: start.Line, EndLine: end.Line})
			}
		}
	}
	return info, nil
}

func recvString(e ast.Expr) string {
	switch t := e.(type) {
	case *ast.StarExpr:
		return "*" + recvString(t.X)
	case *ast.Ident:
		return t.Name
	case *ast.IndexExpr:
		return recvString(t.X)
	case *ast.IndexListExpr:
		return recvString(t.X)
	case *ast.ParenExpr:
		return recvString(t.X)
	}
	return "?"
}

func returnedStringLiteral(fn *ast.FuncDecl) string {
	if fn.Body == nil || len(fn.Body.List) != 1 {
		return ""
	}
	ret, ok := fn.Body.List[0].(*ast.ReturnStmt)
	if !ok || len(ret.Results) != 1 {
		return ""
	}
	lit, ok := ret.Results[0].(*ast.BasicLit)
	if !ok || lit.Kind != token.STRING {
		return ""
	}
	s, err := strconv.Unquote(lit.Value)
	if err != nil {
		return ""
	}
	return s
}

func shortHash(b []byte) string {
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:])[:12]
}

// firstSentence collapses whitespace and keeps the first sentence, capped at 120 runes.
func firstSentence(s string) string {
	s = strings.Join(strings.Fields(s), " ")
	if i := strings.Index(s, ". "); i >= 0 {
		s = s[:i+1]
	}
	if i := strings.Index(s, "。"); i >= 0 {
		s = s[:i+len("。")]
	}
	if r := []rune(s); len(r) > 120 {
		s = string(r[:120]) + "…"
	}
	return s
}

func parseGoMod(src []byte) *GoModule {
	m := &GoModule{}
	inRequire := false
	for _, raw := range strings.Split(string(src), "\n") {
		line := strings.TrimSpace(raw)
		switch {
		case strings.HasPrefix(line, "module "):
			m.Path = strings.Trim(strings.TrimSpace(strings.TrimPrefix(line, "module ")), `"`)
		case strings.HasPrefix(line, "require") && strings.HasSuffix(line, "("):
			inRequire = true
		case inRequire && line == ")":
			inRequire = false
		case inRequire || strings.HasPrefix(line, "require "):
			line = strings.TrimSpace(strings.TrimPrefix(line, "require "))
			if line == "" || strings.HasPrefix(line, "//") || strings.Contains(line, "// indirect") {
				continue
			}
			if fields := strings.Fields(line); len(fields) > 0 {
				m.Requires = append(m.Requires, fields[0])
			}
		}
	}
	return m
}
