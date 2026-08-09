package biz

import (
	"context"
	"testing"
)

func TestPermAtLeast(t *testing.T) {
	if !PermAtLeast(PermAdmin, PermView) {
		t.Fatal("admin>=view")
	}
	if PermAtLeast(PermView, PermUse) {
		t.Fatal("view!<use")
	}
	if !PermAtLeast(PermUse, PermUse) {
		t.Fatal("use>=use")
	}
}

type fakeResourceReader struct {
	resources map[string]*Resource
	grants    map[string][]ResourceGrant
	userOrgs  map[string][]string
}

func (f *fakeResourceReader) GetResource(_ context.Context, id string) (*Resource, error) {
	res, ok := f.resources[id]
	if !ok {
		return nil, errNotFound
	}
	return res, nil
}

func (f *fakeResourceReader) ListGrants(_ context.Context, resourceID string) ([]ResourceGrant, error) {
	return f.grants[resourceID], nil
}

func (f *fakeResourceReader) UserOrgIDs(_ context.Context, userID string) ([]string, error) {
	return f.userOrgs[userID], nil
}

type errNotFoundType struct{}

func (errNotFoundType) Error() string { return "not found" }

var errNotFound = errNotFoundType{}

func TestAccessChecker(t *testing.T) {
	const (
		resID    = "res-1"
		ownerID  = "user-owner"
		memberID = "user-member"
		otherID  = "user-other"
		orgID    = "org-home"
		agentA   = "agent-a"
		agentB   = "agent-b"
	)

	baseResource := func() *Resource {
		return &Resource{
			ID:          resID,
			Type:        ResourceTypeSkill,
			Name:        "skill-1",
			OwnerUserID: ownerID,
			Visibility:  VisibilityPrivate,
			HomeOrgID:   orgID,
		}
	}

	tests := []struct {
		name            string
		resource        *Resource
		grants          []ResourceGrant
		userOrgs        map[string][]string
		callerUserID    string
		agentIDForBound string
		wantPerm        Perm
		wantCanView     bool
		wantCanUse      bool
		wantCanEdit     bool
	}{
		{
			name:         "owner",
			resource:     baseResource(),
			callerUserID: ownerID,
			wantPerm:     PermAdmin,
			wantCanView:  true,
			wantCanUse:   true,
			wantCanEdit:  true,
		},
		{
			name: "org member + visibility=org",
			resource: func() *Resource {
				r := baseResource()
				r.Visibility = VisibilityOrg
				return r
			}(),
			userOrgs: map[string][]string{
				memberID: {orgID},
			},
			callerUserID: memberID,
			wantPerm:     PermUse,
			wantCanView:  true,
			wantCanUse:   true,
			wantCanEdit:  false,
		},
		{
			name: "non-member",
			resource: func() *Resource {
				r := baseResource()
				r.Visibility = VisibilityOrg
				return r
			}(),
			userOrgs: map[string][]string{
				otherID: {"org-other"},
			},
			callerUserID: otherID,
			wantPerm:     "",
			wantCanView:  false,
			wantCanUse:   false,
			wantCanEdit:  false,
		},
		{
			name:     "user grant edit",
			resource: baseResource(),
			grants: []ResourceGrant{
				{ResourceID: resID, GranteeType: "user", GranteeID: otherID, Perm: PermEdit},
			},
			callerUserID: otherID,
			wantPerm:     PermEdit,
			wantCanView:  true,
			wantCanUse:   true,
			wantCanEdit:  true,
		},
		{
			name:     "org grant use",
			resource: baseResource(),
			grants: []ResourceGrant{
				{ResourceID: resID, GranteeType: "org", GranteeID: orgID, Perm: PermUse},
			},
			userOrgs: map[string][]string{
				memberID: {orgID},
			},
			callerUserID: memberID,
			wantPerm:     PermUse,
			wantCanView:  true,
			wantCanUse:   true,
			wantCanEdit:  false,
		},
		{
			name: "bound mismatch",
			resource: func() *Resource {
				r := baseResource()
				r.Visibility = VisibilityOrg
				r.BoundAgentID = agentA
				return r
			}(),
			userOrgs: map[string][]string{
				memberID: {orgID},
			},
			callerUserID:    memberID,
			agentIDForBound: agentB,
			wantPerm:        PermView,
			wantCanView:     true,
			wantCanUse:      false,
			wantCanEdit:     false,
		},
		{
			name: "public visibility",
			resource: func() *Resource {
				r := baseResource()
				r.Visibility = VisibilityPublic
				return r
			}(),
			userOrgs: map[string][]string{
				otherID: {orgID},
			},
			callerUserID: otherID,
			wantPerm:     "",
			wantCanView:  false,
			wantCanUse:   false,
			wantCanEdit:  false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			reader := &fakeResourceReader{
				resources: map[string]*Resource{resID: tt.resource},
				grants:    map[string][]ResourceGrant{resID: tt.grants},
				userOrgs:  tt.userOrgs,
			}
			checker := NewAccessChecker(reader)
			ctx := context.Background()

			gotPerm, err := checker.EffectivePerm(ctx, tt.callerUserID, resID, tt.agentIDForBound)
			if err != nil {
				t.Fatalf("EffectivePerm: %v", err)
			}
			if gotPerm != tt.wantPerm {
				t.Fatalf("EffectivePerm = %q, want %q", gotPerm, tt.wantPerm)
			}

			canView, err := checker.Can(ctx, tt.callerUserID, resID, PermView, tt.agentIDForBound)
			if err != nil {
				t.Fatalf("Can(view): %v", err)
			}
			if canView != tt.wantCanView {
				t.Fatalf("Can(view) = %v, want %v", canView, tt.wantCanView)
			}

			canUse, err := checker.Can(ctx, tt.callerUserID, resID, PermUse, tt.agentIDForBound)
			if err != nil {
				t.Fatalf("Can(use): %v", err)
			}
			if canUse != tt.wantCanUse {
				t.Fatalf("Can(use) = %v, want %v", canUse, tt.wantCanUse)
			}

			canEdit, err := checker.Can(ctx, tt.callerUserID, resID, PermEdit, tt.agentIDForBound)
			if err != nil {
				t.Fatalf("Can(edit): %v", err)
			}
			if canEdit != tt.wantCanEdit {
				t.Fatalf("Can(edit) = %v, want %v", canEdit, tt.wantCanEdit)
			}
		})
	}
}
