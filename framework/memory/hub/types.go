package hub

// Identity is the sixath-facing caller context. ExternalIDs are Adapter-only.
type Identity struct {
	UserID, OrgID, TeamID, AgentID, SessionID, TaskID string
	ExternalIDs                                       map[string]string
}

// AssetKind classifies governed assets.
type AssetKind string

const (
	AssetKindChatMemory AssetKind = "chat_memory"
	AssetKindSkill      AssetKind = "skill"
	AssetKindWiki       AssetKind = "wiki"
	AssetKindCodeGraph  AssetKind = "code_graph"
	AssetKindProcedural AssetKind = "procedural"
	AssetKindUnit       AssetKind = "unit"
	AssetKindCustom     AssetKind = "custom"
)

// AssetStatus is the provider-facing lifecycle. DB mapping for local units: see hub/local.
type AssetStatus string

const (
	AssetDraft      AssetStatus = "draft"
	AssetActive     AssetStatus = "active"
	AssetStale      AssetStatus = "stale"
	AssetSuperseded AssetStatus = "superseded"
	AssetArchived   AssetStatus = "archived"
)

// Visibility controls sharing (provider-enforced).
type Visibility string

const (
	VisibilityPrivate    Visibility = "private"
	VisibilityTeam       Visibility = "team"
	VisibilityRestricted Visibility = "restricted"
	VisibilityAgent      Visibility = "agent"
	VisibilityTask       Visibility = "task"
)

// AssetRef is a hub-agnostic asset handle. Hub is the sole write-routing authority.
type AssetRef struct {
	Kind       AssetKind
	ID         string
	Hub        string // "local" or provider Catalog name
	Name       string
	Version    string
	Visibility Visibility
	OwnerID    string
	Status     AssetStatus
	Confidence float64 // P0: unused for filter/sort; local extract may set 0.5
	SourceRef  string
	Meta       map[string]any
}

// Capabilities declares what a provider supports.
type Capabilities struct {
	Write bool
	// Extra flags reserved for P1+ (wiki, code_graph, signed_skills, …).
	Flags map[string]bool
}

func (c Capabilities) Has(flag string) bool {
	if c.Flags == nil {
		return false
	}
	return c.Flags[flag]
}
