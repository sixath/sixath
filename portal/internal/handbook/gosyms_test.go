package handbook

import (
	"strings"
	"testing"
)

func TestParseGoFile_SymbolsAndTables(t *testing.T) {
	src := `// Package repo stores things. Second sentence.
package repo

type Box[T any] struct{ v T }

func (b *Box[T]) Put(v T) { b.v = v }

func (Row) TableName() string { return "rows" }

type Row struct{}

func helper() int {
	return 1
}
`
	gi, err := parseGoFile("repo/box.go", []byte(src))
	if err != nil {
		t.Fatal(err)
	}
	if gi.Package != "repo" || gi.Doc != "Package repo stores things." {
		t.Fatalf("package = %q doc = %q", gi.Package, gi.Doc)
	}
	var names []string
	for _, s := range gi.Symbols {
		names = append(names, s.Kind+":"+s.Name)
	}
	if got := strings.Join(names, ","); got != "type:Box,method:(*Box).Put,method:(Row).TableName,type:Row,func:helper" {
		t.Fatalf("symbols = %s", got)
	}
	h := gi.Symbols[4]
	if h.Line != 12 || h.EndLine != 14 || len(h.BodyHash) != 12 {
		t.Fatalf("helper = %#v", h)
	}
	if len(gi.Tables) != 1 || gi.Tables[0].Name != "rows" || gi.Tables[0].Kind != RegTable || gi.Tables[0].Line != 8 {
		t.Fatalf("tables = %#v", gi.Tables)
	}
}

func TestParseGoFile_SyntaxError(t *testing.T) {
	if _, err := parseGoFile("x.go", []byte("package x\nfunc (")); err == nil {
		t.Fatal("want parse error")
	}
}

func TestParseGoFile_IgnoresLineDirectives(t *testing.T) {
	src := "package x\n\n//line other.go:100\nfunc f() {\n}\n"
	gi, err := parseGoFile("x.go", []byte(src))
	if err != nil {
		t.Fatal(err)
	}
	if len(gi.Symbols) != 1 || gi.Symbols[0].Line != 4 || gi.Symbols[0].EndLine != 5 {
		t.Fatalf("symbols = %#v", gi.Symbols)
	}
}

func TestParseGoMod(t *testing.T) {
	m := parseGoMod([]byte("module \"example.com/a\"\n\nrequire github.com/x/y v1.0.0\nrequire (\n\t// comment\n\tgithub.com/p/q v0.1.0\n\tgithub.com/z/z v1.0.0 // indirect\n)\n"))
	if m.Path != "example.com/a" || strings.Join(m.Requires, ",") != "github.com/x/y,github.com/p/q" {
		t.Fatalf("mod = %#v", m)
	}
}

func TestParseGoMod_TrailingComments(t *testing.T) {
	m := parseGoMod([]byte("module example.com/a // the service\n\nrequire github.com/x/y v1.0.0 // pinned\nrequire ( // direct deps\n\tgithub.com/p/q v0.1.0 // why\n)\n"))
	if m.Path != "example.com/a" || strings.Join(m.Requires, ",") != "github.com/x/y,github.com/p/q" {
		t.Fatalf("mod = %#v", m)
	}
}

func TestFirstSentence(t *testing.T) {
	cases := map[string]string{
		"Package a does x. And y.": "Package a does x.",
		"包 a 处理订单。其余说明。":           "包 a 处理订单。",
		"no period":                "no period",
		strings.Repeat("长", 200):   strings.Repeat("长", 120) + "…",
	}
	for in, want := range cases {
		if got := firstSentence(in); got != want {
			t.Fatalf("firstSentence(%q) = %q, want %q", in, got, want)
		}
	}
}
