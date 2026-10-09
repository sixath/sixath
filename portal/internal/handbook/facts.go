// Package handbook builds deterministic repository handbooks: facts gathered by walking the
// repository (file inventory, Go symbols, register candidates) rendered as an agent Skill.
package handbook

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strings"
)

const (
	// MaxFileBytes skips larger files; they are almost always data or generated code.
	MaxFileBytes = 512 << 10
	// MaxFiles caps the walk so one huge repository cannot stall the rebuild loop.
	MaxFiles   = 20000
	sniffBytes = 8000
)

var skipDirNames = map[string]bool{
	"vendor": true, "node_modules": true, "third_party": true, "testdata": true,
	"dist": true, "build": true, "target": true, "out": true,
}

var skipFileNames = map[string]bool{
	"go.sum": true, "package-lock.json": true, "yarn.lock": true, "pnpm-lock.yaml": true,
}

var langByExt = map[string]string{
	".go": "go", ".proto": "proto", ".sql": "sql", ".py": "python", ".java": "java",
	".js": "javascript", ".jsx": "javascript", ".ts": "typescript", ".tsx": "typescript",
	".sh": "shell", ".lua": "lua", ".c": "c", ".h": "c", ".cc": "cpp", ".cpp": "cpp", ".hpp": "cpp",
	".rs": "rust", ".php": "php", ".rb": "ruby", ".kt": "kotlin", ".cs": "csharp", ".scala": "scala",
	".yaml": "yaml", ".yml": "yaml", ".json": "json", ".toml": "toml", ".xml": "xml",
	".md": "markdown", ".html": "html", ".css": "css", ".vue": "vue",
}

// File is one source file kept in the handbook.
type File struct {
	Path  string `json:"path"` // slash-separated, relative to the repository root
	Lang  string `json:"lang"`
	Size  int64  `json:"size"`
	Lines int    `json:"lines"`
	Hash  string `json:"hash"` // sha256 of the content, hex
	Test  bool   `json:"test,omitempty"`
}

// GoPackage summarizes one Go package directory.
type GoPackage struct {
	Dir   string `json:"dir"`
	Name  string `json:"name"`
	Doc   string `json:"doc,omitempty"`
	Files int    `json:"files"`
	Main  bool   `json:"main,omitempty"`
}

// Coverage records what the walk skipped or could not analyze.
type Coverage struct {
	SkippedLarge     int      `json:"skipped_large"`
	SkippedBinary    int      `json:"skipped_binary"`
	SkippedGenerated int      `json:"skipped_generated"`
	Truncated        bool     `json:"truncated"`
	Unreadable       []string `json:"unreadable,omitempty"`
	GoParseErrors    []string `json:"go_parse_errors,omitempty"`
	Graph            string   `json:"graph"`
}

// Facts are the deterministic facts of one repository checkout.
type Facts struct {
	Files     []File              `json:"files"`
	Packages  []GoPackage         `json:"packages"`
	Symbols   map[string][]Symbol `json:"symbols"`
	Registers []RegisterHit       `json:"registers"`
	Module    *GoModule           `json:"module,omitempty"`
	Coverage  Coverage            `json:"coverage"`
}

// CollectFacts walks root once and gathers the facts of every kept file.
func CollectFacts(ctx context.Context, root string) (*Facts, error) {
	return collectFacts(ctx, root, MaxFiles)
}

func collectFacts(ctx context.Context, root string, maxFiles int) (*Facts, error) {
	f := &Facts{Symbols: map[string][]Symbol{}, Coverage: Coverage{Graph: "none"}}
	pkgs := map[string]*GoPackage{}
	err := filepath.WalkDir(root, func(p string, d fs.DirEntry, err error) error {
		rel, relErr := filepath.Rel(root, p)
		if relErr != nil {
			return relErr
		}
		rel = filepath.ToSlash(rel)
		if err != nil {
			if rel == "." {
				return err
			}
			f.Coverage.Unreadable = append(f.Coverage.Unreadable, rel)
			if d != nil && d.IsDir() {
				return fs.SkipDir
			}
			return nil
		}
		if d.IsDir() {
			if rel != "." && (strings.HasPrefix(d.Name(), ".") || skipDirNames[d.Name()]) {
				return fs.SkipDir
			}
			return nil
		}
		if !d.Type().IsRegular() || strings.HasPrefix(d.Name(), ".") || skipFileNames[d.Name()] {
			return nil
		}
		if len(f.Files) >= maxFiles {
			f.Coverage.Truncated = true
			return fs.SkipAll
		}
		if len(f.Files)%200 == 0 {
			if err := ctx.Err(); err != nil {
				return err
			}
		}
		return f.addFile(p, rel, d, pkgs)
	})
	if err != nil {
		return nil, err
	}
	for _, pk := range pkgs {
		f.Packages = append(f.Packages, *pk)
	}
	sort.Slice(f.Packages, func(i, j int) bool { return f.Packages[i].Dir < f.Packages[j].Dir })
	f.Registers = dedupeRegisters(f.Registers)
	sortRegisters(f.Registers)
	return f, nil
}

func (f *Facts) addFile(abs, rel string, d fs.DirEntry, pkgs map[string]*GoPackage) error {
	info, err := d.Info()
	if err != nil {
		f.Coverage.Unreadable = append(f.Coverage.Unreadable, rel)
		return nil
	}
	if info.Size() > MaxFileBytes {
		f.Coverage.SkippedLarge++
		return nil
	}
	src, err := os.ReadFile(abs)
	if err != nil {
		f.Coverage.Unreadable = append(f.Coverage.Unreadable, rel)
		return nil
	}
	if bytes.IndexByte(src[:min(len(src), sniffBytes)], 0) >= 0 {
		f.Coverage.SkippedBinary++
		return nil
	}
	if isGenerated(rel, src) {
		f.Coverage.SkippedGenerated++
		return nil
	}
	sum := sha256.Sum256(src)
	file := File{
		Path: rel, Lang: langOf(rel), Size: int64(len(src)), Lines: countLines(src),
		Hash: hex.EncodeToString(sum[:]), Test: isTestFile(rel),
	}
	f.Files = append(f.Files, file)
	if rel == "go.mod" {
		f.Module = parseGoMod(src)
	}
	if file.Test {
		return nil
	}
	if file.Lang == "go" {
		gi, err := parseGoFile(rel, src)
		if err != nil {
			f.Coverage.GoParseErrors = append(f.Coverage.GoParseErrors, rel)
		} else {
			if len(gi.Symbols) > 0 {
				f.Symbols[rel] = gi.Symbols
			}
			f.Registers = append(f.Registers, gi.Tables...)
			dir := path.Dir(rel)
			pk := pkgs[dir]
			if pk == nil {
				pk = &GoPackage{Dir: dir, Name: gi.Package}
				pkgs[dir] = pk
			}
			pk.Files++
			if pk.Doc == "" {
				pk.Doc = gi.Doc
			}
			if gi.Package == "main" {
				pk.Main = true
			}
		}
	}
	f.Registers = append(f.Registers, scanRegisters(rel, file.Lang, src)...)
	return nil
}

func isGenerated(rel string, src []byte) bool {
	base := path.Base(rel)
	if strings.HasSuffix(base, ".pb.go") || strings.HasSuffix(base, "_gen.go") ||
		strings.HasPrefix(base, "zz_generated") || strings.HasSuffix(base, ".min.js") {
		return true
	}
	head := src[:min(len(src), 2048)]
	return bytes.Contains(head, []byte("Code generated")) && bytes.Contains(head, []byte("DO NOT EDIT"))
}

func isTestFile(rel string) bool {
	base := path.Base(rel)
	return strings.HasSuffix(base, "_test.go") || strings.Contains(base, ".test.") ||
		strings.Contains(base, ".spec.") || strings.HasPrefix(base, "test_")
}

func langOf(rel string) string {
	if l, ok := langByExt[strings.ToLower(path.Ext(rel))]; ok {
		return l
	}
	return "other"
}

func countLines(b []byte) int {
	if len(b) == 0 {
		return 0
	}
	n := bytes.Count(b, []byte{'\n'})
	if b[len(b)-1] != '\n' {
		n++
	}
	return n
}
