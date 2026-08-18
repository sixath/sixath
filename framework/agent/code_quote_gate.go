package agent

import (
	"encoding/json"
	"fmt"
	"regexp"
	"strings"
	"unicode"
)

const (
	toolRCARead = "rca_read"
	toolRCAGrep = "rca_grep"
)

// CodeQuoteSource is one rca_read / rca_grep payload used as ground truth.
type CodeQuoteSource struct {
	Path    string
	Content string
}

const codeQuoteGateSoftPrompt = `源码引用与 rca_read 原文不一致（伪源码或漏掉了 if/else/return）。请从下面的工具原文连续摘抄，不要把不相邻的语句拼在一起；声称会调用/写库时必须带上包围它的条件。

The quoted code dropped a control-flow guard. Re-quote rca_read verbatim including the enclosing if/else/return.

Missing context:
%s`

var (
	codeFenceRE = regexp.MustCompile("(?s)```(?:[a-zA-Z0-9_+-]+)?\\r?\\n(.*?)```")
	callNameRE  = regexp.MustCompile(`\b([A-Za-z_][A-Za-z0-9_]*)\s*\(`)
)

var skipCallNames = map[string]struct{}{
	"if": {}, "for": {}, "switch": {}, "select": {}, "return": {},
	"make": {}, "len": {}, "cap": {}, "append": {}, "copy": {}, "delete": {},
	"new": {}, "close": {}, "panic": {}, "recover": {}, "range": {},
	"fmt": {}, "strings": {}, "strconv": {}, "errors": {},
}

// EvaluateCodeQuoteGate checks fenced quotes in finalText against rca_read/grep sources.
// No sources or no fences → allow. Reconstructed / dropped-if quotes → Soft inject.
func EvaluateCodeQuoteGate(sources []CodeQuoteSource, finalText string) EvidenceGateResult {
	if len(sources) == 0 {
		return EvidenceGateResult{Allow: true}
	}
	fences := extractCodeFences(finalText)
	if len(fences) == 0 {
		return EvidenceGateResult{Allow: true}
	}

	parsed := make([]parsedSource, 0, len(sources))
	for _, src := range sources {
		p := parseCodeQuoteSource(src)
		if len(p.lines) == 0 {
			continue
		}
		parsed = append(parsed, p)
	}
	if len(parsed) == 0 {
		return EvidenceGateResult{Allow: true}
	}

	var missing []string
	for _, fence := range fences {
		if reason, ok := fenceMatchesSources(fence, parsed); !ok {
			if reason != "" {
				missing = append(missing, reason)
			}
		}
	}
	if len(missing) == 0 {
		return EvidenceGateResult{Allow: true}
	}
	return EvidenceGateResult{
		Allow:  false,
		Action: "inject",
		Reason: "code quote mismatch",
		Prompt: fmt.Sprintf(codeQuoteGateSoftPrompt, strings.Join(uniqueStrings(missing), "\n")),
	}
}

type srcLine struct {
	raw    string
	norm   string
	ifCond string
}

type parsedSource struct {
	path  string
	lines []srcLine
}

func parseCodeQuoteSource(src CodeQuoteSource) parsedSource {
	rawLines := splitSourceLines(src.Content)
	conds := annotateEnclosingIfs(rawLines)
	lines := make([]srcLine, 0, len(rawLines))
	for i, raw := range rawLines {
		norm := normalizeCodeLine(raw)
		if norm == "" {
			continue
		}
		cond := ""
		if i < len(conds) {
			cond = conds[i]
		}
		lines = append(lines, srcLine{raw: raw, norm: norm, ifCond: cond})
	}
	return parsedSource{path: src.Path, lines: lines}
}

func splitSourceLines(content string) []string {
	content = strings.ReplaceAll(content, "\r\n", "\n")
	content = strings.ReplaceAll(content, "\r", "\n")
	raw := strings.Split(content, "\n")
	out := make([]string, 0, len(raw))
	for _, line := range raw {
		out = append(out, stripReadLinePrefix(line))
	}
	return out
}

func stripReadLinePrefix(line string) string {
	line = strings.TrimSuffix(line, "\r")
	i := strings.Index(line, "|")
	if i <= 0 {
		return line
	}
	for _, r := range line[:i] {
		if r < '0' || r > '9' {
			return line
		}
	}
	return line[i+1:]
}

func annotateEnclosingIfs(rawLines []string) []string {
	out := make([]string, len(rawLines))
	type frame struct {
		bodyDepth int
		cond      string
	}
	var stack []frame
	depth := 0
	for i, raw := range rawLines {
		if len(stack) > 0 {
			out[i] = stack[len(stack)-1].cond
		}
		pending := extractIfCond(strings.TrimSpace(raw))
		inString := false
		escaped := false
		for _, r := range raw {
			if inString {
				if escaped {
					escaped = false
					continue
				}
				if r == '\\' {
					escaped = true
					continue
				}
				if r == '"' {
					inString = false
				}
				continue
			}
			if r == '"' {
				inString = true
				continue
			}
			switch r {
			case '{':
				depth++
				if pending != "" {
					stack = append(stack, frame{bodyDepth: depth, cond: pending})
					pending = ""
				}
			case '}':
				for len(stack) > 0 && stack[len(stack)-1].bodyDepth == depth {
					stack = stack[:len(stack)-1]
				}
				depth--
				if depth < 0 {
					depth = 0
				}
			}
		}
	}
	return out
}

func extractIfCond(trimmed string) string {
	s := strings.TrimSpace(strings.TrimPrefix(trimmed, "}"))
	s = strings.TrimSpace(s)
	switch {
	case strings.HasPrefix(s, "else if"):
		s = strings.TrimSpace(s[len("else if"):])
	case strings.HasPrefix(s, "else"):
		rest := strings.TrimSpace(s[len("else"):])
		if rest == "{" || rest == "" {
			return "else"
		}
		return ""
	case hasIfKeyword(s):
		s = strings.TrimSpace(s[len("if"):])
	default:
		return ""
	}
	s = strings.TrimSpace(strings.TrimSuffix(s, "{"))
	return strings.TrimSpace(s)
}

func hasIfKeyword(s string) bool {
	if !strings.HasPrefix(s, "if") {
		return false
	}
	if len(s) == 2 {
		return true
	}
	return !isIdentChar(rune(s[2]))
}

func isIdentChar(r rune) bool {
	return unicode.IsLetter(r) || unicode.IsDigit(r) || r == '_'
}

func extractCodeFences(text string) []string {
	matches := codeFenceRE.FindAllStringSubmatch(text, -1)
	out := make([]string, 0, len(matches))
	for _, m := range matches {
		if len(m) < 2 {
			continue
		}
		body := strings.TrimSpace(m[1])
		if body == "" {
			continue
		}
		out = append(out, body)
	}
	return out
}

func fenceMatchesSources(fence string, sources []parsedSource) (reason string, ok bool) {
	qLines := quoteSignificantLines(fence)
	if len(qLines) == 0 {
		return "", true
	}
	if !fenceOverlapsSources(qLines, fence, sources) {
		return "", true
	}

	var lastReason string
	for _, src := range sources {
		if r, pass := fenceMatchesSource(qLines, fence, src); pass {
			return "", true
		} else if r != "" {
			lastReason = r
		}
	}
	if lastReason == "" {
		lastReason = "quoted code is not a contiguous excerpt of rca_read (伪源码)"
	}
	return lastReason, false
}

func quoteSignificantLines(fence string) []string {
	fence = strings.ReplaceAll(fence, "\r\n", "\n")
	var out []string
	for _, line := range strings.Split(fence, "\n") {
		norm := normalizeCodeLine(line)
		if norm == "" || isEllipsis(norm) {
			continue
		}
		out = append(out, norm)
	}
	return out
}

func isEllipsis(norm string) bool {
	s := strings.Trim(norm, ".…")
	return s == "" || s == "}" || s == "{"
}

func fenceOverlapsSources(qLines []string, fence string, sources []parsedSource) bool {
	calls := callNamesIn(strings.Join(qLines, " ") + " " + fence)
	for _, src := range sources {
		for _, sl := range src.lines {
			for _, q := range qLines {
				if linesMatch(q, sl.norm) {
					return true
				}
			}
			for _, name := range calls {
				if strings.Contains(sl.raw, name+"(") || strings.Contains(sl.norm, name+"(") || strings.Contains(sl.norm, name+"()") {
					return true
				}
			}
		}
	}
	return false
}

func fenceMatchesSource(qLines []string, fence string, src parsedSource) (reason string, ok bool) {
	matchedIdx := make([]int, 0, len(qLines))
	start := 0
	for _, q := range qLines {
		hit := -1
		for i := start; i < len(src.lines); i++ {
			if linesMatch(q, src.lines[i].norm) {
				hit = i
				break
			}
		}
		if hit < 0 {
			// allow match anywhere (quotes may skip leading lines)
			for i := 0; i < start && i < len(src.lines); i++ {
				if linesMatch(q, src.lines[i].norm) {
					hit = i
					break
				}
			}
		}
		if hit >= 0 {
			matchedIdx = append(matchedIdx, hit)
			start = hit + 1
		}
	}
	if len(matchedIdx) == 0 {
		return "", false
	}

	minI, maxI := matchedIdx[0], matchedIdx[0]
	for _, i := range matchedIdx {
		if i < minI {
			minI = i
		}
		if i > maxI {
			maxI = i
		}
	}
	quoteBlob := strings.Join(qLines, "\n")
	for i := minI; i <= maxI; i++ {
		sl := src.lines[i]
		if !isControlFlowLine(sl.norm) {
			continue
		}
		if !quoteContainsNorm(quoteBlob, sl.norm) && (sl.ifCond == "" || !quoteContainsNorm(quoteBlob, normalizeCodeLine(sl.ifCond))) {
			return fmt.Sprintf("%s: dropped guard %q (errcode / if/else/return must be quoted verbatim)", src.path, controlFlowHint(sl)), false
		}
	}

	for _, i := range matchedIdx {
		sl := src.lines[i]
		if sl.ifCond == "" {
			continue
		}
		if quoteContainsNorm(quoteBlob, normalizeCodeLine(sl.ifCond)) {
			continue
		}
		if quoteContainsNorm(quoteBlob, "if "+strings.TrimSpace(sl.ifCond)) {
			continue
		}
		return fmt.Sprintf("%s: call is inside `if %s` — quote the enclosing if (伪源码)", src.path, sl.ifCond), false
	}

	if !segmentsContiguous(qLines, src, matchedIdx) {
		return fmt.Sprintf("%s: quoted statements are not contiguous in rca_read (伪源码)", src.path), false
	}
	return "", true
}

func segmentsContiguous(qLines []string, src parsedSource, matchedIdx []int) bool {
	if len(matchedIdx) < 2 {
		return true
	}
	// Adjacent quote lines that both matched must not skip a control-flow source line.
	qToSrc := map[int]int{}
	qi := 0
	for _, q := range qLines {
		_ = q
		if qi < len(matchedIdx) {
			qToSrc[qi] = matchedIdx[qi]
			qi++
		}
	}
	for i := 1; i < len(matchedIdx); i++ {
		prev, cur := matchedIdx[i-1], matchedIdx[i]
		if cur <= prev {
			return false
		}
		for j := prev + 1; j < cur; j++ {
			if isControlFlowLine(src.lines[j].norm) {
				return false
			}
		}
	}
	return true
}

func controlFlowHint(sl srcLine) string {
	if sl.ifCond != "" {
		return sl.ifCond
	}
	return sl.raw
}

func isControlFlowLine(norm string) bool {
	switch {
	case strings.HasPrefix(norm, "if ") || strings.HasPrefix(norm, "if(") || strings.HasPrefix(norm, "if()"):
		return true
	case strings.HasPrefix(norm, "} else") || strings.HasPrefix(norm, "else if") || strings.HasPrefix(norm, "else {"):
		return true
	case strings.HasPrefix(norm, "return ") || norm == "return" || strings.HasPrefix(norm, "return)"):
		return true
	case strings.HasPrefix(norm, "for ") || strings.HasPrefix(norm, "switch "):
		return true
	default:
		return false
	}
}

func quoteContainsNorm(blob, piece string) bool {
	piece = strings.TrimSpace(piece)
	if piece == "" {
		return true
	}
	if strings.Contains(blob, piece) {
		return true
	}
	pn := normalizeCodeLine(piece)
	if pn == "" {
		return true
	}
	return strings.Contains(blob, pn)
}

func linesMatch(q, s string) bool {
	if q == "" || s == "" {
		return false
	}
	if q == s {
		return true
	}
	if strings.Contains(s, q) || strings.Contains(q, s) {
		return true
	}
	return false
}

func callNamesIn(s string) []string {
	ms := callNameRE.FindAllStringSubmatch(s, -1)
	seen := map[string]struct{}{}
	var out []string
	for _, m := range ms {
		if len(m) < 2 {
			continue
		}
		name := m[1]
		if _, skip := skipCallNames[name]; skip {
			continue
		}
		if _, ok := seen[name]; ok {
			continue
		}
		seen[name] = struct{}{}
		out = append(out, name)
	}
	return out
}

func normalizeCodeLine(s string) string {
	s = stripReadLinePrefix(s)
	s = strings.TrimSpace(s)
	s = stripLineComment(s)
	s = strings.TrimSpace(s)
	if s == "" || strings.HasPrefix(s, "//") {
		return ""
	}
	s = collapseParens(s)
	s = collapseSpace(s)
	return s
}

func stripLineComment(s string) string {
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
		if c == '"' {
			inString = true
			continue
		}
		if c == '/' && i+1 < len(s) && s[i+1] == '/' {
			if i >= 5 && strings.Contains(strings.ToLower(s[max(0, i-8):i]), "http") {
				continue
			}
			return strings.TrimSpace(s[:i])
		}
	}
	return s
}

func collapseParens(s string) string {
	var b strings.Builder
	depth := 0
	for _, r := range s {
		switch r {
		case '(':
			if depth == 0 {
				b.WriteByte('(')
			}
			depth++
		case ')':
			if depth > 0 {
				depth--
			}
			if depth == 0 {
				b.WriteByte(')')
			}
		default:
			if depth == 0 {
				b.WriteRune(r)
			}
		}
	}
	return b.String()
}

func collapseSpace(s string) string {
	return strings.Join(strings.Fields(s), " ")
}

func uniqueStrings(in []string) []string {
	seen := map[string]struct{}{}
	out := make([]string, 0, len(in))
	for _, s := range in {
		if s == "" {
			continue
		}
		if _, ok := seen[s]; ok {
			continue
		}
		seen[s] = struct{}{}
		out = append(out, s)
	}
	return out
}

// CollectCodeQuoteSources extracts rca_read content and rca_grep snippets from this turn.
func CollectCodeQuoteSources(records []ToolCallRecord) []CodeQuoteSource {
	var out []CodeQuoteSource
	for _, rec := range records {
		m := toolResultMap(rec.Result)
		if m == nil {
			continue
		}
		switch rec.ToolName {
		case toolRCARead:
			path := strings.TrimSpace(anyString(m["file"]))
			if path == "" {
				path = strings.TrimSpace(anyString(m["path"]))
			}
			content := anyString(m["content"])
			if content == "" {
				continue
			}
			out = append(out, CodeQuoteSource{Path: path, Content: content})
		case toolRCAGrep:
			out = append(out, grepSnippetsAsSources(m)...)
		}
	}
	return out
}

func grepSnippetsAsSources(m map[string]any) []CodeQuoteSource {
	var out []CodeQuoteSource
	switch matches := m["matches"].(type) {
	case []any:
		for _, item := range matches {
			mm, ok := item.(map[string]any)
			if !ok {
				continue
			}
			snippet := anyString(mm["snippet"])
			if snippet == "" {
				continue
			}
			path := anyString(mm["file"])
			if path == "" {
				path = anyString(mm["path"])
			}
			out = append(out, CodeQuoteSource{Path: path, Content: snippet})
		}
	case []map[string]any:
		for _, mm := range matches {
			snippet := anyString(mm["snippet"])
			if snippet == "" {
				continue
			}
			path := anyString(mm["file"])
			if path == "" {
				path = anyString(mm["path"])
			}
			out = append(out, CodeQuoteSource{Path: path, Content: snippet})
		}
	}
	return out
}

func toolResultMap(v any) map[string]any {
	switch x := v.(type) {
	case map[string]any:
		return x
	case string:
		// JSON-encoded tool payload
		if strings.TrimSpace(x) == "" || x[0] != '{' {
			return nil
		}
		var m map[string]any
		if err := json.Unmarshal([]byte(x), &m); err != nil {
			return nil
		}
		if inner, ok := m["result"].(map[string]any); ok {
			return inner
		}
		return m
	default:
		return nil
	}
}

func anyString(v any) string {
	if s, ok := v.(string); ok {
		return s
	}
	return ""
}
