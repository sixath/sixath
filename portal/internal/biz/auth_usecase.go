package biz

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"log"
	"net/url"
	"strings"
	"time"

	"backend/internal/conf"
	pkgErrors "backend/internal/pkg/errors"
)

const (
	kindWeComState  = "wecom_state"
	kindWeComTicket = "wecom_ticket"
	weComProvider   = "wecom"
	weComSSOBase    = "https://login.work.weixin.qq.com/wwlogin/sso/login"
	weComStateTTL   = 10 * time.Minute
	weComTicketTTL  = 60 * time.Second
)

// AuthSession is returned after successful login or registration.
type AuthSession struct {
	Token         string
	UserID        string
	Email         string
	Orgs          []OrgMembership
	EmailVerified bool
}

// InvitePreview describes an invite link without exposing secrets.
type InvitePreview struct {
	OrgName string
	Valid   bool
}

// AuthUsecase handles email/password login, invite registration, and email verification.
type AuthUsecase struct {
	identities        IdentityRepo
	invites           InviteRepo
	mailer            Mailer
	enableVerifyEmail bool
	verifyTokenTTL    time.Duration

	ephemeral     AuthEphemeralRepo
	wecom         WeComOAuthClient
	wecomEnabled  bool
	corpID        string
	agentID       string
	redirectURI   string
	publicBaseURL string
}

// NewAuthUsecase wires email auth. Nil mailer defaults to NoopMailer. WeCom is disabled.
func NewAuthUsecase(identities IdentityRepo, invites InviteRepo, mailer Mailer, enableVerifyEmail bool) *AuthUsecase {
	if mailer == nil {
		mailer = NoopMailer{}
	}
	return &AuthUsecase{
		identities:        identities,
		invites:           invites,
		mailer:            mailer,
		enableVerifyEmail: enableVerifyEmail,
		verifyTokenTTL:    24 * time.Hour,
	}
}

// NewAuthUsecaseWithWeCom wires email auth plus WeCom SSO dependencies.
func NewAuthUsecaseWithWeCom(
	identities IdentityRepo,
	invites InviteRepo,
	mailer Mailer,
	enableVerifyEmail bool,
	ephemeral AuthEphemeralRepo,
	wecom WeComOAuthClient,
	corpID, agentID, redirectURI, publicBaseURL string,
) *AuthUsecase {
	uc := NewAuthUsecase(identities, invites, mailer, enableVerifyEmail)
	uc.ephemeral = ephemeral
	uc.wecom = wecom
	uc.corpID = strings.TrimSpace(corpID)
	uc.agentID = strings.TrimSpace(agentID)
	uc.redirectURI = strings.TrimSpace(redirectURI)
	uc.publicBaseURL = strings.TrimSpace(publicBaseURL)
	uc.wecomEnabled = uc.wecom != nil && uc.corpID != "" && uc.agentID != "" && uc.redirectURI != ""
	return uc
}

// ProvideAuthUsecase wires AuthUsecase from config; SMTP host enables verify-email flow.
func ProvideAuthUsecase(identities IdentityRepo, invites InviteRepo, ephemeral AuthEphemeralRepo, auth *conf.Auth) *AuthUsecase {
	mailer := NewMailer(auth)
	enableVerify := auth != nil && strings.TrimSpace(auth.GetSmtpHost()) != ""

	corpID := ""
	agentID := ""
	secret := ""
	publicBaseURL := ""
	if auth != nil {
		corpID = strings.TrimSpace(auth.GetWecomCorpId())
		agentID = strings.TrimSpace(auth.GetWecomAgentId())
		secret = strings.TrimSpace(auth.GetWecomSecret())
		publicBaseURL = strings.TrimSpace(auth.GetPublicBaseUrl())
	}
	redirectURI := resolveWeComRedirectURI(auth)
	enabled := corpID != "" && agentID != "" && secret != "" && redirectURI != ""

	var wecom WeComOAuthClient
	if enabled {
		wecom = NewWeComOAuthClient(corpID, secret, nil)
	}
	return NewAuthUsecaseWithWeCom(identities, invites, mailer, enableVerify, ephemeral, wecom, corpID, agentID, redirectURI, publicBaseURL)
}

func resolveWeComRedirectURI(auth *conf.Auth) string {
	if auth == nil {
		return ""
	}
	if uri := strings.TrimSpace(auth.GetWecomRedirectUri()); uri != "" {
		return uri
	}
	base := strings.TrimRight(strings.TrimSpace(auth.GetPublicBaseUrl()), "/")
	if base == "" {
		return ""
	}
	return base + "/api/v1/auth/wecom/callback"
}

func sanitizeNext(next string) string {
	next = strings.TrimSpace(next)
	if next == "" || !strings.HasPrefix(next, "/") || strings.HasPrefix(next, "//") {
		return "/"
	}
	return next
}

func randomHexID() (string, error) {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return hex.EncodeToString(b), nil
}

// WeComEnabled reports whether CorpApp SSO is configured.
func (uc *AuthUsecase) WeComEnabled() bool {
	return uc.wecomEnabled
}

// PublicBaseURL returns the configured public base URL (may be empty).
func (uc *AuthUsecase) PublicBaseURL() string {
	return uc.publicBaseURL
}

// StartWeCom creates OAuth state and returns the WeCom SSO login URL.
func (uc *AuthUsecase) StartWeCom(ctx context.Context, next string) (ssoURL string, err error) {
	if !uc.wecomEnabled || uc.ephemeral == nil {
		return "", ErrWeComLoginDisabled
	}
	state, err := randomHexID()
	if err != nil {
		return "", err
	}
	payload, err := json.Marshal(map[string]string{"next": sanitizeNext(next)})
	if err != nil {
		return "", err
	}
	if err := uc.ephemeral.Put(ctx, state, kindWeComState, string(payload), time.Now().Add(weComStateTTL)); err != nil {
		return "", err
	}
	q := url.Values{}
	q.Set("login_type", "CorpApp")
	q.Set("appid", uc.corpID)
	q.Set("agentid", uc.agentID)
	q.Set("redirect_uri", uc.redirectURI)
	q.Set("state", state)
	return weComSSOBase + "?" + q.Encode(), nil
}

// HandleWeComCallback exchanges code for userid, upserts identity, and returns a one-time ticket.
func (uc *AuthUsecase) HandleWeComCallback(ctx context.Context, code, state string) (ticket, next string, err error) {
	if !uc.wecomEnabled || uc.ephemeral == nil || uc.wecom == nil {
		return "", "", ErrWeComLoginDisabled
	}
	payloadJSON, err := uc.ephemeral.Consume(ctx, state, kindWeComState, time.Now())
	if err != nil {
		if errors.Is(err, pkgErrors.ErrNotFound) {
			return "", "", ErrInvalidState
		}
		return "", "", err
	}
	var statePayload struct {
		Next string `json:"next"`
	}
	if err := json.Unmarshal([]byte(payloadJSON), &statePayload); err != nil {
		return "", "", ErrInvalidState
	}
	next = sanitizeNext(statePayload.Next)

	userid, err := uc.wecom.GetUserID(ctx, code)
	if err != nil {
		if errors.Is(err, ErrWeComNoUserID) {
			return "", "", ErrInvalidWeComUser
		}
		return "", "", ErrInvalidCode
	}
	if userid == "" {
		return "", "", ErrInvalidWeComUser
	}

	userID, err := uc.upsertWeComUser(ctx, userid)
	if err != nil {
		return "", "", err
	}

	ticket, err = randomHexID()
	if err != nil {
		return "", "", err
	}
	ticketPayload, err := json.Marshal(map[string]string{"user_id": userID})
	if err != nil {
		return "", "", err
	}
	if err := uc.ephemeral.Put(ctx, ticket, kindWeComTicket, string(ticketPayload), time.Now().Add(weComTicketTTL)); err != nil {
		return "", "", err
	}
	return ticket, next, nil
}

func (uc *AuthUsecase) upsertWeComUser(ctx context.Context, userid string) (string, error) {
	userID, err := uc.identities.GetIdentity(ctx, weComProvider, userid)
	if err == nil {
		return userID, nil
	}
	if !errors.Is(err, pkgErrors.ErrNotFound) {
		return "", err
	}

	user, err := uc.identities.CreateUserForIdentity(ctx, userid, time.Now())
	if err != nil {
		return "", err
	}
	err = uc.identities.CreateIdentity(ctx, weComProvider, userid, user.ID)
	if err == nil {
		return user.ID, nil
	}
	if errors.Is(err, pkgErrors.ErrConflict) {
		_ = uc.identities.DeleteUser(ctx, user.ID)
		userID, err = uc.identities.GetIdentity(ctx, weComProvider, userid)
		if err != nil {
			return "", err
		}
		return userID, nil
	}
	return "", err
}

// ExchangeWeComTicket consumes a one-time ticket and issues a Bearer session.
func (uc *AuthUsecase) ExchangeWeComTicket(ctx context.Context, ticket string) (*AuthSession, error) {
	if !uc.wecomEnabled || uc.ephemeral == nil {
		return nil, ErrWeComLoginDisabled
	}
	if ticket == "" {
		return nil, ErrInvalidTicket
	}
	payloadJSON, err := uc.ephemeral.Consume(ctx, ticket, kindWeComTicket, time.Now())
	if err != nil {
		if errors.Is(err, pkgErrors.ErrNotFound) {
			return nil, ErrInvalidTicket
		}
		return nil, err
	}
	var payload struct {
		UserID string `json:"user_id"`
	}
	if err := json.Unmarshal([]byte(payloadJSON), &payload); err != nil || payload.UserID == "" {
		return nil, ErrInvalidTicket
	}
	user, err := uc.identities.GetUser(ctx, payload.UserID)
	if err != nil {
		if errors.Is(err, pkgErrors.ErrNotFound) {
			return nil, ErrInvalidTicket
		}
		return nil, err
	}
	return uc.issueSession(ctx, user)
}

func (uc *AuthUsecase) Login(ctx context.Context, email, password string) (*AuthSession, error) {
	user, err := uc.identities.GetUserByEmail(ctx, email)
	if err != nil {
		if errors.Is(err, pkgErrors.ErrNotFound) {
			return nil, ErrUnauthorized
		}
		return nil, err
	}
	if !CheckPassword(user.PasswordHash, password) {
		return nil, ErrUnauthorized
	}
	return uc.issueSession(ctx, user)
}

func (uc *AuthUsecase) Register(ctx context.Context, email, password, invitePlain string) (*AuthSession, error) {
	if invitePlain == "" {
		return nil, ErrBadRequest
	}

	invite, err := uc.invites.GetInviteByTokenHash(ctx, HashTokenSHA256Hex(invitePlain))
	if err != nil {
		if errors.Is(err, pkgErrors.ErrNotFound) {
			return nil, ErrBadRequest
		}
		return nil, err
	}
	if !InviteUsable(invite, time.Now()) {
		return nil, ErrBadRequest
	}

	_, err = uc.identities.GetUserByEmail(ctx, email)
	if err == nil {
		return nil, ErrConflict
	}
	if !errors.Is(err, pkgErrors.ErrNotFound) {
		return nil, err
	}

	passwordHash, err := HashPassword(password)
	if err != nil {
		return nil, err
	}

	// Claim invite slot before creating the user to avoid max_uses races under concurrency.
	// If user creation or membership fails afterward, the slot stays consumed (acceptable for correctness).
	if err := uc.invites.IncrementInviteUsed(ctx, invite.ID); err != nil {
		if errors.Is(err, pkgErrors.ErrConflict) {
			return nil, ErrBadRequest
		}
		return nil, err
	}

	user, err := uc.identities.CreateUserWithPassword(ctx, "", emailLocalPart(email), email, passwordHash)
	if err != nil {
		if errors.Is(err, pkgErrors.ErrConflict) {
			return nil, ErrConflict
		}
		return nil, err
	}

	if err := uc.identities.AddMember(ctx, invite.OrgID, user.ID, "member"); err != nil {
		return nil, err
	}

	session, err := uc.issueSession(ctx, user)
	if err != nil {
		return nil, err
	}

	if uc.enableVerifyEmail {
		uc.sendVerifyEmailBestEffort(ctx, email, user.ID)
	}

	return session, nil
}

func (uc *AuthUsecase) sendVerifyEmailBestEffort(ctx context.Context, email, userID string) {
	plain, err := uc.identities.CreateVerifyToken(ctx, userID, time.Now().Add(uc.verifyTokenTTL))
	if err != nil {
		log.Printf("auth: CreateVerifyToken for %q: %v", email, err)
		return
	}
	if err := uc.mailer.SendVerifyEmail(ctx, email, plain); err != nil {
		log.Printf("auth: SendVerifyEmail to %q: %v", email, err)
	}
}

func (uc *AuthUsecase) PreviewInvite(ctx context.Context, invitePlain string) (*InvitePreview, error) {
	return uc.previewInvite(ctx, invitePlain)
}

func (uc *AuthUsecase) VerifyEmail(ctx context.Context, tokenPlain string) error {
	if tokenPlain == "" {
		return ErrBadRequest
	}
	userID, err := uc.identities.ConsumeVerifyToken(ctx, HashTokenSHA256Hex(tokenPlain))
	if err != nil {
		if errors.Is(err, pkgErrors.ErrNotFound) {
			return ErrBadRequest
		}
		return err
	}
	return uc.identities.SetEmailVerified(ctx, userID, time.Now())
}

// ResendVerifyEmailResult describes a resend attempt for the settings UI.
type ResendVerifyEmailResult struct {
	Sent                 bool
	AlreadyVerified      bool
	VerificationDisabled bool
}

// ResendVerifyEmail creates a fresh verify token and emails it to the user.
// No-op (AlreadyVerified) when the mailbox is already confirmed.
func (uc *AuthUsecase) ResendVerifyEmail(ctx context.Context, userID string) (*ResendVerifyEmailResult, error) {
	userID = strings.TrimSpace(userID)
	if userID == "" {
		return nil, ErrUnauthorized
	}
	user, err := uc.identities.GetUser(ctx, userID)
	if err != nil {
		if errors.Is(err, pkgErrors.ErrNotFound) {
			return nil, ErrUnauthorized
		}
		return nil, err
	}
	if user.EmailVerifiedAt != nil {
		return &ResendVerifyEmailResult{AlreadyVerified: true}, nil
	}
	if !uc.enableVerifyEmail {
		return &ResendVerifyEmailResult{VerificationDisabled: true}, nil
	}
	email := strings.TrimSpace(user.Email)
	if email == "" {
		return nil, ErrBadRequest
	}
	uc.sendVerifyEmailBestEffort(ctx, email, user.ID)
	return &ResendVerifyEmailResult{Sent: true}, nil
}

func (uc *AuthUsecase) previewInvite(ctx context.Context, invitePlain string) (*InvitePreview, error) {
	if invitePlain == "" {
		return &InvitePreview{Valid: false}, nil
	}
	invite, err := uc.invites.GetInviteByTokenHash(ctx, HashTokenSHA256Hex(invitePlain))
	if err != nil {
		if errors.Is(err, pkgErrors.ErrNotFound) {
			return &InvitePreview{Valid: false}, nil
		}
		return nil, err
	}
	orgName := ""
	org, err := uc.identities.GetOrg(ctx, invite.OrgID)
	if err == nil && org != nil {
		orgName = org.Name
	}
	return &InvitePreview{
		OrgName: orgName,
		Valid:   InviteUsable(invite, time.Now()),
	}, nil
}

func (uc *AuthUsecase) issueSession(ctx context.Context, user *User) (*AuthSession, error) {
	token, err := newBearerToken()
	if err != nil {
		return nil, err
	}
	if err := IssueToken(ctx, uc.identities, user.ID, token); err != nil {
		return nil, err
	}
	orgs, err := uc.identities.ListUserOrgs(ctx, user.ID)
	if err != nil {
		return nil, err
	}
	emailVerified := user.EmailVerifiedAt != nil
	return &AuthSession{
		Token:         token,
		UserID:        user.ID,
		Email:         user.Email,
		Orgs:          orgs,
		EmailVerified: emailVerified,
	}, nil
}

func emailLocalPart(email string) string {
	if i := strings.IndexByte(email, '@'); i > 0 {
		return email[:i]
	}
	return email
}
