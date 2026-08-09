package biz

import (
	"context"
	"errors"
	"log"
	"strings"
	"time"

	"backend/internal/conf"
	pkgErrors "backend/internal/pkg/errors"
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
	identities         IdentityRepo
	invites            InviteRepo
	mailer             Mailer
	enableVerifyEmail  bool
	verifyTokenTTL     time.Duration
}

// NewAuthUsecase wires email auth. Nil mailer defaults to NoopMailer.
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

// ProvideAuthUsecase wires AuthUsecase from config; SMTP host enables verify-email flow.
func ProvideAuthUsecase(identities IdentityRepo, invites InviteRepo, auth *conf.Auth) *AuthUsecase {
	mailer := NewMailer(auth)
	enableVerify := auth != nil && strings.TrimSpace(auth.GetSmtpHost()) != ""
	return NewAuthUsecase(identities, invites, mailer, enableVerify)
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
