package biz

import "context"

// VisiblePayloadRefs loads ACL data for one resource type in a few queries and
// returns payload refs the caller can access at the given permission level.
func VisiblePayloadRefs(ctx context.Context, repo ResourceRepo, callerUserID string, resourceType ResourceType, need Perm) (map[string]struct{}, error) {
	resources, err := repo.ListAllByType(ctx, resourceType)
	if err != nil {
		return nil, err
	}
	if len(resources) == 0 {
		return map[string]struct{}{}, nil
	}

	orgIDs, err := repo.UserOrgIDs(ctx, callerUserID)
	if err != nil {
		return nil, err
	}

	// Collect resource IDs for a single batch grants query.
	allIDs := make([]string, 0, len(resources))
	for _, r := range resources {
		allIDs = append(allIDs, r.ID)
	}
	grantsByID, err := repo.ListGrantsByResourceIDs(ctx, allIDs)
	if err != nil {
		return nil, err
	}

	visible := make(map[string]struct{}, len(resources))
	for _, r := range resources {
		combined := grantsByID[r.ID]
		have := EvaluatePerm(r, orgIDs, combined, callerUserID, "")
		if PermAtLeast(have, need) {
			visible[r.PayloadRef] = struct{}{}
		}
	}
	return visible, nil
}
