package tool

import (
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"strings"
)

const (
	callGraphMaxCallees  = 24
	callGraphMaxSiblings = 32
	callGraphMaxFileSize = 1 << 20
)

// CallGraph is a language-agnostic caller→callee sketch for the current read.
type CallGraph struct {
	Language string          `json:"language,omitempty"`
	Nodes    []CallGraphNode `json:"nodes,omitempty"`
	Edges    []CallGraphEdge `json:"edges,omitempty"`
}

// CallGraphNode is one function in the sketch.
type CallGraphNode struct {
	ID        string `json:"id"`
	Kind      string `json:"kind,omitempty"`
	Name      string `json:"name"`
	File      string `json:"file,omitempty"`
	StartLine int    `json:"start_line,omitempty"`
	EndLine   int    `json:"end_line,omitempty"`
	Resolved  bool   `json:"resolved"`
}

// CallGraphEdge is a call from one function to another.
type CallGraphEdge struct {
	From string   `json:"from"`
	To   string   `json:"to"`
	When []string `json:"when,omitempty"`
}

type funcSpan struct {
	name  string
	file  string
	start int
	end   int
}

// BuildCallGraph resolves CFG callees in the same file / same directory.
// Non-Go or empty CFG returns nil. Missing callees stay as unresolved nodes.
func BuildCallGraph(src []byte, absFile, relFile string, cf []ControlFlowFunc) *CallGraph {
	if len(cf) == 0 || controlFlowLanguage(relFile) != "go" {
		return nil
	}
	relFile = filepath.ToSlash(relFile)
	index := map[string]funcSpan{}
	indexGoFuncs(index, src, relFile)
	indexSiblingGoFuncs(index, absFile, relFile)

	nodes := map[string]CallGraphNode{}
	var edges []CallGraphEdge
	addNode := func(sp funcSpan, resolved bool) string {
		id := sp.name
		if resolved && sp.file != "" {
			id = sp.name + "@" + sp.file
		}
		if _, ok := nodes[id]; !ok {
			nodes[id] = CallGraphNode{
				ID:        id,
				Kind:      "function",
				Name:      sp.name,
				File:      sp.file,
				StartLine: sp.start,
				EndLine:   sp.end,
				Resolved:  resolved,
			}
		}
		return id
	}

	calleeCount := 0
	for _, fn := range cf {
		from := addNode(funcSpan{name: fn.Function, file: relFile, start: fn.StartLine, end: fn.EndLine}, true)
		seenCall := map[string]struct{}{}
		for _, name := range uniqueCFGCalls(fn) {
			if _, ok := seenCall[name]; ok {
				continue
			}
			seenCall[name] = struct{}{}
			if calleeCount >= callGraphMaxCallees {
				break
			}
			calleeCount++
			sp, ok := index[name]
			var to string
			if ok {
				to = addNode(sp, true)
			} else {
				to = addNode(funcSpan{name: name}, false)
			}
			edges = append(edges, CallGraphEdge{
				From: from,
				To:   to,
				When: gatedWhenForCFGCall(fn, name),
			})
		}
	}
	if len(nodes) == 0 {
		return nil
	}
	out := &CallGraph{Language: "go", Edges: edges}
	for _, n := range nodes {
		out.Nodes = append(out.Nodes, n)
	}
	return out
}

func uniqueCFGCalls(fn ControlFlowFunc) []string {
	seen := map[string]struct{}{}
	var out []string
	for _, p := range fn.Paths {
		for _, c := range p.Calls {
			if _, ok := seen[c]; ok {
				continue
			}
			seen[c] = struct{}{}
			out = append(out, c)
		}
	}
	return out
}

func gatedWhenForCFGCall(fn ControlFlowFunc, name string) []string {
	if len(fn.Paths) == 0 {
		return nil
	}
	allHave := true
	var whens []string
	seen := map[string]struct{}{}
	for _, p := range fn.Paths {
		has := false
		for _, c := range p.Calls {
			if c == name {
				has = true
				break
			}
		}
		if !has {
			allHave = false
			continue
		}
		for _, w := range p.When {
			if _, ok := seen[w]; ok {
				continue
			}
			seen[w] = struct{}{}
			whens = append(whens, w)
		}
	}
	if allHave {
		return nil
	}
	if len(whens) > 4 {
		return whens[:4]
	}
	return whens
}

func indexGoFuncs(index map[string]funcSpan, src []byte, relFile string) {
	fset := token.NewFileSet()
	parsed, err := parser.ParseFile(fset, relFile, src, parser.SkipObjectResolution)
	if err != nil || parsed == nil {
		return
	}
	for _, decl := range parsed.Decls {
		fn, ok := decl.(*ast.FuncDecl)
		if !ok || fn.Name == nil {
			continue
		}
		name := fn.Name.Name
		if name == "" {
			continue
		}
		if _, exists := index[name]; exists {
			continue
		}
		index[name] = funcSpan{
			name:  name,
			file:  relFile,
			start: fset.Position(fn.Pos()).Line,
			end:   fset.Position(fn.End()).Line,
		}
	}
}

func indexSiblingGoFuncs(index map[string]funcSpan, absFile, relFile string) {
	if strings.TrimSpace(absFile) == "" {
		return
	}
	dir := filepath.Dir(absFile)
	ents, err := os.ReadDir(dir)
	if err != nil {
		return
	}
	relDir := filepath.ToSlash(filepath.Dir(relFile))
	if relDir == "." {
		relDir = ""
	}
	self := filepath.Base(absFile)
	n := 0
	for _, e := range ents {
		if n >= callGraphMaxSiblings {
			break
		}
		if e.IsDir() {
			continue
		}
		name := e.Name()
		if name == self || !strings.HasSuffix(strings.ToLower(name), ".go") {
			continue
		}
		if strings.HasSuffix(name, "_test.go") || strings.HasSuffix(name, "_gen.go") {
			continue
		}
		full := filepath.Join(dir, name)
		info, err := e.Info()
		if err != nil || info.Size() > callGraphMaxFileSize {
			continue
		}
		b, err := os.ReadFile(full)
		if err != nil {
			continue
		}
		rel := name
		if relDir != "" {
			rel = relDir + "/" + name
		}
		indexGoFuncs(index, b, rel)
		n++
	}
}
