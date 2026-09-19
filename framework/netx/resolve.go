package netx

import (
	"fmt"
	"strings"
)

// Resolve picks the outbound tunnel. A nil Effective means direct.
func Resolve(b Binding, agent *Spec, catalog map[string]Spec, destHost string) (*Effective, error) {
	mode := NormalizeMode(b.Mode)
	switch mode {
	case ModeOff:
		return nil, nil
	case ModeProxy:
		id := strings.TrimSpace(b.ProxyID)
		spec, ok := catalog[id]
		if !ok {
			return nil, fmt.Errorf("proxy %q not found", id)
		}
		return &Effective{Spec: spec, Force: true}, nil
	case ModeInherit:
		if agent == nil || specEmpty(*agent) {
			return nil, nil
		}
		if MatchNoProxy(destHost, agent.NoProxy) {
			return nil, nil
		}
		return &Effective{Spec: *agent, Force: false}, nil
	default:
		return nil, fmt.Errorf("unknown egress mode %q", strings.TrimSpace(b.Mode))
	}
}

func specEmpty(s Spec) bool {
	return s.ID == "" && s.Type == "" && s.Host == "" && s.Port == 0 && s.User == "" && s.Password == "" && len(s.NoProxy) == 0
}
