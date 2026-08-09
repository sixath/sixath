package local

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

const (
	codegraphMaxFileBytes = 256 << 10
	codegraphMaxFiles     = 2000
)

// symbol patterns are intentionally simple (P3b — not a full AST graph).
var codeSymbolPatterns = []*regexp.Regexp{
	regexp.MustCompile(`(?m)^func\s+(?:\([^)]+\)\s+)?([A-Za-z_][\w]*)\s*\(`),          // Go
	regexp.MustCompile(`(?m)^(?:export\s+)?(?:async\s+)?function\s+([A-Za-z_][\w]*)\s*\(`), // JS/TS
	regexp.MustCompile(`(?m)^(?:export\s+)?(?:const|let|var)\s+([A-Za-z_][\w]*)\s*=`),
	regexp.MustCompile(`(?m)^(?:export\s+)?class\s+([A-Za-z_][\w]*)\b`),
	regexp.MustCompile(`(?m)^(?:export\s+)?(?:type|interface)\s+([A-Za-z_][\w]*)\b`),
	regexp.MustCompile(`(?m)^def\s+([A-Za-z_][\w]*)\s*\(`), // Python
	regexp.MustCompile(`(?m)^class\s+([A-Za-z_][\w]*)\b`),
}

// DirCodeGraph is a lightweight path+symbol index over a source tree (P3b).
// Capabilities flag remains code_graph; this is not a dependency graph engine.
type DirCodeGraph struct {
	Root string
}

// NewDirCodeGraph validates root exists and is a directory.
func NewDirCodeGraph(root string) (*DirCodeGraph, error) {
	root = strings.TrimSpace(root)
	if root == "" {
		return nil, fmt.Errorf("hub/local: codegraph root is empty")
	}
	abs, err := filepath.Abs(root)
	if err != nil {
		return nil, fmt.Errorf("hub/local: codegraph root: %w", err)
	}
	st, err := os.Stat(abs)
	if err != nil {
		return nil, fmt.Errorf("hub/local: codegraph root: %w", err)
	}
	if !st.IsDir() {
		return nil, fmt.Errorf("hub/local: codegraph root is not a directory: %s", abs)
	}
	return &DirCodeGraph{Root: abs}, nil
}

func (g *DirCodeGraph) Search(ctx context.Context, query string, limit int) ([]KnowledgeHit, error) {
	if g == nil || g.Root == "" {
		return nil, nil
	}
	q := strings.TrimSpace(query)
	if q == "" {
		return nil, nil
	}
	if limit <= 0 {
		limit = 5
	}
	ql := strings.ToLower(q)
	var hits []KnowledgeHit
	files := 0
	err := filepath.WalkDir(g.Root, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		if err := ctx.Err(); err != nil {
			return err
		}
		if d.IsDir() {
			name := d.Name()
			if name == ".git" || name == "node_modules" || name == "vendor" || name == "dist" || name == "bin" {
				return filepath.SkipDir
			}
			return nil
		}
		if !isCodeFile(d.Name()) {
			return nil
		}
		files++
		if files > codegraphMaxFiles {
			return errStopWalk
		}
		rel, err := filepath.Rel(g.Root, path)
		if err != nil {
			return nil
		}
		rel = filepath.ToSlash(rel)
		base := strings.ToLower(filepath.Base(path))
		pathHit := strings.Contains(strings.ToLower(rel), ql) || strings.Contains(base, ql)

		body, err := readCapped(path, codegraphMaxFileBytes)
		if err != nil {
			return nil
		}
		syms := extractSymbols(body)
		var matched []string
		for _, s := range syms {
			if strings.Contains(strings.ToLower(s), ql) {
				matched = append(matched, s)
			}
		}
		if !pathHit && len(matched) == 0 {
			return nil
		}
		content := formatCodeHit(rel, matched, pathHit)
		score := 0.4
		if pathHit {
			score += 0.3
		}
		if len(matched) > 0 {
			score += 0.3
		}
		hits = append(hits, KnowledgeHit{
			ID:      rel,
			Source:  "codegraph",
			Content: content,
			Score:   score,
		})
		if len(hits) >= limit {
			return errStopWalk
		}
		return nil
	})
	if err != nil && err != errStopWalk {
		return hits, err
	}
	return hits, nil
}

// Read returns a capped file body plus extracted symbol names for id=rel path.
func (g *DirCodeGraph) Read(_ context.Context, id string) (*KnowledgeHit, error) {
	if g == nil || g.Root == "" {
		return nil, fmt.Errorf("hub/local: codegraph not configured")
	}
	rel := strings.TrimSpace(strings.ReplaceAll(id, "\\", "/"))
	if rel == "" || strings.Contains(rel, "..") {
		return nil, fmt.Errorf("hub/local: invalid codegraph id")
	}
	full := filepath.Join(g.Root, filepath.FromSlash(rel))
	absFull, err := filepath.Abs(full)
	if err != nil {
		return nil, err
	}
	if !pathUnderRoot(g.Root, absFull) {
		return nil, fmt.Errorf("hub/local: codegraph path escapes root")
	}
	body, err := readCapped(absFull, codegraphMaxFileBytes)
	if err != nil {
		return nil, err
	}
	syms := extractSymbols(body)
	var b strings.Builder
	b.WriteString(rel)
	if len(syms) > 0 {
		b.WriteString("\nsymbols: ")
		b.WriteString(strings.Join(syms, ", "))
	}
	b.WriteString("\n\n")
	b.WriteString(body)
	return &KnowledgeHit{ID: rel, Source: "codegraph", Content: b.String(), Score: 1}, nil
}

func isCodeFile(name string) bool {
	ext := strings.ToLower(filepath.Ext(name))
	switch ext {
	case ".go", ".ts", ".tsx", ".js", ".jsx", ".py", ".java", ".rs", ".kt":
		return true
	default:
		return false
	}
}

func extractSymbols(body string) []string {
	seen := map[string]struct{}{}
	var out []string
	for _, re := range codeSymbolPatterns {
		for _, m := range re.FindAllStringSubmatch(body, 40) {
			if len(m) < 2 {
				continue
			}
			name := m[1]
			if name == "" {
				continue
			}
			if _, ok := seen[name]; ok {
				continue
			}
			seen[name] = struct{}{}
			out = append(out, name)
			if len(out) >= 80 {
				return out
			}
		}
	}
	return out
}

func formatCodeHit(rel string, matched []string, pathHit bool) string {
	var b strings.Builder
	b.WriteString(rel)
	if pathHit {
		b.WriteString(" [path]")
	}
	if len(matched) > 0 {
		b.WriteString("\nsymbols: ")
		if len(matched) > 8 {
			matched = matched[:8]
		}
		b.WriteString(strings.Join(matched, ", "))
	}
	return b.String()
}

var _ CodeGraphSearcher = (*DirCodeGraph)(nil)
