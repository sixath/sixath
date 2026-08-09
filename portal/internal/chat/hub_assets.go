package chat

import (
	"context"
	"fmt"
	"sort"
	"strings"

	"backend/internal/biz"

	"github.com/sixath/framework/memory/hub"
	"github.com/sixath/framework/memory/hub/local"
	"github.com/sixath/framework/skills"
)

// AssetJSON is the HTTP/JSON shape for hub AssetRef.
type AssetJSON struct {
	Kind   string `json:"kind"`
	ID     string `json:"id"`
	Hub    string `json:"hub"`
	Name   string `json:"name,omitempty"`
	Status string `json:"status,omitempty"`
}

// LoadoutView is GET .../hub/loadout response.
type LoadoutView struct {
	Provider string      `json:"provider"`
	Items    []AssetJSON `json:"items"`
	Total    int         `json:"total"`
}

// BindingsView is GET .../hub/bindings response (explicit binds only).
type BindingsView struct {
	Provider string      `json:"provider"`
	Items    []AssetJSON `json:"items"`
	Total    int         `json:"total"`
}

// BindAssetsRequest is POST body for bind/unbind.
type BindAssetsRequest struct {
	Assets []AssetJSON `json:"assets"`
}

func assetToJSON(a hub.AssetRef) AssetJSON {
	return AssetJSON{
		Kind:   string(a.Kind),
		ID:     a.ID,
		Hub:    a.Hub,
		Name:   a.Name,
		Status: string(a.Status),
	}
}

func assetsFromJSON(in []AssetJSON, defaultHub string) []hub.AssetRef {
	out := make([]hub.AssetRef, 0, len(in))
	for _, a := range in {
		hubName := strings.TrimSpace(a.Hub)
		if hubName == "" {
			hubName = defaultHub
		}
		kind := hub.AssetKind(strings.TrimSpace(a.Kind))
		if kind == "" {
			kind = hub.AssetKindSkill
		}
		id := strings.TrimSpace(a.ID)
		if id == "" {
			id = strings.TrimSpace(a.Name)
		}
		st := hub.AssetStatus(strings.TrimSpace(a.Status))
		if st == "" {
			st = hub.AssetActive
		}
		out = append(out, hub.AssetRef{
			Kind:   kind,
			ID:     id,
			Hub:    hubName,
			Name:   strings.TrimSpace(a.Name),
			Status: st,
		})
	}
	return out
}

func govWriter(gov hub.GovernanceProvider) (hub.GovernanceWriter, error) {
	w, ok := gov.(hub.GovernanceWriter)
	if !ok {
		return nil, fmt.Errorf("hub: provider %q does not support writes", gov.Name())
	}
	return w, nil
}

// ListAgentLoadout returns ResolveLoadout merged with workspace skills (local SkillLister).
func ListAgentLoadout(ctx context.Context, rt biz.RuntimeToolsConfig, agentID string, skillsIdx *skills.Index) (LoadoutView, error) {
	gov, _, err := ResolveForRuntimeTools(rt)
	if err != nil {
		return LoadoutView{}, err
	}
	id := hub.Identity{AgentID: agentID}
	// Prefer per-request LocalGovernance with skills when provider is local.
	if gov.Name() == "local" {
		store := HubBindingStore()
		if store == nil {
			store = local.NewMemoryBindingStore()
		}
		lg := local.NewLocalGovernance(store, skillIndexLister{idx: skillsIdx}, local.GovernanceConfig{})
		refs, err := lg.ResolveLoadout(ctx, id)
		if err != nil {
			return LoadoutView{}, err
		}
		return loadoutView(gov.Name(), refs), nil
	}
	refs, err := gov.ResolveLoadout(ctx, id)
	if err != nil {
		return LoadoutView{}, err
	}
	return loadoutView(gov.Name(), refs), nil
}

type skillIndexLister struct {
	idx *skills.Index
}

func (s skillIndexLister) ListSkills(context.Context) ([]hub.AssetRef, error) {
	if s.idx == nil {
		return nil, nil
	}
	all := s.idx.All()
	out := make([]hub.AssetRef, 0, len(all))
	for _, m := range all {
		out = append(out, hub.AssetRef{
			Kind:   hub.AssetKindSkill,
			ID:     m.Name,
			Hub:    "local",
			Name:   m.Name,
			Status: hub.AssetActive,
		})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out, nil
}

func loadoutView(provider string, refs []hub.AssetRef) LoadoutView {
	items := make([]AssetJSON, len(refs))
	for i, r := range refs {
		items[i] = assetToJSON(r)
	}
	return LoadoutView{Provider: provider, Items: items, Total: len(items)}
}

// ListAgentBindings returns explicit bindings only (not default skills_dirs).
func ListAgentBindings(ctx context.Context, rt biz.RuntimeToolsConfig, agentID string) (BindingsView, error) {
	gov, _, err := ResolveForRuntimeTools(rt)
	if err != nil {
		return BindingsView{}, err
	}
	store := HubBindingStore()
	if store == nil {
		return BindingsView{Provider: gov.Name(), Items: []AssetJSON{}, Total: 0}, nil
	}
	binds, err := store.ListByAgent(agentID)
	if err != nil {
		return BindingsView{}, err
	}
	items := make([]AssetJSON, 0, len(binds))
	for _, b := range binds {
		items = append(items, AssetJSON{
			Kind:   string(b.AssetKind),
			ID:     b.AssetID,
			Hub:    b.Hub,
			Name:   b.Name,
			Status: string(b.Status),
		})
	}
	return BindingsView{Provider: gov.Name(), Items: items, Total: len(items)}, nil
}

// BindAgentAssets binds refs via Resolve'd GovernanceWriter.
// External skills (Hub != local) must pass SkillTrustGate materialization first (§3.5.3).
func BindAgentAssets(ctx context.Context, rt biz.RuntimeToolsConfig, agentID string, assets []AssetJSON) error {
	gov, _, err := ResolveForRuntimeTools(rt)
	if err != nil {
		return err
	}
	w, err := govWriter(gov)
	if err != nil {
		return err
	}
	refs := assetsFromJSON(assets, gov.Name())
	for _, r := range refs {
		if r.ID == "" {
			return fmt.Errorf("hub: asset id required")
		}
	}
	refs, err = PrepareBindRefs(ctx, refs)
	if err != nil {
		return err
	}
	return w.BindAssets(ctx, agentID, refs)
}

// UnbindAgentAssets unbinds refs via Resolve'd GovernanceWriter.
func UnbindAgentAssets(ctx context.Context, rt biz.RuntimeToolsConfig, agentID string, assets []AssetJSON) error {
	gov, _, err := ResolveForRuntimeTools(rt)
	if err != nil {
		return err
	}
	w, err := govWriter(gov)
	if err != nil {
		return err
	}
	return w.UnbindAssets(ctx, agentID, assetsFromJSON(assets, gov.Name()))
}

// ClearAgentBindings removes all explicit bindings for the agent (provider switch helper).
func ClearAgentBindings(ctx context.Context, rt biz.RuntimeToolsConfig, agentID string) (int, error) {
	view, err := ListAgentBindings(ctx, rt, agentID)
	if err != nil {
		return 0, err
	}
	if len(view.Items) == 0 {
		return 0, nil
	}
	if err := UnbindAgentAssets(ctx, rt, agentID, view.Items); err != nil {
		return 0, err
	}
	return len(view.Items), nil
}

// SetAssetStatusRequest is POST body for status changes (approve draft → active).
type SetAssetStatusRequest struct {
	Asset  AssetJSON `json:"asset"`
	Status string    `json:"status"`
}

// SetAgentAssetStatus updates asset status via GovernanceWriter (e.g. draft→active approve).
func SetAgentAssetStatus(ctx context.Context, rt biz.RuntimeToolsConfig, asset AssetJSON, status string) error {
	gov, _, err := ResolveForRuntimeTools(rt)
	if err != nil {
		return err
	}
	w, err := govWriter(gov)
	if err != nil {
		return err
	}
	st := hub.AssetStatus(strings.TrimSpace(status))
	if st == "" {
		return fmt.Errorf("hub: status required")
	}
	refs := assetsFromJSON([]AssetJSON{asset}, gov.Name())
	if len(refs) == 0 || refs[0].ID == "" {
		return fmt.Errorf("hub: asset id required")
	}
	return w.SetStatus(ctx, refs[0], st)
}
