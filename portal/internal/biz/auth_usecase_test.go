package biz

import (
	"context"
	"fmt"
	"sync"
	"testing"
	"time"

	pkgErrors "backend/internal/pkg/errors"
)

const testInvitePlain = "invite-plain-token"

func TestAuthUsecaseLoginSuccess(t *testing.T) {
	hash, err := HashPassword("secret-pass")
	if err != nil {
		t.Fatalf("HashPassword: %v", err)
	}
	identities := newAuthIdentityFake()
	identities.usersByEmail["ada@example.com"] = &User{
		ID:           "user-1",
		Email:        "ada@example.com",
		PasswordHash: hash,
	}
	identities.orgs["org-1"] = &Org{ID: "org-1", Name: "Acme"}
	identities.memberships["user-1"] = []OrgMembership{{OrgID: "org-1", Name: "Acme", Role: "member"}}

	uc := NewAuthUsecase(identities, newAuthInviteFake(), nil, false)
	session, err := uc.Login(context.Background(), "ada@example.com", "secret-pass")
	if err != nil {
		t.Fatalf("Login() error = %v", err)
	}
	if session.Token == "" {
		t.Fatal("Login() token is empty")
	}
	if session.UserID != "user-1" || session.Email != "ada@example.com" {
		t.Fatalf("session = %#v, want user-1/ada@example.com", session)
	}
	if len(session.Orgs) != 1 || session.Orgs[0].OrgID != "org-1" {
		t.Fatalf("session orgs = %#v, want org-1 membership", session.Orgs)
	}
	if len(identities.upserted) != 1 {
		t.Fatalf("token upserts = %d, want 1", len(identities.upserted))
	}
}

func TestAuthUsecaseLoginBadPasswordUnauthorized(t *testing.T) {
	hash, err := HashPassword("secret-pass")
	if err != nil {
		t.Fatalf("HashPassword: %v", err)
	}
	identities := newAuthIdentityFake()
	identities.usersByEmail["ada@example.com"] = &User{
		ID:           "user-1",
		Email:        "ada@example.com",
		PasswordHash: hash,
	}
	uc := NewAuthUsecase(identities, newAuthInviteFake(), nil, false)

	_, err = uc.Login(context.Background(), "ada@example.com", "wrong-pass")
	if !isReason(err, "UNAUTHORIZED") {
		t.Fatalf("Login(bad password) error = %v, want UNAUTHORIZED", err)
	}

	_, err = uc.Login(context.Background(), "missing@example.com", "secret-pass")
	if !isReason(err, "UNAUTHORIZED") {
		t.Fatalf("Login(unknown email) error = %v, want UNAUTHORIZED", err)
	}
}

func TestAuthUsecaseRegisterValidInvite(t *testing.T) {
	identities := newAuthIdentityFake()
	identities.orgs["org-1"] = &Org{ID: "org-1", Name: "Acme"}
	invites := newAuthInviteFake()
	invites.byHash[HashTokenSHA256Hex(testInvitePlain)] = &OrgInvite{
		ID:      "invite-1",
		OrgID:   "org-1",
		MaxUses: 1,
	}

	uc := NewAuthUsecase(identities, invites, nil, false)
	session, err := uc.Register(context.Background(), "new@example.com", "secret-pass", testInvitePlain)
	if err != nil {
		t.Fatalf("Register() error = %v", err)
	}
	if session.Token == "" || session.UserID == "" {
		t.Fatalf("session = %#v, want token and user id", session)
	}
	if session.Email != "new@example.com" {
		t.Fatalf("session email = %q, want new@example.com", session.Email)
	}
	if len(identities.added) != 1 || identities.added[0] != "org-1:"+session.UserID+":member" {
		t.Fatalf("added memberships = %v, want org-1 member", identities.added)
	}
	if got := identities.usersByEmail["new@example.com"].Name; got != "new" {
		t.Fatalf("user name = %q, want new", got)
	}
	if invites.usedCount["invite-1"] != 1 {
		t.Fatalf("used_count = %d, want 1", invites.usedCount["invite-1"])
	}
}

func TestAuthUsecaseRegisterReusedSingleUseInviteBadRequest(t *testing.T) {
	identities := newAuthIdentityFake()
	identities.orgs["org-1"] = &Org{ID: "org-1", Name: "Acme"}
	invites := newAuthInviteFake()
	invites.byHash[HashTokenSHA256Hex(testInvitePlain)] = &OrgInvite{
		ID:        "invite-1",
		OrgID:     "org-1",
		MaxUses:   1,
		UsedCount: 1,
	}

	uc := NewAuthUsecase(identities, invites, nil, false)
	_, err := uc.Register(context.Background(), "new@example.com", "secret-pass", testInvitePlain)
	if !isReason(err, "INVALID_INVITE") {
		t.Fatalf("Register(reused invite) error = %v, want INVALID_INVITE", err)
	}
	if len(identities.usersByEmail) != 0 {
		t.Fatalf("users created = %d, want 0", len(identities.usersByEmail))
	}
}

func TestAuthUsecaseRegisterDuplicateEmailConflict(t *testing.T) {
	identities := newAuthIdentityFake()
	identities.usersByEmail["exists@example.com"] = &User{ID: "user-1", Email: "exists@example.com"}
	identities.orgs["org-1"] = &Org{ID: "org-1", Name: "Acme"}
	invites := newAuthInviteFake()
	invites.byHash[HashTokenSHA256Hex(testInvitePlain)] = &OrgInvite{
		ID:      "invite-1",
		OrgID:   "org-1",
		MaxUses: 1,
	}

	uc := NewAuthUsecase(identities, invites, nil, false)
	_, err := uc.Register(context.Background(), "exists@example.com", "secret-pass", testInvitePlain)
	if !isReason(err, "CONFLICT") {
		t.Fatalf("Register(duplicate email) error = %v, want CONFLICT", err)
	}
	if invites.usedCount["invite-1"] != 0 {
		t.Fatalf("used_count = %d, want 0", invites.usedCount["invite-1"])
	}
}

func TestAuthUsecasePreviewInviteValidAndInvalid(t *testing.T) {
	identities := newAuthIdentityFake()
	identities.orgs["org-1"] = &Org{ID: "org-1", Name: "Acme"}
	invites := newAuthInviteFake()
	invites.byHash[HashTokenSHA256Hex(testInvitePlain)] = &OrgInvite{
		ID:      "invite-1",
		OrgID:   "org-1",
		MaxUses: 1,
	}

	uc := NewAuthUsecase(identities, invites, nil, false)

	valid, err := uc.PreviewInvite(context.Background(), testInvitePlain)
	if err != nil {
		t.Fatalf("PreviewInvite(valid) error = %v", err)
	}
	if !valid.Valid || valid.OrgName != "Acme" {
		t.Fatalf("PreviewInvite(valid) = %#v, want valid Acme preview", valid)
	}

	invalid, err := uc.PreviewInvite(context.Background(), "missing-token")
	if err != nil {
		t.Fatalf("PreviewInvite(invalid) error = %v", err)
	}
	if invalid.Valid {
		t.Fatalf("PreviewInvite(invalid) = %#v, want invalid preview", invalid)
	}
}

func TestAuthUsecaseVerifyEmailSuccess(t *testing.T) {
	identities := newAuthIdentityFake()
	identities.usersByID["user-1"] = &User{ID: "user-1", Email: "ada@example.com"}
	plain := "verify-plain-token"
	identities.verifyByHash[HashTokenSHA256Hex(plain)] = "user-1"

	uc := NewAuthUsecase(identities, newAuthInviteFake(), nil, false)
	if err := uc.VerifyEmail(context.Background(), plain); err != nil {
		t.Fatalf("VerifyEmail() error = %v", err)
	}
	if identities.usersByID["user-1"].EmailVerifiedAt == nil {
		t.Fatal("EmailVerifiedAt is nil after VerifyEmail")
	}
	if _, ok := identities.verifyByHash[HashTokenSHA256Hex(plain)]; ok {
		t.Fatal("verify token was not consumed")
	}
}

func TestAuthUsecaseRegisterConcurrentSingleUseInvite(t *testing.T) {
	identities := newAuthIdentityFake()
	identities.orgs["org-1"] = &Org{ID: "org-1", Name: "Acme"}
	invites := newAuthInviteFake()
	invites.byHash[HashTokenSHA256Hex(testInvitePlain)] = &OrgInvite{
		ID:      "invite-1",
		OrgID:   "org-1",
		MaxUses: 1,
	}

	uc := NewAuthUsecase(identities, invites, nil, false)
	var wg sync.WaitGroup
	results := make([]error, 2)
	emails := []string{"first@example.com", "second@example.com"}
	for i := range emails {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			_, err := uc.Register(context.Background(), emails[idx], "secret-pass", testInvitePlain)
			results[idx] = err
		}(i)
	}
	wg.Wait()

	successes := 0
	for _, err := range results {
		if err == nil {
			successes++
		}
	}
	if successes != 1 {
		t.Fatalf("successful registrations = %d, want 1; errors = %v", successes, results)
	}
	if invites.usedCount["invite-1"] != 1 {
		t.Fatalf("used_count = %d, want 1", invites.usedCount["invite-1"])
	}
	if len(identities.usersByEmail) != 1 {
		t.Fatalf("users created = %d, want 1", len(identities.usersByEmail))
	}
}

func TestAuthUsecaseRegisterVerifyEmailBestEffort(t *testing.T) {
	identities := newAuthIdentityFake()
	identities.orgs["org-1"] = &Org{ID: "org-1", Name: "Acme"}
	invites := newAuthInviteFake()
	invites.byHash[HashTokenSHA256Hex(testInvitePlain)] = &OrgInvite{
		ID:      "invite-1",
		OrgID:   "org-1",
		MaxUses: 1,
	}
	mailer := authMailerFake{err: pkgErrors.ErrConflict}

	uc := NewAuthUsecase(identities, invites, mailer, true)
	session, err := uc.Register(context.Background(), "new@example.com", "secret-pass", testInvitePlain)
	if err != nil {
		t.Fatalf("Register() error = %v, want session despite mail failure", err)
	}
	if session.Token == "" {
		t.Fatal("Register() token is empty")
	}
}

type authIdentityFake struct {
	usersByEmail map[string]*User
	usersByID    map[string]*User
	orgs         map[string]*Org
	memberships  map[string][]OrgMembership
	verifyByHash map[string]string
	added        []string
	upserted     map[string]string
	nextUserNum  int
}

func newAuthIdentityFake() *authIdentityFake {
	return &authIdentityFake{
		usersByEmail: map[string]*User{},
		usersByID:    map[string]*User{},
		orgs:         map[string]*Org{},
		memberships:  map[string][]OrgMembership{},
		verifyByHash: map[string]string{},
		upserted:     map[string]string{},
	}
}

func (f *authIdentityFake) CreateUser(context.Context, string) (*User, error) {
	panic("not implemented")
}

func (f *authIdentityFake) GetUser(_ context.Context, id string) (*User, error) {
	if u, ok := f.usersByID[id]; ok {
		return u, nil
	}
	return nil, pkgErrors.ErrNotFound
}

func (f *authIdentityFake) GetUserByEmail(_ context.Context, email string) (*User, error) {
	if u, ok := f.usersByEmail[email]; ok {
		return u, nil
	}
	return nil, pkgErrors.ErrNotFound
}

func (f *authIdentityFake) CreateUserWithPassword(_ context.Context, id, name, email, passwordHash string) (*User, error) {
	if _, ok := f.usersByEmail[email]; ok {
		return nil, pkgErrors.ErrConflict
	}
	f.nextUserNum++
	if id == "" {
		id = fmt.Sprintf("user-%d", f.nextUserNum)
	}
	user := &User{ID: id, Name: name, Email: email, PasswordHash: passwordHash}
	f.usersByEmail[email] = user
	f.usersByID[id] = user
	return user, nil
}

func (f *authIdentityFake) SetEmailVerified(_ context.Context, userID string, at time.Time) error {
	user, ok := f.usersByID[userID]
	if !ok {
		return pkgErrors.ErrNotFound
	}
	user.EmailVerifiedAt = &at
	return nil
}

func (f *authIdentityFake) SetUserEmailPassword(context.Context, string, string, string) error {
	panic("not implemented")
}

func (f *authIdentityFake) CreateOrg(context.Context, string) (*Org, error) {
	panic("not implemented")
}

func (f *authIdentityFake) GetOrg(_ context.Context, orgID string) (*Org, error) {
	if org, ok := f.orgs[orgID]; ok {
		return org, nil
	}
	return nil, pkgErrors.ErrNotFound
}

func (f *authIdentityFake) AddMember(_ context.Context, orgID, userID, role string) error {
	f.added = append(f.added, fmt.Sprintf("%s:%s:%s", orgID, userID, role))
	if org, ok := f.orgs[orgID]; ok {
		f.memberships[userID] = append(f.memberships[userID], OrgMembership{
			OrgID: orgID,
			Name:  org.Name,
			Role:  role,
		})
	}
	return nil
}

func (f *authIdentityFake) MemberRole(context.Context, string, string) (string, error) {
	return "", nil
}

func (f *authIdentityFake) UserOrgIDs(_ context.Context, userID string) ([]string, error) {
	var orgIDs []string
	for _, m := range f.memberships[userID] {
		orgIDs = append(orgIDs, m.OrgID)
	}
	return orgIDs, nil
}

func (f *authIdentityFake) ListUserOrgs(_ context.Context, userID string) ([]OrgMembership, error) {
	return f.memberships[userID], nil
}

func (f *authIdentityFake) UpsertTokenHash(_ context.Context, userID, tokenHash string) error {
	f.upserted[tokenHash] = userID
	return nil
}

func (f *authIdentityFake) UserIDByTokenHash(context.Context, string) (string, error) {
	panic("not implemented")
}

func (f *authIdentityFake) CreateVerifyToken(_ context.Context, userID string, _ time.Time) (string, error) {
	plain := fmt.Sprintf("verify-%s", userID)
	f.verifyByHash[HashTokenSHA256Hex(plain)] = userID
	return plain, nil
}

func (f *authIdentityFake) ConsumeVerifyToken(_ context.Context, tokenHash string) (string, error) {
	userID, ok := f.verifyByHash[tokenHash]
	if !ok {
		return "", pkgErrors.ErrNotFound
	}
	delete(f.verifyByHash, tokenHash)
	return userID, nil
}

type authInviteFake struct {
	mu        sync.Mutex
	byHash    map[string]*OrgInvite
	usedCount map[string]int
}

func newAuthInviteFake() *authInviteFake {
	return &authInviteFake{
		byHash:    map[string]*OrgInvite{},
		usedCount: map[string]int{},
	}
}

func (f *authInviteFake) CreateInvite(context.Context, string, string, int, *time.Time) (*OrgInvite, string, error) {
	panic("not implemented")
}

func (f *authInviteFake) GetInviteByTokenHash(_ context.Context, tokenHash string) (*OrgInvite, error) {
	invite, ok := f.byHash[tokenHash]
	if !ok {
		return nil, pkgErrors.ErrNotFound
	}
	return f.cloneInvite(invite), nil
}

func (f *authInviteFake) ListInvitesByOrg(context.Context, string) ([]*OrgInvite, error) {
	panic("not implemented")
}

func (f *authInviteFake) IncrementInviteUsed(_ context.Context, id string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	invite := f.inviteByIDLocked(id)
	if invite == nil || !InviteUsable(invite, time.Now()) {
		return pkgErrors.ErrConflict
	}
	f.usedCount[id]++
	return nil
}

func (f *authInviteFake) RevokeInvite(context.Context, string) error {
	panic("not implemented")
}

func (f *authInviteFake) inviteByID(id string) *OrgInvite {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.inviteByIDLocked(id)
}

func (f *authInviteFake) inviteByIDLocked(id string) *OrgInvite {
	for _, invite := range f.byHash {
		if invite.ID == id {
			return f.cloneInvite(invite)
		}
	}
	return nil
}

func (f *authInviteFake) cloneInvite(invite *OrgInvite) *OrgInvite {
	copy := *invite
	copy.UsedCount = invite.UsedCount + f.usedCount[invite.ID]
	return &copy
}

type authMailerFake struct {
	err error
}

func (f authMailerFake) SendVerifyEmail(context.Context, string, string) error {
	return f.err
}
