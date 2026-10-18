package ilink

import (
	"regexp"
	"strings"
)

// DefaultSplitRunes is the per-message limit; Phase 0 delivered 4000 CJK runes (~11.9 KB) intact.
const DefaultSplitRunes = 4000

var (
	headingRe = regexp.MustCompile(`^\s{0,3}#{1,6}\s+`)
	fenceRe   = regexp.MustCompile("^\\s*(```|~~~)")
)

// PlainText simplifies Markdown for WeChat, which renders text only: it drops heading marks,
// bold markers and code fences while keeping code block contents verbatim.
func PlainText(md string) string {
	lines := strings.Split(strings.ReplaceAll(md, "\r\n", "\n"), "\n")
	out := make([]string, 0, len(lines))
	inFence := false
	for _, line := range lines {
		if fenceRe.MatchString(line) {
			inFence = !inFence
			continue
		}
		if inFence {
			out = append(out, line)
			continue
		}
		line = headingRe.ReplaceAllString(line, "")
		line = strings.ReplaceAll(line, "**", "")
		line = strings.ReplaceAll(line, "__", "")
		out = append(out, line)
	}
	return strings.TrimSpace(strings.Join(out, "\n"))
}

// SplitText cuts s into parts of at most maxRunes runes (<=0 means DefaultSplitRunes), preferring
// a newline in the second half of each window. Blank input yields no parts.
func SplitText(s string, maxRunes int) []string {
	if maxRunes <= 0 {
		maxRunes = DefaultSplitRunes
	}
	r := []rune(strings.TrimSpace(s))
	var parts []string
	for len(r) > 0 {
		if len(r) <= maxRunes {
			parts = append(parts, string(r))
			break
		}
		cut := maxRunes
		for i := maxRunes; i > maxRunes/2; i-- {
			if r[i-1] == '\n' {
				cut = i
				break
			}
		}
		if part := strings.TrimSpace(string(r[:cut])); part != "" {
			parts = append(parts, part)
		}
		r = r[cut:]
		for len(r) > 0 && (r[0] == '\n' || r[0] == '\r') {
			r = r[1:]
		}
	}
	return parts
}
