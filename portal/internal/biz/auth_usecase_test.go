package biz

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
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
	mu                  sync.Mutex
	usersByEmail        map[string]*User
	usersByID           map[string]*User
	orgs                map[string]*Org
	memberships         map[string][]OrgMembership
	verifyByHash        map[string]string
	identities          map[string]string // provider\0subject -> userID
	added               []string
	upserted            map[string]string
	deletedUsers        []string
	nextUserNum         int
	getIdentityMissOnce bool
}

func newAuthIdentityFake() *authIdentityFake {
	return &authIdentityFake{
		usersByEmail: map[string]*User{},
		usersByID:    map[string]*User{},
		orgs:         map[string]*Org{},
		memberships:  map[string][]OrgMembership{},
		verifyByHash: map[string]string{},
		identities:   map[string]string{},
		upserted:     map[string]string{},
	}
}

func identityKey(provider, subject string) string {
	return provider + "\x00" + subject
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

func (f *authIdentityFake) ListOrgMembers(context.Context, string) ([]OrgMemberInfo, error) {
	panic("not implemented")
}

func (f *authIdentityFake) ListUsers(context.Context, string, int) ([]UserSummary, error) {
	panic("not implemented")
}

func (f *authIdentityFake) CreateUserForIdentity(_ context.Context, name string, verifiedAt time.Time) (*User, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.nextUserNum++
	id := fmt.Sprintf("user-%d", f.nextUserNum)
	at := verifiedAt
	user := &User{ID: id, Name: name, Email: "", EmailVerifiedAt: &at}
	f.usersByID[id] = user
	return user, nil
}

func (f *authIdentityFake) DeleteUser(_ context.Context, userID string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if _, ok := f.usersByID[userID]; !ok {
		return pkgErrors.ErrNotFound
	}
	delete(f.usersByID, userID)
	f.deletedUsers = append(f.deletedUsers, userID)
	return nil
}

func (f *authIdentityFake) GetIdentity(_ context.Context, provider, subject string) (string, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.getIdentityMissOnce {
		f.getIdentityMissOnce = false
		return "", pkgErrors.ErrNotFound
	}
	userID, ok := f.identities[identityKey(provider, subject)]
	if !ok {
		return "", pkgErrors.ErrNotFound
	}
	return userID, nil
}

func (f *authIdentityFake) CreateIdentity(_ context.Context, provider, subject, userID string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	key := identityKey(provider, subject)
	if _, ok := f.identities[key]; ok {
		return pkgErrors.ErrConflict
	}
	f.identities[key] = userID
	return nil
}

func (f *authIdentityFake) RemoveMember(context.Context, string, string) error {
	panic("not implemented")
}

func (f *authIdentityFake) UpdateMemberRole(context.Context, string, string, string) error {
	panic("not implemented")
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

type authEphemeralFake struct {
	mu    sync.Mutex
	items map[string]ephemeralItem
}

type ephemeralItem struct {
	kind     string
	payload  string
	expires  time.Time
	consumed bool
}

func newAuthEphemeralFake() *authEphemeralFake {
	return &authEphemeralFake{items: map[string]ephemeralItem{}}
}

func (f *authEphemeralFake) Put(_ context.Context, id, kind, payloadJSON string, expiresAt time.Time) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.items[id] = ephemeralItem{kind: kind, payload: payloadJSON, expires: expiresAt}
	return nil
}

func (f *authEphemeralFake) Consume(_ context.Context, id, kind string, now time.Time) (string, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	item, ok := f.items[id]
	if !ok || item.kind != kind || item.consumed || !item.expires.After(now) {
		return "", pkgErrors.ErrNotFound
	}
	item.consumed = true
	f.items[id] = item
	return item.payload, nil
}

func (f *authEphemeralFake) get(id string) (ephemeralItem, bool) {
	f.mu.Lock()
	defer f.mu.Unlock()
	item, ok := f.items[id]
	return item, ok
}

type fakeWeComClient struct {
	userid string
	err    error
	codes  []string
}

func (f *fakeWeComClient) GetUserID(_ context.Context, code string) (string, error) {
	f.codes = append(f.codes, code)
	if f.err != nil {
		return "", f.err
	}
	return f.userid, nil
}

func newEnabledWeComUC(identities *authIdentityFake, ephemeral *authEphemeralFake, wecom WeComOAuthClient) *AuthUsecase {
	return NewAuthUsecaseWithWeCom(
		identities, newAuthInviteFake(), nil, false,
		ephemeral, wecom,
		"wwcorp", "1000001",
		"https://portal.example.com/api/v1/auth/wecom/callback",
		"https://portal.example.com",
	)
}

func TestAuthUsecaseWeComEnabledFalseWhenIncomplete(t *testing.T) {
	uc := NewAuthUsecase(newAuthIdentityFake(), newAuthInviteFake(), nil, false)
	if uc.WeComEnabled() {
		t.Fatal("WeComEnabled() = true, want false for email-only usecase")
	}
	uc2 := NewAuthUsecaseWithWeCom(
		newAuthIdentityFake(), newAuthInviteFake(), nil, false,
		newAuthEphemeralFake(), &fakeWeComClient{userid: "u"},
		"wwcorp", "1000001", "", "https://portal.example.com",
	)
	if uc2.WeComEnabled() {
		t.Fatal("WeComEnabled() = true, want false when redirectURI empty")
	}
}

func TestAuthUsecaseStartWeComDisabled(t *testing.T) {
	uc := NewAuthUsecase(newAuthIdentityFake(), newAuthInviteFake(), nil, false)
	_, err := uc.StartWeCom(context.Background(), "/")
	if !isReason(err, "WECOM_LOGIN_DISABLED") {
		t.Fatalf("StartWeCom(disabled) error = %v, want WECOM_LOGIN_DISABLED", err)
	}
}

func TestAuthUsecaseStartWeComPutsStateAndSSOURL(t *testing.T) {
	identities := newAuthIdentityFake()
	ephemeral := newAuthEphemeralFake()
	uc := newEnabledWeComUC(identities, ephemeral, &fakeWeComClient{userid: "zhangsan"})

	ssoURL, err := uc.StartWeCom(context.Background(), "//evil.com")
	if err != nil {
		t.Fatalf("StartWeCom() error = %v", err)
	}
	u, err := url.Parse(ssoURL)
	if err != nil {
		t.Fatalf("Parse SSO URL: %v", err)
	}
	if u.Scheme != "https" || u.Host != "login.work.weixin.qq.com" || u.Path != "/wwlogin/sso/login" {
		t.Fatalf("SSO URL host/path = %s://%s%s", u.Scheme, u.Host, u.Path)
	}
	q := u.Query()
	if q.Get("login_type") != "CorpApp" {
		t.Fatalf("login_type = %q, want CorpApp", q.Get("login_type"))
	}
	if q.Get("appid") != "wwcorp" {
		t.Fatalf("appid = %q, want wwcorp", q.Get("appid"))
	}
	if q.Get("agentid") != "1000001" {
		t.Fatalf("agentid = %q, want 1000001", q.Get("agentid"))
	}
	if q.Get("redirect_uri") != "https://portal.example.com/api/v1/auth/wecom/callback" {
		t.Fatalf("redirect_uri = %q", q.Get("redirect_uri"))
	}
	state := q.Get("state")
	if state == "" {
		t.Fatal("state is empty")
	}
	item, ok := ephemeral.get(state)
	if !ok || item.kind != "wecom_state" {
		t.Fatalf("ephemeral state = %#v, want wecom_state", item)
	}
	var payload struct {
		Next string `json:"next"`
	}
	if err := json.Unmarshal([]byte(item.payload), &payload); err != nil {
		t.Fatalf("unmarshal state payload: %v", err)
	}
	if payload.Next != "/" {
		t.Fatalf("sanitized next = %q, want /", payload.Next)
	}
	if time.Until(item.expires) < 9*time.Minute || time.Until(item.expires) > 10*time.Minute+time.Second {
		t.Fatalf("state TTL = %v, want ~10m", time.Until(item.expires))
	}
}

func TestAuthUsecaseHandleWeComCallbackInvalidState(t *testing.T) {
	uc := newEnabledWeComUC(newAuthIdentityFake(), newAuthEphemeralFake(), &fakeWeComClient{userid: "zhangsan"})
	_, _, err := uc.HandleWeComCallback(context.Background(), "code", "missing-state")
	if !isReason(err, "INVALID_STATE") {
		t.Fatalf("HandleWeComCallback(bad state) error = %v, want INVALID_STATE", err)
	}
}

func TestAuthUsecaseHandleWeComCallbackFirstLogin(t *testing.T) {
	identities := newAuthIdentityFake()
	ephemeral := newAuthEphemeralFake()
	wecom := &fakeWeComClient{userid: "zhangsan"}
	uc := newEnabledWeComUC(identities, ephemeral, wecom)

	ssoURL, err := uc.StartWeCom(context.Background(), "/chat")
	if err != nil {
		t.Fatalf("StartWeCom: %v", err)
	}
	state := mustQuery(t, ssoURL, "state")

	ticket, next, err := uc.HandleWeComCallback(context.Background(), "oauth-code", state)
	if err != nil {
		t.Fatalf("HandleWeComCallback: %v", err)
	}
	if next != "/chat" {
		t.Fatalf("next = %q, want /chat", next)
	}
	if ticket == "" {
		t.Fatal("ticket is empty")
	}
	if len(wecom.codes) != 1 || wecom.codes[0] != "oauth-code" {
		t.Fatalf("GetUserID codes = %v", wecom.codes)
	}
	if len(identities.added) != 0 {
		t.Fatalf("AddMember called: %v", identities.added)
	}
	userID, err := identities.GetIdentity(context.Background(), "wecom", "zhangsan")
	if err != nil {
		t.Fatalf("GetIdentity: %v", err)
	}
	user := identities.usersByID[userID]
	if user == nil || user.Name != "zhangsan" || user.EmailVerifiedAt == nil {
		t.Fatalf("created user = %#v", user)
	}
	item, ok := ephemeral.get(ticket)
	if !ok || item.kind != "wecom_ticket" {
		t.Fatalf("ticket ephemeral = %#v", item)
	}
}

func TestAuthUsecaseHandleWeComCallbackSecondLogin(t *testing.T) {
	identities := newAuthIdentityFake()
	at := time.Now()
	identities.usersByID["user-existing"] = &User{ID: "user-existing", Name: "zhangsan", EmailVerifiedAt: &at}
	identities.identities[identityKey("wecom", "zhangsan")] = "user-existing"
	ephemeral := newAuthEphemeralFake()
	uc := newEnabledWeComUC(identities, ephemeral, &fakeWeComClient{userid: "zhangsan"})

	ssoURL, err := uc.StartWeCom(context.Background(), "/")
	if err != nil {
		t.Fatalf("StartWeCom: %v", err)
	}
	state := mustQuery(t, ssoURL, "state")

	ticket, _, err := uc.HandleWeComCallback(context.Background(), "code", state)
	if err != nil {
		t.Fatalf("HandleWeComCallback: %v", err)
	}
	item, _ := ephemeral.get(ticket)
	var payload struct {
		UserID string `json:"user_id"`
	}
	if err := json.Unmarshal([]byte(item.payload), &payload); err != nil {
		t.Fatalf("unmarshal ticket: %v", err)
	}
	if payload.UserID != "user-existing" {
		t.Fatalf("ticket user_id = %q, want user-existing", payload.UserID)
	}
	if identities.nextUserNum != 0 {
		t.Fatalf("CreateUserForIdentity called, nextUserNum=%d", identities.nextUserNum)
	}
}

func TestAuthUsecaseHandleWeComCallbackIdentityConflictOrphan(t *testing.T) {
	identities := newAuthIdentityFake()
	at := time.Now()
	identities.usersByID["user-winner"] = &User{ID: "user-winner", Name: "zhangsan", EmailVerifiedAt: &at}
	identities.identities[identityKey("wecom", "zhangsan")] = "user-winner"
	identities.getIdentityMissOnce = true
	ephemeral := newAuthEphemeralFake()
	uc := newEnabledWeComUC(identities, ephemeral, &fakeWeComClient{userid: "zhangsan"})

	ssoURL, err := uc.StartWeCom(context.Background(), "/")
	if err != nil {
		t.Fatalf("StartWeCom: %v", err)
	}
	state := mustQuery(t, ssoURL, "state")

	ticket, _, err := uc.HandleWeComCallback(context.Background(), "code", state)
	if err != nil {
		t.Fatalf("HandleWeComCallback: %v", err)
	}
	item, _ := ephemeral.get(ticket)
	var payload struct {
		UserID string `json:"user_id"`
	}
	_ = json.Unmarshal([]byte(item.payload), &payload)
	if payload.UserID != "user-winner" {
		t.Fatalf("ticket user_id = %q, want user-winner", payload.UserID)
	}
	if len(identities.deletedUsers) != 1 {
		t.Fatalf("deletedUsers = %v, want 1 orphan delete", identities.deletedUsers)
	}
}

func TestAuthUsecaseHandleWeComCallbackNoUserID(t *testing.T) {
	ephemeral := newAuthEphemeralFake()
	uc := newEnabledWeComUC(newAuthIdentityFake(), ephemeral, &fakeWeComClient{err: ErrWeComNoUserID})
	ssoURL, err := uc.StartWeCom(context.Background(), "/")
	if err != nil {
		t.Fatalf("StartWeCom: %v", err)
	}
	_, _, err = uc.HandleWeComCallback(context.Background(), "code", mustQuery(t, ssoURL, "state"))
	if !isReason(err, "INVALID_WECOM_USER") {
		t.Fatalf("error = %v, want INVALID_WECOM_USER", err)
	}
}

func TestAuthUsecaseHandleWeComCallbackCodeFailure(t *testing.T) {
	ephemeral := newAuthEphemeralFake()
	uc := newEnabledWeComUC(newAuthIdentityFake(), ephemeral, &fakeWeComClient{err: errors.New("api down")})
	ssoURL, err := uc.StartWeCom(context.Background(), "/")
	if err != nil {
		t.Fatalf("StartWeCom: %v", err)
	}
	_, _, err = uc.HandleWeComCallback(context.Background(), "code", mustQuery(t, ssoURL, "state"))
	if !isReason(err, "INVALID_CODE") {
		t.Fatalf("error = %v, want INVALID_CODE", err)
	}
}

func TestAuthUsecaseExchangeWeComTicket(t *testing.T) {
	identities := newAuthIdentityFake()
	ephemeral := newAuthEphemeralFake()
	wecom := &fakeWeComClient{userid: "zhangsan"}
	uc := newEnabledWeComUC(identities, ephemeral, wecom)

	ssoURL, err := uc.StartWeCom(context.Background(), "/")
	if err != nil {
		t.Fatalf("StartWeCom: %v", err)
	}
	ticket, _, err := uc.HandleWeComCallback(context.Background(), "code", mustQuery(t, ssoURL, "state"))
	if err != nil {
		t.Fatalf("callback: %v", err)
	}

	session, err := uc.ExchangeWeComTicket(context.Background(), ticket)
	if err != nil {
		t.Fatalf("ExchangeWeComTicket: %v", err)
	}
	if session.Token == "" || session.UserID == "" {
		t.Fatalf("session = %#v", session)
	}
	if session.Email != "" {
		t.Fatalf("email = %q, want empty", session.Email)
	}
	if !session.EmailVerified {
		t.Fatal("email_verified = false, want true")
	}
	if len(session.Orgs) != 0 {
		t.Fatalf("orgs = %#v, want empty", session.Orgs)
	}

	// After AddMember, re-login exchange should include orgs.
	userID := session.UserID
	identities.orgs["org-1"] = &Org{ID: "org-1", Name: "Acme"}
	_ = identities.AddMember(context.Background(), "org-1", userID, "owner")

	ssoURL2, err := uc.StartWeCom(context.Background(), "/")
	if err != nil {
		t.Fatalf("StartWeCom2: %v", err)
	}
	ticket2, _, err := uc.HandleWeComCallback(context.Background(), "code2", mustQuery(t, ssoURL2, "state"))
	if err != nil {
		t.Fatalf("callback2: %v", err)
	}
	session2, err := uc.ExchangeWeComTicket(context.Background(), ticket2)
	if err != nil {
		t.Fatalf("Exchange2: %v", err)
	}
	if len(session2.Orgs) != 1 || session2.Orgs[0].OrgID != "org-1" {
		t.Fatalf("session2 orgs = %#v, want org-1", session2.Orgs)
	}
}

func TestAuthUsecaseExchangeWeComTicketSingleUse(t *testing.T) {
	identities := newAuthIdentityFake()
	ephemeral := newAuthEphemeralFake()
	uc := newEnabledWeComUC(identities, ephemeral, &fakeWeComClient{userid: "zhangsan"})

	ssoURL, err := uc.StartWeCom(context.Background(), "/")
	if err != nil {
		t.Fatalf("StartWeCom: %v", err)
	}
	ticket, _, err := uc.HandleWeComCallback(context.Background(), "code", mustQuery(t, ssoURL, "state"))
	if err != nil {
		t.Fatalf("callback: %v", err)
	}
	if _, err := uc.ExchangeWeComTicket(context.Background(), ticket); err != nil {
		t.Fatalf("first exchange: %v", err)
	}
	_, err = uc.ExchangeWeComTicket(context.Background(), ticket)
	if !isReason(err, "INVALID_TICKET") {
		t.Fatalf("second exchange error = %v, want INVALID_TICKET", err)
	}
}

func mustQuery(t *testing.T, rawURL, key string) string {
	t.Helper()
	u, err := url.Parse(rawURL)
	if err != nil {
		t.Fatalf("parse url: %v", err)
	}
	v := u.Query().Get(key)
	if v == "" {
		t.Fatalf("query %q missing in %s", key, rawURL)
	}
	return v
}

func TestSanitizeNext(t *testing.T) {
	cases := []struct{ in, want string }{
		{"", "/"},
		{"/chat", "/chat"},
		{"//evil", "/"},
		{"http://x", "/"},
		{"  /ok  ", "/ok"},
	}
	for _, tc := range cases {
		if got := sanitizeNext(tc.in); got != tc.want {
			t.Fatalf("sanitizeNext(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
}
