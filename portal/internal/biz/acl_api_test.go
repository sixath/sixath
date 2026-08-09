package biz

import (
	"context"
	"testing"
	"time"
)

func TestACLAPIUsecaseAddsMembersCreatesGrantsAndIssuesToken(t *testing.T) {
	repo := &aclAPIRepo{
		memberRoles: map[string]string{"org-1/owner": "owner"},
		resources: map[string]*Resource{
			"resource-1": {ID: "resource-1", OwnerUserID: "owner"},
		},
	}
	uc := NewACLAPIUsecase(repo, repo, &aclAPIInviteFake{}, NewAccessChecker(repo), "bootstrap")
	ctx := WithCallerUserID(context.Background(), "owner")

	if err := uc.AddOrgMember(ctx, "org-1", "member", ""); err != nil {
		t.Fatalf("AddOrgMember() error = %v", err)
	}
	if got := repo.memberRoles["org-1/member"]; got != "member" {
		t.Fatalf("member role = %q, want member", got)
	}

	if err := uc.CreateGrant(ctx, "resource-1", "user", "member", PermUse); err != nil {
		t.Fatalf("CreateGrant() error = %v", err)
	}
	if len(repo.grants) != 1 || repo.grants[0].Perm != PermUse {
		t.Fatalf("grants = %#v, want one use grant", repo.grants)
	}

	token, err := uc.IssueUserToken(ctx, "owner")
	if err != nil {
		t.Fatalf("IssueUserToken() error = %v", err)
	}
	if token == "" || repo.tokenHashes["owner"] == "" {
		t.Fatalf("token or persisted hash missing")
	}
}

func TestACLAPIUsecaseRejectsUnauthorizedMutations(t *testing.T) {
	repo := &aclAPIRepo{
		memberRoles: map[string]string{"org-1/member": "member"},
		resources: map[string]*Resource{
			"resource-1": {ID: "resource-1", OwnerUserID: "owner"},
		},
	}
	uc := NewACLAPIUsecase(repo, repo, &aclAPIInviteFake{}, NewAccessChecker(repo), "bootstrap")
	ctx := WithCallerUserID(context.Background(), "member")

	if err := uc.AddOrgMember(ctx, "org-1", "other", "member"); err != ErrForbiddenPerm {
		t.Fatalf("AddOrgMember() error = %v, want ErrForbiddenPerm", err)
	}
	if err := uc.CreateGrant(ctx, "resource-1", "user", "other", PermView); err != ErrForbiddenPerm {
		t.Fatalf("CreateGrant() error = %v, want ErrForbiddenPerm", err)
	}
	if _, err := uc.IssueUserToken(ctx, "owner"); err != ErrForbiddenPerm {
		t.Fatalf("IssueUserToken() error = %v, want ErrForbiddenPerm", err)
	}
}

func TestACLAPIUsecaseCreateOrgAndListMyOrgs(t *testing.T) {
	repo := &aclAPIRepo{
		memberRoles: map[string]string{},
		memberships: map[string][]OrgMembership{},
	}
	uc := NewACLAPIUsecase(repo, repo, &aclAPIInviteFake{}, NewAccessChecker(repo), "bootstrap")
	ctx := WithCallerUserID(context.Background(), "user-1")

	org, err := uc.CreateOrg(ctx, "Acme")
	if err != nil {
		t.Fatalf("CreateOrg() error = %v", err)
	}
	if org.Name != "Acme" || org.ID == "" {
		t.Fatalf("org = %#v, want non-empty id and Acme", org)
	}
	if got := repo.memberRoles[org.ID+"/user-1"]; got != "owner" {
		t.Fatalf("creator role = %q, want owner", got)
	}

	repo.memberships["user-1"] = []OrgMembership{{OrgID: org.ID, Name: "Acme", Role: "owner"}}
	orgs, err := uc.ListMyOrgs(ctx)
	if err != nil {
		t.Fatalf("ListMyOrgs() error = %v", err)
	}
	if len(orgs) != 1 || orgs[0].OrgID != org.ID {
		t.Fatalf("orgs = %#v, want one membership for %q", orgs, org.ID)
	}
}

func TestACLAPIUsecaseInviteOwnerChecks(t *testing.T) {
	repo := &aclAPIRepo{
		memberRoles: map[string]string{
			"org-1/owner":  "owner",
			"org-1/member": "member",
		},
	}
	invites := &aclAPIInviteFake{}
	uc := NewACLAPIUsecase(repo, repo, invites, NewAccessChecker(repo), "bootstrap")

	ownerCtx := WithCallerUserID(context.Background(), "owner")
	plain, invite, err := uc.CreateInvite(ownerCtx, "org-1", 1, 24)
	if err != nil {
		t.Fatalf("CreateInvite(owner) error = %v", err)
	}
	if plain == "" || invite == nil || invite.OrgID != "org-1" {
		t.Fatalf("invite = %#v plain=%q, want org-1 invite", invite, plain)
	}

	memberCtx := WithCallerUserID(context.Background(), "member")
	if _, _, err := uc.CreateInvite(memberCtx, "org-1", 1, 0); err != ErrForbiddenPerm {
		t.Fatalf("CreateInvite(member) error = %v, want ErrForbiddenPerm", err)
	}
	if _, err := uc.ListInvites(memberCtx, "org-1"); err != ErrForbiddenPerm {
		t.Fatalf("ListInvites(member) error = %v, want ErrForbiddenPerm", err)
	}
	if err := uc.RevokeInvite(memberCtx, "org-1", invite.ID); err != ErrForbiddenPerm {
		t.Fatalf("RevokeInvite(member) error = %v, want ErrForbiddenPerm", err)
	}

	list, err := uc.ListInvites(ownerCtx, "org-1")
	if err != nil {
		t.Fatalf("ListInvites(owner) error = %v", err)
	}
	if len(list) != 1 {
		t.Fatalf("invites = %#v, want one invite", list)
	}
	if err := uc.RevokeInvite(ownerCtx, "org-1", invite.ID); err != nil {
		t.Fatalf("RevokeInvite(owner) error = %v", err)
	}
}

type aclAPIInviteFake struct {
	created []*OrgInvite
	plain   map[string]string
}

func (f *aclAPIInviteFake) CreateInvite(_ context.Context, orgID, createdBy string, maxUses int, expiresAt *time.Time) (*OrgInvite, string, error) {
	invite := &OrgInvite{
		ID:        "invite-" + orgID,
		OrgID:     orgID,
		CreatedBy: createdBy,
		MaxUses:   maxUses,
		ExpiresAt: expiresAt,
		CreatedAt: time.Now(),
	}
	if f.plain == nil {
		f.plain = map[string]string{}
	}
	plain := "plain-" + invite.ID
	f.plain[invite.ID] = plain
	f.created = append(f.created, invite)
	return invite, plain, nil
}

func (f *aclAPIInviteFake) GetInviteByTokenHash(context.Context, string) (*OrgInvite, error) {
	return nil, nil
}

func (f *aclAPIInviteFake) ListInvitesByOrg(_ context.Context, orgID string) ([]*OrgInvite, error) {
	var out []*OrgInvite
	for _, invite := range f.created {
		if invite.OrgID == orgID {
			out = append(out, invite)
		}
	}
	return out, nil
}

func (f *aclAPIInviteFake) IncrementInviteUsed(context.Context, string) error { return nil }
func (f *aclAPIInviteFake) RevokeInvite(_ context.Context, id string) error {
	for _, invite := range f.created {
		if invite.ID == id {
			now := time.Now()
			invite.RevokedAt = &now
			return nil
		}
	}
	return nil
}

type aclAPIRepo struct {
	memberRoles map[string]string
	memberships map[string][]OrgMembership
	resources   map[string]*Resource
	grants      []ResourceGrant
	tokenHashes map[string]string
}

func (r *aclAPIRepo) CreateUser(context.Context, string) (*User, error) { return nil, nil }
func (r *aclAPIRepo) GetUser(_ context.Context, id string) (*User, error) {
	return &User{ID: id}, nil
}
func (r *aclAPIRepo) GetUserByEmail(context.Context, string) (*User, error) { return nil, nil }
func (r *aclAPIRepo) CreateUserWithPassword(context.Context, string, string, string, string) (*User, error) {
	return nil, nil
}
func (r *aclAPIRepo) SetEmailVerified(context.Context, string, time.Time) error { return nil }
func (r *aclAPIRepo) SetUserEmailPassword(context.Context, string, string, string) error { return nil }
func (r *aclAPIRepo) CreateOrg(_ context.Context, name string) (*Org, error) {
	return &Org{ID: "org-" + name, Name: name}, nil
}
func (r *aclAPIRepo) GetOrg(context.Context, string) (*Org, error)     { return nil, nil }
func (r *aclAPIRepo) AddMember(_ context.Context, orgID, userID, role string) error {
	r.memberRoles[orgID+"/"+userID] = role
	return nil
}
func (r *aclAPIRepo) MemberRole(_ context.Context, orgID, userID string) (string, error) {
	return r.memberRoles[orgID+"/"+userID], nil
}
func (r *aclAPIRepo) UserOrgIDs(_ context.Context, userID string) ([]string, error) {
	var orgIDs []string
	for key := range r.memberRoles {
		if len(key) > len(userID)+1 && key[len(key)-len(userID):] == userID {
			orgIDs = append(orgIDs, key[:len(key)-len(userID)-1])
		}
	}
	return orgIDs, nil
}
func (r *aclAPIRepo) ListUserOrgs(_ context.Context, userID string) ([]OrgMembership, error) {
	if r.memberships == nil {
		return nil, nil
	}
	return r.memberships[userID], nil
}
func (r *aclAPIRepo) UpsertTokenHash(_ context.Context, userID, tokenHash string) error {
	if r.tokenHashes == nil {
		r.tokenHashes = map[string]string{}
	}
	r.tokenHashes[userID] = tokenHash
	return nil
}
func (r *aclAPIRepo) UserIDByTokenHash(context.Context, string) (string, error) { return "", nil }
func (r *aclAPIRepo) CreateVerifyToken(context.Context, string, time.Time) (string, error) {
	return "", nil
}
func (r *aclAPIRepo) ConsumeVerifyToken(context.Context, string) (string, error) { return "", nil }
func (r *aclAPIRepo) GetResource(_ context.Context, id string) (*Resource, error) {
	return r.resources[id], nil
}
func (r *aclAPIRepo) ListGrants(_ context.Context, resourceID string) ([]ResourceGrant, error) {
	var grants []ResourceGrant
	for _, grant := range r.grants {
		if grant.ResourceID == resourceID {
			grants = append(grants, grant)
		}
	}
	return grants, nil
}
func (r *aclAPIRepo) CreateResource(context.Context, *Resource) (*Resource, error) { return nil, nil }
func (r *aclAPIRepo) UpdateResource(context.Context, *Resource) error              { return nil }
func (r *aclAPIRepo) DeleteResource(context.Context, string) error                 { return nil }
func (r *aclAPIRepo) GetByPayload(context.Context, ResourceType, string) (*Resource, error) {
	return nil, nil
}
func (r *aclAPIRepo) ListAllByType(context.Context, ResourceType) ([]*Resource, error) {
	return nil, nil
}
func (r *aclAPIRepo) ListGrantsByResourceIDs(_ context.Context, resourceIDs []string) (map[string][]ResourceGrant, error) {
	out := make(map[string][]ResourceGrant, len(resourceIDs))
	for _, id := range resourceIDs {
		for _, grant := range r.grants {
			if grant.ResourceID == id {
				out[id] = append(out[id], grant)
			}
		}
	}
	return out, nil
}
func (r *aclAPIRepo) CreateGrant(_ context.Context, grant ResourceGrant) error {
	r.grants = append(r.grants, grant)
	return nil
}
