package local

import (
	"context"
	"sort"

	"github.com/sixath/framework/memory/hub"
)

const localHubName = "local"

// SkillLister lists local skills_dirs entries for default Loadout.
type SkillLister interface {
	ListSkills(ctx context.Context) ([]hub.AssetRef, error)
}

// GovernanceConfig configures LocalGovernance.
type GovernanceConfig struct {
	// LoadoutIncludeVisibleUnits when true includes visible units in default loadout (default false).
	LoadoutIncludeVisibleUnits bool
}

// LocalGovernance is the default in-process GovernanceProvider + Writer.
type LocalGovernance struct {
	store  BindingStore
	skills SkillLister
	cfg    GovernanceConfig
}

func NewLocalGovernance(store BindingStore, skills SkillLister, cfg GovernanceConfig) *LocalGovernance {
	if store == nil {
		store = NewMemoryBindingStore()
	}
	return &LocalGovernance{store: store, skills: skills, cfg: cfg}
}

func (g *LocalGovernance) Name() string { return localHubName }

func (g *LocalGovernance) Capabilities() hub.Capabilities {
	return hub.Capabilities{Write: true}
}

func (g *LocalGovernance) ResolveLoadout(ctx context.Context, id hub.Identity) ([]hub.AssetRef, error) {
	var out []hub.AssetRef
	if g.skills != nil {
		sk, err := g.skills.ListSkills(ctx)
		if err != nil {
			return nil, err
		}
		for _, s := range sk {
			s.Hub = localHubName
			if s.Kind == "" {
				s.Kind = hub.AssetKindSkill
			}
			if s.Status == "" {
				s.Status = hub.AssetActive
			}
			if LoadoutEligible(s.Status) {
				out = append(out, s)
			}
		}
	}
	bindings, err := g.store.ListByAgent(id.AgentID)
	if err != nil {
		return nil, err
	}
	for _, b := range bindings {
		st := b.Status
		if st == "" {
			st = hub.AssetActive
		}
		if !LoadoutEligible(st) {
			continue
		}
		// Explicit bindings always included (including unit kind).
		out = append(out, hub.AssetRef{
			Kind:       b.AssetKind,
			ID:         b.AssetID,
			Hub:        localHubName,
			Name:       b.Name,
			Status:     st,
			OwnerID:    b.OwnerID,
			Visibility: b.Visibility,
			Meta:       b.Meta,
		})
	}
	// Default bulk units are intentionally omitted unless flag (P0 locked choice).
	_ = g.cfg.LoadoutIncludeVisibleUnits
	sort.SliceStable(out, func(i, j int) bool {
		if out[i].Kind != out[j].Kind {
			return out[i].Kind < out[j].Kind
		}
		return out[i].ID < out[j].ID
	})
	return dedupeLoadout(out), nil
}

func dedupeLoadout(in []hub.AssetRef) []hub.AssetRef {
	seen := map[string]struct{}{}
	out := make([]hub.AssetRef, 0, len(in))
	for _, a := range in {
		k := string(a.Kind) + "\x00" + a.ID
		if _, ok := seen[k]; ok {
			continue
		}
		seen[k] = struct{}{}
		out = append(out, a)
	}
	return out
}

func (g *LocalGovernance) CheckAccess(_ context.Context, id hub.Identity, asset hub.AssetRef, _ string) (bool, error) {
	if asset.Hub != "" && asset.Hub != localHubName {
		return false, nil
	}
	if asset.Visibility == hub.VisibilityPrivate {
		return asset.OwnerID != "" && asset.OwnerID == id.UserID, nil
	}
	if asset.OwnerID != "" && asset.OwnerID == id.UserID {
		return true, nil
	}
	if id.OrgID != "" && asset.Visibility != hub.VisibilityPrivate {
		return true, nil
	}
	if id.AgentID != "" {
		bindings, err := g.store.ListByAgent(id.AgentID)
		if err != nil {
			return false, err
		}
		for _, b := range bindings {
			if b.AssetKind == asset.Kind && b.AssetID == asset.ID {
				return true, nil
			}
		}
	}
	return false, nil
}

func (g *LocalGovernance) ListAccessible(ctx context.Context, id hub.Identity, filter hub.AssetFilter) (hub.Page[hub.AssetRef], error) {
	loadout, err := g.ResolveLoadout(ctx, id)
	if err != nil {
		return hub.Page[hub.AssetRef]{}, err
	}
	items := make([]hub.AssetRef, 0, len(loadout))
	for _, a := range loadout {
		if filter.Kind != "" && a.Kind != filter.Kind {
			continue
		}
		if filter.Status != "" && a.Status != filter.Status {
			continue
		}
		items = append(items, a)
	}
	total := len(items)
	if filter.Offset > 0 {
		if filter.Offset >= len(items) {
			items = nil
		} else {
			items = items[filter.Offset:]
		}
	}
	if filter.Limit > 0 && len(items) > filter.Limit {
		items = items[:filter.Limit]
	}
	return hub.Page[hub.AssetRef]{Items: items, Total: total}, nil
}

func (g *LocalGovernance) BindAssets(ctx context.Context, agentID string, refs []hub.AssetRef) error {
	if err := hub.EnforceHub(g, refs...); err != nil {
		return err
	}
	for _, r := range refs {
		st := r.Status
		if st == "" {
			st = hub.AssetActive
		}
		if err := g.store.Upsert(Binding{
			AgentID:    agentID,
			AssetKind:  r.Kind,
			AssetID:    r.ID,
			Hub:        localHubName,
			Name:       r.Name,
			Status:     st,
			OwnerID:    r.OwnerID,
			Visibility: r.Visibility,
			Meta:       r.Meta,
		}); err != nil {
			return err
		}
	}
	return nil
}

func (g *LocalGovernance) UnbindAssets(_ context.Context, agentID string, refs []hub.AssetRef) error {
	if err := hub.EnforceHub(g, refs...); err != nil {
		return err
	}
	for _, r := range refs {
		if err := g.store.Delete(agentID, r.Kind, r.ID); err != nil {
			return err
		}
	}
	return nil
}

func (g *LocalGovernance) SetVisibility(_ context.Context, asset hub.AssetRef, v hub.Visibility) error {
	if err := hub.EnforceHub(g, asset); err != nil {
		return err
	}
	return g.store.UpdateMeta(asset.Kind, asset.ID, &v, nil)
}

func (g *LocalGovernance) SetStatus(_ context.Context, asset hub.AssetRef, status hub.AssetStatus) error {
	if err := hub.EnforceHub(g, asset); err != nil {
		return err
	}
	return g.store.UpdateMeta(asset.Kind, asset.ID, nil, &status)
}

// Ensure LocalGovernance implements both interfaces.
var (
	_ hub.GovernanceProvider = (*LocalGovernance)(nil)
	_ hub.GovernanceWriter   = (*LocalGovernance)(nil)
)

// StaticSkills is a test/helper SkillLister.
type StaticSkills []hub.AssetRef

func (s StaticSkills) ListSkills(context.Context) ([]hub.AssetRef, error) {
	out := make([]hub.AssetRef, len(s))
	copy(out, s)
	return out, nil
}
