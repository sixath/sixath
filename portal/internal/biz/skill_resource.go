package biz

import (
	"context"
	"os"
	"path/filepath"

	"github.com/google/uuid"
)

// SkillResourceUsecase manages organization-shared Skills stored outside agent workspaces.
type SkillResourceUsecase struct {
	identities IdentityRepo
	resources  ResourceRepo
	access     *AccessChecker
	dataRoot   string
}

// NewSkillResourceUsecase creates a SkillResourceUsecase.
func NewSkillResourceUsecase(identities IdentityRepo, resources ResourceRepo, dataRoot string) *SkillResourceUsecase {
	return &SkillResourceUsecase{
		identities: identities,
		resources:  resources,
		access:     NewAccessChecker(resources),
		dataRoot:   dataRoot,
	}
}

// RegisterOrgSkill writes SKILL.md under {data_root}/skills/{id}/ and creates
// a Skill Resource visible to members of homeOrgID.
func (uc *SkillResourceUsecase) RegisterOrgSkill(ctx context.Context, homeOrgID, name, content string) (*Resource, error) {
	caller, err := requireCaller(ctx)
	if err != nil {
		return nil, err
	}
	orgIDs, err := uc.identities.UserOrgIDs(ctx, caller)
	if err != nil {
		return nil, err
	}
	if !orgContains(orgIDs, homeOrgID) {
		return nil, ErrInvalidHomeOrg
	}

	resource := &Resource{
		ID:          uuid.NewString(),
		Type:        ResourceTypeSkill,
		Name:        name,
		OwnerUserID: caller,
		Visibility:  VisibilityOrg,
		HomeOrgID:   homeOrgID,
		PayloadRef:  "",
	}
	resource.PayloadRef = resource.ID
	skillDir := filepath.Join(uc.dataRoot, "skills", resource.ID)
	if err := os.MkdirAll(skillDir, 0o755); err != nil {
		return nil, err
	}
	if err := os.WriteFile(filepath.Join(skillDir, "SKILL.md"), []byte(content), 0o644); err != nil {
		_ = os.RemoveAll(skillDir)
		return nil, err
	}
	created, err := uc.resources.CreateResource(ctx, resource)
	if err != nil {
		_ = os.RemoveAll(skillDir)
		return nil, err
	}
	return created, nil
}

// SharedSkillDirs returns shared Skill directories that the caller may use with agentID.
func (uc *SkillResourceUsecase) SharedSkillDirs(ctx context.Context, agentID string) ([]string, error) {
	caller, err := requireCaller(ctx)
	if err != nil {
		return nil, err
	}
	resources, err := uc.resources.ListAllByType(ctx, ResourceTypeSkill)
	if err != nil {
		return nil, err
	}
	dirs := make([]string, 0, len(resources))
	for _, resource := range resources {
		canUse, err := uc.access.Can(ctx, caller, resource.ID, PermUse, agentID)
		if err != nil {
			return nil, err
		}
		if canUse {
			dirs = append(dirs, filepath.Join(uc.dataRoot, "skills", resource.ID))
		}
	}
	return dirs, nil
}
