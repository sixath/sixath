package fake

import (
	"context"
	"fmt"
	"sort"
	"sync"

	"github.com/sixath/framework/memory/hub"
	"github.com/sixath/framework/memory/hub/local"
)

const Name = "fake"

// Adapter is a test/dev external Governance+Knowledge+SkillSource.
// Inject TransportErr to exercise §3.5.1 read fail-open paths.
type Adapter struct {
	mu           sync.Mutex
	skills       map[string]hub.SkillContent // skillID → content
	store        local.BindingStore
	TransportErr error // when set, read methods return ErrTransport wrap
}

func New(store local.BindingStore) *Adapter {
	if store == nil {
		store = local.NewMemoryBindingStore()
	}
	return &Adapter{skills: map[string]hub.SkillContent{}, store: store}
}

func (a *Adapter) Name() string { return Name }

func (a *Adapter) Capabilities() hub.Capabilities {
	return hub.Capabilities{Write: true, Flags: map[string]bool{"external": true}}
}

// PutSkill registers remote skill content for FetchSkill / ListAccessible.
func (a *Adapter) PutSkill(c hub.SkillContent) {
	a.mu.Lock()
	defer a.mu.Unlock()
	c.Hub = Name
	a.skills[c.SkillID] = c
}

func (a *Adapter) FetchSkill(_ context.Context, hubName, skillID string) (hub.SkillContent, error) {
	if hubName != Name {
		return hub.SkillContent{}, fmt.Errorf("%w: wrong hub", hub.ErrNotSupported)
	}
	if a.TransportErr != nil {
		return hub.SkillContent{}, fmt.Errorf("%w: %v", hub.ErrTransport, a.TransportErr)
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	c, ok := a.skills[skillID]
	if !ok {
		return hub.SkillContent{}, fmt.Errorf("%w: skill %q", hub.ErrSkillNotMaterialized, skillID)
	}
	return c, nil
}

func (a *Adapter) ResolveLoadout(ctx context.Context, id hub.Identity) ([]hub.AssetRef, error) {
	if a.TransportErr != nil {
		return nil, fmt.Errorf("%w: %v", hub.ErrTransport, a.TransportErr)
	}
	binds, err := a.store.ListByAgent(id.AgentID)
	if err != nil {
		return nil, err
	}
	out := make([]hub.AssetRef, 0, len(binds))
	for _, b := range binds {
		st := b.Status
		if st == "" {
			st = hub.AssetActive
		}
		if !local.LoadoutEligible(st) {
			continue
		}
		out = append(out, hub.AssetRef{
			Kind: b.AssetKind, ID: b.AssetID, Hub: Name, Name: b.Name, Status: st,
		})
	}
	return out, nil
}

func (a *Adapter) CheckAccess(ctx context.Context, id hub.Identity, asset hub.AssetRef, action string) (bool, error) {
	if a.TransportErr != nil {
		return false, fmt.Errorf("%w: %v", hub.ErrTransport, a.TransportErr)
	}
	if asset.Hub != "" && asset.Hub != Name {
		return false, nil
	}
	return true, nil
}

func (a *Adapter) ListAccessible(ctx context.Context, id hub.Identity, filter hub.AssetFilter) (hub.Page[hub.AssetRef], error) {
	if a.TransportErr != nil {
		return hub.Page[hub.AssetRef]{}, fmt.Errorf("%w: %v", hub.ErrTransport, a.TransportErr)
	}
	a.mu.Lock()
	items := make([]hub.AssetRef, 0, len(a.skills))
	for _, c := range a.skills {
		st := hub.AssetActive
		if !c.Signed {
			st = hub.AssetDraft
		}
		items = append(items, hub.AssetRef{
			Kind: hub.AssetKindSkill, ID: c.SkillID, Hub: Name, Name: c.Name, Version: c.Version, Status: st,
		})
	}
	a.mu.Unlock()
	sort.Slice(items, func(i, j int) bool { return items[i].ID < items[j].ID })
	if filter.Kind != "" {
		filtered := items[:0]
		for _, it := range items {
			if it.Kind == filter.Kind {
				filtered = append(filtered, it)
			}
		}
		items = filtered
	}
	return hub.Page[hub.AssetRef]{Items: items, Total: len(items)}, nil
}

func (a *Adapter) BindAssets(ctx context.Context, agentID string, refs []hub.AssetRef) error {
	if err := hub.EnforceHub(a, refs...); err != nil {
		return err
	}
	for _, r := range refs {
		st := r.Status
		if st == "" {
			st = hub.AssetActive
		}
		if err := a.store.Upsert(local.Binding{
			AgentID: agentID, AssetKind: r.Kind, AssetID: r.ID, Hub: Name, Name: r.Name, Status: st, Meta: r.Meta,
		}); err != nil {
			return err
		}
	}
	return nil
}

func (a *Adapter) UnbindAssets(ctx context.Context, agentID string, refs []hub.AssetRef) error {
	if err := hub.EnforceHub(a, refs...); err != nil {
		return err
	}
	for _, r := range refs {
		if err := a.store.Delete(agentID, r.Kind, r.ID); err != nil {
			return err
		}
	}
	return nil
}

func (a *Adapter) SetVisibility(ctx context.Context, asset hub.AssetRef, v hub.Visibility) error {
	if err := hub.EnforceHub(a, asset); err != nil {
		return err
	}
	return a.store.UpdateMeta(asset.Kind, asset.ID, &v, nil)
}

func (a *Adapter) SetStatus(ctx context.Context, asset hub.AssetRef, status hub.AssetStatus) error {
	if err := hub.EnforceHub(a, asset); err != nil {
		return err
	}
	return a.store.UpdateMeta(asset.Kind, asset.ID, nil, &status)
}

func (a *Adapter) DescribeTools() []hub.ToolDesc {
	return []hub.ToolDesc{{
		Name:        "knowledge_search",
		Description: "Fake external knowledge search",
		InputSchema: map[string]any{"type": "object", "properties": map[string]any{"query": map[string]any{"type": "string"}}, "required": []string{"query"}},
	}}
}

func (a *Adapter) Call(ctx context.Context, id hub.Identity, tool string, args map[string]any) (any, error) {
	if a.TransportErr != nil {
		return nil, fmt.Errorf("%w: %v", hub.ErrTransport, a.TransportErr)
	}
	if tool != "knowledge_search" {
		return nil, hub.ErrNotSupported
	}
	return map[string]any{"hits": []any{}, "hub": Name}, nil
}

var (
	_ hub.GovernanceProvider = (*Adapter)(nil)
	_ hub.GovernanceWriter   = (*Adapter)(nil)
	_ hub.KnowledgeProvider  = (*Adapter)(nil)
	_ hub.SkillSource        = (*Adapter)(nil)
)
