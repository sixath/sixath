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
// Matching is case-insensitive on name; id matches exactly (trimmed).
// When multiple names could match at the same @, the longest name wins.
func Parse(text string, cands []Candidate) Result {
	if text == "" || len(cands) == 0 {
		return Result{Stripped: text}
	}

	type match struct {
		agentID string
		start   int
		end     int // exclusive end of @token
		nameLen int
	}
	var best *match

	runes := []rune(text)
	for i := 0; i < len(runes); i++ {
		if runes[i] != '@' {
			continue
		}
		// Require start of string or whitespace before @.
		if i > 0 && !unicode.IsSpace(runes[i-1]) {
			continue
		}
		tokenStart := i + 1
		tokenEnd := tokenStart
		for tokenEnd < len(runes) && !unicode.IsSpace(runes[tokenEnd]) {
			tokenEnd++
		}
		if tokenEnd == tokenStart {
			continue
		}
		token := string(runes[tokenStart:tokenEnd])

		var local *match
		for _, c := range cands {
			id := strings.TrimSpace(c.ID)
			name := strings.TrimSpace(c.Name)
			if id != "" && token == id {
				m := match{agentID: id, start: i, end: tokenEnd, nameLen: len([]rune(id))}
				if local == nil || m.nameLen > local.nameLen {
					cp := m
					local = &cp
				}
				continue
			}
			if name != "" && strings.EqualFold(token, name) {
				m := match{agentID: id, start: i, end: tokenEnd, nameLen: len([]rune(name))}
				if local == nil || m.nameLen > local.nameLen {
					cp := m
					local = &cp
				}
			}
		}
		if local == nil {
			continue
		}
		// First @ that hits wins (even if a later @ has a longer name).
		best = local
		break
	}

	if best == nil {
		return Result{Stripped: text}
	}

	stripped := stripMention(runes, best.start, best.end)
	return Result{
		Hit:      true,
		AgentID:  best.agentID,
		Stripped: stripped,
	}
}

func stripMention(runes []rune, start, end int) string {
	// Expand to adjacent spaces so "hi @bot there" → "hi there".
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
