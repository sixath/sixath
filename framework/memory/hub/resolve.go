package hub

import "fmt"

// Catalog holds registered provider instances by name.
type Catalog struct {
	Gov  map[string]GovernanceProvider
	Know map[string]KnowledgeProvider
}

// Defaults are global provider name slots (usually "local").
type Defaults struct {
	Governance string
	Knowledge  string
}

// AgentHubConfig overrides defaults per agent. Nil/empty string = use default.
type AgentHubConfig struct {
	Governance *string
	Knowledge  *string
	// FallbackToDefaultOnReadError: on runtime transport read failure, retry Defaults (design §3.5.1).
	FallbackToDefaultOnReadError bool
}

// Resolve maps names to instances. Missing catalog name is a hard error (assembly-time).
// Runtime reachability is not checked here.
func Resolve(cat Catalog, def Defaults, agent AgentHubConfig) (GovernanceProvider, KnowledgeProvider, error) {
	gName := def.Governance
	if agent.Governance != nil && *agent.Governance != "" {
		gName = *agent.Governance
	}
	kName := def.Knowledge
	if agent.Knowledge != nil && *agent.Knowledge != "" {
		kName = *agent.Knowledge
	}
	if gName == "" || kName == "" {
		return nil, nil, fmt.Errorf("hub: defaults and overrides must yield non-empty provider names")
	}
	gov := cat.Gov[gName]
	if gov == nil {
		return nil, nil, fmt.Errorf("hub: governance provider %q not registered", gName)
	}
	know := cat.Know[kName]
	if know == nil {
		return nil, nil, fmt.Errorf("hub: knowledge provider %q not registered", kName)
	}
	return gov, know, nil
}
