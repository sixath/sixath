package hub

import "context"

// AssetFilter narrows ListAccessible.
type AssetFilter struct {
	Kind       AssetKind
	Status     AssetStatus
	Visibility Visibility
	Query      string
	Limit      int
	Offset     int
}

// Page is a simple offset page.
type Page[T any] struct {
	Items []T
	Total int
}

// GovernanceProvider is the read/query surface for team/agent asset governance.
type GovernanceProvider interface {
	Name() string
	Capabilities() Capabilities
	ResolveLoadout(ctx context.Context, id Identity) ([]AssetRef, error)
	CheckAccess(ctx context.Context, id Identity, asset AssetRef, action string) (bool, error)
	ListAccessible(ctx context.Context, id Identity, filter AssetFilter) (Page[AssetRef], error)
}

// GovernanceWriter mutates bindings and asset metadata.
// Providers with Capabilities().Write==true must implement this (optionally via type assert).
//
// External Hub skills must go through SkillTrustGate materialization before runtime
// execution (design §3.5.3); that gate is separate from CommitProceduralRepair.
type GovernanceWriter interface {
	BindAssets(ctx context.Context, agentID string, refs []AssetRef) error
	UnbindAssets(ctx context.Context, agentID string, refs []AssetRef) error
	SetVisibility(ctx context.Context, asset AssetRef, v Visibility) error
	SetStatus(ctx context.Context, asset AssetRef, status AssetStatus) error
}
