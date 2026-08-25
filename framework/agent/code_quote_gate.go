package agent

import (
	"encoding/json"
	"fmt"
	"regexp"
	"strings"
	"unicode"

	"github.com/sixath/framework/tool"
)

const (
	toolRCARead = "rca_read"
	toolRCAGrep = "rca_grep"
)

// CodeQuoteSource is one rca_read / rca_grep payload used as ground truth.
type CodeQuoteSource struct {
	Path        string
	Content     string
	ControlFlow []tool.ControlFlowFunc
}

const codeQuoteGateSoftPrompt = `源码引用与 rca_read 原文不一致（伪源码、漏掉了控制流门，或正文声称会执行受控调用）。
先保留面向用户的中文结论，不要改成路径表或 P1/P2 报告。只修正结论对错和 fenced 代码：代码必须从工具原文连续摘抄。若正文点名会执行某次调用，用一句话说明前提（对应 control_flow 的 when），或写明「不会执行/跳过」。不要为过闸把整张路径表贴进回答。

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

// EvaluateCodeQuoteGate checks the final answer against rca_read/grep sources.
// Fence quotes must be contiguous excerpts. Gated calls from control_flow must
// cite a path when (or skip wording). No sources / no CFG and no fences → allow.
func EvaluateCodeQuoteGate(sources []CodeQuoteSource, finalText string) EvidenceGateResult {
	if len(sources) == 0 {
		return EvidenceGateResult{Allow: true}
	}

	var missing []string
	fences := extractCodeFences(finalText)
	if len(fences) > 0 {
		parsed := make([]parsedSource, 0, len(sources))
		for _, src := range sources {
			p := parseCodeQuoteSource(src)
			if len(p.lines) == 0 {
				continue
			}
			parsed = append(parsed, p)
		}
		for _, fence := range fences {
			if len(parsed) == 0 {
				break
			}
			if reason, ok := fenceMatchesSources(fence, parsed); !ok && reason != "" {
				missing = append(missing, reason)
			}
		}
	}
	missing = append(missing, gatedCallClaimMismatches(finalText, sources)...)
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

var gatedCallSkipPhrases = []string{
	"跳过", "被跳过", "不会执行", "未执行", "不调用", "不会调用", "不写入", "未写入", "不会写",
	"条件不成立", "不满足", "不会进", "不会进入", "走不到", "不会走", "不进入", "进不去",
	"skipped", "not called", "does not call", "will not call", "won't call",
	"not executed", "does not execute",
}

func gatedCallClaimMismatches(final string, sources []CodeQuoteSource) []string {
	type key struct{ name, path string }
	seen := map[key]struct{}{}
	var out []string
	for _, src := range sources {
		if len(src.ControlFlow) == 0 {
			continue
		}
		for _, fn := range src.ControlFlow {
			for _, name := range gatedCallsIn(fn) {
				k := key{name, fn.File}
				if _, ok := seen[k]; ok {
					continue
				}
				seen[k] = struct{}{}
				if !strings.Contains(final, name) {
					continue
				}
				exclusive := exclusiveWhenForCall(fn, name)
				if len(exclusive) == 0 {
					continue
				}
				if answerAcknowledgesCFG(final, name, exclusive, fn) {
					continue
				}
				out = append(out, fmt.Sprintf("%s: %s is gated by %s — cite control_flow when or say it is skipped",
					fn.File, name, strings.Join(exclusive, "; ")))
			}
		}
	}
	return out
}

func gatedCallsIn(fn tool.ControlFlowFunc) []string {
	if len(fn.Paths) == 0 {
		return nil
	}
	total := len(fn.Paths)
	count := map[string]int{}
	order := make([]string, 0)
	for _, p := range fn.Paths {
		seen := map[string]struct{}{}
		for _, c := range p.Calls {
			if _, ok := seen[c]; ok {
				continue
			}
			seen[c] = struct{}{}
			if count[c] == 0 {
				order = append(order, c)
			}
			count[c]++
		}
	}
	var out []string
	for _, name := range order {
		if count[name] < total {
			out = append(out, name)
		}
	}
	return out
}

func exclusiveWhenForCall(fn tool.ControlFlowFunc, name string) []string {
	var with, without []tool.ControlFlowPath
	for _, p := range fn.Paths {
		if pathCalls(p, name) {
			with = append(with, p)
		} else {
			without = append(without, p)
		}
	}
	if len(with) == 0 || len(without) == 0 {
		return nil
	}
	withoutSet := map[string]struct{}{}
	for _, p := range without {
		for _, w := range p.When {
			withoutSet[compactCodeText(w)] = struct{}{}
		}
	}
	inter := map[string]int{}
	for _, p := range with {
		seen := map[string]struct{}{}
		for _, w := range p.When {
			c := compactCodeText(w)
			if c == "" {
				continue
			}
			if _, ok := seen[c]; ok {
				continue
			}
			seen[c] = struct{}{}
			inter[c]++
		}
	}
	var exclusive []string
	seenRaw := map[string]struct{}{}
	for _, p := range with {
		for _, w := range p.When {
			c := compactCodeText(w)
			if inter[c] != len(with) {
				continue
			}
			if _, ok := withoutSet[c]; ok {
				continue
			}
			if _, ok := seenRaw[c]; ok {
				continue
			}
			seenRaw[c] = struct{}{}
			exclusive = append(exclusive, w)
		}
	}
	if len(exclusive) > 0 {
		return exclusive
	}
	var fallback []string
	for _, p := range with {
		for _, w := range p.When {
			c := compactCodeText(w)
			if c == "" {
				continue
			}
			if _, ok := seenRaw[c]; ok {
				continue
			}
			seenRaw[c] = struct{}{}
			fallback = append(fallback, w)
		}
	}
	return fallback
}

func pathCalls(p tool.ControlFlowPath, name string) bool {
	for _, c := range p.Calls {
		if c == name {
			return true
		}
	}
	return false
}

func answerAcknowledgesCFG(final, name string, exclusive []string, fn tool.ControlFlowFunc) bool {
	for _, w := range exclusive {
		if condMentionedIn(final, w) {
			return true
		}
		if acknowledgeRangeOrElse(final, w) {
			return true
		}
	}
	for _, p := range fn.Paths {
		if pathCalls(p, name) && condMentionedIn(final, p.ID) {
			return true
		}
	}
	for _, idx := range allStringIndexes(final, name) {
		win := runeWindowAround(final, idx, len(name), 240)
		for _, phrase := range gatedCallSkipPhrases {
			if strings.Contains(win, phrase) {
				return true
			}
		}
	}
	return false
}

func acknowledgeRangeOrElse(final, cond string) bool {
	c := strings.TrimSpace(cond)
	if strings.HasPrefix(c, "range ") {
		rest := strings.TrimSpace(strings.TrimPrefix(c, "range "))
		if strings.Contains(final, "遍历") && (rest == "" || strings.Contains(final, rest) || strings.Contains(compactCodeText(final), compactCodeText(rest))) {
			return true
		}
		if rest != "" && strings.Contains(compactCodeText(final), compactCodeText(rest)) {
			return true
		}
	}
	if strings.HasPrefix(c, "!") || strings.HasPrefix(c, "!(") {
		for _, p := range []string{"否则", "不然", "else"} {
			if strings.Contains(final, p) {
				return true
			}
		}
	}
	return false
}

func condMentionedIn(final, cond string) bool {
	condCompact := compactCodeText(cond)
	if condCompact == "" {
		return false
	}
	blob := compactCodeText(final)
	if strings.Contains(blob, condCompact) {
		return true
	}
	if alt := strings.ReplaceAll(condCompact, "==", "="); alt != condCompact && strings.Contains(blob, alt) {
		return true
	}
	if alt := strings.ReplaceAll(condCompact, "!=", "≠"); alt != condCompact && strings.Contains(blob, alt) {
		return true
	}
	return false
}

func compactCodeText(s string) string {
	s = strings.ReplaceAll(s, "\r", "")
	s = strings.ReplaceAll(s, "\n", "")
	s = strings.ReplaceAll(s, "\t", "")
	return strings.ReplaceAll(s, " ", "")
}

func allStringIndexes(s, sub string) []int {
	if sub == "" {
		return nil
	}
	var out []int
	from := 0
	for {
		i := strings.Index(s[from:], sub)
		if i < 0 {
			return out
		}
		pos := from + i
		out = append(out, pos)
		from = pos + len(sub)
	}
}

func runeWindowAround(s string, byteIdx, byteLen, radius int) string {
	if byteIdx < 0 {
		byteIdx = 0
	}
	if byteIdx > len(s) {
		byteIdx = len(s)
	}
	endByte := byteIdx + byteLen
	if endByte > len(s) {
		endByte = len(s)
	}
	runes := []rune(s)
	start := len([]rune(s[:byteIdx])) - radius
	if start < 0 {
		start = 0
	}
	end := len([]rune(s[:endByte])) + radius
	if end > len(runes) {
		end = len(runes)
	}
	return string(runes[start:end])
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
			out = append(out, CodeQuoteSource{
				Path:        path,
				Content:     content,
				ControlFlow: parseControlFlowField(m["control_flow"]),
			})
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

func parseControlFlowField(v any) []tool.ControlFlowFunc {
	if v == nil {
		return nil
	}
	switch x := v.(type) {
	case []tool.ControlFlowFunc:
		return x
	case tool.ControlFlowFunc:
		return []tool.ControlFlowFunc{x}
	}
	b, err := json.Marshal(v)
	if err != nil || len(b) == 0 || string(b) == "null" {
		return nil
	}
	var out []tool.ControlFlowFunc
	if err := json.Unmarshal(b, &out); err == nil && len(out) > 0 {
		return out
	}
	var one tool.ControlFlowFunc
	if err := json.Unmarshal(b, &one); err == nil && one.Function != "" {
		return []tool.ControlFlowFunc{one}
	}
	return nil
}
