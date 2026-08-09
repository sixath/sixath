package biz

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"time"
)

// User is an authenticated Portal identity.
type User struct {
	ID              string
	Name            string
	Email           string
	PasswordHash    string
	EmailVerifiedAt *time.Time
	CreatedAt       time.Time
	UpdatedAt       time.Time
}

// OrgMembership is a user's membership in an organization.
type OrgMembership struct {
	OrgID string
	Name  string
	Role  string
}

// Org is a collection of users that can receive resource grants.
type Org struct {
	ID        string
	Name      string
	CreatedAt time.Time
	UpdatedAt time.Time
}

// IdentityRepo stores users, organizations, memberships, and bearer token hashes.
type IdentityRepo interface {
	CreateUser(ctx context.Context, name string) (*User, error)
	GetUser(ctx context.Context, id string) (*User, error)
	GetUserByEmail(ctx context.Context, email string) (*User, error)
	CreateUserWithPassword(ctx context.Context, id, name, email, passwordHash string) (*User, error)
	SetEmailVerified(ctx context.Context, userID string, at time.Time) error
	SetUserEmailPassword(ctx context.Context, userID, email, passwordHash string) error
	CreateOrg(ctx context.Context, name string) (*Org, error)
	GetOrg(ctx context.Context, orgID string) (*Org, error)
	AddMember(ctx context.Context, orgID, userID, role string) error
	MemberRole(ctx context.Context, orgID, userID string) (string, error)
	UserOrgIDs(ctx context.Context, userID string) ([]string, error)
	ListUserOrgs(ctx context.Context, userID string) ([]OrgMembership, error)
	UpsertTokenHash(ctx context.Context, userID, tokenHash string) error
	UserIDByTokenHash(ctx context.Context, tokenHash string) (string, error)
	CreateVerifyToken(ctx context.Context, userID string, expiresAt time.Time) (plainToken string, err error)
	ConsumeVerifyToken(ctx context.Context, tokenHash string) (userID string, err error)
}

// ResourceRepo stores resources and grants while providing the data AccessChecker needs.
type ResourceRepo interface {
	ResourceReader
	CreateResource(ctx context.Context, resource *Resource) (*Resource, error)
	UpdateResource(ctx context.Context, resource *Resource) error
	DeleteResource(ctx context.Context, id string) error
	GetByPayload(ctx context.Context, resourceType ResourceType, payloadRef string) (*Resource, error)
	ListAllByType(ctx context.Context, resourceType ResourceType) ([]*Resource, error)
	ListGrantsByResourceIDs(ctx context.Context, resourceIDs []string) (map[string][]ResourceGrant, error)
	CreateGrant(ctx context.Context, grant ResourceGrant) error
}

// EnsureMember adds a membership only when the user is not already in the organization.
func EnsureMember(ctx context.Context, repo IdentityRepo, orgID, userID, role string) error {
	orgIDs, err := repo.UserOrgIDs(ctx, userID)
	if err != nil {
		return err
	}
	for _, id := range orgIDs {
		if id == orgID {
			return nil
		}
	}
	return repo.AddMember(ctx, orgID, userID, role)
}

// LookupUserByTokenHash resolves a stored SHA-256 token hash to its user.
func LookupUserByTokenHash(ctx context.Context, repo IdentityRepo, tokenHash string) (*User, error) {
	userID, err := repo.UserIDByTokenHash(ctx, tokenHash)
	if err != nil {
		return nil, err
	}
	return repo.GetUser(ctx, userID)
}

// HashTokenSHA256Hex hashes a plaintext token the way bearer and invite tokens are stored.
func HashTokenSHA256Hex(plain string) string {
	sum := sha256.Sum256([]byte(plain))
	return hex.EncodeToString(sum[:])
}

// IssueToken hashes a plaintext bearer token before storing it.
func IssueToken(ctx context.Context, repo IdentityRepo, userID, token string) error {
	return repo.UpsertTokenHash(ctx, userID, HashTokenSHA256Hex(token))
}

// ProvideAccessChecker adapts ResourceRepo to the narrower AccessChecker dependency.
func ProvideAccessChecker(repo ResourceRepo) *AccessChecker {
	return NewAccessChecker(repo)
}
