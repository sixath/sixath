package tool

import (
	"bufio"
	"fmt"
	"go/scanner"
	"go/token"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"unicode/utf16"

	"github.com/sixath/framework/tool/lsp"
)

const rcaSymbolScanMaxToken = 1024 * 1024 // 1MiB; default bufio scanner is 64KiB

var rcaSymbolSkipDirNames = map[string]struct{}{
	".git":         {},
	".hg":          {},
	".svn":         {},
	".idea":        {},
	".vscode":      {},
	"node_modules": {},
	"vendor":       {},
}

type symbolCandidate struct {
	location lsp.Location
	tier     int
}

// resolveSymbolCandidates finds symbol occurrences in Go files under one guarded repo root.
// The returned unique value indicates whether the best candidate can be used for LSP navigation.
func resolveSymbolCandidates(roots []string, repo, symbol string, maxResults int) ([]lsp.Location, bool, bool, error) {
	root, err := resolveRCASymbolRoot(roots, repo)
	if err != nil {
		return nil, false, false, err
	}

	pkgPart, namePart := splitRCASymbol(symbol)
	if namePart == "" {
		return nil, false, false, fmt.Errorf("symbol is required")
	}
	wordPattern := regexp.MustCompile(`\b` + regexp.QuoteMeta(namePart) + `\b`)
	declarationPattern := regexp.MustCompile(`^\s*(?:func\s+(?:\([^)]*\)\s+)?|type\s+)` + regexp.QuoteMeta(namePart) + `\b`)
	qualifiedPattern := (*regexp.Regexp)(nil)
	if pkgPart != "" {
		qualifiedPattern = regexp.MustCompile(`\b` + regexp.QuoteMeta(pkgPart) + `\.` + regexp.QuoteMeta(namePart) + `\b`)
	}

	var candidates []symbolCandidate
	err = filepath.WalkDir(root, func(full string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if entry.IsDir() {
			name := entry.Name()
			if _, skip := rcaSymbolSkipDirNames[name]; skip {
				return fs.SkipDir
			}
			return nil
		}
		if filepath.Ext(entry.Name()) != ".go" {
			return nil
		}
		rel, err := filepath.Rel(root, full)
		if err != nil {
			return err
		}
		guarded, _, err := resolveInRepos(roots, repo, rel)
		if err != nil || guarded != full {
			return nil
		}
		return collectRCASymbolCandidates(full, filepath.ToSlash(rel), repo, namePart, pkgPart, wordPattern, declarationPattern, qualifiedPattern, &candidates)
	})
	if err != nil {
		return nil, false, false, fmt.Errorf("search symbol %q: %w", symbol, err)
	}
	if len(candidates) == 0 {
		return nil, false, false, nil
	}

	sort.Slice(candidates, func(i, j int) bool {
		left, right := candidates[i], candidates[j]
		if left.tier != right.tier {
			return left.tier < right.tier
		}
		if left.location.File != right.location.File {
			return left.location.File < right.location.File
		}
		if left.location.Line != right.location.Line {
			return left.location.Line < right.location.Line
		}
		return left.location.Character < right.location.Character
	})

	unique := len(candidates) == 1 || (candidates[0].tier == 1 && candidates[1].tier > 1)
	if maxResults <= 0 {
		maxResults = rcaSymbolMaxResultsDefault
	}
	truncated := len(candidates) > maxResults
	if truncated {
		candidates = candidates[:maxResults]
	}
	locations := make([]lsp.Location, len(candidates))
	for i, candidate := range candidates {
		locations[i] = candidate.location
	}
	return locations, unique, truncated, nil
}

func resolveRCASymbolRoot(roots []string, repo string) (string, error) {
	root, _, err := resolveInRepos(roots, repo, ".")
	return root, err
}

func splitRCASymbol(symbol string) (pkgPart, namePart string) {
	symbol = strings.TrimSpace(symbol)
	if index := strings.LastIndex(symbol, "."); index >= 0 {
		return strings.TrimSpace(symbol[:index]), strings.TrimSpace(symbol[index+1:])
	}
	return "", symbol
}

func collectRCASymbolCandidates(full, rel, repo, namePart, pkgPart string, wordPattern, declarationPattern, qualifiedPattern *regexp.Regexp, candidates *[]symbolCandidate) error {
	file, err := os.Open(full)
	if err != nil {
		return err
	}
	defer file.Close()

	pathMatchesPkg := pkgPart != "" && strings.Contains(strings.ToLower(rel), strings.ToLower(pkgPart))
	lineScanner := bufio.NewScanner(file)
	lineScanner.Buffer(make([]byte, 64*1024), rcaSymbolScanMaxToken)
	for line := 1; lineScanner.Scan(); line++ {
		text := lineScanner.Text()
		if len(text) > rcaSymbolScanMaxToken {
			continue
		}
		matches := wordPattern.FindAllStringIndex(text, -1)
		identifierOffsets := rcaSymbolIdentifierOffsets(text, namePart)
		for _, match := range matches {
			tier := 4
			switch {
			case declarationPattern.MatchString(text):
				tier = 1
			case identifierOffsets[match[0]]:
				tier = 2
			case pathMatchesPkg || strings.Contains(text, "package "+pkgPart):
				tier = 3
			}
			*candidates = append(*candidates, symbolCandidate{
				location: lsp.Location{
					Repo: repo, File: rel, Line: line, Character: utf16Length(text[:match[0]]), Name: namePart,
				},
				tier: tier,
			})
		}
	}
	if err := lineScanner.Err(); err != nil {
		// Oversized lines are skipped via a larger buffer; treat residual scan errors as non-fatal for this file.
		if err == bufio.ErrTooLong {
			return nil
		}
		return err
	}
	return nil
}

// findNearestGoModuleRoot walks upward from absFile's directory within repoRoot
// and returns the nearest directory containing go.mod. Falls back to repoRoot.
func findNearestGoModuleRoot(repoRoot, absFile string) string {
	repoRoot = filepath.Clean(repoRoot)
	dir := filepath.Clean(filepath.Dir(absFile))
	for {
		rel, err := filepath.Rel(repoRoot, dir)
		if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
			return repoRoot
		}
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return dir
		}
		if lsp.NormalizeRoot(dir) == lsp.NormalizeRoot(repoRoot) {
			return repoRoot
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return repoRoot
		}
		dir = parent
	}
}

// snapRCASymbolCharacter moves character 0 to a useful Go identifier on the line
// (preferName if present, else the first identifier). Best-effort: returns the
// input character when the file/line cannot be read or no identifier exists.
func snapRCASymbolCharacter(absFile string, line, character int, preferName string) int {
	if character != 0 || line < 1 || strings.TrimSpace(absFile) == "" {
		return character
	}
	f, err := os.Open(absFile)
	if err != nil {
		return character
	}
	defer f.Close()

	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 64*1024), rcaSymbolScanMaxToken)
	for current := 1; sc.Scan(); current++ {
		if current != line {
			continue
		}
		text := sc.Text()
		if preferName != "" {
			if idx := indexGoIdentifier(text, preferName); idx >= 0 {
				return utf16Length(text[:idx])
			}
		}
		if idx := firstGoIdentifierOffset(text); idx >= 0 {
			return utf16Length(text[:idx])
		}
		return character
	}
	return character
}

func indexGoIdentifier(text, name string) int {
	if name == "" {
		return -1
	}
	fileSet := token.NewFileSet()
	file := fileSet.AddFile("", -1, len(text))
	var lexer scanner.Scanner
	lexer.Init(file, []byte(text), nil, 0)
	for {
		pos, tok, lit := lexer.Scan()
		if tok == token.EOF {
			return -1
		}
		if tok == token.IDENT && lit == name {
			return file.Offset(pos)
		}
	}
}

func firstGoIdentifierOffset(text string) int {
	fileSet := token.NewFileSet()
	file := fileSet.AddFile("", -1, len(text))
	var lexer scanner.Scanner
	lexer.Init(file, []byte(text), nil, 0)
	for {
		pos, tok, _ := lexer.Scan()
		if tok == token.EOF {
			return -1
		}
		if tok == token.IDENT {
			return file.Offset(pos)
		}
	}
}

func firstGoIdentifierName(text string) string {
	fileSet := token.NewFileSet()
	file := fileSet.AddFile("", -1, len(text))
	var lexer scanner.Scanner
	lexer.Init(file, []byte(text), nil, 0)
	for {
		_, tok, lit := lexer.Scan()
		if tok == token.EOF {
			return ""
		}
		if tok == token.IDENT {
			return lit
		}
	}
}

func goIdentifierOnLine(absFile string, line int) string {
	if line < 1 || strings.TrimSpace(absFile) == "" {
		return ""
	}
	f, err := os.Open(absFile)
	if err != nil {
		return ""
	}
	defer f.Close()
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 64*1024), rcaSymbolScanMaxToken)
	for current := 1; sc.Scan(); current++ {
		if current != line {
			continue
		}
		return firstGoIdentifierName(sc.Text())
	}
	return ""
}

// remapLocationsToRepoRoot converts module-relative (or absolute) LSP locations
// into paths relative to the configured RCA repository root.
func remapLocationsToRepoRoot(repoRoot, moduleRoot string, locations []lsp.Location) []lsp.Location {
	repoRoot = filepath.Clean(repoRoot)
	moduleRoot = filepath.Clean(moduleRoot)
	out := make([]lsp.Location, 0, len(locations))
	for _, loc := range locations {
		abs := loc.File
		if !filepath.IsAbs(abs) {
			abs = filepath.Join(moduleRoot, filepath.FromSlash(loc.File))
		}
		rel, err := filepath.Rel(repoRoot, abs)
		if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
			out = append(out, loc)
			continue
		}
		loc.File = filepath.ToSlash(rel)
		out = append(out, loc)
	}
	return out
}

func utf16Length(value string) int {
	return len(utf16.Encode([]rune(value)))
}

func rcaSymbolIdentifierOffsets(text, name string) map[int]bool {
	fileSet := token.NewFileSet()
	file := fileSet.AddFile("", -1, len(text))
	var lexer scanner.Scanner
	lexer.Init(file, []byte(text), nil, scanner.ScanComments)

	offsets := make(map[int]bool)
	for {
		position, tokenType, literal := lexer.Scan()
		if tokenType == token.EOF {
			return offsets
		}
		if tokenType == token.IDENT && literal == name {
			offsets[file.Offset(position)] = true
		}
	}
}
