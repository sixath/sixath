package mention

import (
	"strings"
	"unicode"
)

// Candidate is an allowlisted agent that may be @-mentioned.
type Candidate struct {
	ID   string
	Name string
}

// Result is the outcome of Parse.
type Result struct {
	Hit      bool
	AgentID  string
	Stripped string // text with the matched mention removed
}

// Parse finds the first allowlisted @Agent mention and strips it from text.
// Matching is case-insensitive on name (names may contain spaces); id matches
// as a contiguous token. When multiple names could match at the same @, the
// longest name wins. The first @ that hits any candidate wins overall.
func Parse(text string, cands []Candidate) Result {
	if text == "" || len(cands) == 0 {
		return Result{Stripped: text}
	}

	runes := []rune(text)
	for i := 0; i < len(runes); i++ {
		if runes[i] != '@' {
			continue
		}
		if i > 0 && !unicode.IsSpace(runes[i-1]) {
			continue
		}
		rest := string(runes[i+1:])
		agentID, consumed := matchCandidate(rest, cands)
		if consumed <= 0 {
			continue
		}
		end := i + 1 + consumed
		return Result{
			Hit:      true,
			AgentID:  agentID,
			Stripped: stripMention(runes, i, end),
		}
	}
	return Result{Stripped: text}
}

// matchCandidate returns agent id and rune length consumed after '@'.
func matchCandidate(rest string, cands []Candidate) (agentID string, consumedRunes int) {
	restRunes := []rune(rest)
	bestLen := 0
	bestID := ""

	for _, c := range cands {
		id := strings.TrimSpace(c.ID)
		name := strings.TrimSpace(c.Name)

		if id != "" {
			idRunes := []rune(id)
			if hasPrefixFold(restRunes, idRunes) && boundaryAfter(restRunes, len(idRunes)) {
				if len(idRunes) > bestLen {
					bestLen = len(idRunes)
					bestID = id
				}
			}
		}
		if name != "" {
			nameRunes := []rune(name)
			if hasPrefixFold(restRunes, nameRunes) && boundaryAfter(restRunes, len(nameRunes)) {
				if len(nameRunes) > bestLen {
					bestLen = len(nameRunes)
					bestID = id
				}
			}
		}
	}
	return bestID, bestLen
}

func hasPrefixFold(haystack, prefix []rune) bool {
	if len(prefix) > len(haystack) {
		return false
	}
	for i := range prefix {
		if unicode.ToLower(haystack[i]) != unicode.ToLower(prefix[i]) {
			return false
		}
	}
	return true
}

func boundaryAfter(runes []rune, n int) bool {
	if n >= len(runes) {
		return true
	}
	return unicode.IsSpace(runes[n])
}

func stripMention(runes []rune, start, end int) string {
	left := start
	for left > 0 && unicode.IsSpace(runes[left-1]) {
		left--
	}
	right := end
	for right < len(runes) && unicode.IsSpace(runes[right]) {
		right++
	}
	var out []rune
	if left > 0 {
		out = append(out, runes[:left]...)
		if right < len(runes) {
			out = append(out, ' ')
			out = append(out, runes[right:]...)
		}
	} else {
		out = append(out, runes[right:]...)
	}
	return strings.TrimSpace(string(out))
}
