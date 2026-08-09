package biz

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

func TestRegisterOrgSkillCreatesOrgResourceAndSkillFileForMember(t *testing.T) {
	root := t.TempDir()
	identity := &fakeIdentityRepo{
		orgIDs:   map[string][]string{"user-1": {"org-1"}},
		upserted: map[string]string{},
	}
	_, _, resources := newAgentACLUsecase()
	uc := NewSkillResourceUsecase(identity, resources, root)
	ctx := WithCallerUserID(context.Background(), "user-1")
	content := "---\nname: shared-skill\ndescription: shared\n---\n# Shared\n"

	resource, err := uc.RegisterOrgSkill(ctx, "org-1", "shared-skill", content)
	if err != nil {
		t.Fatalf("RegisterOrgSkill: %v", err)
	}
	if resource.Type != ResourceTypeSkill || resource.Visibility != VisibilityOrg || resource.HomeOrgID != "org-1" || resource.OwnerUserID != "user-1" {
		t.Fatalf("resource = %#v, want org skill owned by user-1", resource)
	}
	skillFile := filepath.Join(root, "skills", resource.ID, "SKILL.md")
	got, err := os.ReadFile(skillFile)
	if err != nil {
		t.Fatalf("read skill file: %v", err)
	}
	if string(got) != content {
		t.Fatalf("skill file = %q, want %q", got, content)
	}
}

func TestRegisterOrgSkillRejectsNonMember(t *testing.T) {
	identity := &fakeIdentityRepo{
		orgIDs:   map[string][]string{"user-1": {}},
		upserted: map[string]string{},
	}
	_, _, resources := newAgentACLUsecase()
	uc := NewSkillResourceUsecase(identity, resources, t.TempDir())

	_, err := uc.RegisterOrgSkill(WithCallerUserID(context.Background(), "user-1"), "org-1", "shared-skill", "content")
	if !isReason(err, "INVALID_HOME_ORG") {
		t.Fatalf("RegisterOrgSkill error = %v, want INVALID_HOME_ORG", err)
	}
	if len(resources.created) != 0 {
		t.Fatalf("created resources = %d, want 0", len(resources.created))
	}
}
