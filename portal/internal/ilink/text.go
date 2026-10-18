package ilink

import (
	"regexp"
	"strings"
	"unicode"
	"unicode/utf8"
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
		line = stripEmphasis(line, "**")
		line = stripEmphasis(line, "__")
		out = append(out, line)
	}
	return strings.TrimSpace(strings.Join(out, "\n"))
}

// stripEmphasis unwraps marker-delimited spans whose outside neighbours are not identifier
// characters, so `**bold**` becomes `bold` while `__init__.py`, `self.__dict__` and `a__b` stay.
func stripEmphasis(line, marker string) string {
	if !strings.Contains(line, marker) {
		return line
	}
	var b strings.Builder
	for i := 0; i < len(line); {
		if strings.HasPrefix(line[i:], marker) && emphasisOpens(line, i, marker) {
			if j := emphasisClose(line, i+len(marker), marker); j >= 0 {
				b.WriteString(line[i+len(marker) : j])
				i = j + len(marker)
				continue
			}
		}
		b.WriteByte(line[i])
		i++
	}
	return b.String()
}

func emphasisOpens(line string, i int, marker string) bool {
	if identBefore(line[:i]) {
		return false
	}
	after := line[i+len(marker):]
	r, _ := utf8.DecodeRuneInString(after)
	return after != "" && after[0] != marker[0] && !unicode.IsSpace(r)
}

// emphasisClose returns the index of the closing marker for a span whose content starts at from, or -1.
func emphasisClose(line string, from int, marker string) int {
	for k := from + 1; k < len(line); {
		idx := strings.Index(line[k:], marker)
		if idx < 0 {
			return -1
		}
		j := k + idx
		r, _ := utf8.DecodeLastRuneInString(line[:j])
		rest := line[j+len(marker):]
		if !unicode.IsSpace(r) && (rest == "" || rest[0] != marker[0]) && !identAfter(rest) {
			return j
		}
		k = j + 1
	}
	return -1
}

// isWordRune matches RE2's \w (ASCII only), so CJK text right next to a marker still unwraps.
func isWordRune(r rune) bool {
	return r == '_' || r < utf8.RuneSelf && (unicode.IsLetter(r) || unicode.IsDigit(r))
}

// identBefore reports whether s ends inside an identifier or attribute path (`a`, `self.`).
func identBefore(s string) bool {
	r, size := utf8.DecodeLastRuneInString(s)
	if s == "" {
		return false
	}
	if r == '.' {
		r, _ = utf8.DecodeLastRuneInString(s[:len(s)-size])
	}
	return isWordRune(r)
}

// identAfter reports whether s continues an identifier, attribute path or call (`b`, `.py`, `(`).
func identAfter(s string) bool {
	r, size := utf8.DecodeRuneInString(s)
	if s == "" {
		return false
	}
	if r == '(' {
		return true
	}
	if r == '.' {
		r, _ = utf8.DecodeRuneInString(s[size:])
	}
	return isWordRune(r)
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
