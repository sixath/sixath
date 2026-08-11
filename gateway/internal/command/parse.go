package command

import (
	"strings"
)

type Kind int

const (
	KindNone Kind = iota
	KindAgentSwitch
	KindAgentList
	KindNew
	KindUnbind
	KindSwitch
	KindUnknown
)

type Command struct {
	Kind   Kind
	Target string
}

// Parse interprets inbound chat text as a gateway slash command.
// The second return value is false when text is not a slash command.
func Parse(text string) (Command, bool) {
	text = strings.TrimSpace(text)
	if text == "" || !strings.HasPrefix(text, "/") {
		return Command{}, false
	}

	body := strings.TrimSpace(text[1:])
	if body == "" {
		return Command{Kind: KindUnknown}, true
	}

	name, rest, _ := strings.Cut(body, " ")
	name = strings.ToLower(strings.TrimSpace(name))
	rest = strings.TrimSpace(rest)

	switch name {
	case "new":
		return Command{Kind: KindNew}, true
	case "unbind":
		return Command{Kind: KindUnbind}, true
	case "switch":
		return Command{Kind: KindSwitch}, true
	case "agent", "agents":
		if rest == "" || strings.EqualFold(rest, "list") {
			return Command{Kind: KindAgentList}, true
		}
		return Command{Kind: KindAgentSwitch, Target: rest}, true
	default:
		return Command{Kind: KindUnknown}, true
	}
}
