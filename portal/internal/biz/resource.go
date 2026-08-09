package biz

import "context"

type Perm string

const (
	PermView  Perm = "view"
	PermUse   Perm = "use"
	PermEdit  Perm = "edit"
	PermAdmin Perm = "admin"
)

func permRank(p Perm) int {
	switch p {
	case PermView:
		return 1
	case PermUse:
		return 2
	case PermEdit:
		return 3
	case PermAdmin:
		return 4
	default:
		return 0
	}
}

func PermAtLeast(have, need Perm) bool {
	return permRank(have) >= permRank(need)
}

func maxPerm(a, b Perm) Perm {
	if PermAtLeast(a, b) {
		return a
	}
	return b
}

type Visibility string

const (
	VisibilityPrivate Visibility = "private"
	VisibilityOrg     Visibility = "org"
	VisibilityPublic  Visibility = "public"
)

type ResourceType string

const (
	ResourceTypeAgent     ResourceType = "agent"
	ResourceTypeTool      ResourceType = "tool"
	ResourceTypeSkill     ResourceType = "skill"
	ResourceTypeMcpServer ResourceType = "mcp_server"
)

type Resource struct {
	ID           string
	Type         ResourceType
	Name         string
	OwnerUserID  string
	Visibility   Visibility
	HomeOrgID    string
	BoundAgentID string
	PayloadRef   string
}

type ResourceGrant struct {
	ResourceID  string
	GranteeType string // user|org
	GranteeID   string
	Perm        Perm
}

type ResourceReader interface {
	GetResource(ctx context.Context, id string) (*Resource, error)
	ListGrants(ctx context.Context, resourceID string) ([]ResourceGrant, error)
	UserOrgIDs(ctx context.Context, userID string) ([]string, error)
}

type AccessChecker struct{ r ResourceReader }

func NewAccessChecker(r ResourceReader) *AccessChecker { return &AccessChecker{r: r} }

// EvaluatePerm computes effective permission from already-loaded ACL inputs (no I/O).
func EvaluatePerm(res *Resource, orgIDs []string, grants []ResourceGrant, callerUserID, agentIDForBound string) Perm {
	if res == nil {
		return ""
	}
	var base Perm

	if callerUserID == res.OwnerUserID {
		base = maxPerm(base, PermAdmin)
	}

	if res.Visibility == VisibilityOrg && orgContains(orgIDs, res.HomeOrgID) {
		base = maxPerm(base, PermUse)
	}

	orgSet := toOrgSet(orgIDs)
	for _, g := range grants {
		switch g.GranteeType {
		case "user":
			if g.GranteeID == callerUserID {
				base = maxPerm(base, g.Perm)
			}
		case "org":
			if orgSet[g.GranteeID] {
				base = maxPerm(base, g.Perm)
			}
		}
	}

	if res.BoundAgentID != "" && agentIDForBound != res.BoundAgentID && callerUserID != res.OwnerUserID {
		if permRank(base) >= permRank(PermUse) {
			base = PermView
		}
	}

	return base
}

// EffectivePerm returns the caller's highest effective permission on the resource; empty if none.
func (c *AccessChecker) EffectivePerm(ctx context.Context, callerUserID, resourceID, agentIDForBound string) (Perm, error) {
	res, err := c.r.GetResource(ctx, resourceID)
	if err != nil {
		return "", err
	}

	orgIDs, err := c.r.UserOrgIDs(ctx, callerUserID)
	if err != nil {
		return "", err
	}

	grants, err := c.r.ListGrants(ctx, resourceID)
	if err != nil {
		return "", err
	}

	return EvaluatePerm(res, orgIDs, grants, callerUserID, agentIDForBound), nil
}

// Can is true when EffectivePerm >= need; bound mismatch denies use-level checks for non-owners.
func (c *AccessChecker) Can(ctx context.Context, callerUserID, resourceID string, need Perm, agentIDForBound string) (bool, error) {
	have, err := c.EffectivePerm(ctx, callerUserID, resourceID, agentIDForBound)
	if err != nil {
		return false, err
	}
	return PermAtLeast(have, need), nil
}

func orgContains(orgIDs []string, orgID string) bool {
	for _, id := range orgIDs {
		if id == orgID {
			return true
		}
	}
	return false
}

func toOrgSet(orgIDs []string) map[string]bool {
	set := make(map[string]bool, len(orgIDs))
	for _, id := range orgIDs {
		set[id] = true
	}
	return set
}
