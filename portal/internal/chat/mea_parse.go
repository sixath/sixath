package chat

import (
	"encoding/json"
	"regexp"
	"strings"

	"github.com/sixath/framework/mea"
)

// meaChecksFence matches a fenced ```mea-checks ... ``` block (optional language spacing).
var meaChecksFence = regexp.MustCompile("(?s)```mea-checks\\s*\\n(.*?)```")

// meaAcceptanceFence matches a fenced ```mea-acceptance ... ``` block (JSON string array).
var meaAcceptanceFence = regexp.MustCompile("(?s)```mea-acceptance\\s*\\n(.*?)```")

// ParseMEAChecks extracts AcceptanceChecks from a ```mea-checks``` JSON fence.
// Returns cleaned user text (fence stripped), checks, and ok=true when len(checks)>0.
func ParseMEAChecks(content string) (clean string, checks []mea.AcceptanceCheck, ok bool) {
	clean = content
	m := meaChecksFence.FindStringSubmatchIndex(content)
	if m == nil {
		return strings.TrimSpace(content), nil, false
	}
	body := strings.TrimSpace(content[m[2]:m[3]])
	clean = strings.TrimSpace(content[:m[0]] + content[m[1]:])
	if body == "" {
		return clean, nil, false
	}
	var parsed []mea.AcceptanceCheck
	if err := json.Unmarshal([]byte(body), &parsed); err != nil {
		return clean, nil, false
	}
	out := make([]mea.AcceptanceCheck, 0, len(parsed))
	for _, c := range parsed {
		c.Type = strings.TrimSpace(c.Type)
		if c.Type == "" {
			continue
		}
		out = append(out, c)
	}
	if len(out) == 0 {
		return clean, nil, false
	}
	return clean, out, true
}

// ParseMEAAcceptance extracts text acceptance criteria from a ```mea-acceptance``` JSON string array fence.
// Returns cleaned user text (fence stripped), acceptance lines, and ok=true when len(acceptance)>0.
func ParseMEAAcceptance(content string) (clean string, acceptance []string, ok bool) {
	clean = content
	m := meaAcceptanceFence.FindStringSubmatchIndex(content)
	if m == nil {
		return strings.TrimSpace(content), nil, false
	}
	body := strings.TrimSpace(content[m[2]:m[3]])
	clean = strings.TrimSpace(content[:m[0]] + content[m[1]:])
	if body == "" {
		return clean, nil, false
	}
	var parsed []string
	if err := json.Unmarshal([]byte(body), &parsed); err != nil {
		return clean, nil, false
	}
	out := make([]string, 0, len(parsed))
	for _, s := range parsed {
		s = strings.TrimSpace(s)
		if s == "" {
			continue
		}
		out = append(out, s)
	}
	if len(out) == 0 {
		return clean, nil, false
	}
	return clean, out, true
}
