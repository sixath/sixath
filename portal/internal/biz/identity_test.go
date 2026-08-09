package biz

import (
	"context"
	"crypto/sha256"
	"fmt"
	"testing"
	"time"
)

type fakeIdentityRepo struct {
	orgIDs     map[string][]string
	added      []string
	tokenUsers map[string]string
	upserted   map[string]string
	users      map[string]*User
}

func (f *fakeIdentityRepo) CreateUser(context.Context, string) (*User, error) {
	panic("not implemented")
}

func (f *fakeIdentityRepo) GetUser(_ context.Context, id string) (*User, error) {
	return f.users[id], nil
}

func (f *fakeIdentityRepo) GetUserByEmail(context.Context, string) (*User, error) {
	panic("not implemented")
}

func (f *fakeIdentityRepo) CreateUserWithPassword(context.Context, string, string, string, string) (*User, error) {
	panic("not implemented")
}

func (f *fakeIdentityRepo) SetEmailVerified(context.Context, string, time.Time) error {
	panic("not implemented")
}

func (f *fakeIdentityRepo) SetUserEmailPassword(context.Context, string, string, string) error {
	panic("not implemented")
}

func (f *fakeIdentityRepo) CreateOrg(context.Context, string) (*Org, error) {
	panic("not implemented")
}

func (f *fakeIdentityRepo) GetOrg(context.Context, string) (*Org, error) {
	panic("not implemented")
}

func (f *fakeIdentityRepo) AddMember(_ context.Context, orgID, userID, role string) error {
	f.added = append(f.added, fmt.Sprintf("%s:%s:%s", orgID, userID, role))
	f.orgIDs[userID] = append(f.orgIDs[userID], orgID)
	return nil
}

func (f *fakeIdentityRepo) MemberRole(context.Context, string, string) (string, error) {
	return "", nil
}

func (f *fakeIdentityRepo) UserOrgIDs(_ context.Context, userID string) ([]string, error) {
	return f.orgIDs[userID], nil
}

func (f *fakeIdentityRepo) ListUserOrgs(context.Context, string) ([]OrgMembership, error) {
	panic("not implemented")
}

func (f *fakeIdentityRepo) UpsertTokenHash(_ context.Context, userID, tokenHash string) error {
	f.upserted[tokenHash] = userID
	return nil
}

func (f *fakeIdentityRepo) UserIDByTokenHash(_ context.Context, tokenHash string) (string, error) {
	return f.tokenUsers[tokenHash], nil
}

func (f *fakeIdentityRepo) CreateVerifyToken(context.Context, string, time.Time) (string, error) {
	panic("not implemented")
}

func (f *fakeIdentityRepo) ConsumeVerifyToken(context.Context, string) (string, error) {
	panic("not implemented")
}

func TestEnsureMemberAddsOnlyMissingMembership(t *testing.T) {
	repo := &fakeIdentityRepo{orgIDs: map[string][]string{"user-1": {"org-1"}}, upserted: map[string]string{}}
	ctx := context.Background()

	if err := EnsureMember(ctx, repo, "org-1", "user-1", "member"); err != nil {
		t.Fatalf("EnsureMember(existing): %v", err)
	}
	if err := EnsureMember(ctx, repo, "org-2", "user-1", "member"); err != nil {
		t.Fatalf("EnsureMember(missing): %v", err)
	}

	if len(repo.added) != 1 || repo.added[0] != "org-2:user-1:member" {
		t.Fatalf("added memberships = %v, want only org-2 membership", repo.added)
	}
}

func TestIssueTokenStoresSHA256Hash(t *testing.T) {
	repo := &fakeIdentityRepo{orgIDs: map[string][]string{}, upserted: map[string]string{}}

	if err := IssueToken(context.Background(), repo, "user-1", "plain-token"); err != nil {
		t.Fatalf("IssueToken: %v", err)
	}

	hash := fmt.Sprintf("%x", sha256.Sum256([]byte("plain-token")))
	if got := repo.upserted[hash]; got != "user-1" {
		t.Fatalf("stored user = %q, want user-1", got)
	}
}

func TestLookupUserByTokenHashReturnsMappedUser(t *testing.T) {
	repo := &fakeIdentityRepo{
		orgIDs:     map[string][]string{},
		upserted:   map[string]string{},
		tokenUsers: map[string]string{"token-hash": "user-1"},
		users:      map[string]*User{"user-1": {ID: "user-1", Name: "Ada"}},
	}

	user, err := LookupUserByTokenHash(context.Background(), repo, "token-hash")
	if err != nil {
		t.Fatalf("LookupUserByTokenHash: %v", err)
	}
	if user.ID != "user-1" || user.Name != "Ada" {
		t.Fatalf("user = %#v, want user-1/Ada", user)
	}
}
