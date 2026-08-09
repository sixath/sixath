package chat

import (
	"context"
	"fmt"

	"github.com/sixath/framework/memory/hub"
)

// MaterializeExternalSkill runs SkillTrustGate for a non-local skill asset.
// Returns the trusted AssetRef (may be draft). local hub skills are returned unchanged.
func MaterializeExternalSkill(ctx context.Context, ref hub.AssetRef) (hub.AssetRef, error) {
	if ref.Hub == "" || ref.Hub == "local" {
		return ref, nil
	}
	if ref.Kind != "" && ref.Kind != hub.AssetKindSkill {
		return ref, nil
	}
	src := HubSkillSource(ref.Hub)
	if src == nil {
		return hub.AssetRef{}, fmt.Errorf("%w: no skill source for hub %q", hub.ErrSkillNotMaterialized, ref.Hub)
	}
	gate := HubTrustGate()
	if gate == nil {
		return hub.AssetRef{}, fmt.Errorf("%w: trust gate not initialized", hub.ErrSkillNotMaterialized)
	}
	content, err := src.FetchSkill(ctx, ref.Hub, ref.ID)
	if err != nil {
		return hub.AssetRef{}, err
	}
	if content.Name == "" {
		content.Name = ref.Name
	}
	res, err := gate.Materialize(ctx, content)
	if err != nil {
		return hub.AssetRef{}, err
	}
	return res.Ref, nil
}

// PrepareBindRefs materializes any external skills before BindAssets.
func PrepareBindRefs(ctx context.Context, refs []hub.AssetRef) ([]hub.AssetRef, error) {
	out := make([]hub.AssetRef, 0, len(refs))
	for _, r := range refs {
		mr, err := MaterializeExternalSkill(ctx, r)
		if err != nil {
			return nil, err
		}
		out = append(out, mr)
	}
	return out, nil
}
