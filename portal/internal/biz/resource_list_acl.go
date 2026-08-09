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

	resourceIDs := make([]string, 0, len(resources))
	for _, resource := range resources {
		resourceIDs = append(resourceIDs, resource.ID)
	}
	grantsByID, err := repo.ListGrantsByResourceIDs(ctx, resourceIDs)
	if err != nil {
		return nil, err
	}

	visible := make(map[string]struct{}, len(resources))
	for _, resource := range resources {
		have := EvaluatePerm(resource, orgIDs, grantsByID[resource.ID], callerUserID, "")
		if PermAtLeast(have, need) {
			visible[resource.PayloadRef] = struct{}{}
		}
	}
	return visible, nil
}
