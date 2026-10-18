package ilink

import (
	"regexp"
	"strconv"
	"strings"
)

// DefaultSplitRunes is the per-message cap; Phase 0 delivered 4000 CJK runes intact.
const DefaultSplitRunes = 4000

var (
	headingRe    = regexp.MustCompile(`^\s{0,3}#{1,6}\s+`)
	linkRe       = regexp.MustCompile(`!?\[([^\]]*)\]\(([^)\s]+)\)`)
	boldStarRe   = regexp.MustCompile(`\*\*([^*\n]+?)\*\*`)
	boldUnderRe  = regexp.MustCompile(`__([^_\n]+?)__`)
	italicStarRe = regexp.MustCompile(`\*([^*\s][^*\n]*?)\*`)
	italicUndRe  = regexp.MustCompile(`(^|[^\pL\pN_])_([^_\s][^_\n]*?)_($|[^\pL\pN_])`)
	strikeRe     = regexp.MustCompile(`~~([^~\n]+?)~~`)
	inlineCodeRe = regexp.MustCompile("`([^`\n]+)`")
)

// SimplifyMarkdown turns assistant Markdown into WeChat-friendly plain text.
// Fenced code blocks keep their content verbatim (fence lines dropped).
func SimplifyMarkdown(s string) string {
	lines := strings.Split(strings.ReplaceAll(s, "\r\n", "\n"), "\n")
	out := make([]string, 0, len(lines))
	inFence := false
	for _, line := range lines {
		if strings.HasPrefix(strings.TrimSpace(line), "```") {
			inFence = !inFence
			continue
		}
		if inFence {
			out = append(out, line)
			continue
		}
		out = append(out, simplifyInline(headingRe.ReplaceAllString(line, "")))
	}
	return strings.TrimSpace(strings.Join(out, "\n"))
}

func simplifyInline(line string) string {
	// Protect inline code so emphasis rules do not touch it.
	var codes []string
	line = inlineCodeRe.ReplaceAllStringFunc(line, func(m string) string {
		codes = append(codes, inlineCodeRe.FindStringSubmatch(m)[1])
		return "\x00" + strconv.Itoa(len(codes)-1) + "\x00"
	})
	line = linkRe.ReplaceAllString(line, "$1 ($2)")
	line = boldStarRe.ReplaceAllString(line, "$1")
	line = boldUnderRe.ReplaceAllString(line, "$1")
	line = italicStarRe.ReplaceAllString(line, "$1")
	line = italicUndRe.ReplaceAllString(line, "$1$2$3")
	line = strikeRe.ReplaceAllString(line, "$1")
	for i, c := range codes {
		line = strings.Replace(line, "\x00"+strconv.Itoa(i)+"\x00", c, 1)
	}
	return line
}

// SplitText cuts s into chunks of at most maxRunes runes, preferring the last newline in each window.
func SplitText(s string, maxRunes int) []string {
	if maxRunes <= 0 {
		maxRunes = DefaultSplitRunes
	}
	var out []string
	rest := []rune(s)
	for len(rest) > maxRunes {
		cut := -1
		for i := maxRunes - 1; i > 0; i-- {
			if rest[i] == '\n' {
				cut = i
				break
			}
		}
		var chunk string
		if cut > 0 {
			chunk = string(rest[:cut])
			rest = rest[cut+1:]
		} else {
			chunk = string(rest[:maxRunes])
			rest = rest[maxRunes:]
		}
		if strings.TrimSpace(chunk) != "" {
			out = append(out, strings.TrimRight(chunk, "\n"))
		}
	}
	if strings.TrimSpace(string(rest)) != "" {
		out = append(out, strings.TrimRight(string(rest), "\n"))
	}
	return out
}

// ShapeReply simplifies Markdown then splits for sendmessage.
func ShapeReply(s string, maxRunes int) []string {
	return SplitText(SimplifyMarkdown(s), maxRunes)
}
