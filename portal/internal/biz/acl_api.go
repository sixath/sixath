package biz

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"errors"
	"strings"
	"time"

	"backend/internal/conf"
	pkgErrors "backend/internal/pkg/errors"

	kratosErrors "github.com/go-kratos/kratos/v2/errors"
)

// ACLAPIUsecase contains the authorization rules for the small hand-written ACL HTTP API.
type ACLAPIUsecase struct {
	identities      IdentityRepo
	resources       ResourceRepo
	invites         InviteRepo
	access          *AccessChecker
	bootstrapUserID string
}

func NewACLAPIUsecase(identities IdentityRepo, resources ResourceRepo, invites InviteRepo, access *AccessChecker, bootstrapUserID string) *ACLAPIUsecase {
	if bootstrapUserID == "" {
		bootstrapUserID = "bootstrap"
	}
	return &ACLAPIUsecase{
		identities:      identities,
		resources:       resources,
		invites:         invites,
		access:          access,
		bootstrapUserID: bootstrapUserID,
	}
}

func ProvideACLAPIUsecase(identities IdentityRepo, resources ResourceRepo, invites InviteRepo, access *AccessChecker, auth *conf.Auth) *ACLAPIUsecase {
	bootstrapUserID := ""
	if auth != nil {
		bootstrapUserID = auth.GetBootstrapUserId()
	}
	return NewACLAPIUsecase(identities, resources, invites, access, bootstrapUserID)
}

func (uc *ACLAPIUsecase) AddOrgMember(ctx context.Context, orgID, userID, role string) error {
	caller, err := requireCaller(ctx)
	if err != nil {
		return err
	}
	if orgID == "" || userID == "" {
		return kratosErrors.BadRequest("INVALID_ARGUMENT", "org_id and user_id are required")
	}
	memberRole, err := uc.identities.MemberRole(ctx, orgID, caller)
	if err != nil {
		return err
	}
	if memberRole != "owner" {
		orgIDs, err := uc.identities.UserOrgIDs(ctx, caller)
		if err != nil {
			return err
		}
		if caller != uc.bootstrapUserID || !orgContains(orgIDs, orgID) {
			return ErrForbiddenPerm
		}
	}
	if role == "" {
		role = "member"
	}
	if role != "owner" && role != "member" {
		return kratosErrors.BadRequest("INVALID_ARGUMENT", "role must be owner or member")
	}
	return uc.identities.AddMember(ctx, orgID, userID, role)
}

// ListOrgMembers returns all members of an org. Caller must be an org member.
func (uc *ACLAPIUsecase) ListOrgMembers(ctx context.Context, orgID string) ([]OrgMemberInfo, error) {
	caller, err := requireCaller(ctx)
	if err != nil {
		return nil, err
	}
	orgID = strings.TrimSpace(orgID)
	if orgID == "" {
		return nil, kratosErrors.BadRequest("INVALID_ARGUMENT", "org_id is required")
	}
	memberRole, err := uc.identities.MemberRole(ctx, orgID, caller)
	if err != nil {
		return nil, err
	}
	if memberRole == "" {
		return nil, ErrForbiddenPerm
	}
	return uc.identities.ListOrgMembers(ctx, orgID)
}

// RemoveOrgMember removes a member from an org. Caller must be org owner.
func (uc *ACLAPIUsecase) RemoveOrgMember(ctx context.Context, orgID, userID string) error {
	if _, err := uc.requireOrgOwner(ctx, orgID); err != nil {
		return err
	}
	userID = strings.TrimSpace(userID)
	if userID == "" {
		return kratosErrors.BadRequest("INVALID_ARGUMENT", "user_id is required")
	}
	targetRole, err := uc.identities.MemberRole(ctx, orgID, userID)
	if err != nil {
		return err
	}
	if targetRole == "" {
		return kratosErrors.NotFound("NOT_FOUND", "member not found")
	}
	if targetRole == "owner" {
		members, err := uc.identities.ListOrgMembers(ctx, orgID)
		if err != nil {
			return err
		}
		ownerCount := 0
		for _, m := range members {
			if m.Role == "owner" {
				ownerCount++
			}
		}
		if ownerCount <= 1 {
			return kratosErrors.BadRequest("INVALID_ARGUMENT", "cannot remove the last owner")
		}
	}
	return uc.identities.RemoveMember(ctx, orgID, userID)
}

// UpdateMemberRole changes a member's role. Caller must be org owner.
func (uc *ACLAPIUsecase) UpdateMemberRole(ctx context.Context, orgID, userID, role string) error {
	if _, err := uc.requireOrgOwner(ctx, orgID); err != nil {
		return err
	}
	userID = strings.TrimSpace(userID)
	role = strings.TrimSpace(role)
	if userID == "" {
		return kratosErrors.BadRequest("INVALID_ARGUMENT", "user_id is required")
	}
	if role != "owner" && role != "member" {
		return kratosErrors.BadRequest("INVALID_ARGUMENT", "role must be owner or member")
	}
	targetRole, err := uc.identities.MemberRole(ctx, orgID, userID)
	if err != nil {
		return err
	}
	if targetRole == "" {
		return kratosErrors.NotFound("NOT_FOUND", "member not found")
	}
	if targetRole == "owner" && role != "owner" {
		members, err := uc.identities.ListOrgMembers(ctx, orgID)
		if err != nil {
			return err
		}
		ownerCount := 0
		for _, m := range members {
			if m.Role == "owner" {
				ownerCount++
			}
		}
		if ownerCount <= 1 {
			return kratosErrors.BadRequest("INVALID_ARGUMENT", "cannot demote the last owner")
		}
	}
	return uc.identities.UpdateMemberRole(ctx, orgID, userID, role)
}

func (uc *ACLAPIUsecase) CreateGrant(ctx context.Context, resourceID, granteeType, granteeID string, perm Perm) error {
	caller, err := requireCaller(ctx)
	if err != nil {
		return err
	}
	if resourceID == "" || granteeID == "" || (granteeType != "user" && granteeType != "org") || !validPerm(perm) {
		return kratosErrors.BadRequest("INVALID_ARGUMENT", "resource_id, valid grantee_type, grantee_id, and perm are required")
	}
	canAdmin, err := uc.access.Can(ctx, caller, resourceID, PermAdmin, "")
	if err != nil {
		return err
	}
	if !canAdmin {
		return ErrForbiddenPerm
	}
	return uc.resources.CreateGrant(ctx, ResourceGrant{
		ResourceID: resourceID, GranteeType: granteeType, GranteeID: granteeID, Perm: perm,
	})
}

func (uc *ACLAPIUsecase) ListGrants(ctx context.Context, resourceID string) ([]ResourceGrant, error) {
	caller, err := requireCaller(ctx)
	if err != nil {
		return nil, err
	}
	if resourceID == "" {
		return nil, kratosErrors.BadRequest("INVALID_ARGUMENT", "resource_id is required")
	}
	canAdmin, err := uc.access.Can(ctx, caller, resourceID, PermAdmin, "")
	if err != nil {
		return nil, err
	}
	if !canAdmin {
		return nil, ErrForbiddenPerm
	}
	return uc.resources.ListGrants(ctx, resourceID)
}

func (uc *ACLAPIUsecase) DeleteGrant(ctx context.Context, resourceID, granteeType, granteeID string) error {
	caller, err := requireCaller(ctx)
	if err != nil {
		return err
	}
	if resourceID == "" || granteeID == "" || (granteeType != "user" && granteeType != "org") {
		return kratosErrors.BadRequest("INVALID_ARGUMENT", "resource_id, valid grantee_type, and grantee_id are required")
	}
	canAdmin, err := uc.access.Can(ctx, caller, resourceID, PermAdmin, "")
	if err != nil {
		return err
	}
	if !canAdmin {
		return ErrForbiddenPerm
	}
	return uc.resources.DeleteGrant(ctx, resourceID, granteeType, granteeID)
}

func (uc *ACLAPIUsecase) GetResourceByPayload(ctx context.Context, resourceType ResourceType, payloadRef string) (*Resource, error) {
	caller, err := requireCaller(ctx)
	if err != nil {
		return nil, err
	}
	if payloadRef == "" {
		return nil, kratosErrors.BadRequest("INVALID_ARGUMENT", "payload_ref is required")
	}
	resource, err := uc.resources.GetByPayload(ctx, resourceType, payloadRef)
	if err != nil {
		return nil, ErrGrantNotFound
	}
	canView, err := uc.access.Can(ctx, caller, resource.ID, PermView, "")
	if err != nil || !canView {
		return nil, ErrGrantNotFound
	}
	return resource, nil
}

func (uc *ACLAPIUsecase) IssueUserToken(ctx context.Context, userID string) (string, error) {
	caller, err := requireCaller(ctx)
	if err != nil {
		return "", err
	}
	if userID == "" {
		return "", kratosErrors.BadRequest("INVALID_ARGUMENT", "user_id is required")
	}
	if caller != userID && caller != uc.bootstrapUserID {
		return "", ErrForbiddenPerm
	}
	if _, err := uc.identities.GetUser(ctx, userID); err != nil {
		return "", err
	}
	token, err := newBearerToken()
	if err != nil {
		return "", err
	}
	if err := IssueToken(ctx, uc.identities, userID, token); err != nil {
		return "", err
	}
	return token, nil
}

func validPerm(perm Perm) bool {
	switch perm {
	case PermView, PermUse, PermEdit, PermAdmin:
		return true
	default:
		return false
	}
}

func newBearerToken() (string, error) {
	raw := make([]byte, 32)
	if _, err := rand.Read(raw); err != nil {
		return "", err
	}
	return strings.TrimRight(base64.RawURLEncoding.EncodeToString(raw), "="), nil
}

func (uc *ACLAPIUsecase) CreateOrg(ctx context.Context, name string) (*Org, error) {
	caller, err := requireCaller(ctx)
	if err != nil {
		return nil, err
	}
	name = strings.TrimSpace(name)
	if name == "" {
		return nil, kratosErrors.BadRequest("INVALID_ARGUMENT", "name is required")
	}
	org, err := uc.identities.CreateOrg(ctx, name)
	if err != nil {
		return nil, err
	}
	if err := uc.identities.AddMember(ctx, org.ID, caller, "owner"); err != nil {
		return nil, err
	}
	return org, nil
}

func (uc *ACLAPIUsecase) ListMyOrgs(ctx context.Context) ([]OrgMembership, error) {
	caller, err := requireCaller(ctx)
	if err != nil {
		return nil, err
	}
	return uc.identities.ListUserOrgs(ctx, caller)
}

// ListUsers returns portal users for grant/member pickers. Any authenticated caller may list.
func (uc *ACLAPIUsecase) ListUsers(ctx context.Context, q string, limit int) ([]UserSummary, error) {
	if _, err := requireCaller(ctx); err != nil {
		return nil, err
	}
	return uc.identities.ListUsers(ctx, q, limit)
}

func (uc *ACLAPIUsecase) CreateInvite(ctx context.Context, orgID string, maxUses, expiresInHours int) (string, *OrgInvite, error) {
	caller, err := uc.requireOrgOwner(ctx, orgID)
	if err != nil {
		return "", nil, err
	}
	var expiresAt *time.Time
	if expiresInHours > 0 {
		t := time.Now().Add(time.Duration(expiresInHours) * time.Hour)
		expiresAt = &t
	}
	invite, plain, err := uc.invites.CreateInvite(ctx, orgID, caller, maxUses, expiresAt)
	if err != nil {
		return "", nil, err
	}
	return plain, invite, nil
}

func (uc *ACLAPIUsecase) ListInvites(ctx context.Context, orgID string) ([]*OrgInvite, error) {
	if _, err := uc.requireOrgOwner(ctx, orgID); err != nil {
		return nil, err
	}
	return uc.invites.ListInvitesByOrg(ctx, orgID)
}

func (uc *ACLAPIUsecase) RevokeInvite(ctx context.Context, orgID, inviteID string) error {
	if _, err := uc.requireOrgOwner(ctx, orgID); err != nil {
		return err
	}
	inviteID = strings.TrimSpace(inviteID)
	if inviteID == "" {
		return kratosErrors.BadRequest("INVALID_ARGUMENT", "invite_id is required")
	}
	invites, err := uc.invites.ListInvitesByOrg(ctx, orgID)
	if err != nil {
		return err
	}
	found := false
	for _, invite := range invites {
		if invite.ID == inviteID {
			found = true
			break
		}
	}
	if !found {
		return kratosErrors.NotFound("NOT_FOUND", "invite not found")
	}
	if err := uc.invites.RevokeInvite(ctx, inviteID); err != nil {
		if errors.Is(err, pkgErrors.ErrNotFound) {
			return kratosErrors.NotFound("NOT_FOUND", "invite not found")
		}
		return err
	}
	return nil
}

func (uc *ACLAPIUsecase) requireOrgOwner(ctx context.Context, orgID string) (string, error) {
	caller, err := requireCaller(ctx)
	if err != nil {
		return "", err
	}
	orgID = strings.TrimSpace(orgID)
	if orgID == "" {
		return "", kratosErrors.BadRequest("INVALID_ARGUMENT", "org_id is required")
	}
	memberRole, err := uc.identities.MemberRole(ctx, orgID, caller)
	if err != nil {
		return "", err
	}
	if memberRole != "owner" {
		orgIDs, err := uc.identities.UserOrgIDs(ctx, caller)
		if err != nil {
			return "", err
		}
		if caller != uc.bootstrapUserID || !orgContains(orgIDs, orgID) {
			return "", ErrForbiddenPerm
		}
	}
	return caller, nil
}

// ── Resource visibility listing ──

// VisiblePayloadRefs is declared in resource_list_acl.go
